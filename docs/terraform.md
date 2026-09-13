# Terraform provider

The provider uses the Terraform Plugin Framework and the same HTTP client as
the CLI. The `flagctl_flag` resource supports create/read/update/delete, import,
replacement, and reconciliation of changes made through the CLI or REST API.
It is currently a local development build, not a published Registry provider.

## Build and connect locally

Prerequisites: Go as described in the main README, Python 3 for the configuration
snippet below, and Terraform. The example requires Terraform 1.3 or later; this
checkpoint was verified with Terraform 1.16.1 on macOS arm64.

From the repository root, build all three executables:

```sh
go build -o bin/flagd ./cmd/flagd
go build -o bin/flagctl ./cmd/flagctl
go build -o bin/terraform-provider-flagctl ./cmd/terraform-provider-flagctl
```

Generate a development override pointing at your absolute build directory:

```sh
python3 - <<'PY'
import json
import os
from pathlib import Path

os.umask(0o077)
root = Path.cwd()
(root / ".cache").mkdir(exist_ok=True)
provider_dir = json.dumps(str(root / "bin"))
(root / ".cache/terraform-dev.tfrc").write_text(
    'disable_checkpoint = true\n'
    'provider_installation {\n'
    '  dev_overrides {\n'
    '    "registry.terraform.io/ahmedr1zwan/flagctl" = ' + provider_dir + '\n'
    '  }\n'
    '}\n'
)
PY
```

This configuration selects only your local provider binary. It contains no
credentials and does not change your global Terraform configuration. Development
overrides bypass the usual provider version/checksum selection and are for local
development. See HashiCorp's [development override documentation](https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides-for-provider-developers).

Start a separate demo service in this terminal:

```sh
./bin/flagd --listen 127.0.0.1:8081 --data-dir .cache/terraform-demo
```

In a second terminal at the repository root, select the configuration and service:

```sh
export TF_CLI_CONFIG_FILE="$PWD/.cache/terraform-dev.tfrc"
export FLAGCTL_SERVER=http://127.0.0.1:8081
umask 077

terraform -chdir=examples/terraform validate
terraform -chdir=examples/terraform plan
terraform -chdir=examples/terraform apply
```

Review the plan and enter `yes` to create the demo flag. Expect a development
override warning; it confirms Terraform is using the local binary. Skip
`terraform init` for this example: init still tries to select a published
provider version, and this provider is not published. This example has no
external modules, remote backend, or other provider installation requirements.

The [example configuration](../examples/terraform/main.tf) manages:

```hcl
resource "flagctl_flag" "checkout" {
  environment = "dev"
  key         = "terraform_checkout"
  description = "Checkout managed by Terraform"
  enabled     = true
}
```

Check both Terraform and the API-backed CLI:

```sh
terraform -chdir=examples/terraform plan -detailed-exitcode
./bin/flagctl flags get terraform_checkout --env dev --output json
```

The second plan should report no changes and exit 0. A plan with changes exits
2 when `-detailed-exitcode` is set; errors exit 1. The CLI should show the same
enabled state, description, and timestamps as Terraform.

## Update, drift, import, and cleanup

Set `enabled = false` and `description = ""` in the example, then run plan and
apply again. Terraform should update the existing flag, disable it, and clear
its description. The creation timestamp stays the same. A subsequent plan
should have no changes.

To demonstrate drift after that edit:

```sh
./bin/flagctl flags toggle terraform_checkout --env dev --enabled=true
terraform -chdir=examples/terraform plan
terraform -chdir=examples/terraform apply
```

Terraform should detect the CLI change and restore the configured `false` state.

To import a flag created outside Terraform, define its resource block with the
matching environment/key and intended mutable values, then run:

```sh
terraform -chdir=examples/terraform import flagctl_flag.checkout dev/terraform_checkout
terraform -chdir=examples/terraform plan
```

