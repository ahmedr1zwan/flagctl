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

## 2. Service — complete

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

- [x] Implement PATCH with explicit boolean state and optional description.
- [x] Implement deletion and missing-record behavior.
- [x] Add server timeouts, header limits, and graceful shutdown (done in step 1).
- [x] Apply the existing JSON body limit and validation rules to PATCH.
- [x] Verify enable, disable, description clearing, and deletion through HTTP.
- [x] Extend the README with verified update/delete examples.

## 3. CLI — complete

- [x] Add the shared HTTP client with context, timeout, and API error handling.
- [x] Add Cobra root command, server configuration, and required environment option.
- [x] Implement `flags create` and `flags list` against the real service.
- [x] Implement `flags get`, `flags toggle --enabled=...`, and `flags delete`.
- [x] Add table and JSON output, help, stderr errors, and failure exit codes.
- [x] Verify create/list across dev/prod, output parsing, and connection errors.
- [x] Verify the lifecycle across dev/prod, parseable JSON, and connection errors.
- [x] Add verified create/list CLI examples to the README.
- [x] Extend the README with get/toggle/delete CLI examples.

## 4. Terraform provider — complete (local build)

- [x] Scaffold the Plugin Framework entry point and endpoint configuration.
- [x] Define the `flagctl_flag` resource schema and stable environment/key ID.
- [x] Implement create/read/update/delete through the shared client.
- [x] Add import and replacement for changes to environment/key.
- [x] Handle refresh after external changes and external deletion.
- [x] Document local provider installation and add a runnable Terraform example.
- [x] Verify apply, no-op plan, update, import, CLI-induced drift, and destroy.

## 5. Tests and compatibility — complete

- [x] Add validation unit tests and HTTP lifecycle/failure tests.
- [x] Add SQLite integration tests with temporary databases and close/reopen persistence.
- [x] Cover concurrent writes, invalid/canceled operations, and private-file protections.
- [x] Add tests for HTTP client errors and command behavior.
- [x] Verify CLI process output, exit codes, and interruption; service shutdown/restart.
- [x] Add provider unit tests for configuration, schema, import, and state handling.
- [x] Add isolated provider acceptance tests for lifecycle, import, drift, and cleanup.
- [x] Document and exercise the current unit/integration test commands.
- [x] Document and exercise separate provider acceptance-test commands.
- [x] Run service tests with race detection and shuffled order; resolve failures.
- [x] Extend race checks to the client and command suites.
- [x] Extend race checks to provider suites.
- [x] Document the v1 compatibility policy.
- [x] Add contract fixtures and verify an older client after an additive API change.

## 6. Packaging, automation, and release

- [x] Implement and verify authentication, credential handling, and transport
  protection before enabling any non-loopback/container-interface listener.
- [x] Add a multi-stage Dockerfile and Compose with persistent storage.
- [x] Verify the container quickstart and persistence after restart.
- [x] Add GitHub Actions formatting, vet, build, and unit/integration checks.
- [x] Add an isolated acceptance-test CI job and confirm hosted runs pass.
- [x] Verify the Docker lifecycle and vulnerability scans in separate CI jobs.
- [x] Add the README CI badge after a successful hosted run.
- [x] Configure GoReleaser and validate snapshot artifacts.
- [x] Publish a tagged GitHub release with binaries and checksums.
- [x] Verify downloaded release binaries and document installation.
- [x] Finish release installation and review README architecture, examples, and test commands.
- [x] Run the quickstart from a clean checkout.
- [x] Audit both resume bullets against the completed evidence in PLAN.md.

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
- 2026-09-12, step 2b: Creation/read regression checks and the update/delete HTTP
  checks passed against isolated real databases. Verified explicit true/false,
  omitted fields, empty descriptions, combined updates, immutable identity and
  timestamps, no-op timestamp preservation, missing records, 204 deletion, repeated
  deletion, environment isolation, and persistence after separate restarts.
- 2026-09-12, step 2b: Twelve rounds of concurrent description/enabled updates
  preserved both fields. Concurrent update/delete requests did not recreate deleted
  records, and duplicate deletes produced one 204 and one 404. Invalid updates,
  oversized/chunked bodies, nulls, duplicate fields, and cross-origin/Host failures
  left records unchanged. Synthetic credentials did not appear in logs/errors.
