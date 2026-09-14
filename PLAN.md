# flagctl implementation plan

Status: steps 1–5 (service, Cobra CLI, Terraform provider, automated tests, and
API compatibility evidence) are complete and verified locally. Next is secure
container access, followed by Docker, CI, and releases. Provider Registry
publication remains separate.

## Goal

Build a small feature-flag service in Go, a Cobra CLI that talks to it, and a
Terraform provider that manages the same flags through its REST API. Complete
the work in small increments: core functionality first, then automated tests,
delivery tooling, and evidence for the resume claims.

The local workspace was empty when first inspected on September 12, 2026. Git
is now initialized on `main` with origin
https://github.com/ahmedr1zwan/flagctl.git. The remote was verified to contain no
refs before setup. Verified increments are now being committed for GitHub
publication using the existing authenticated account. The project requires
Go 1.27.1, verified using Go's automatic toolchain download, because the installed
system Go 1.25.5 is outside the currently security-supported release lines.

## Initial scope and design

- A flag is a boolean setting identified by `(environment, key)`, with a
  description and creation/update timestamps. New flags default to disabled.
- Environments such as `dev`, `staging`, and `prod` are names on flag records.
  The same key can have independent values in different environments. Separate
  environment-management commands are outside the initial scope.
- Use Go's standard HTTP server, a `/v1` JSON API, and SQLite persistence.
  The implemented store uses pinned `modernc.org/sqlite` v1.58.0 and builds with
  CGO disabled. Its private data directory defaults to `data` and is configurable
  through `--data-dir`.
- The service owns validation and persistence. Both clients use the HTTP API;
  neither accesses the database directly.
- CLI commands use pinned Cobra v1.10.2 through a shared HTTP client. The
  Terraform provider uses pinned Plugin Framework v1.19.0 and that same client.
- Start as a local, single-instance service. Step 1 enforces literal loopback
  listen addresses; authentication and encrypted transport are prerequisites
  for any future network access. No API credentials are needed or loaded now.
  A web dashboard, targeting rules, percentage rollouts, multi-instance deployment,
  and application SDKs are outside this first release.

```text
Cobra CLI ----------> shared Go HTTP client --HTTP /v1--> Go service --> SQLite
Terraform provider -> shared Go HTTP client -----------^
```

Proposed layout; create directories only as their implementations arrive:

```text
cmd/flagd/                       Service entry point
cmd/flagctl/                     CLI entry point
cmd/terraform-provider-flagctl/  Provider entry point
internal/flags/                 Domain types and validation
internal/store/                 SQLite queries and schema setup
internal/api/                   HTTP routes, requests, responses, and errors
internal/client/                HTTP client shared by CLI and provider
internal/cli/                   Cobra commands and output formatting
internal/provider/              Provider configuration and flag resource
examples/terraform/             Runnable provider example
docs/                          API contract and compatibility policy
```

## Planned API contract

