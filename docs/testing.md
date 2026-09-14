# Tests

The committed Go suite currently covers domain validation, SQLite integration,
and the REST API. It uses standard Go testing tools without additional test
dependencies. Client/CLI tests, Terraform acceptance tests, and explicit API
compatibility checks are upcoming increments.

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

To run the two types of checks separately:

```sh
# Pure validation unit tests.
go test ./internal/flags

# SQLite and HTTP integration tests, plus handler failure/context tests.
go test ./internal/store ./internal/api
```

`[no test files]` for the client, CLI, provider, and command entry points is
expected at this checkpoint. Those packages are compiled but do not yet have
committed tests. Previously recorded one-off smoke checks are not counted as
automated test coverage.

## Race detection and coverage

Run the service suite with the race detector, randomized test order, and a fresh
coverage result:

```sh
mkdir -p .cache
CGO_ENABLED=1 go test -race -shuffle=on -count=1 \
  -coverprofile=.cache/service-coverage.out \
  ./internal/flags ./internal/store ./internal/api
go tool cover -func=.cache/service-coverage.out
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

These are per-package statement coverage figures for this checkpoint. They do
not measure the whole application, prove API backward compatibility, or replace
a security review. Filesystem permission/symlink checks target Unix filesystems;
those specific checks are skipped on Windows.

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

Next: client and command tests, followed by an isolated Terraform acceptance
suite and version-compatibility fixtures. CI will run these commands once the
planned GitHub Actions workflows are added.