- 2026-09-12, step 2b: Build with CGO disabled, vet, module verification, formatting,
  and SQLite integrity checks passed. `govulncheck@v1.8.0` reported no known
  vulnerabilities. All verification servers were stopped and temporary databases
  removed. No user data was modified by verification.
- 2026-09-12, GitHub publication: Existing keyring-backed GitHub authentication
  provided repository write access; no new keys were needed. Reviewed the staged
  file list and checked common credential patterns before committing. Published
  baseline commit `9de54c0` with the verified create/read service. New increments
  are committed separately, with completed and planned features distinguished in
  the README.
- 2026-09-13, step 3a: Pinned Cobra v1.10.2 and added the shared HTTP client plus
  create/list CLI commands. CGO-disabled CLI build, vet, formatting, and module
  verification passed. `govulncheck@v1.8.0` reported no known vulnerabilities.
- 2026-09-13, step 3a: Real-service checks passed for create/list, default and
  explicit boolean values, dev/prod isolation, duplicates, table and JSON output,
  empty lists, concurrent clients, and persistence visible after a service restart.
  Verified required environments, server configuration precedence, help, and
  failure exit codes. Fixed Cobra's group-command fallback so unimplemented flag
  commands fail rather than printing help and returning success.
- 2026-09-13, step 3a: Local fake-server checks passed for connection errors,
  request timeout/cancellation, redirect refusal, malformed/incomplete/oversized
  responses, unexpected HTTP status codes, and tolerance of additive response
  fields. Synthetic sensitive response messages and endpoint credentials were
  not echoed; proxy settings were bypassed and table control characters escaped.
  All verification servers were stopped and isolated databases removed. Dedicated
  Go unit and acceptance suites remain deferred.
- 2026-09-13, step 3b: Re-ran the existing create/list checks before extending
  the CLI, then ran them again after the shared client changes. Added get,
  explicit-state toggle, and delete; complete real-service lifecycle checks
  passed with table/JSON output, required arguments/environment/state, no-op
  timestamps, description preservation, dev/prod isolation, and missing records.
- 2026-09-13, step 3b: Verified updates persist across restart and deletions remain
  absent after another restart. Invalid CLI inputs made no HTTP requests. A fake
  local server verified exact methods and paths, false PATCH values with omitted
  descriptions, bodyless 204 handling, rejection of incorrect identities/states
  and malformed responses, additive fields, sanitized errors, redirect refusal,
  and timeout/cancellation for each new command. No command-level retries occurred.
- 2026-09-13, step 3b: Direct shared-client checks passed for partial and combined
  updates, explicit empty descriptions and false values, unchanged omitted
  fields, no-op timestamps, input validation, and typed API 404 errors. CGO-disabled
  builds, vet, module verification, formatting, and whitespace checks passed.
  `govulncheck@v1.8.0` reported no known vulnerabilities. These are one-off
  verification checks; the committed Go unit/acceptance suites are still planned
  for step 5. Test databases were removed and local verification servers stopped.
  No user data or real credentials were used in the checks.
- 2026-09-13, step 4: Added Plugin Framework v1.19.0, the provider executable,
  endpoint/timeout configuration, and the `flagctl_flag` resource. Verified with
  Terraform 1.16.1 on macOS arm64 through a local development override, using
  temporary Terraform workspaces and real isolated flagd databases. No Registry
  publication, cloud accounts, credentials, or global Terraform configuration
  changes were required.
- 2026-09-13, step 4: Schema/validate, default values, parallel dev/prod creation,
  apply, no-op plans, explicit true/false, description clearing, timestamp
  preservation, CLI drift reconciliation, import, and destroy passed. Verified
  external deletion plans recreation, key/environment changes require replacement,
  and deletion between a saved destroy plan and apply succeeds. Duplicate creates
  preserved unmanaged flags; invalid/missing imports failed without adopting them.
- 2026-09-13, step 4: Configuration and identifier validation, UTF-8 byte limits,
  unknown provider values, server/environment precedence, timeouts, and connection
  failures passed. Refresh errors retained state. Synthetic credential-bearing
  environment values and remote error details were absent from diagnostics. The
  published example configuration was copied into an isolated workspace and
  verified through validate, apply, an empty second plan, and destroy.
