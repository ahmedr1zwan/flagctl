# flagctl

Manage boolean feature flags independently across environments through a
versioned REST API. Built in Go with SQLite persistence, flagctl supports the
complete flag lifecycle: create, list, read, enable/disable, edit, and delete.
The Cobra CLI supports create, list, get, toggle, and delete with table or JSON output.
The Terraform Plugin Framework provider manages the same flags declaratively,
including import and drift reconciliation.

| Component | Status |
| --- | --- |
| Go REST service and persistent SQLite storage | Implemented |
| Environment isolation, input validation, and local access protections | Implemented |
| Cobra CLI lifecycle with JSON/table output | Implemented |
| Terraform Plugin Framework provider lifecycle, import, and drift | Implemented; local build |
| Validation, SQLite, REST API, client, and command tests with race checks | Implemented |
| Provider unit/acceptance tests and API compatibility checks | Next |
| Docker, CI, and published binaries | Planned |

The service is currently for local development. Verification results and
the remaining milestones are recorded in [TODO.md](TODO.md) and [PLAN.md](PLAN.md).

Architecture:

```mermaid
flowchart LR
    CLI["Cobra CLI"] --> Client["Shared Go HTTP client"]
    Terraform["Terraform provider"] --> Client
    Client --> API["Go REST API (/v1)"]
    API --> DB[(SQLite)]
```

## Run the current service

