package provider

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

const sensitiveMarker = "synthetic-sensitive-value"

func requireNoDiagnostics(t *testing.T, diagnostics diag.Diagnostics) {
	t.Helper()
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
}

func requireSafeError(t *testing.T, diagnostics diag.Diagnostics, summary string) {
	t.Helper()
	if !diagnostics.HasError() {
		t.Fatal("expected an error diagnostic")
	}
	found := false
	for _, diagnostic := range diagnostics {
		found = found || diagnostic.Summary() == summary
		if strings.Contains(diagnostic.Summary()+diagnostic.Detail(), sensitiveMarker) {
			t.Error("diagnostic exposed the synthetic sensitive value")
		}
	}
	if !found {
		t.Fatalf("missing %q diagnostic: %v", summary, diagnostics)
	}
}

func configureProvider(t *testing.T, model providerModel) fwprovider.ConfigureResponse {
	t.Helper()
	p := New("test")()
	var schema fwprovider.SchemaResponse
	p.Schema(t.Context(), fwprovider.SchemaRequest{}, &schema)
	state := tfsdk.State{Schema: schema.Schema}
	requireNoDiagnostics(t, state.Set(t.Context(), &model))
	var response fwprovider.ConfigureResponse
	p.Configure(t.Context(), fwprovider.ConfigureRequest{
		Config: tfsdk.Config{Schema: schema.Schema, Raw: state.Raw},
	}, &response)
	if service, ok := response.ResourceData.(*client.Client); ok {
		t.Cleanup(service.Close)
	}
	return response
}

func TestProviderProtocolSchema(t *testing.T) {
	p := New("test-version")()
	var metadata fwprovider.MetadataResponse
	p.Metadata(t.Context(), fwprovider.MetadataRequest{}, &metadata)
	if metadata.TypeName != "flagctl" || metadata.Version != "test-version" {
		t.Fatalf("unexpected provider metadata: %+v", metadata)
	}
	server := providerserver.NewProtocol6(p)()
	response, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Diagnostics) != 0 || response.Provider == nil || response.ResourceSchemas["flagctl_flag"] == nil {
		t.Fatalf("provider schema failed protocol validation: %+v", response)
	}
}

func TestProviderConfiguration(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"flags":[]}`)
	}))
	defer server.Close()
	for _, tc := range []struct {
		name        string
		environment string
		server      types.String
	}{
		{"environment fallback", server.URL, types.StringNull()},
		{"explicit configuration wins", "http://user:" + sensitiveMarker + "@localhost", types.StringValue(server.URL)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FLAGCTL_SERVER", tc.environment)
			response := configureProvider(t, providerModel{Server: tc.server, Timeout: types.StringNull()})
			requireNoDiagnostics(t, response.Diagnostics)
			service, ok := response.ResourceData.(*client.Client)
			if !ok {
				t.Fatal("provider did not configure the shared client")
			}
			if _, err := service.List(t.Context(), "dev"); err != nil {
				t.Fatal(err)
			}
		})
	}
	if hits.Load() != 2 {
		t.Fatalf("configured requests = %d, want 2", hits.Load())
	}
	t.Run("defaults configure without contacting a service", func(t *testing.T) {
		t.Setenv("FLAGCTL_SERVER", "")
		response := configureProvider(t, providerModel{Server: types.StringNull(), Timeout: types.StringNull()})
		requireNoDiagnostics(t, response.Diagnostics)
		if response.ResourceData == nil {
			t.Fatal("default configuration did not produce a client")
		}
	})
}

func TestProviderRejectsInvalidAndUnknownConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, environment, attribute, summary string
		server, timeout                       types.String
	}{
		{"unknown server", "", "server", "Unknown service address", types.StringUnknown(), types.StringNull()},
		{"unknown timeout", "", "timeout", "Unknown request timeout", types.StringNull(), types.StringUnknown()},
		{"invalid environment", "http://user:" + sensitiveMarker + "@localhost", "server", "Invalid service address", types.StringNull(), types.StringNull()},
		{"explicit empty server", "http://localhost", "server", "Invalid service address", types.StringValue(""), types.StringNull()},
		{"credential-bearing server", "", "server", "Invalid service address", types.StringValue("http://user:" + sensitiveMarker + "@localhost"), types.StringNull()},
		{"non-loopback server", "", "server", "Invalid service address", types.StringValue("http://example.com"), types.StringNull()},
		{"invalid duration", "", "timeout", "Invalid request timeout", types.StringNull(), types.StringValue(sensitiveMarker)},
		{"zero duration", "", "timeout", "Invalid request timeout", types.StringNull(), types.StringValue("0s")},
		{"negative duration", "", "timeout", "Invalid request timeout", types.StringNull(), types.StringValue("-1s")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FLAGCTL_SERVER", tc.environment)
			response := configureProvider(t, providerModel{Server: tc.server, Timeout: tc.timeout})
			requireSafeError(t, response.Diagnostics, tc.summary)
			if response.ResourceData != nil {
				t.Error("invalid configuration produced a client")
			}
			for _, diagnostic := range response.Diagnostics {
				withPath, ok := diagnostic.(diag.DiagnosticWithPath)
				if !ok || !withPath.Path().Equal(path.Root(tc.attribute)) {
					t.Errorf("diagnostic lacks %s path", tc.attribute)
				}
			}
		})
	}
}

func TestProviderRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	response := configureProvider(t, providerModel{Server: types.StringValue(server.URL), Timeout: types.StringValue("50ms")})
	requireNoDiagnostics(t, response.Diagnostics)
	_, err := response.ResourceData.(*client.Client).List(t.Context(), "dev")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("configured timeout was not applied: %v", err)
	}
}

func TestSchemaValidatorsDeferUnknownValues(t *testing.T) {
	var schema fwprovider.SchemaResponse
	New("test")().Schema(t.Context(), fwprovider.SchemaRequest{}, &schema)
	for _, name := range []string{"server", "timeout"} {
		attribute := schema.Schema.Attributes[name].(interface{ StringValidators() []validator.String })
		for _, value := range []types.String{types.StringNull(), types.StringUnknown(), types.StringValue(sensitiveMarker)} {
			for _, check := range attribute.StringValidators() {
				var response validator.StringResponse
				check.ValidateString(t.Context(), validator.StringRequest{Path: path.Root(name), ConfigValue: value}, &response)
				if value.IsNull() || value.IsUnknown() {
					requireNoDiagnostics(t, response.Diagnostics)
				} else {
					requireSafeError(t, response.Diagnostics, "Invalid flagctl configuration")
				}
			}
		}
	}
}