- 2026-09-13, step 4: The initial dependency scan identified vulnerable framework
  transitive dependencies. Pinned patched gRPC v1.82.1, x/net v0.56.0, and x/text
  v0.39.0; the final `govulncheck@v1.8.0` run reported `No vulnerabilities found.`
  Rebuilt the provider and reran its lifecycle checks after dependency updates.
  All three CGO-disabled builds, vet, module verification, Go/Terraform formatting,
  and whitespace checks passed. Existing CLI create/list and full-lifecycle
  regression checks passed as well. All verification servers stopped and
  temporary state/databases were removed. These remain one-off checks; committed
  automated Go and provider acceptance suites are the next increment.
- 2026-09-13, step 5a: Added committed Go tests for validation, SQLite, and the
  REST API using only existing dependencies and standard testing tools. Checks
  cover byte/name boundaries, defaults, explicit false/empty values, CRUD,
  ordering, environment isolation, no-op timestamps, persistence after reopening,
  canceled/invalid operations, and concurrent create/update/delete behavior.
- 2026-09-13, step 5a: Added private-file, symlink/sidecar, URI-path, and newer-schema
  checks, plus real HTTP tests for HEAD/204, strict JSON, media types, known-length
  and chunked body limits, Host/origin protections, error codes, safe logs, and
  bounded contexts. Corrected a test fixture to create a private child directory:
  Go's temporary test directory itself was correctly rejected by store.Open.
- 2026-09-13, step 5a: `CGO_ENABLED=0 go test ./...` and `go vet ./...` passed.
  The service suite also passed with `CGO_ENABLED=1 go test -race -shuffle=on
  -count=1 -coverprofile=.cache/service-coverage.out ./internal/flags
  ./internal/store ./internal/api`. Statement coverage: flags 100.0%, store 87.2%,
  API 98.7%. No races were reported. Formatting/whitespace checks passed; test
  databases and HTTP servers were cleaned up. Client/CLI, provider acceptance,
  and explicit compatibility suites remain unfinished and are not included in
  these coverage figures.
- 2026-09-13, step 5b: Added automated shared-client tests for the real-service
  lifecycle, exact HTTP methods/paths/payloads, explicit false/empty updates,
  invalid inputs making no requests, and typed/sanitized API errors. Response
  tests cover missing/invalid fields, wrong identities/update values, malformed
  or oversized JSON, body/header limits, and tolerance of additive fields.
- 2026-09-13, step 5b: Client transport checks verify redirect refusal, no
  application-level retries after HTTP failures, proxy bypass, rejection of an
  untrusted TLS certificate, and timeout/cancellation before headers and during
  body reads. Synthetic sensitive values stay out of returned errors.
- 2026-09-13, step 5b: Added real-service CLI lifecycle checks for dev/prod,
  JSON/table output, no-op toggles, duplicates/missing flags, terminal-control
  escaping, help, configuration precedence, and invalid commands. Output-write
  failures acknowledge completed mutations; subprocess tests verify actual
  stdout/stderr, exit codes, and SIGINT cancellation. Service entry-point checks
  verify loopback restrictions, startup failures, health, graceful shutdown,
  listener closure, and persistence after restart.
- 2026-09-13, step 5b: `CGO_ENABLED=0 go test ./...`, `go vet ./...`, formatting,
  and whitespace checks passed. The full suite passed with `CGO_ENABLED=1 go
  test -race -shuffle=on -count=1 -coverprofile=.cache/application-coverage.out
  ./...`; no races were reported. Coverage: client 98.5%, CLI 95.8%, flagctl
  entry point 100.0%, flagd entry point 82.1%; existing flags/store/API coverage
  remains 100.0%/87.2%/98.7%. Provider packages still have no automated tests.
  Fixed the test harness to collect child-process coverage in the parent report
  and prevent Go coverage warnings from contaminating CLI stderr assertions.
  Tests use isolated databases/servers and controlled subprocess environments;
  no real credentials or user data were used. Production code and dependencies
  are unchanged. Provider unit/acceptance and API compatibility tests are next.