Use Go 1.27.1 or a newer supported, patched release. The `go.mod` minimum selects
Go 1.27.1; an older Go installation with automatic toolchain selection enabled
downloads it on the first build. This does not replace your system Go installation.
See [Go downloads](https://go.dev/dl/) and
[toolchain selection](https://go.dev/doc/toolchain).

From this repository's directory:

```sh
go build -o bin/flagd ./cmd/flagd
./bin/flagd
```

Expect a log line containing `flagd listening` and `address=127.0.0.1:8080`.
Leave that terminal running. No API keys, `.env` file, or external database server
are needed. The service creates `data/flags.db` under the working directory.
SQLite uses the pinned [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)
driver, which builds without a C compiler. The first build downloads dependencies.

If a previous checkpoint's server is running, stop it with Ctrl+C before starting
the rebuilt binary. Otherwise it will still serve the older code.

In a second terminal:

```sh
curl --noproxy '*' -i http://127.0.0.1:8080/healthz
```

Expect `HTTP/1.1 200 OK`, `Content-Type: application/json`, and:

```json
{"status":"ok"}
```

Stop the server with **Ctrl+C** in the first terminal. It logs `flagd stopped` and
exits cleanly. Running the curl command again should fail to connect.

If port 8080 is occupied, choose another loopback port:

```sh
./bin/flagd --listen 127.0.0.1:8081
curl --noproxy '*' -i http://127.0.0.1:8081/healthz
```

`--listen '[::1]:8080'` supports IPv6 loopback. Port `0` selects an available port,
which is printed in the startup log. Hostnames, wildcard addresses, and
non-loopback IPs are rejected. `./bin/flagd --help` prints the available options.

## Use the CLI

With `flagd` running, build and use the CLI from a second terminal in the repository:

```sh
go build -o bin/flagctl ./cmd/flagctl

./bin/flagctl flags create checkout_v2 --env dev --description "New checkout"
./bin/flagctl flags list --env dev
./bin/flagctl flags list --env dev --output json
./bin/flagctl flags create checkout_v2 --env prod --enabled=true --output json
```

Default table output:

```text
ENVIRONMENT  KEY          ENABLED  DESCRIPTION
dev          checkout_v2  false    New checkout
```

`--env` is required. Create defaults to disabled with an empty description.
Use `--enabled=true` or `--enabled=false` to set the initial state. Repeating a
create fails with a useful `HTTP 409, already_exists` error and leaves the record
unchanged. The CLI and curl examples below share the same demo flags, so run either
creation example first; the other will then report a duplicate.

JSON create/get/toggle output is one flag object; JSON list output is `{"flags":[...]}`.
An empty environment produces `{"flags":[]}`, or just the header in table mode.
Successful output goes to stdout; errors go to stderr with exit code 1 and no
result on stdout. Use JSON for scripts; table descriptions escape control
characters and line breaks. For example:

```sh
./bin/flagctl flags list --env dev -o json | python3 -m json.tool
./bin/flagctl flags create --help
./bin/flagctl flags list --help
```

Continue the CLI demo with the dev and prod flags created above:

```sh
# Read one flag, including its timestamps.
./bin/flagctl flags get checkout_v2 --env dev --output json

# Set an explicit state. Repeating the command preserves that state.
./bin/flagctl flags toggle checkout_v2 --env dev --enabled=true
./bin/flagctl flags toggle checkout_v2 --env dev --enabled=false --output json

# Delete only the dev flag. The prod flag remains available.
./bin/flagctl flags delete checkout_v2 --env dev --output json
./bin/flagctl flags get checkout_v2 --env prod
```

`toggle` requires `--enabled=true` or `--enabled=false`; omitting the option or
its value fails before making a request. It preserves the description and
creation timestamp. Repeating a command that changes nothing also preserves
the update timestamp. Get, toggle, and delete each require exactly one key and
an explicit environment. A missing flag returns `HTTP 404, not_found`; a second
delete also fails with 404.

After the service acknowledges deletion with HTTP 204, the CLI produces this
JSON confirmation (the REST response itself has no body):

```json
{"environment":"dev","key":"checkout_v2","deleted":true}
```

The default delete table uses `ENVIRONMENT`, `KEY`, and `DELETED` columns.
These commands remove the dev demo flag; create it again if you want to try
the REST update examples below.

Server configuration uses this precedence: explicit `--server`, then a nonempty
`FLAGCTL_SERVER`, then `http://127.0.0.1:8080`. `--timeout` defaults to `10s` and must
be positive. `--output` (or `-o`) accepts `table` or `json`.

For a server already running on port 8081:

```sh
./bin/flagctl --server http://127.0.0.1:8081 flags list --env dev
FLAGCTL_SERVER=http://127.0.0.1:8081 ./bin/flagctl flags list --env dev --timeout 5s
```

The client accepts loopback IPs or `localhost` with an optional port and a root
URL path. Credentials in URLs, other paths, queries, fragments, and remote hosts
are rejected without echoing their values. Local HTTP requests bypass proxy
environment variables and redirects are not followed. Responses are limited to
8 MiB and decoded with validation while tolerating additive response fields.
Use HTTP with the current `flagd`; if using a local HTTPS endpoint, the client
uses normal certificate verification and provides no insecure bypass.

Stop `flagd` and repeat `flags list` to check connection-error handling. Ctrl+C
cancels an in-progress request. The CLI does not automatically retry operations:
if a mutation times out, is interrupted, or returns an invalid response, use
`flags get` or `flags list` to check the current state before retrying. The
service may already have committed the change. Restarting `flagd` with the same
data directory preserves creations, updates, and deletions made through the CLI.

## Manage flags with Terraform

The local provider exposes one `flagctl_flag` resource:

```hcl
terraform {
  required_providers {
    flagctl = {
      source = "ahmedr1zwan/flagctl"
    }
  }
}

provider "flagctl" {}

resource "flagctl_flag" "checkout" {
  environment = "dev"
  key         = "terraform_checkout"
  description = "Checkout managed by Terraform"
  enabled     = true
}
```

Build and configure the local provider using the [Terraform guide](docs/terraform.md),
then run the [example](examples/terraform/main.tf). The guide covers apply,
no-op plans, updates, CLI-induced drift, import, replacement, and destroy.
The provider has not been published to the Terraform Registry; the guide uses
a development override and skips `terraform init` for this local example.
No API keys or cloud account are needed.

## Create and read flags over REST

With the service running, use a second terminal:

```sh
# Create: 201 Created, Location header, and the new flag.
curl --noproxy '*' -i http://127.0.0.1:8080/v1/environments/dev/flags \
  -H 'Content-Type: application/json' \
  -d '{"key":"checkout_v2","description":"New checkout"}'

# List dev flags: 200 and {"flags":[...]} sorted by key.
curl --noproxy '*' -i http://127.0.0.1:8080/v1/environments/dev/flags

# Read one flag: 200 and the same flag object.
curl --noproxy '*' -i http://127.0.0.1:8080/v1/environments/dev/flags/checkout_v2

# The same key in prod can have a different value: 201, enabled=true.
curl --noproxy '*' -i http://127.0.0.1:8080/v1/environments/prod/flags \
  -H 'Content-Type: application/json' \
  -d '{"key":"checkout_v2","enabled":true}'
```

The dev flag defaults to `enabled: false`. Timestamps are generated by the
service in UTC. Omitted descriptions default to an empty string. Repeating a
create in the same environment returns **409** and leaves the existing record
unchanged. An environment without any records returns `{"flags":[]}`.

To verify persistence, stop the server with Ctrl+C, run `./bin/flagd` again from
the same directory, and repeat the list/get requests. Both environments' flags
should still exist. To repeat the whole demo with empty storage, use a **new**
directory, for example `./bin/flagd --data-dir .cache/demo-2`; this preserves
your original database.

## Update and delete flags

After creating `checkout_v2` in dev with the commands above:

```sh
# Enable: 200, enabled=true; description stays unchanged.
curl --noproxy '*' -i -X PATCH http://127.0.0.1:8080/v1/environments/dev/flags/checkout_v2 \
  -H 'Content-Type: application/json' -d '{"enabled":true}'

# Disable and clear the description together: 200.
curl --noproxy '*' -i -X PATCH http://127.0.0.1:8080/v1/environments/dev/flags/checkout_v2 \
  -H 'Content-Type: application/json' -d '{"enabled":false,"description":""}'

# Delete only the dev flag: 204, no response body.
curl --noproxy '*' -i -X DELETE http://127.0.0.1:8080/v1/environments/dev/flags/checkout_v2

# Read after deletion: 404. The prod flag is unaffected.
curl --noproxy '*' -i http://127.0.0.1:8080/v1/environments/dev/flags/checkout_v2
```

PATCH requires at least one of `enabled` or `description`. Omitted fields stay
unchanged, `false` disables, and `""` clears the description. Updates preserve
`created_at`; repeating a PATCH that changes nothing preserves `updated_at` too.
Identity and timestamps cannot be supplied in a PATCH. Missing records return
404 for both PATCH and DELETE; a repeated DELETE also returns 404.

Updates use a transaction so concurrent partial edits preserve each other's
omitted fields. Concurrent changes to the same field use last-write-wins
semantics. Restart the service after a PATCH to verify the change persists, then
restart it after DELETE to verify the record stays absent.

## Data storage

`--data-dir` selects a dedicated directory; its default is `data` relative to the
process's working directory. The database name inside it is always `flags.db`.
Starting from a different working directory selects a different default database;
use an absolute `--data-dir` when you need a fixed location.

New data directories use Unix mode `0700`, and database files use `0600`. Existing
directories/files with group or other permissions are rejected. The data directory
itself and the database/SQLite sidecar files must not be symlinks. Use a dedicated,
trusted directory on a local filesystem. Do not point the service at someone
else's SQLite file. These permission checks target macOS/Linux; they do not
provide encryption or protection from other programs running as your user.

The store has a versioned schema, a unique `(environment, key)` constraint,
parameterized queries, and one pooled database connection to serialize operations.
This checkpoint targets one service instance and small local datasets. Listing
returns all flags in the chosen environment; pagination is not implemented.

## Verify errors and access protections

With the server running on its default port:

```sh
# Health checks support HEAD too: 200, with no response body.
curl --noproxy '*' -I http://127.0.0.1:8080/healthz

# Wrong method: 405, Allow: GET, HEAD, and a JSON error.
curl --noproxy '*' -i -X POST http://127.0.0.1:8080/healthz

# Missing flag: 404 and a JSON error.
curl --noproxy '*' -i http://127.0.0.1:8080/v1/environments/dev/flags/missing

# Invalid identifier: 400; no record is created.
curl --noproxy '*' -i http://127.0.0.1:8080/v1/environments/dev/flags \
  -H 'Content-Type: application/json' -d '{"key":"INVALID KEY"}'

# Cross-origin browser mutation: 403; no record is created.
curl --noproxy '*' -i http://127.0.0.1:8080/v1/environments/dev/flags \
  -H 'Origin: https://unrelated.example' \
  -H 'Content-Type: application/json' -d '{"key":"blocked"}'
```

Creation and updates require JSON. Missing/wrong content types return 415, request bodies
larger than 16 KiB return 413, and invalid JSON/types, unknown/duplicate fields,
or explicit `null` values return 400. Descriptions are limited to 1,024 UTF-8
bytes. Unsupported methods such as PUT return 405. DELETE requires no JSON body
or content-type header. See the [API contract](docs/api-v1.md) for details.

Verify the server refuses to expose itself on your network:

```sh
./bin/flagd --listen 0.0.0.0:8080
echo $?
```

Expect a loopback-address error and exit code `1`. No listener is started by
that command.

Build/static checks, from the repository directory:

```sh
go build ./...
go vet ./...
go mod verify
gofmt -l cmd internal
```

Build, vet, and formatting checks print nothing when successful; module
verification prints `all modules verified`. Run the committed tests with:

```sh
go test ./...
CGO_ENABLED=1 go test -race -shuffle=on ./...
```

The suite covers validation, SQLite persistence/concurrency, HTTP behavior and
access protections, client failures, CLI workflows/output, process exit codes,
and service shutdown/restart. It uses temporary databases and local test servers;
no keys or running service are needed. Provider unit and Terraform acceptance
tests remain upcoming increments. See the [test guide](docs/testing.md) for
requirements, coverage, and commands, and [TODO.md](TODO.md#verification-log)
for recorded results.

To repeat the known-vulnerability check (requires internet access):

```sh
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
```

The step 4 scan reported `No vulnerabilities found.` The scanner is a development
tool and does not add a dependency to the application module. It checks known
vulnerabilities, not every possible security defect.

## Security and credentials

- This is a local development service. It enforces a loopback-only listener and
  currently exposes health and the flag lifecycle. Local processes can
  reach it; loopback binding is not authentication. Do not expose it with a
  tunnel or reverse proxy. Authentication and transport security must precede
  any future network deployment.
- HTTP header/read/write/idle timeouts and a header-size limit are configured.
  Flag creation/update bodies are limited to 16 KiB, and database operations use a
  bounded request context.
  Responses use `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.
  There are no file-serving or debug endpoints and no permissive CORS headers.
- Host must match the bound IP and port, or `localhost` on that port. Host checks
  defend against DNS rebinding. Go's browser-origin protection rejects unsafe
  cross-origin requests before they reach mutations; native clients such as
  curl need no Origin header. These checks do not authenticate local clients.
- The service does not load credential files, read API-key environment
  variables, or send outbound requests. Service logs contain lifecycle
  messages, startup errors, and operation names on storage failures, not request
  headers, query strings, bodies, or raw database errors.
- The CLI reads `FLAGCTL_SERVER` for configuration and sends requests only to its
  configured local service. It neither reads the database nor loads credentials.
  Help does not print the environment-provided server value, and errors do not
  echo raw transport errors, response bodies, or remote error messages.
- The Terraform provider shares those HTTP protections. Terraform state and
  plans contain flag values and descriptions; keep them private. The provider
  does not load API credentials. Local provider overrides are development-only.
- `.gitignore` excludes common `.env`, private-key, credential, database, and
  Terraform state/plan files. Ignore rules are a guardrail: they do not detect
  secrets pasted into source, protect already tracked files, or encrypt data.
  Keep real credentials outside this repository and never force-add secret files.
- Feature-flag **keys** such as `checkout_v2` are identifiers, not API credentials.
  Flag descriptions and values must not become a secret store.

Keep Go patched. The Go project maintains security fixes for its two most recent
major release lines; Go 1.25.5, originally installed in this workspace, is older
than those lines as of September 12, 2026. See
[Go's security guidance](https://go.dev/doc/security/). This checkpoint does not
constitute a production security audit.

## Files to explore

- `cmd/flagd/main.go`: options, loopback enforcement, server limits, and shutdown.
- `cmd/flagctl/main.go` and `internal/cli/`: Cobra commands, output, and cancellation.
- `internal/client/`: shared HTTP client, endpoint validation, and typed API errors.
- `cmd/terraform-provider-flagctl/` and `internal/provider/`: provider configuration,
  flag schema, lifecycle, import, and state refresh.
- [Terraform guide](docs/terraform.md) and [example](examples/terraform/main.tf):
  build and run the local provider.
- [Test guide](docs/testing.md): current automated suites and verification scope.
- `internal/api/`: routing, Host/origin protections, strict request decoding, and JSON errors.
- `internal/flags/flag.go`: flag model and input validation.
- `internal/store/`: private SQLite files, schema initialization, and flag queries.
- [API contract](docs/api-v1.md): implemented routes, payloads, errors, and compatibility policy.
- [Plan](PLAN.md) and [checklist](TODO.md): next increments and completion evidence.
