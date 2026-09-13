package provider

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*flagResource)(nil)
	_ resource.ResourceWithConfigure   = (*flagResource)(nil)
	_ resource.ResourceWithImportState = (*flagResource)(nil)
)

type flagResource struct {
	client *client.Client
}

type flagModel struct {
	ID          types.String `tfsdk:"id"`
	Environment types.String `tfsdk:"environment"`
	Key         types.String `tfsdk:"key"`
	Description types.String `tfsdk:"description"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func newFlagResource() resource.Resource { return &flagResource{} }

func (r *flagResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_flag"
}

func (r *flagResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "A boolean feature flag identified by environment/key. Import with that same identifier.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, Description: "Stable identifier in environment/key form.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"environment": schema.StringAttribute{
				Required: true, Description: "Environment name, such as dev or prod. Changing it replaces the flag.",
				Validators:    []validator.String{stringCheck{description: "Must be a valid 1-63 character environment name.", validate: flags.ValidateEnvironment}},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"key": schema.StringAttribute{
				Required: true, Description: "Flag key, unique within its environment. Changing it replaces the flag.",
				Validators:    []validator.String{stringCheck{description: "Must be a valid 1-63 character flag key.", validate: flags.ValidateKey}},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				Description: "Description, at most 1024 UTF-8 bytes. Defaults to empty; an empty string clears it.",
				Validators:  []validator.String{stringCheck{description: "Must be valid UTF-8 and at most 1024 bytes.", validate: flags.ValidateDescription}},
			},
			"enabled": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Desired enabled state. Defaults to false.",
			},
			"created_at": schema.StringAttribute{
				Computed: true, Description: "Service-generated UTC creation timestamp in RFC3339 format.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed: true, Description: "Service-generated UTC update timestamp in RFC3339 format; unchanged for no-op updates.",
			},
		},
	}
}

func (r *flagResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
	if request.ProviderData == nil {
		return // Offline schema/configuration validation does not configure clients.
	}
	service, ok := request.ProviderData.(*client.Client)
	if !ok || service == nil {
		response.Diagnostics.AddError("Unexpected provider client", "The provider supplied an invalid HTTP client. Report this provider implementation error.")
		return
	}
	r.client = service
}

func (r *flagResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var data flagModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &data)...)
	if response.Diagnostics.HasError() || !r.configured(&response.Diagnostics) {
		return
	}
	flag, err := r.client.Create(ctx, data.Environment.ValueString(), flags.CreateInput{
		Key: data.Key.ValueString(), Description: data.Description.ValueString(), Enabled: data.Enabled.ValueBool(),
	})
	if err != nil {
		response.Diagnostics.AddError("Unable to create flag", err.Error()+". If the flag already exists, import it instead. After an interrupted request, check the service before retrying.")
		return
	}
	data.setFlag(flag)
	response.Diagnostics.Append(response.State.Set(ctx, &data)...)
}

func (r *flagResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var data flagModel
	response.Diagnostics.Append(request.State.Get(ctx, &data)...)
	if response.Diagnostics.HasError() || !r.configured(&response.Diagnostics) {
		return
	}
	flag, err := r.client.Get(ctx, data.Environment.ValueString(), data.Key.ValueString())
	if isNotFound(err) {
		response.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		response.Diagnostics.AddError("Unable to read flag", err.Error())
		return
	}
	data.setFlag(flag)
	response.Diagnostics.Append(response.State.Set(ctx, &data)...)
}

func (r *flagResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var data flagModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &data)...)
	if response.Diagnostics.HasError() || !r.configured(&response.Diagnostics) {
		return
	}
	description, enabled := data.Description.ValueString(), data.Enabled.ValueBool()
	// Terraform manages both mutable fields, including false and empty values.
	flag, err := r.client.Update(ctx, data.Environment.ValueString(), data.Key.ValueString(), flags.UpdateInput{
		Description: &description, Enabled: &enabled,
	})
	if err != nil {
		response.Diagnostics.AddError("Unable to update flag", err.Error()+". Refresh with a new plan before retrying; this operation never recreates a missing flag.")
		return
	}
	data.setFlag(flag)
	response.Diagnostics.Append(response.State.Set(ctx, &data)...)
}

func (r *flagResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var data flagModel
	response.Diagnostics.Append(request.State.Get(ctx, &data)...)
	if response.Diagnostics.HasError() || !r.configured(&response.Diagnostics) {
		return
	}
	if err := r.client.Delete(ctx, data.Environment.ValueString(), data.Key.ValueString()); err != nil && !isNotFound(err) {
		response.Diagnostics.AddError("Unable to delete flag", err.Error()+". Check the service and run a new plan before retrying.")
	}
	// Successful deletion (including an already absent flag) automatically removes state.
}

func (r *flagResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	environment, key, ok := strings.Cut(request.ID, "/")
	if !ok || flags.ValidateIdentity(environment, key) != nil {
		response.Diagnostics.AddError("Invalid flag import ID", "Use environment/key with valid lowercase identifiers, for example dev/checkout_v2.")
		return
	}
	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("id"), request.ID)...)
	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("environment"), environment)...)
	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("key"), key)...)
}

func (r *flagResource) configured(diagnostics *diag.Diagnostics) bool {
	if r.client == nil {
		diagnostics.AddError("Provider is not configured", "The flagctl HTTP client is unavailable. Check provider configuration.")
		return false
	}
	return true
}

func isNotFound(err error) bool {
	var apiError *client.APIError
	return errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound && apiError.Code == "not_found"
}

func (data *flagModel) setFlag(flag flags.Flag) {
	data.ID = types.StringValue(flag.Environment + "/" + flag.Key)
	data.Environment = types.StringValue(flag.Environment)
	data.Key = types.StringValue(flag.Key)
	data.Description = types.StringValue(flag.Description)
	data.Enabled = types.BoolValue(flag.Enabled)
	data.CreatedAt = types.StringValue(flag.CreatedAt.UTC().Format(time.RFC3339Nano))
	data.UpdatedAt = types.StringValue(flag.UpdatedAt.UTC().Format(time.RFC3339Nano))
}