- 2026-09-14, step 5c: Added provider unit tests for protocol schema validation,
  default/explicit/environment configuration, unknown values, timeout application,
  schema validators, import IDs, and unconfigured resource errors. Failed API
  operations preserve prior state; failed creation does not adopt a resource.
  Only the expected typed not-found error removes state or permits missing-flag
  deletion. Synthetic sensitive values stay out of provider diagnostics.
- 2026-09-14, step 5c: Added terraform-plugin-testing v1.16.0 acceptance tests
  using protocol 6 and the installed Terraform 1.16.1 on macOS arm64. Every case
  starts an isolated SQLite-backed HTTP service, uses temporary Terraform
  configuration/state, and ignores developer Terraform environment overrides.
  No Registry publication, prebuilt provider, running service, or keys are needed.
- 2026-09-14, step 5c: Acceptance checks passed for defaults, dev/prod isolation,
  no-op plans, updates, false/empty values, timestamps, import/state equivalence,
  external drift, recreation after external deletion, key/environment replacement,
  invalid inputs/imports, and deletion between plan and apply. State checks compare
  Terraform attributes to fresh API reads. Destroy checks confirm managed flags
  are absent before database cleanup; duplicate creation leaves unmanaged flags
  untouched through Terraform's error cleanup.
- 2026-09-14, step 5c: CGO-disabled builds/tests, vet, module verification, formatting,
  and whitespace checks passed. The full normal suite and the provider suite
  with `TF_ACC=1` passed with race detection, shuffled order, and fresh coverage.
  Provider statement coverage is 85.5% for unit tests and 98.4% including
  acceptance tests. `govulncheck@v1.8.0 -test ./...` reported no vulnerabilities
  after resolving the testing dependency graph. Production source is unchanged;
  dependency versions/checksums are pinned. API compatibility evidence is next.
- 2026-09-14, step 5d: Added 31 ordered v1 wire fixtures from the contract at
  f57c5a4 and a standalone historical-client fixture copied byte-for-byte from
  afd343d. SHA-256 checks protect the archived client/domain source. The driver
  uses those sources in a separate module, with workspace discovery, saved Go
  configuration, automatic downloads, proxy access, and CGO disabled for its
  build. It verifies lifecycle, typed errors, defaults, ordering, dev/prod
  isolation, partial updates, and timestamp preservation against real storage.
- 2026-09-14, step 5d: Committed and verified the baseline before adding the
  read-only API response `id` (`environment/key`). The same frozen fixtures and
  historical client passed after the addition. Separate tests verify IDs in
  create/get/update/list responses and reject ID inputs without changing flags.
  Negative controls confirm the contract matcher rejects missing fields,
  changed types/defaults/error codes, null/truncated/reordered lists, and invalid
  timestamps, while allowing additive fields and human-readable message edits.
- 2026-09-14, step 5d: CGO-disabled builds/tests and vet passed. The full normal
  suite and the Terraform provider acceptance suite passed with race detection,
  shuffled order, and fresh coverage. API coverage is 98.8%; provider coverage
  remains 98.4% including acceptance. The current CLI and Terraform provider
  continue working after the API change. The compatibility policy and test guide
  now describe the evidence and frozen-baseline maintenance rules. Docker,
  secure container access, hosted CI, and published releases remain unfinished.
- 2026-09-14, step 5d: `govulncheck@v1.8.0 -test ./...` on the application and
  a separate scan of the historical-client module both reported no vulnerabilities.
  Module verification, Go formatting, and whitespace checks passed. No application
  dependencies changed and no real credentials were used for verification.

- 2026-09-14, step 6a: Added TLS 1.3, private token/key file loading, sanitized
  connection errors, and explicit network listener/public-origin configuration.
  Host/origin checks remain active; flag routes require a bearer token in secure
  mode, while TLS health GET/HEAD remains anonymous. File loading rejects unsafe
  permissions, symlinks, non-regular/oversized files, and invalid credentials.
- 2026-09-14, step 6a: Connected token/CA file paths to the shared client, Cobra
  options/environment fallbacks, and Terraform configuration. Remote access
  requires HTTPS and a token; HTTP cannot carry credentials. Verified malformed,
  missing, duplicate, query/cookie, and incorrect authentication; trust/hostname,
  expiry, and TLS-version failures; no redirects/proxy credential forwarding;
  and errors/help without secret contents.
