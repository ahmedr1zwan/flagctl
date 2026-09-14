package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/api"
	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/ahmedr1zwan/flagctl/internal/store"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

type acceptanceFixture struct {
	client     *client.Client
	endpoint   string
	workingDir string
}

func newAcceptanceFixture(t *testing.T) *acceptanceFixture {
	t.Helper()
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 to run Terraform acceptance tests")
	}
	// Require an installed binary rather than allowing an implicit CLI download.
	binary := os.Getenv("TF_ACC_TERRAFORM_PATH")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("terraform")
		if err != nil {
			t.Fatal("install Terraform or set TF_ACC_TERRAFORM_PATH to its executable")
		}
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() {
		t.Fatal("TF_ACC_TERRAFORM_PATH must identify an installed Terraform executable")
	}

	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	configuration := filepath.Join(root, "terraform.tfrc")
	if err := os.WriteFile(configuration, []byte("disable_checkpoint = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// These tests run sequentially: Terraform's test harness uses process-wide
	// environment variables. Ignore developer CLI flags, tokens, debug logs,
	// caches, workspaces, and persistence settings; restore them through t.Cleanup.
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "TF_") {
			t.Setenv(name, "")
		}
	}
	t.Setenv("TF_ACC", "1")
	t.Setenv("TF_ACC_TERRAFORM_PATH", binary)
	t.Setenv("TF_CLI_CONFIG_FILE", configuration)
	t.Setenv("TF_IN_AUTOMATION", "1")
	t.Setenv("CHECKPOINT_DISABLE", "1")
	t.Setenv("FLAGCTL_SERVER", "")

	backend, err := store.Open(t.Context(), filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewUnstartedServer(nil)
	server.Config.Handler = api.NewHandler(backend, server.Listener.Addr().String())
	server.Start()
	t.Cleanup(server.Close)
	service, err := client.New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	workingDir := filepath.Join(root, "terraform")
	if err := os.Mkdir(workingDir, 0700); err != nil {
		t.Fatal(err)
	}
	return &acceptanceFixture{client: service, endpoint: server.URL, workingDir: workingDir}
}

func (f *acceptanceFixture) testCase(t *testing.T, steps ...resource.TestStep) resource.TestCase {
	return resource.TestCase{
		WorkingDir: f.workingDir,
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"flagctl": func() (tfprotov6.ProviderServer, error) { return providerserver.NewProtocol6WithError(New("test")())() },
		},
		CheckDestroy: func(_ *terraform.State) error {
			// Inspect the API after Terraform destroys its resources. Removing the
			// temporary database alone must never count as successful cleanup.
			for _, environment := range []string{"dev", "prod", "staging"} {
				flags, err := f.client.List(t.Context(), environment)
				if err != nil {
					return err
				}
				if len(flags) != 0 {
					return fmt.Errorf("destroy left %d flags in %s", len(flags), environment)
				}
			}
			return nil
		},
		Steps: steps,
	}
}

func (f *acceptanceFixture) config(resources string) string {
	return fmt.Sprintf("provider \"flagctl\" {\n  server = %q\n}\n%s", f.endpoint, resources)
}

func requireFlagAbsent(t *testing.T, service *client.Client, environment, key string) {
	t.Helper()
	_, err := service.Get(t.Context(), environment, key)
	var apiError *client.APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != 404 || apiError.Code != "not_found" {
		t.Fatalf("expected %s/%s to be absent: %v", environment, key, err)
	}
}

// Compare every managed flag's state to a fresh API read, including computed
// fields. This catches providers that report success without persisting changes.
type stateMatchesService struct{ client *client.Client }

func (check stateMatchesService) CheckState(ctx context.Context, request statecheck.CheckStateRequest, response *statecheck.CheckStateResponse) {
	if request.State == nil || request.State.Values == nil || request.State.Values.RootModule == nil {
		response.Error = fmt.Errorf("Terraform returned no resource state")
		return
	}
	count := 0
	for _, resource := range request.State.Values.RootModule.Resources {
		if resource.Type != "flagctl_flag" {
			continue
		}
		count++
		attributes := resource.AttributeValues
		environment, _ := attributes["environment"].(string)
		key, _ := attributes["key"].(string)
		flag, err := check.client.Get(ctx, environment, key)
		if err != nil {
			response.Error = err
			return
		}
		want := map[string]any{
			"id": environment + "/" + key, "description": flag.Description, "enabled": flag.Enabled,
			"created_at": flag.CreatedAt.UTC().Format(time.RFC3339Nano), "updated_at": flag.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}
		for name, value := range want {
			if attributes[name] != value {
				response.Error = fmt.Errorf("%s.%s differs between state and API", resource.Address, name)
				return
			}
		}
	}
	if count == 0 {
		response.Error = fmt.Errorf("Terraform state contains no managed flags")
	}
}
