# Tests

The committed Go suite covers domain validation, SQLite integration, the REST
API, the shared HTTP client, Cobra commands, the service/CLI entry points, and
the Terraform provider. The provider acceptance suite uses pinned
`terraform-plugin-testing` v1.16.0 and runs separately from ordinary Go tests.
Explicit API compatibility checks are the next increment.

## Run the current suite

From the repository root:

```sh
go test ./...
```

The first run may download the Go toolchain and dependencies. Tests create their
own private database directories and HTTP servers on assigned loopback ports.
They do not use your running service, `FLAGCTL_SERVER`, project database,
Terraform state, or real credentials. Temporary files and servers are cleaned
up even when a test fails. No Terraform installation is required for this suite.

To run focused parts of the suite:

```sh
# Pure validation unit tests.
go test ./internal/flags

# SQLite and HTTP integration tests, plus handler failure/context tests.
go test ./internal/store ./internal/api

# Client/CLI behavior, process exit codes, interruption, and service restart.
go test ./internal/client ./internal/cli ./cmd/flagctl ./cmd/flagd

# Provider configuration, schema validation, import IDs, and state errors.
go test ./internal/provider -run '^Test(Provider|Resource|Schema)'
```

`cmd/terraform-provider-flagctl` still reports `[no test files]`. Acceptance
tests serve the provider in the Go test process using its production protocol
version; they do not exercise the standalone provider executable's startup.
The earlier one-off binary smoke checks are separate from this coverage.

## Terraform acceptance tests

Install Terraform, then run:

```sh
TF_ACC=1 go test ./internal/provider -run '^TestAcc' -count=1 -timeout=10m
```

The suite was verified with Terraform 1.16.1 on macOS arm64. The executable is
selected from `TF_ACC_TERRAFORM_PATH`, or from `terraform` on `PATH`. The tests
fail with an installation message if it is unavailable; they do not download a
Terraform binary automatically. For an explicit executable:

```sh
TF_ACC=1 TF_ACC_TERRAFORM_PATH=/absolute/path/to/terraform \
  go test ./internal/provider -run '^TestAcc' -count=1 -timeout=10m
```

The suite uses HashiCorp's [Framework acceptance testing integration](https://developer.hashicorp.com/terraform/plugin/framework/acctests)
to run Terraform with a protocol 6 provider server built from the current code.
No prebuilt provider, development override, Registry publication, API keys, or
running service is needed. Each case owns a temporary SQLite database, loopback
HTTP server, Terraform working directory, and CLI configuration file.

Cases run sequentially because the testing library uses process-wide Terraform
environment settings. The fixture temporarily clears inherited `TF_*` options,
including CLI arguments, token variables, debug logging, and working-directory
persistence. It then sets the test configuration and executable explicitly and
restores the original values on cleanup. Global configuration files are not
modified. An existing `FLAGCTL_SERVER` cannot direct tests at your own service.

Tests check plans and compare state attributes with fresh API reads. The suite
covers default values, no-op plans, mutable fields, timestamps, import, drift,
external deletion, replacement, validation failures, and deletion between plan
and apply. Destroy checks inspect the API before temporary storage is removed.
The duplicate-create case instead verifies its unmanaged flag survives both
the failed apply and Terraform's cleanup.

## Race detection and coverage

Run the full suite with the race detector, randomized test order, and a fresh
coverage result:

```sh
mkdir -p .cache
CGO_ENABLED=1 go test -race -shuffle=on -count=1 \
  -coverprofile=.cache/application-coverage.out ./...
go tool cover -func=.cache/application-coverage.out
```

The race detector requires a supported platform and a C compiler. Regular
builds and tests also support `CGO_ENABLED=0`. To reproduce a shuffled test
failure, use the seed printed by Go, for example `-shuffle=12345`.

Verified on macOS arm64 with Go 1.27.1:

| Package | Statement coverage | Main checks |
| --- | --- | --- |
| `internal/flags` | 100.0% | Identifier boundaries, UTF-8 byte limits, omitted fields versus explicit false/empty values |
| `internal/store` | 87.2% | CRUD, ordering, environment isolation, close/reopen persistence, concurrent writes, cancellation, private files, symlink rejection, newer schema rejection |
| `internal/api` | 98.7% | HTTP lifecycle, HEAD/204 semantics, strict JSON, body/media limits, Host/origin protection, stable error codes, safe logs, bounded request contexts |
| `internal/client` | 98.5% | Real-service lifecycle, request serialization, invalid input, response validation, safe errors, redirect refusal, timeouts, proxy bypass, TLS trust, response size limits |
| `internal/cli` | 95.8% | Real-service workflows, JSON/table output, terminal escaping, configuration precedence, invalid commands, output failures, cancellation |
| `cmd/flagctl` | 100.0% | Actual entry point in child processes: help, success, error exit codes, stdout/stderr separation, SIGINT |
| `cmd/flagd` | 82.1% | Listen-address restrictions, startup failures, health, graceful shutdown, restart persistence |
| `internal/provider` | 85.5% | Protocol schema, configuration precedence, unknown values, timeouts, validators, import identity, safe errors and prior-state preservation |

For provider unit and acceptance coverage together, run:

```sh
CGO_ENABLED=1 TF_ACC=1 go test -race -shuffle=on -count=1 \
  -coverprofile=.cache/provider-coverage.out ./internal/provider -timeout=10m
go tool cover -func=.cache/provider-coverage.out
```

This command verified **98.4%** statement coverage for `internal/provider` with
no races reported. The table above uses the ordinary suite with acceptance
disabled. The standalone provider entry point still reports 0%. Coverage does
not prove API backward compatibility or replace a security review. Unix
permission/symlink checks and the subprocess interrupt test are skipped on Windows.

CLI process tests invoke the real entry point in a child test executable. They
use a controlled endpoint and inherit only ordinary runtime paths, not developer
credential/configuration environment variables. During coverage runs, children
write their coverage data to the parent test's coverage directory, so their
execution appears in the report without runtime warnings on application stderr.

## What the tests protect

- Duplicate creation preserves the original record. The same key in dev and
  prod remains independent, and lists stay ordered.
- Partial updates preserve omitted fields. Explicit false and empty descriptions
  work, creation timestamps survive updates, and no-op timestamps stay stable.
- Updates and deletions survive closing and reopening SQLite. Concurrent partial
  updates preserve both changes; racing an update against deletion cannot
  recreate the deleted record.
- Invalid and canceled operations leave data unchanged. HTTP checks reject null,
  duplicate, case-mismatched, unknown, incorrectly typed, and oversized inputs.
  Body limits are checked with both known-length and chunked requests.
- Database and sidecar links, non-files, unsafe permissions, and unsupported
  schema versions are rejected. URI metacharacters in a directory name cannot
  turn persistent storage into an in-memory database.
- Error responses and storage logs omit synthetic sensitive values from request
  headers, query strings, bodies, and underlying database errors.
- The client refuses redirects and makes no application-level retries after
  HTTP failures. Invalid inputs make no requests. Malformed, oversized, or
  mismatched responses fail without echoing raw remote content into errors.
- Proxy environment variables do not route local requests through a proxy;
  HTTPS rejects an untrusted certificate. Timeouts and cancellation work while
  waiting for headers and while reading response bodies.
- CLI JSON stays machine-readable, tables escape terminal control characters,
  and errors go to stderr with a failing process exit code. If output fails
  after a successful mutation, the error explains that the change committed.
- Service shutdown closes the listener, restart retains flags, and startup
  errors do not silently leave a running service.
- Provider API errors preserve prior state. An unknown 404 error cannot silently
  drop a managed flag, and failed creation does not adopt an existing flag.
- Terraform updates both mutable fields, replaces changed identities, detects
  drift/deletion, imports complete state, and removes managed flags on destroy.

Next: version-compatibility fixtures. CI will run these commands once the planned
GitHub Actions workflows are added.