Import requires that the resource address is not already in state. For a
round-trip import demo **only in this example workspace**, first run
`terraform -chdir=examples/terraform state rm flagctl_flag.checkout`; this
removes Terraform's tracking without deleting the service flag. Then import it
using the commands above. Import reads the actual flag, including both
timestamps. A missing flag or invalid import identifier fails.

Changing `key` or `environment` plans replacement. Deleting a flag outside
Terraform causes the next normal plan to propose creating it again if its
resource block remains configured. Destroy the demo when finished:

```sh
terraform -chdir=examples/terraform destroy
./bin/flagctl flags get terraform_checkout --env dev
unset TF_CLI_CONFIG_FILE FLAGCTL_SERVER
```

After confirming destroy, get should fail with HTTP 404. Stop the demo service
with Ctrl+C in its terminal. If you changed the key/environment while trying
replacement, use the new identity for the final get check.

## Configuration and behavior

Provider configuration:

```hcl
provider "flagctl" {
  server  = "http://127.0.0.1:8081"
  timeout = "5s"
}
```

| Attribute | Behavior |
| --- | --- |
| `server` | Optional loopback origin. Explicit value overrides nonempty `FLAGCTL_SERVER`, then defaults to `http://127.0.0.1:8080`. |
| `timeout` | Optional positive Go duration per HTTP request; defaults to `10s`. |

Explicit empty values are invalid. Provider configuration must be known before
planning resources. Keep the service endpoint consistent for a Terraform
workspace: changing it directs that workspace's operations to a different service.

Resource attributes:

| Attribute | Behavior |
| --- | --- |
| `environment` | Required; changing it replaces the flag. |
| `key` | Required; unique within its environment; changing it replaces the flag. |
| `description` | Optional; defaults to `""`. Maximum 1,024 UTF-8 bytes. Terraform manages this value even when omitted. |
| `enabled` | Optional; defaults to `false`. Terraform manages this value even when omitted. |
| `id` | Computed as `environment/key`; also the import identifier. |
| `created_at` | Computed UTC RFC3339 timestamp; preserved during updates. |
| `updated_at` | Computed UTC RFC3339 timestamp; preserved for no-op updates. |

Environment/key names use the same API rules: 1–63 lowercase ASCII letters,
digits, underscores, or hyphens, starting with a letter or digit. The provider
validates these constraints during planning and the service validates every
request. Existing unmanaged flags produce a duplicate error on create; import
them deliberately. Updates never recreate missing flags. A recognized API
`404/not_found` removes the resource during refresh and is successful cleanup
during delete. Other read/delete errors retain state and report a diagnostic.

## Security and current limits

No API keys or cloud accounts are needed. The provider uses the shared client's
loopback-only endpoints, request timeout, response validation/limits, proxy
bypass, and redirect refusal. It does not read the SQLite database or load
credentials. Authentication and transport protection are still required before
any network deployment.

Terraform state and saved plans contain flag names, values, and descriptions;
they are not secret storage. State, plans, variable files, and local CLI
configuration are ignored by Git. Keep them private and keep credentials out of
Terraform configuration and flag descriptions. Terraform itself may display
configuration values or include them in debug logs, even though provider-generated
API errors do not echo raw service messages.

Requests are not automatically retried. After a timeout or interruption, inspect
the flag through the CLI and run a fresh plan before retrying. If creation
committed but Terraform could not save state, import the existing flag. Avoid
concurrent CLI mutations while applying Terraform changes; updates to the same
field use the service's last-write-wins behavior.

The verified checks for this checkpoint used a real local Terraform CLI and
temporary service/database/state directories. Dedicated Go unit and Terraform
acceptance suites are the next stage. Registry publication, data sources, and
remote service access are not included in this increment.

Implementation references: HashiCorp's [provider configuration](https://developer.hashicorp.com/terraform/plugin/framework/providers),
[plan modifiers](https://developer.hashicorp.com/terraform/plugin/framework/resources/plan-modification),
and [resource import](https://developer.hashicorp.com/terraform/plugin/framework/resources/import).