- 2026-09-14, step 6a verification: Normal CGO-disabled tests, vet, module
  verification, all-package race/shuffle tests, and real Terraform acceptance
  tests passed. Provider acceptance coverage is 98.5%; the new security package
  is 90.3%. The frozen v1 contract and historical client remain unchanged and
  pass. The dependency/test vulnerability scan reported no known vulnerabilities.
  All three binaries build on macOS arm64 and cross-compile for Linux/Windows
  amd64; cross-compiled binaries were not executed. Secret-file loading remains
  disabled on Windows until ACL-aware validation exists.
- 2026-09-14, step 6a manual check: Ran the OpenSSL certificate/token setup against
  built binaries on an assigned loopback port. Verified curl TLS health, CLI
  create/toggle/list/delete, missing-token 401, rejection without CA trust, and
  clean shutdown. Captured output/logs contained no token. Ephemeral credentials
  and database were removed afterward. A TLS test initially left a speculative
  connection open; draining responses and closing its clients resolved the test
  cleanup issue, confirmed by 20 repeated race runs and the full suite.
  Docker, hosted CI, and published releases remain unfinished.

- 2026-09-15, step 6b: Added a digest-pinned multi-stage Dockerfile with a static
  non-root scratch runtime and a build-context allowlist. A synthetic token in a
  source directory exposed an initial ignore-rule mistake; re-excluding directory
  contents fixed it, and the exported context now contains only production Go
  source plus module files. Runtime filesystem inspection found no shell,
  compiler/source, database, token, or private-key files in the image.
- 2026-09-15, step 6b: Added Compose with host-loopback HTTPS, read-only credential
  mounts, an owned private named volume, a read-only root filesystem, dropped
  capabilities, no-new-privileges, and resource/log limits. A new `flagd healthcheck`
  command verifies the certificate/public Host against the internal listener,
  including when the published port differs. It needs no token or private key.
- 2026-09-15, step 6b verification: Docker Desktop 4.90.0 / Engine 29.7.2 / Compose
  5.5.1 ran the opt-in integration suite on native Linux arm64 and emulated Linux
  amd64. Verified TLS health, anonymous 401, CLI toggling, dev/prod isolation,
  private database ownership/modes, restart and complete-container-replacement
  persistence, deletion, and clean shutdown. Tests removed only their isolated
  containers, networks, volumes, and image tags; reusable build caches remain.
- 2026-09-15, step 6b verification: The documented OpenSSL/Compose quickstart
  passed with curl and built CLI commands, manual health probing, authenticated
  operations, and persistence after recreation. Normal CGO-disabled tests, vet,
  all-package race/shuffle tests, and source/test plus actual container-binary
  vulnerability scans passed. `cmd/flagd` coverage is now 91.0%; frozen API/client
  compatibility checks remain green. Hosted CI and GoReleaser releases are next.

- 2026-09-15, step 6c: Added push/PR/manual GitHub Actions CI with four independent
  Ubuntu 24.04 amd64 jobs for Go quality/tests, real Terraform acceptance,
  Docker integration, and vulnerability scans. Third-party actions use verified
  full commit pins; checkout does not persist credentials, the workflow token is
  read-only, and tests need no personal keys or repository secrets. Timeouts and
  concurrency cancellation bound stale runs. Coverage is printed in job logs;
  credentials, Terraform state, and databases are not uploaded as artifacts.
- 2026-09-15, step 6c local verification: actionlint 1.7.12, tracked Go formatting,
  Terraform example formatting, module download/verification, vet, CGO-disabled
  builds/tests, full race/shuffle tests, real Terraform acceptance with race
  coverage, and the Linux amd64 Compose lifecycle all passed. Application/test
  and frozen-client govulncheck 1.8.0 scans found no known vulnerabilities.
  Frozen API/client fixtures and module dependencies are unchanged.
