# Tests

The committed Go suite covers domain validation, SQLite integration, the REST
API, the shared HTTP client, Cobra commands, the service/CLI entry points, and
the Terraform provider. The provider acceptance suite uses pinned
`terraform-plugin-testing` v1.16.0 and runs separately from ordinary Go tests.
The normal suite also includes frozen API contract and historical-client checks.

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

## API compatibility checks

```sh
go test ./internal/api -run '^Test(V1|FlagResponseID)' -count=1
```

The 31 ordered [wire fixtures](../internal/api/testdata/v1/README.md) protect the
v1 response shapes, defaults, status codes, headers, partial updates, environment
isolation, list ordering, empty arrays, and representative errors. They run
against an isolated SQLite-backed service and permit extra response object fields.
Negative controls verify that breaking shape/type/value changes fail the matcher.

The [historical client fixture](../internal/api/testdata/v1-client/README.md)
contains exact client and domain source from commit `afd343d`, with SHA-256
digests. It is a development snapshot, not a published release. The test builds
it in a standalone module using the test runner's Go toolchain, then exercises
its lifecycle against the current service. It needs no Git history, external
modules, or running service, and inherits only runtime/cache paths.

The archived source is unchanged after adding `id` to flag responses. Both that
client and the pre-addition wire fixtures pass; separate tests protect the new
field's value and read-only behavior. Preserve the baseline when adding features,
as described in the [v1 policy](api-v1.md#v1-compatibility-policy).

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

Verified on macOS arm64 with Go 1.27.1 on September 15, 2026:

| Package | Statement coverage | Main checks |
| --- | --- | --- |
| `internal/flags` | 100.0% | Identifier boundaries, UTF-8 byte limits, omitted fields versus explicit false/empty values |
| `internal/store` | 87.2% | CRUD, ordering, environment isolation, close/reopen persistence, concurrent writes, cancellation, private files, symlink rejection, newer schema rejection |
| `internal/api` | 98.9% | HTTP lifecycle, HEAD/204 semantics, strict JSON, body/media limits, Host/origin protection, stable errors, safe logs, bounded contexts, v1 fixtures, historical-client compatibility, read-only IDs |
| `internal/security` | 90.3% | Private token/key files, symlinks/FIFOs, origin validation, token comparison/redaction, certificate validity and trust configuration |
| `internal/client` | 97.2% | Real-service lifecycle, request serialization, invalid input, response validation, safe errors, redirect refusal, timeouts, proxy bypass, TLS trust, response size limits |
| `internal/cli` | 96.1% | Real-service workflows, JSON/table output, terminal escaping, configuration precedence, invalid commands, output failures, cancellation |
| `cmd/flagctl` | 100.0% | Actual entry point in child processes: help, success, error exit codes, stdout/stderr separation, SIGINT |
| `cmd/flagd` | 91.0% | Listen-address restrictions, startup failures, TLS health probe, graceful shutdown, restart persistence |
| `internal/provider` | 87.8% | Protocol schema, configuration precedence, unknown values, timeouts, validators, import identity, safe errors and prior-state preservation |

For provider unit and acceptance coverage together, run:

```sh
CGO_ENABLED=1 TF_ACC=1 go test -race -shuffle=on -count=1 \
  -coverprofile=.cache/provider-coverage.out ./internal/provider -timeout=10m
go tool cover -func=.cache/provider-coverage.out
```

This command verified **98.5%** statement coverage for `internal/provider` with
no races reported. The table above uses the ordinary suite with acceptance
disabled. The standalone provider entry point and shared test fixtures (`internal/testutil`)
still report 0%; the fixtures are not imported by application binaries. Coverage does
not prove API backward compatibility or replace a security review. Unix
permission/symlink checks and the subprocess interrupt test are skipped on Windows.

CLI process tests invoke the real entry point in a child test executable. They
use a controlled endpoint and inherit only ordinary runtime paths, not developer
credential/configuration environment variables. During coverage runs, children
write their coverage data to the parent test's coverage directory, so their
execution appears in the report without runtime warnings on application stderr.

The historical client is a separate, CGO-disabled executable under `testdata`.
Its source is excluded from the current application's coverage profile and its
subprocess is not race-instrumented. The current service and compatibility test
harness run under the race detector with the command above.

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
- The unchanged historical client still completes its lifecycle after an
  additive API field, while contract checks reject changes to existing behavior.

Secure-access tests also verify:

- TLS 1.3, certificate chain/hostname/expiry checks, and refusal of TLS 1.2.
- Missing, incorrect, duplicate, URL, and cookie credentials cannot read or
  mutate flags; anonymous health is available only over TLS in secure mode.
- Partial security configuration fails before storage is created. Network
  listeners require opt-in and enforce the public origin independently of the
  bound address. Forwarded headers do not bypass TLS requirements.
- CLI/provider token and CA path precedence, explicit empty overrides, and
  sanitized failures. Test processes ignore developer credential environment
  variables and create private, ephemeral certificates/token files.
- A real Terraform TLS lifecycle covers creation, no-op planning, update, import,
  destroy cleanup, and absence of the token from resource state. Normal local
  acceptance and frozen compatibility tests continue to run separately.

The [secure-access demo](security.md) was also verified with built binaries,
OpenSSL 3, curl, and the CLI. Linux and Windows amd64 cross-builds pass; runtime
checks were performed on macOS arm64. Secret-file loading fails closed on Windows
until ACL-aware privacy validation is implemented.

## Docker integration tests

With a local Docker Engine and Compose available to a non-root macOS/Linux user:

```sh
FLAGCTL_DOCKER_TEST=1 go test -v -count=1 ./tests/docker -timeout=15m
```

Normal Go runs skip this test. The opt-in run creates its own ephemeral
certificates/token, unique Compose project, temporary host CLI, and database
volume. It clears inherited flagctl/Compose configuration and does not modify
the developer's running service, state, or Docker context. Cleanup removes its
own containers, network, volume, and generated image tag; build caches remain.
The test exports the effective Docker context to verify that a synthetic secret
nested in a source directory never reaches the builder.

Verified with Docker Desktop 4.90.0, Engine 29.7.2, and Compose 5.5.1: TLS health,
401 rejection, CLI changes, environment isolation, runtime restrictions, private
database files, and persistence across restart and complete container replacement.
The suite passed on native Linux arm64 and Linux amd64 under emulation using
`DOCKER_DEFAULT_PLATFORM=linux/amd64`. The [Docker quickstart](docker.md) was also
verified with OpenSSL and curl. The actual runtime binary passed `govulncheck`
and the exported image contained no source, credentials, or shell.

The ordinary `cmd/flagd` suite tests the health probe's public Host, direct loopback
connection, trust/hostname failures, redirects/proxy refusal, response limits,
and cancellation. Container processes themselves are not race-instrumented;
the normal Go race suite covers the service code.

Next: GitHub Actions CI, followed by release artifacts.