Write the exact request/response shapes before implementing the routes. This is
the proposed surface, not a claim that these endpoints already exist.
The detailed contract is in [docs/api-v1.md](docs/api-v1.md). Health and all flag
routes below are implemented. Frozen contract fixtures and an archived client
now verify the v1 compatibility baseline and the additive response-ID change.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/healthz` | Service health |
| POST | `/v1/environments/{env}/flags` | Create a flag; return 201 |
| GET | `/v1/environments/{env}/flags` | List that environment's flags; return 200 |
| GET | `/v1/environments/{env}/flags/{key}` | Read one flag; return 200 |
| PATCH | `/v1/environments/{env}/flags/{key}` | Set enabled state and/or description; return 200 |
| DELETE | `/v1/environments/{env}/flags/{key}` | Delete a flag; return 204 |

Use consistent JSON errors with stable codes. Document validation errors,
duplicate keys (409), and missing records (404). Validate environment/key names
and disallow `/` so identities remain unambiguous. Keep list ordering stable.
PATCH must distinguish an omitted field from `enabled: false` or an empty
description. Environment and key are immutable identifiers.

The CLI command `flags toggle` requires an explicit target state through
`--enabled=true` or `--enabled=false`. It uses PATCH to set that state, so repeating
the command does not reverse an earlier successful change. This also matches
Terraform's declarative updates. Document this command behavior clearly.

## Stages and completion checkpoints

### 1. Repository foundation and service skeleton

Verify the remote and connect the local folder without overwriting any remote
work. Initialize the Go module, add a truthful README and `.gitignore`, define
the flag model and API contract, and add `cmd/flagd` with `/healthz`.

Checkpoint: the server builds, starts locally, and answers a health request.
This is the first implementation increment; stop here to review before adding
the storage and CRUD functionality.

### 2. Core service and persistent flags

Add SQLite schema setup with a unique `(environment, key)` constraint. Build
create/list/get first, then PATCH/delete in a separate increment. Add payload
validation, consistent errors, and request-body limits. Server timeouts, header
limits, and graceful shutdown were brought forward into step 1. Before adding
mutations, review browser-origin/Host handling and enforce JSON payloads. Keep
database files private to the running user and avoid logging sensitive inputs.

Completed checkpoint 2a: SQLite create/list/get, private files, strict JSON and
body limits, Host validation, browser-origin protection, stable list ordering,
environment isolation, duplicate rejection, and restart persistence. Verified
against isolated real databases, including concurrent create requests.

Completed checkpoint 2b: transactional partial updates, explicit enable/disable,
description clearing, no-op timestamp preservation, deletion, and missing-record
handling. Creation/read regression checks and update/delete concurrency,
persistence, validation, and security checks passed. The dedicated Go test suite
was deferred at this checkpoint and added for the service in step 5a. The shared
client and Cobra CLI were added in step 3.

Checkpoint: use curl to create, list, read, enable, disable, and delete flags.
Verify the same key is isolated between `dev` and `prod`, duplicate creation
fails clearly, and data survives a server restart.

### 3. Cobra CLI

Build the shared HTTP client, then `flags create` and `flags list`. Follow with
`flags get`, `flags toggle`, and `flags delete`. Add required `--env`, configurable
`--server` / `FLAGCTL_SERVER`, request timeouts, help, and `--output table|json`.
Keep successful machine-readable output on stdout, errors on stderr, and return
nonzero exit codes for failures.

Completed checkpoint 3a: shared HTTP create/list client, Cobra commands, required
environments, server flag/environment/default precedence, timeouts, cancellation,
table/JSON output, and errors with nonzero exit codes. Verified against a real
service and local servers returning malformed, oversized, delayed, and redirect
responses. Endpoint credentials are rejected, proxies are bypassed, and table
cells escape terminal controls.

Completed checkpoint 3b: shared client get/update/delete and CLI get/toggle/delete.
Toggle requires an explicit target value and sends only the enabled field.
Verified the complete lifecycle, no-op timestamps, description preservation,
dev/prod isolation, restart persistence, missing-record errors, and table/JSON
output. Local adverse-response checks cover wrong identities/states, safe errors,
bodyless 204 responses, no redirects, timeout, and cancellation. Shared client
checks also verified empty descriptions, combined updates, and typed 404 errors.
The Terraform provider was added in step 4; dedicated Go suites remain in step 5.

Verified usage examples (also included in the README):

```sh
flagctl flags create checkout_v2 --env dev --description "New checkout"
flagctl flags list --env dev --output table
flagctl flags toggle checkout_v2 --env dev --enabled=true
flagctl flags get checkout_v2 --env dev --output json
```

Checkpoint: perform the complete flag lifecycle through a running service using
the CLI, verify JSON can be parsed, and verify an unavailable server gives a
useful error. At this point the service/CLI portion is demonstrable.

### 4. Terraform provider

Add a Plugin Framework provider with configurable service endpoint and one
`flagctl_flag` resource. Implement create/read/update/delete, import using
`environment/key`, and replacement when environment or key changes. Reuse the
HTTP client. Handle external deletion by removing the missing resource from
Terraform state during refresh, and let Terraform detect CLI-made drift.

Start with a local provider installation and document its setup. Terraform
Registry publication is an optional follow-up, separate from GitHub binary
releases. A data source is also optional.

Implemented resource example:

```hcl
resource "flagctl_flag" "checkout" {
  key         = "checkout_v2"
  environment = "dev"
  description = "New checkout"
  enabled     = true
}
```

Checkpoint: apply creates the flag, a second plan has no changes, an enabled-state
edit updates it, import works, a CLI edit produces drift, and destroy removes it.

Completed checkpoint 4: the provider manages `flagctl_flag` with defaults,
immutable environment/key, timestamps, complete CRUD, and `environment/key`
import. Verified with Terraform 1.16.1 using isolated state and real service
instances: apply, no-op plans, mutable-field updates/clearing, CLI drift, external
deletion, key/environment replacement, import, and destroy. Errors preserve
state and unmanaged flags are not silently adopted. Configuration validation,
unknown provider values, endpoint precedence, timeouts, and sanitized diagnostics
were exercised. The local setup and runnable example are documented in
[docs/terraform.md](docs/terraform.md). Automated acceptance suites remain in step 5.

### 5. Automated tests and API compatibility evidence

Once the core flows work, add meaningful unit tests for validation, handlers,
client error handling, and CLI behavior. Add storage integration tests using
temporary databases and provider acceptance tests against an isolated real
service through Terraform's testing tooling.

Cover provider creation, update, import, no-op plans, drift, external deletion,
and destroy cleanup. Keep acceptance tests explicit and isolated from normal
unit test runs. Run the race detector where appropriate.

Document the `/v1` compatibility promise: preserve existing field meanings,
types, status codes, and defaults; avoid adding required inputs; use a new major
API version for breaking changes. Add contract fixtures and demonstrate an
older client still works after an additive API change. A `/v1` prefix alone is
not sufficient evidence for the resume's backward-compatibility claim.

Checkpoint: reproducible unit/integration and acceptance test commands pass,
including the documented compatibility scenario.

Completed checkpoint 5a: committed validation unit tests, SQLite integration
tests, and HTTP integration/failure tests. Coverage includes defaults, partial
updates, persistence after reopening, concurrency, invalid/canceled operations,
file protections, strict payload decoding, Host/origin checks, and safe errors.
The suite passes with CGO disabled and with race detection and shuffled test
order. Commands and per-package coverage are in [docs/testing.md](docs/testing.md).

Completed checkpoint 5b: client request/response and transport tests, real-service
CLI workflows, JSON/table output and output-write failures, configuration
precedence, subprocess exit codes and interruption, and service startup,
shutdown, and restart persistence. Tests exercise sanitized errors, redirect
refusal, proxy bypass, normal TLS certificate verification, timeout/cancellation,
and invalid inputs making no requests. The full suite passes with CGO disabled
and with race detection, shuffled order, and coverage. Child CLI execution is
included in the entry-point coverage report.

Completed checkpoint 5c: provider unit tests verify protocol schema, configuration
precedence, timeouts, unknown values, validation, import identity, safe diagnostics,
and state preservation after API errors. Acceptance tests use pinned
terraform-plugin-testing v1.16.0 with protocol 6 and a real Terraform CLI against
isolated SQLite/HTTP service fixtures. Verified defaults, no-op plans, updates
and clearing, timestamp preservation, import, drift, external deletion,
identity replacement, duplicate-create protection, invalid configuration/imports,
and deletion between plan and apply. Destroy checks inspect the API before
temporary storage is removed. Normal and acceptance suites pass with race
detection and shuffled order; provider coverage is 85.5% without acceptance and
98.4% with acceptance. The dependency scan including test code found no known
vulnerabilities.

Completed checkpoint 5d: froze the pre-addition v1 request/response contract and
archived the client/domain source from commit afd343d in a standalone test module
with source digests. The client builds without Git history or dependency downloads
and completes its lifecycle against the current real service. Added a read-only
`id` (`environment/key`) to flag API responses, preserving the old fixtures and
client source. New tests verify IDs in create/get/update/list and reject attempts
to supply them as inputs. Negative controls reject breaking response changes.
The v1 policy documents stable behavior, additive changes, and major-version
requirements. Normal tests and Terraform acceptance tests pass after the API
addition, including race checks. Step 5 is complete.

### 6. Docker, CI, releases, and final documentation

Add a multi-stage Dockerfile and Compose setup with a persistent database volume.
Before enabling a container-interface listener, implement and verify explicit
network access configuration, authentication, and transport protection. Keep the
Compose host port bound to loopback and credentials outside committed files.
Verify restart persistence. The current server intentionally rejects wildcard
listeners, so Docker networking needs this deliberate security increment.

Add GitHub Actions for formatting checks, `go vet`, builds, unit/integration
tests, and a separate acceptance-test job with isolated service setup. Add a
README badge once the workflow exists and has run successfully.

Configure GoReleaser to build versioned service, CLI, and provider artifacts with
checksums. Validate a local snapshot, then publish a real tagged GitHub release
and verify downloaded binaries work. Finish the README with architecture,
installation, verified CLI and Terraform examples, and the test/release commands.

Checkpoint: a clean checkout follows the quickstart, Docker retains flags across
restarts, hosted CI is green, and an actual GitHub release contains usable binaries.

## Working approach

Use one small increment at a time. Explain the design choices and the resulting
behavior, manually check each new core flow, and update TODO.md with evidence.
The early increments used build and smoke checks, as requested. From step 5a,
add committed tests in focused increments and run the relevant existing suites
when behavior changes.

Keep changes suitable for descriptive commits such as `add service skeleton`,
`persist environment-scoped flags`, `add Cobra create and list commands`, and
`implement Terraform flag resource`. Do not manufacture historical commits or
claim a completed checkpoint without verifying it.

## Resume evidence

| Target claim | Evidence needed before claiming completion |
| --- | --- |
| Go service and versioned REST API | Documented `/v1` contract and working persistent CRUD |
| Backward-compatible API | Compatibility policy and passing old-client/contract checks |
| Cobra CLI across environments | Demonstrated create/list/toggle with JSON and table output |
| Terraform Plugin Framework provider | Working lifecycle, import, and drift handling |
| Unit and acceptance tests | Meaningful passing suites and documented commands |
| Docker | Working container quickstart with persistent storage |
| GitHub Actions CI | Successful hosted workflow runs |
| GoReleaser-published binaries | Actual release artifacts, not just configuration files |

The full proposed resume bullets describe the finished target. Keep submitted
resume wording aligned with completed and verified rows.

## Reference documentation

- [Cobra](https://cobra.dev/)
- [Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework)
