# Historical v1 client

`internal/client/client.go` and `internal/flags/flag.go` are byte-for-byte copies
from [afd343df4121e52e8096e05414b71e7373e7e69f](https://github.com/ahmedr1zwan/flagctl/commit/afd343df4121e52e8096e05414b71e7373e7e69f),
the commit that completed the CLI lifecycle. `snapshot.json` records their source
commit and SHA-256 digests. This is a development snapshot, not a published release.

`main.go` is the compatibility driver, added with these tests. It uses only the
archived client's public API to exercise defaults, create/list/get, explicit
updates, no-op timestamps, environment isolation, deletion, and typed errors.
The minimal `go.mod` retains the original module path and Go requirement but
needs no external modules: the archived client and domain code use only the
standard library. Neither source file imports the current application module.

`TestV1HistoricalClient` verifies the snapshot digests, compiles this module with
the test runner's Go toolchain, and runs it against a temporary current service.
The build disables workspace discovery, saved Go environment configuration,
automatic toolchain downloads, module proxy access, and CGO. The build and client
processes inherit only runtime/cache paths, not developer credentials. No Git
installation or repository history is needed when running the test.

Do not edit or regenerate the archived sources to make a service change pass.
Keep this baseline when adding later snapshots. A security issue in fixture
code should receive an explicit review and provenance update, not silent edits.
The driver can gain assertions without replacing the archived implementation.
