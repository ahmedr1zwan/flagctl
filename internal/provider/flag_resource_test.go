package provider

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func emptyFlagState(t *testing.T) tfsdk.State {
	t.Helper()
	var response resource.SchemaResponse
	newFlagResource().Schema(t.Context(), resource.SchemaRequest{}, &response)
	return tfsdk.State{Schema: response.Schema, Raw: tftypes.NewValue(response.Schema.Type().TerraformType(t.Context()), nil)}
}

func savedFlagState(t *testing.T) tfsdk.State {
	t.Helper()
	state := emptyFlagState(t)
	requireNoDiagnostics(t, state.Set(t.Context(), &flagModel{
		ID: types.StringValue("dev/checkout"), Environment: types.StringValue("dev"), Key: types.StringValue("checkout"),
		Enabled: types.BoolValue(false), Description: types.StringValue("saved description"),
		CreatedAt: types.StringValue("2026-09-14T00:00:00Z"), UpdatedAt: types.StringValue("2026-09-14T00:00:00Z"),
	}))
	return state
}

// Framework responses start with prior state, except create, which starts null.
// This lets failure tests detect accidental state writes during an API error.
func invokeResource(t *testing.T, r *flagResource, operation string, prior tfsdk.State) (tfsdk.State, diag.Diagnostics) {
	t.Helper()
	plan := tfsdk.Plan{Schema: prior.Schema, Raw: prior.Raw}
	switch operation {
	case "create":
		response := resource.CreateResponse{State: emptyFlagState(t)}
		r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &response)
		return response.State, response.Diagnostics
	case "read":
		response := resource.ReadResponse{State: prior}
		r.Read(t.Context(), resource.ReadRequest{State: prior}, &response)
		return response.State, response.Diagnostics
	case "update":
		response := resource.UpdateResponse{State: prior}
		r.Update(t.Context(), resource.UpdateRequest{Plan: plan, State: prior}, &response)
		return response.State, response.Diagnostics
	case "delete":
		response := resource.DeleteResponse{State: prior}
		r.Delete(t.Context(), resource.DeleteRequest{State: prior}, &response)
		return response.State, response.Diagnostics
	default:
		t.Fatalf("unknown test operation: %s", operation)
		return tfsdk.State{}, nil
	}
}

func TestResourceConfiguration(t *testing.T) {
	for _, data := range []any{nil, "wrong client type", (*client.Client)(nil)} {
		r := &flagResource{}
		var response resource.ConfigureResponse
		r.Configure(t.Context(), resource.ConfigureRequest{ProviderData: data}, &response)
		if data == nil {
			requireNoDiagnostics(t, response.Diagnostics)
		} else {
			requireSafeError(t, response.Diagnostics, "Unexpected provider client")
		}
		for _, operation := range []string{"create", "read", "update", "delete"} {
			_, diagnostics := invokeResource(t, r, operation, savedFlagState(t))
			requireSafeError(t, diagnostics, "Provider is not configured")
		}
	}
}

