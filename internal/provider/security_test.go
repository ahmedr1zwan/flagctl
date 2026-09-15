package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/ahmedr1zwan/flagctl/internal/testutil"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
)

func TestMain(m *testing.M) {
	for _, name := range []string{"FLAGCTL_SERVER", "FLAGCTL_TOKEN_FILE", "FLAGCTL_CA_FILE"} {
		_ = os.Unsetenv(name)
	}
	os.Exit(m.Run())
}

func TestProviderSecureConfiguration(t *testing.T) {
	server, files := testutil.NewSecureAPI(t)
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit=%v", explicit), func(t *testing.T) {
			model := providerModel{Server: types.StringValue(server.URL)}
			t.Setenv("FLAGCTL_TOKEN_FILE", files.TokenFile)
			t.Setenv("FLAGCTL_CA_FILE", files.CAFile)
			if explicit {
				t.Setenv("FLAGCTL_TOKEN_FILE", sensitiveMarker)
				t.Setenv("FLAGCTL_CA_FILE", sensitiveMarker)
				model.TokenFile = types.StringValue(files.TokenFile)
				model.CAFile = types.StringValue(files.CAFile)
			}
			response := configureProvider(t, model)
			requireNoDiagnostics(t, response.Diagnostics)
			if _, err := response.ResourceData.(*client.Client).List(t.Context(), "dev"); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, tc := range []struct {
		name, summary string
		token, ca     types.String
	}{
		{"unknown token", "Unknown token file", types.StringUnknown(), types.StringNull()},
		{"unknown CA", "Unknown CA file", types.StringNull(), types.StringUnknown()},
		{"bad token path", "Invalid service configuration", types.StringValue(sensitiveMarker), types.StringValue(files.CAFile)},
		{"bad CA path", "Invalid service configuration", types.StringValue(files.TokenFile), types.StringValue(sensitiveMarker)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := configureProvider(t, providerModel{Server: types.StringValue(server.URL), TokenFile: tc.token, CAFile: tc.ca})
			requireSafeError(t, response.Diagnostics, tc.summary)
			if response.ResourceData != nil {
				t.Fatal("invalid security configuration produced client")
			}
		})
	}
	t.Setenv("FLAGCTL_TOKEN_FILE", files.TokenFile)
	t.Setenv("FLAGCTL_CA_FILE", files.CAFile)
	response := configureProvider(t, providerModel{Server: types.StringValue(server.URL), TokenFile: types.StringValue("")})
	requireNoDiagnostics(t, response.Diagnostics)
	if _, err := response.ResourceData.(*client.Client).List(t.Context(), "dev"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatal("empty token_file failed to override environment")
	}
	response = configureProvider(t, providerModel{Server: types.StringValue(server.URL), CAFile: types.StringValue("")})
	requireNoDiagnostics(t, response.Diagnostics)
	if _, err := response.ResourceData.(*client.Client).List(t.Context(), "dev"); err == nil {
		t.Fatal("empty ca_file failed to restore system trust")
	}
}

type noCredentialState struct{ token string }

func (check noCredentialState) CheckState(_ context.Context, request statecheck.CheckStateRequest, response *statecheck.CheckStateResponse) {
	data, err := json.Marshal(request.State)
	if err != nil {
		response.Error = err
		return
	}
	if strings.Contains(string(data), check.token) {
		response.Error = fmt.Errorf("Terraform state exposed the API token")
	}
}

func TestAccAuthenticatedFlagLifecycle(t *testing.T) {
	f := newAcceptanceFixtureWithTLS(t, true)
	config := f.config(flagConfig("dev", "secure", ""))
	enabled := f.config(flagConfig("dev", "secure", "enabled = true"))
	if strings.Contains(config, f.credentials.Token) {
		t.Fatal("configuration contains raw token")
	}
	checks := func(enabled bool) []statecheck.StateCheck {
		return append(flagStateChecks(f, "dev/secure", "", enabled), noCredentialState{token: f.credentials.Token})
	}
	resource.Test(t, f.testCase(t,
		resource.TestStep{Config: config, ConfigPlanChecks: plannedAction(plancheck.ResourceActionCreate), ConfigStateChecks: checks(false)},
		resource.TestStep{Config: config, ConfigPlanChecks: plannedAction(plancheck.ResourceActionNoop), ConfigStateChecks: checks(false)},
		resource.TestStep{Config: enabled, ConfigPlanChecks: plannedAction(plancheck.ResourceActionUpdate), ConfigStateChecks: checks(true)},
		resource.TestStep{ResourceName: flagAddress, ImportState: true, ImportStateVerify: true},
	))
}