- 2026-09-15, step 6c hosted verification: Commit bdbc0a1 passed all four jobs in
  [run 34993245977](https://github.com/ahmedr1zwan/flagctl/actions/runs/34993245977).
  Logs confirm all eight acceptance cases ran, including authenticated TLS;
  provider acceptance coverage is 98.5%. Docker verified Linux amd64 runtime
  restrictions, credential exclusions, authentication, persistence through restart
  and replacement, private files, and cleanup. Ordinary and race suites passed,
  including the historical client; both vulnerability scans found no known
  vulnerabilities. Added the README badge after this success. Next is GoReleaser
  snapshot validation, followed by a tagged release and downloaded-binary checks.

- 2026-09-16, step 6d: Configured pinned GoReleaser 2.18.1 with twelve static
  service/CLI/provider builds bundled into four Linux/macOS Intel/ARM archives,
  versioned provider filenames, embedded `--version` output, and SHA-256 checksums.
  All archive checksums and expected contents passed local inspection. The macOS
  ARM snapshot passed CLI lifecycle, environment isolation, restart persistence,
  and deletion, plus Terraform mirror init, apply, no-op plan, update, CLI drift,
  reconciliation, import, and destroy using the actual packaged provider.
- 2026-09-16, step 6d: Added a tag workflow that reuses all CI checks, uploads a
  draft, verifies downloaded assets on four native runners, and publishes only
  after every target passes. Only upload/publish receive write permissions.
  Workflow lint, release config validation, Terraform formatting, Go vet,
  CGO-disabled tests, and shuffled race tests passed locally. Added release
  installation and mirror setup documentation. Tagged publication and hosted
  downloaded-artifact verification remain pending.

- 2026-09-16, step 6d: A fresh clone passed the README source builds, health,
  CLI create/toggle/list/get/delete, and the exact documented Terraform override
  setup, apply, no-op plan, and destroy. A corrupted archive was rejected by the
  release verifier before execution.
- 2026-09-16, step 6d release gate: The `v0.1.0` tag's hosted scan detected
  [GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443) and
  [GO-2026-6348](https://pkg.go.dev/vuln/GO-2026-6348) in gRPC v1.82.1. The pipeline
  blocked draft creation and publication. Updated to gRPC v1.83.2, which fixes
  both advisories, with its required transitive dependency updates. Installation
  instructions now target v0.1.1; the existing v0.1.0 tag will not be moved.
- 2026-09-16, step 6d dependency-fix verification: The updated source/test scan
  reports no vulnerabilities. Module verification, vet, all-package shuffled
  race tests with real Terraform acceptance enabled, and rebuilt GoReleaser
  snapshot checks pass. The fixed macOS ARM package passes the full CLI/provider
  lifecycle verifier. The original release run passed Go, Terraform, and Docker
  but correctly skipped publication after the vulnerability failure.

- 2026-09-16, step 6d hosted artifact check: All CI jobs passed for v0.1.1 and
  GoReleaser uploaded four archives plus checksums to a draft. The four download
  jobs failed before executing binaries because GitHub hides drafts from tokens
  without push access. Corrected their scoped permissions and added manual
  recovery of an existing draft: rerun CI on its original tag, skip rebuilding,
  then download/verify/publish. The application tag and uploaded bytes are unchanged.

- 2026-09-16, step 6d published verification: [Release run 35178030769](https://github.com/ahmedr1zwan/flagctl/actions/runs/35178030769)
  passed the complete CI suite and all four native downloaded-artifact jobs,
  then published [v0.1.1](https://github.com/ahmedr1zwan/flagctl/releases/tag/v0.1.1).
  Linux/macOS Intel/ARM each verified checksums and all three versions, CLI CRUD
  and environment isolation, restart persistence, Terraform mirror init, apply,
  no-op plans, update, drift reconciliation, import, and destroy. Both source/test
  and archived-client vulnerability scans reported no vulnerabilities.
- 2026-09-16, step 6d public-download verification: The exact documented curl,
  checksum, extraction, version, and filesystem-mirror setup commands passed
  in a fresh temporary directory. The public macOS ARM bundle passed service/CLI
  operations, Terraform init/apply/no-op/destroy, and the complete release smoke
  script. Provider build metadata confirms gRPC v1.83.2 and unmodified release
  commit 3831501. Original tags and uploaded artifact bytes were preserved.
- 2026-09-16, final documentation: Completed the installation/architecture/example/
  test-command review and the original two-bullet resume audit in
  docs/resume-evidence.md. All original plan checkpoints are complete. Registry
  publication, Windows support, and macOS signing/notarization remain optional
  follow-ups and are not claimed by the resume evidence.