func TestResourceAPIFailuresPreserveState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"server failure", 500, `{"error":{"code":"internal_error","message":"` + sensitiveMarker + `"}}`},
		{"unknown not-found code", 404, `{"error":{"code":"wrong_route","message":"` + sensitiveMarker + `"}}`},
		{"invalid error body", 404, sensitiveMarker},
		{"conflict", 409, `{"error":{"code":"already_exists","message":"` + sensitiveMarker + `"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			service, err := client.New(server.URL, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close()
			r := &flagResource{}
			var configured resource.ConfigureResponse
			r.Configure(t.Context(), resource.ConfigureRequest{ProviderData: service}, &configured)
			requireNoDiagnostics(t, configured.Diagnostics)
			for _, operation := range []string{"create", "read", "update", "delete"} {
				t.Run(operation, func(t *testing.T) {
					prior := savedFlagState(t)
					state, diagnostics := invokeResource(t, r, operation, prior)
					requireSafeError(t, diagnostics, "Unable to "+operation+" flag")
					if operation == "create" {
						if !state.Raw.IsNull() {
							t.Error("failed create adopted a resource into state")
						}
					} else if !state.Raw.Equal(prior.Raw) {
						t.Error("API failure changed prior state")
					}
				})
			}
		})
	}
}

func TestResourceNotFoundHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		io.WriteString(w, `{"error":{"code":"not_found"}}`)
	}))
	defer server.Close()
	service, err := client.New(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	r := &flagResource{client: service}
	prior := savedFlagState(t)
	state, diagnostics := invokeResource(t, r, "read", prior)
	requireNoDiagnostics(t, diagnostics)
	if !state.Raw.IsNull() {
		t.Error("externally deleted resource was retained in state")
	}
	_, diagnostics = invokeResource(t, r, "delete", prior)
	requireNoDiagnostics(t, diagnostics)
	state, diagnostics = invokeResource(t, r, "update", prior)
	requireSafeError(t, diagnostics, "Unable to update flag")
	if !state.Raw.Equal(prior.Raw) {
		t.Error("missing update target changed state")
	}
}

func TestResourceImportIdentity(t *testing.T) {
	for _, id := range []string{"dev/checkout", "prod/checkout_v2", "stage-1/" + strings.Repeat("a", 63)} {
		response := resource.ImportStateResponse{State: emptyFlagState(t)}
		newFlagResource().(resource.ResourceWithImportState).ImportState(t.Context(), resource.ImportStateRequest{ID: id}, &response)
		requireNoDiagnostics(t, response.Diagnostics)
		var model flagModel
		requireNoDiagnostics(t, response.State.Get(t.Context(), &model))
		if model.ID.ValueString() != id || model.Environment.ValueString()+"/"+model.Key.ValueString() != id {
			t.Errorf("import identity mismatch for %q", id)
		}
		if !model.Enabled.IsNull() || !model.Description.IsNull() || !model.CreatedAt.IsNull() || !model.UpdatedAt.IsNull() {
			t.Error("import invented attributes before refreshing the API")
		}
	}
	for _, id := range []string{"", "checkout", "/checkout", "dev/", "Dev/checkout", "dev/checkout/extra", "../checkout", "dev/" + strings.Repeat("a", 64), "dev/" + sensitiveMarker + "/secret"} {
		response := resource.ImportStateResponse{State: emptyFlagState(t)}
		newFlagResource().(resource.ResourceWithImportState).ImportState(t.Context(), resource.ImportStateRequest{ID: id}, &response)
		requireSafeError(t, response.Diagnostics, "Invalid flag import ID")
		if !response.State.Raw.IsNull() {
			t.Error("invalid import wrote state")
		}
	}
}

func TestResourceSchemaValidation(t *testing.T) {
	var schema resource.SchemaResponse
	newFlagResource().Schema(t.Context(), resource.SchemaRequest{}, &schema)
	for _, tc := range []struct{ name, valid, invalid string }{
		{"environment", "prod", "../prod"},
		{"key", strings.Repeat("a", 63), strings.Repeat("a", 64)},
		{"description", strings.Repeat("🌍", 256), strings.Repeat("🌍", 257)},
	} {
		attribute := schema.Schema.Attributes[tc.name].(interface{ StringValidators() []validator.String })
		for _, value := range []types.String{types.StringNull(), types.StringUnknown(), types.StringValue(tc.valid), types.StringValue(tc.invalid)} {
			for _, check := range attribute.StringValidators() {
				var response validator.StringResponse
				check.ValidateString(t.Context(), validator.StringRequest{Path: path.Root(tc.name), ConfigValue: value}, &response)
				if value == types.StringValue(tc.invalid) {
					requireSafeError(t, response.Diagnostics, "Invalid flagctl configuration")
				} else {
					requireNoDiagnostics(t, response.Diagnostics)
				}
			}
		}
	}
}
