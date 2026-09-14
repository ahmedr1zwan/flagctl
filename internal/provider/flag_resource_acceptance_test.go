package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/ahmedr1zwan/flagctl/internal/client"
	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const flagAddress = "flagctl_flag.test"

func flagConfig(environment, key, mutable string) string {
	return fmt.Sprintf(`resource "flagctl_flag" "test" {
  environment = %q
  key = %q
  %s
}
`, environment, key, mutable)
}

func plannedAction(action plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{
		PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(flagAddress, action)},
		PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
	}
}

func flagStateChecks(f *acceptanceFixture, id, description string, enabled bool) []statecheck.StateCheck {
	return []statecheck.StateCheck{
		stateMatchesService{client: f.client},
		statecheck.ExpectKnownValue(flagAddress, tfjsonpath.New("id"), knownvalue.StringExact(id)),
		statecheck.ExpectKnownValue(flagAddress, tfjsonpath.New("description"), knownvalue.StringExact(description)),
		statecheck.ExpectKnownValue(flagAddress, tfjsonpath.New("enabled"), knownvalue.Bool(enabled)),
	}
}

func TestAccFlagLifecycle(t *testing.T) {
	f := newAcceptanceFixture(t)
	production := `resource "flagctl_flag" "production" {
  environment = "prod"
  key = "checkout"
  enabled = true
  description = "production setting"
}
`
	defaults := f.config(flagConfig("dev", "checkout", "") + production)
	enabled := f.config(flagConfig("dev", "checkout", "enabled = true\ndescription = \"new checkout\"") + production)
	cleared := f.config(flagConfig("dev", "checkout", "enabled = false\ndescription = \"\"") + production)
	created := statecheck.CompareValue(compare.ValuesSame())
	unchangedUpdate := statecheck.CompareValue(compare.ValuesSame())
	checks := func(description string, enabled bool) []statecheck.StateCheck {
		return append(flagStateChecks(f, "dev/checkout", description, enabled),
			created.AddStateValue(flagAddress, tfjsonpath.New("created_at")),
			statecheck.ExpectKnownValue("flagctl_flag.production", tfjsonpath.New("enabled"), knownvalue.Bool(true)),
			statecheck.ExpectKnownValue("flagctl_flag.production", tfjsonpath.New("description"), knownvalue.StringExact("production setting")),
		)
	}
	resource.Test(t, f.testCase(t,
		resource.TestStep{
			Config: defaults, ConfigPlanChecks: plannedAction(plancheck.ResourceActionCreate),
			ConfigStateChecks: append(checks("", false), unchangedUpdate.AddStateValue(flagAddress, tfjsonpath.New("updated_at"))),
		},
		resource.TestStep{
			Config: defaults, ConfigPlanChecks: plannedAction(plancheck.ResourceActionNoop),
			ConfigStateChecks: append(checks("", false), unchangedUpdate.AddStateValue(flagAddress, tfjsonpath.New("updated_at"))),
		},
		resource.TestStep{Config: enabled, ConfigPlanChecks: plannedAction(plancheck.ResourceActionUpdate), ConfigStateChecks: checks("new checkout", true)},
		resource.TestStep{Config: cleared, ConfigPlanChecks: plannedAction(plancheck.ResourceActionUpdate), ConfigStateChecks: checks("", false)},
		resource.TestStep{ResourceName: flagAddress, ImportState: true, ImportStateVerify: true},
		resource.TestStep{Config: cleared, PlanOnly: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
	))
}

func TestAccFlagDriftAndExternalDeletion(t *testing.T) {
	f := newAcceptanceFixture(t)
	config := f.config(flagConfig("dev", "checkout", "enabled = true\ndescription = \"managed\""))
	checks := flagStateChecks(f, "dev/checkout", "managed", true)
	resource.Test(t, f.testCase(t,
		resource.TestStep{Config: config, ConfigPlanChecks: plannedAction(plancheck.ResourceActionCreate), ConfigStateChecks: checks},
		resource.TestStep{
			PreConfig: func() {
				enabled, description := false, "external edit"
				if _, err := f.client.Update(t.Context(), "dev", "checkout", flags.UpdateInput{Enabled: &enabled, Description: &description}); err != nil {
					t.Fatal(err)
				}
			},
			Config: config, ConfigPlanChecks: plannedAction(plancheck.ResourceActionUpdate), ConfigStateChecks: checks,
		},
		resource.TestStep{
			PreConfig: func() {
				if err := f.client.Delete(t.Context(), "dev", "checkout"); err != nil {
					t.Fatal(err)
				}
			},
			Config: config, ConfigPlanChecks: plannedAction(plancheck.ResourceActionCreate), ConfigStateChecks: checks,
		},
	))
}

func TestAccFlagIdentityReplacement(t *testing.T) {
	f := newAcceptanceFixture(t)
	resource.Test(t, f.testCase(t,
		resource.TestStep{
			Config: f.config(flagConfig("dev", "checkout", "")), ConfigPlanChecks: plannedAction(plancheck.ResourceActionCreate),
			ConfigStateChecks: flagStateChecks(f, "dev/checkout", "", false),
		},
		resource.TestStep{
			Config: f.config(flagConfig("dev", "checkout_v2", "")), ConfigPlanChecks: plannedAction(plancheck.ResourceActionDestroyBeforeCreate),
			ConfigStateChecks: flagStateChecks(f, "dev/checkout_v2", "", false),
		},
		resource.TestStep{
			PreConfig: func() { requireFlagAbsent(t, f.client, "dev", "checkout") },
			Config:    f.config(flagConfig("staging", "checkout_v2", "")), ConfigPlanChecks: plannedAction(plancheck.ResourceActionDestroyBeforeCreate),
			ConfigStateChecks: flagStateChecks(f, "staging/checkout_v2", "", false),
		},
		resource.TestStep{
			PreConfig: func() { requireFlagAbsent(t, f.client, "dev", "checkout_v2") },
			Config:    f.config(flagConfig("staging", "checkout_v2", "")), PlanOnly: true,
		},
	))
}

func TestAccFlagDuplicateDoesNotAdopt(t *testing.T) {
	f := newAcceptanceFixture(t)
	original, err := f.client.Create(t.Context(), "dev", flags.CreateInput{Key: "checkout", Enabled: true, Description: "unmanaged"})
	if err != nil {
		t.Fatal(err)
	}
	testCase := f.testCase(t, resource.TestStep{
		Config:      f.config(flagConfig("dev", "checkout", "")),
		ExpectError: regexp.MustCompile("Unable to create flag"),
	})
	// This flag belongs to somebody else. Terraform must neither adopt it after
	// the conflict nor delete it during the testing framework's cleanup.
	testCase.CheckDestroy = func(_ *terraform.State) error {
		current, err := f.client.Get(t.Context(), "dev", "checkout")
		if err != nil {
			return err
		}
		if current != original {
			return fmt.Errorf("failed create or cleanup modified the unmanaged flag")
		}
		return nil
	}
	resource.Test(t, testCase)
}

func TestAccFlagImportErrors(t *testing.T) {
	for _, tc := range []struct{ name, id, message string }{
		{"invalid identity", "dev/checkout/extra", "Invalid flag import ID"},
		{"missing flag", "dev/missing", "Cannot import non-existent remote object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAcceptanceFixture(t)
			resource.Test(t, f.testCase(t, resource.TestStep{
				Config:       f.config(flagConfig("dev", "checkout", "")),
				ResourceName: flagAddress, ImportState: true, ImportStateId: tc.id,
				ExpectError: regexp.MustCompile(tc.message),
			}))
		})
	}
}

func TestAccFlagInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, config, message string }{
		{"environment", flagConfig("Dev", "checkout", ""), "Invalid flagctl configuration"},
		{"description byte limit", flagConfig("dev", "checkout", fmt.Sprintf("description = %q", strings.Repeat("🌍", 257))), "Invalid flagctl configuration"},
		{"required key", `resource "flagctl_flag" "test" { environment = "dev" }`, "Missing required argument"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAcceptanceFixture(t)
			resource.Test(t, f.testCase(t, resource.TestStep{
				Config: f.config(tc.config), ExpectError: regexp.MustCompile(tc.message),
			}))
		})
	}
}

type deleteBeforeApply struct{ client *client.Client }

func (check deleteBeforeApply) CheckPlan(ctx context.Context, _ plancheck.CheckPlanRequest, response *plancheck.CheckPlanResponse) {
	response.Error = check.client.Delete(ctx, "dev", "checkout")
}

func TestAccFlagDeleteAfterPlan(t *testing.T) {
	f := newAcceptanceFixture(t)
	resource.Test(t, f.testCase(t,
		resource.TestStep{Config: f.config(flagConfig("dev", "checkout", "")), ConfigStateChecks: flagStateChecks(f, "dev/checkout", "", false)},
		resource.TestStep{
			Config: f.config(""),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(flagAddress, plancheck.ResourceActionDestroy),
					deleteBeforeApply{client: f.client},
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
		},
	))
}
