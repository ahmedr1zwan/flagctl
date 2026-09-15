// Package provider manages flags through Terraform and the shared HTTP client.
package provider

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = (*flagctlProvider)(nil)

type flagctlProvider struct {
	version string
}

type providerModel struct {
	TokenFile types.String `tfsdk:"token_file"`
	CAFile    types.String `tfsdk:"ca_file"`
	Server    types.String `tfsdk:"server"`
	Timeout   types.String `tfsdk:"timeout"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &flagctlProvider{version: version} }
}

func (p *flagctlProvider) Metadata(_ context.Context, _ provider.MetadataRequest, response *provider.MetadataResponse) {
	response.TypeName = "flagctl"
	response.Version = p.version
}

func (p *flagctlProvider) Schema(_ context.Context, _ provider.SchemaRequest, response *provider.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Manage environment-scoped feature flags through a flagd service.",
		Attributes: map[string]schema.Attribute{
			"server": schema.StringAttribute{
				Optional:    true,
				Description: "Service origin (HTTP for loopback, HTTPS for authenticated remote access). Overrides FLAGCTL_SERVER; defaults to http://127.0.0.1:8080. Credentials, non-root paths, queries, and fragments are forbidden.",
				Validators: []validator.String{stringCheck{
					description: "Must be an HTTP(S) origin; HTTP is restricted to loopback.",
					validate:    client.ValidateServer,
				}},
			},
			"token_file": schema.StringAttribute{
				Optional:    true,
				Description: "Path to a private bearer token file. Overrides FLAGCTL_TOKEN_FILE; requires HTTPS. The token contents are never stored in Terraform state.",
			},
			"ca_file": schema.StringAttribute{
				Optional:    true,
				Description: "Path to a PEM CA bundle. Overrides FLAGCTL_CA_FILE; empty uses system trust. Certificate and hostname verification cannot be disabled.",
			},
			"timeout": schema.StringAttribute{
				Optional:    true,
				Description: "Maximum duration of each HTTP request, such as 5s or 1m. Defaults to 10s; must be positive.",
				Validators: []validator.String{stringCheck{
					description: "Must be a positive Go duration, such as 5s or 1m.",
					validate:    func(value string) error { _, err := requestTimeout(value); return err },
				}},
			},
		},
	}
}

func (p *flagctlProvider) Configure(ctx context.Context, request provider.ConfigureRequest, response *provider.ConfigureResponse) {
	var config providerModel
	response.Diagnostics.Append(request.Config.Get(ctx, &config)...)
	if response.Diagnostics.HasError() {
		return
	}
	if config.TokenFile.IsUnknown() {
		response.Diagnostics.AddAttributeError(path.Root("token_file"), "Unknown token file", "token_file must be known before planning flag resources.")
	}
	if config.CAFile.IsUnknown() {
		response.Diagnostics.AddAttributeError(path.Root("ca_file"), "Unknown CA file", "ca_file must be known before planning flag resources.")
	}
	if config.Server.IsUnknown() {
		response.Diagnostics.AddAttributeError(path.Root("server"), "Unknown service address", "server must be known before planning flag resources.")
	}
	if config.Timeout.IsUnknown() {
		response.Diagnostics.AddAttributeError(path.Root("timeout"), "Unknown request timeout", "timeout must be known before planning flag resources.")
	}
	if response.Diagnostics.HasError() {
		return
	}
	server := client.DefaultServer
	if value := os.Getenv("FLAGCTL_SERVER"); value != "" {
		server = value
	}
	if !config.Server.IsNull() {
		server = config.Server.ValueString()
	}
	if err := client.ValidateServer(server); err != nil {
		response.Diagnostics.AddAttributeError(path.Root("server"), "Invalid service address", err.Error())
		return
	}
	timeout := 10 * time.Second
	if !config.Timeout.IsNull() {
		var err error
		timeout, err = requestTimeout(config.Timeout.ValueString())
		if err != nil {
			response.Diagnostics.AddAttributeError(path.Root("timeout"), "Invalid request timeout", err.Error())
			return
		}
	}
	tokenFile, caFile := os.Getenv("FLAGCTL_TOKEN_FILE"), os.Getenv("FLAGCTL_CA_FILE")
	if !config.TokenFile.IsNull() {
		tokenFile = config.TokenFile.ValueString()
	}
	if !config.CAFile.IsNull() {
		caFile = config.CAFile.ValueString()
	}
	service, err := client.NewWithConfig(client.Config{Server: server, Timeout: timeout, TokenFile: tokenFile, CAFile: caFile})
	if err != nil {
		response.Diagnostics.AddError("Invalid service configuration", err.Error())
		return
	}
	// Terraform owns the plugin process lifetime. Resources share this client;
	// individual operations must not close it while other resources are using it.
	response.ResourceData = service
}

func (p *flagctlProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{newFlagResource}
}

func (p *flagctlProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

func requestTimeout(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, errors.New("timeout must be a positive Go duration, such as 5s or 1m")
	}
	return duration, nil
}
