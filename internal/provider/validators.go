package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// stringCheck reuses the service's validation rules without reflecting values
// in diagnostics. Unknown values are checked after Terraform resolves them.
type stringCheck struct {
	description string
	validate    func(string) error
}

var _ validator.String = stringCheck{}

func (v stringCheck) Description(_ context.Context) string { return v.description }

func (v stringCheck) MarkdownDescription(_ context.Context) string { return v.description }

func (v stringCheck) ValidateString(_ context.Context, request validator.StringRequest, response *validator.StringResponse) {
	if request.ConfigValue.IsNull() || request.ConfigValue.IsUnknown() {
		return
	}
	if err := v.validate(request.ConfigValue.ValueString()); err != nil {
		response.Diagnostics.AddAttributeError(request.Path, "Invalid flagctl configuration", err.Error())
	}
}
