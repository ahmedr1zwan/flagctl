# Resume evidence

The two original resume bullets are supported by the completed implementation
and verification below. These claims describe the project's demonstrated scope;
they do not imply production deployment, a Terraform Registry listing, or a
formal security audit.

## Service and CLI

> Built a Go feature-flag service exposing a versioned, backward-compatible REST
> API, and a Cobra-based CLI for creating, listing, and toggling flags across
> environments with JSON/table output.

| Claim | Evidence |
| --- | --- |
| Go feature-flag service | `cmd/flagd`, `internal/api`, and `internal/store`; SQLite-backed CRUD, validation, and restart persistence. |
| Versioned REST API | [v1 API contract](api-v1.md), including routes, request/response shapes, errors, and compatibility policy. |
| Backward compatibility | Frozen pre-change fixtures and an archived client from commit `afd343d` run against the service after the additive response `id` field. Breaking-change negative controls verify the checks fail when the contract changes. |
| Cobra CLI | `internal/cli` provides create/list/get/toggle/delete; toggle takes an explicit boolean state. |
| Environments and output | CLI integration and release tests verify independent `dev`/`prod` flags; command tests cover JSON/table output and errors. |

The compatibility claim is backed by a specific preserved v1 contract and an
older-client scenario. It is not a claim that arbitrary future changes will be
compatible. The [test guide](testing.md) explains the frozen fixtures and commands.

## Terraform provider and delivery

> Developed a Terraform provider with the Terraform Plugin Framework to manage
> flags declaratively as infrastructure as code, with unit and acceptance tests,
> GitHub Actions CI, and GoReleaser-published binaries.

| Claim | Evidence |
| --- | --- |
| Terraform Plugin Framework | `internal/provider` and `cmd/terraform-provider-flagctl`, with one `flagctl_flag` resource and shared HTTP client. |
| Declarative management | Real Terraform apply/update/import/drift/replacement/destroy scenarios; [runnable example](../examples/terraform/main.tf) and [provider guide](terraform.md). |
| Unit and acceptance tests | Normal Go suites plus explicit `TF_ACC=1` acceptance tests against isolated service instances, including authenticated TLS; race checks are part of CI. |
| GitHub Actions CI | Go quality/tests, Terraform acceptance, Docker lifecycle, and vulnerability jobs. The [release run](https://github.com/ahmedr1zwan/flagctl/actions/runs/35178030769) records the full suite, four-platform verification, and publication. |
| GoReleaser-published binaries | [v0.1.1 GitHub release](https://github.com/ahmedr1zwan/flagctl/releases/tag/v0.1.1), produced by pinned GoReleaser 2.18.1 with SHA-256 checksums. Four native Linux/macOS Intel/ARM jobs verify downloaded service, CLI, and provider binaries before publication. |
| Docker in the project technology list | Multi-stage image, non-root Compose runtime, authenticated TLS, persistent volume, and hosted container lifecycle tests; [Docker guide](docker.md). |

The provider binary is installed through a local filesystem mirror. Terraform
Registry publication is a separate optional follow-up and is not claimed by
these bullets. Windows and Apple Developer ID signing/notarization are also
outside this release. See [release installation](releases.md) for exact platforms,
commands, and checksum scope.

The initial `v0.1.0` tag was blocked by newly reported gRPC vulnerabilities and
has no published release. The dependency was patched before publishing `v0.1.1`.
