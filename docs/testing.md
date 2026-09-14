# Tests

The committed Go suite covers domain validation, SQLite integration, the REST
API, the shared HTTP client, Cobra commands, and the service/CLI entry points.
It uses standard Go testing tools without additional test dependencies.
Provider unit/acceptance tests and explicit API compatibility checks are
upcoming increments.

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
```

`[no test files]` for `internal/provider` and `cmd/terraform-provider-flagctl`
is expected at this checkpoint. Those packages are compiled but do not yet
have committed tests. Previously recorded one-off Terraform smoke checks are
not counted as automated test coverage.

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

These are per-package statement coverage figures for this checkpoint. Provider
packages still report 0% under coverage. These figures do not prove API backward
compatibility or replace a security review. Unix permission/symlink checks and
the subprocess interrupt test are skipped on Windows.

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

Next: provider unit tests, an isolated Terraform acceptance suite, and
version-compatibility fixtures. CI will run these commands once the
planned GitHub Actions workflows are added.
