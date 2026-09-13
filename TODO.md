# flagctl checklist

Follow PLAN.md in order. Complete one increment, verify it, then record the result
before moving on. Checkboxes mean implemented and verified, not merely scaffolded.

## Planning

- [x] Inspect the workspace and installed Go toolchain.
- [x] Define the scope, architecture, and proposed API.
- [x] Break the resume targets into stages with completion checkpoints.
- [x] Separate core work from later automated tests and delivery tooling.

## 1. Foundation — complete

- [x] Verify the GitHub remote and establish the local Git repository.
- [x] Initialize `github.com/ahmedr1zwan/flagctl` as a Go module.
- [x] Add `.gitignore` and a README that accurately states current progress.
- [x] Define flag types, identity rules, and the v1 request/response contract.
- [x] Add `cmd/flagd` with configurable listen address and `GET /healthz`.
- [x] Build and manually verify the health endpoint; record the commands/results.
- [x] Enforce loopback-only listening and verify common credential files are ignored.
- [x] Use a supported, patched Go toolchain and run a known-vulnerability scan.

## 2. Service — creation/reads complete; updates/deletion next

First, persistent creation and reads:

- [x] Add SQLite schema setup and unique environment/key storage.
- [x] Use private database-file permissions and keep sensitive inputs out of logs.
- [x] Implement create, list, and get routes.
- [x] Validate identifiers and payloads; return consistent JSON errors.
- [x] Review browser-origin/Host handling and enforce JSON for mutation requests.
- [x] Verify creation, reads, duplicate rejection, and environment isolation.
- [x] Restart the service and verify stored flags remain available.
- [x] Add creation-body limits; reject nulls, duplicate fields, and unknown fields.
- [x] Update the README with creation/read and restart-persistence examples.

Then, updates and deletion:

- [ ] Implement PATCH with explicit boolean state and optional description.
- [ ] Implement deletion and missing-record behavior.
- [x] Add server timeouts, header limits, and graceful shutdown (done in step 1).
- [ ] Apply the existing JSON body limit and validation rules to PATCH.
- [ ] Verify enable, disable, description clearing, and deletion using curl.
- [ ] Extend the README with verified update/delete examples.

## 3. CLI — two increments

- [ ] Add the shared HTTP client with context, timeout, and API error handling.
- [ ] Add Cobra root command, server configuration, and required environment option.
- [ ] Implement `flags create` and `flags list` against the real service.
- [ ] Implement `flags get`, `flags toggle --enabled=...`, and `flags delete`.
- [ ] Add table and JSON output, help, stderr errors, and failure exit codes.
- [ ] Verify the lifecycle across dev/prod, parseable JSON, and connection errors.
- [ ] Add verified CLI examples to the README.

## 4. Terraform provider

- [ ] Scaffold the Plugin Framework entry point and endpoint configuration.
- [ ] Define the `flagctl_flag` resource schema and stable environment/key ID.
- [ ] Implement create/read/update/delete through the shared client.
- [ ] Add import and replacement for changes to environment/key.
- [ ] Handle refresh after external changes and external deletion.
- [ ] Document local provider installation and add a runnable Terraform example.
- [ ] Verify apply, no-op plan, update, import, CLI-induced drift, and destroy.

## 5. Tests and compatibility — after core features

- [ ] Add unit tests for validation, HTTP handlers, client errors, and CLI behavior.
- [ ] Add SQLite integration tests with temporary databases and restart coverage.
- [ ] Add isolated provider acceptance tests for lifecycle, import, drift, and cleanup.
- [ ] Document and exercise separate unit/integration and acceptance test commands.
- [ ] Run appropriate race checks and resolve failures.
- [ ] Document the v1 compatibility policy.
- [ ] Add contract fixtures and verify an older client after an additive API change.

## 6. Packaging, automation, and release

- [ ] Implement and verify authentication, credential handling, and transport
  protection before enabling any non-loopback/container-interface listener.
- [ ] Add a multi-stage Dockerfile and Compose with persistent storage.
- [ ] Verify the container quickstart and persistence after restart.
- [ ] Add GitHub Actions formatting, vet, build, and unit/integration checks.
- [ ] Add an isolated acceptance-test CI job and confirm hosted runs pass.
- [ ] Configure GoReleaser and validate snapshot artifacts.
- [ ] Publish a tagged GitHub release with binaries and checksums.
- [ ] Verify downloaded release binaries and document installation.
- [ ] Finish the README architecture, examples, test commands, and CI badge.
- [ ] Run the quickstart from a clean checkout.
- [ ] Audit both resume bullets against the completed evidence in PLAN.md.

## Verification log

- 2026-09-12: Workspace inspected: empty; no local Git metadata. Installed Go:
  `go1.25.5 darwin/arm64`. Planning documents created. No application code or
  application tests exist yet. Remote contents have not been verified.
- 2026-09-12, step 1: `git ls-remote` succeeded with no refs; initialized `main`
  and added the specified GitHub origin. No files committed or pushed.
- 2026-09-12, step 1: Downloaded verified `go1.27.1 darwin/arm64` using Go's
  toolchain mechanism into ignored `.cache/go-mod`; the system installation was
  not changed. Builds and vet used project-local `GOCACHE`/`GOMODCACHE` paths to
  respect the workspace sandbox.
- 2026-09-12, step 1: `go build -o bin/flagd ./cmd/flagd`, `go vet ./...`, and
  `gofmt -l cmd internal` passed. No third-party application dependencies.
- 2026-09-12, step 1: One-off smoke checks ran the real binary on the default
  IPv4 port, an assigned IPv4 port, and IPv6 loopback. Verified GET JSON, HEAD,
  JSON 404/405 responses, Allow and security headers, no exposed `.env`/debug
  routes, occupied-port failure, invalid/non-loopback address rejection, oversized
  header rejection, incomplete-header timeout, and clean SIGINT/SIGTERM shutdown.
  All test server processes were stopped. Dedicated Go test suites remain deferred.
- 2026-09-12, step 1: Synthetic credential markers in request headers, body,
  path, and query did not appear in responses or application logs. `git check-ignore`
  confirmed common credential, Terraform state/plan, database, build, and cache
  paths are ignored. No real credentials were used for these checks.
- 2026-09-12, step 1: `go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...`
  reported `No vulnerabilities found.` This checks known vulnerabilities for this
  build configuration; it is not a guarantee against all security defects.
- 2026-09-12, step 2a: Pinned `modernc.org/sqlite` v1.58.0 and recorded checksums
  in go.sum. `CGO_ENABLED=0 go build -o bin/flagd ./cmd/flagd`, `go vet ./...`,
  `go mod verify`, and formatting checks passed. The vulnerability scan using
  `govulncheck@v1.8.0` reported `No vulnerabilities found.`
- 2026-09-12, step 2a: One-off HTTP checks against isolated real databases passed:
  create/list/get/HEAD, default values, UTC timestamps, stable ordering, dev/prod
  isolation, duplicate rejection without overwriting, and missing-record errors.
  Restarting on IPv6 preserved both environments' records and timestamps.
- 2026-09-12, step 2a: Invalid names, unknown/duplicate/case-mismatched JSON fields,
  nulls, wrong types, trailing JSON, invalid UTF-8, unsupported media types, and
  oversized bodies were rejected. Checked 63-character identifier and 1,024-byte
  description boundaries, exactly 16 KiB bodies, and oversized chunked bodies.
- 2026-09-12, step 2a: Unrelated Host names and cross-origin mutations were rejected;
  native and same-origin requests succeeded. Synthetic credential markers did not
  appear in logs/error responses. An SQL-like description round-tripped as data.
- 2026-09-12, step 2a: Eight concurrent identical creates produced one 201 and seven
  409 responses; twelve concurrent distinct creates succeeded. SQLite integrity
  and schema-version checks passed. Verified 0700 directories/0600 database files,
  rejection of unsafe permissions, directory/database/sidecar symlinks, and newer
  schema versions. Temporary test databases were removed and test servers stopped;
  user data was not used. Dedicated Go test suites remain deferred.
