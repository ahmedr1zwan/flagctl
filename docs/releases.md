# Install a release

[v0.1.1](https://github.com/ahmedr1zwan/flagctl/releases/tag/v0.1.1) is the first
published release. It bundles `flagd`, `flagctl`, and a versioned Terraform provider.
Choose the archive matching your operating system and CPU:

| Platform | Archive suffix |
| --- | --- |
| Linux Intel/AMD 64-bit | `linux_amd64` |
| Linux ARM 64-bit | `linux_arm64` |
| macOS Intel | `darwin_amd64` |
| macOS Apple Silicon | `darwin_arm64` |

No Go compiler, C compiler, or database installation is needed. Terraform is
needed only for the provider; verification uses Terraform 1.16.1. Windows is not
included because private credential-file validation needs Windows ACL support.
macOS executables are not Apple Developer ID signed or notarized.

## Download and verify

From a new working directory, select your platform below. These commands use
curl, tar, and `shasum` (available on macOS and Ubuntu):

```sh
mkdir flagctl-0.1.1
cd flagctl-0.1.1
version=0.1.1
target=darwin_arm64 # change using the platform table above
archive="flagctl_${version}_${target}.tar.gz"
base="https://github.com/ahmedr1zwan/flagctl/releases/download/v${version}"
curl --fail --location --output "$archive" "$base/$archive"
curl --fail --location --output checksums.txt "$base/checksums.txt"
awk -v file="$archive" '$2 == file { print }' checksums.txt | shasum -a 256 -c -
```

Continue only when the checksum reports `OK`. The checksums detect damaged or
mismatched downloads; they are distributed with the release, not separately
signed. Extract and inspect the embedded versions:

```sh
tar -xzf "$archive"
./flagd --version
./flagctl --version
./terraform-provider-flagctl_v0.1.1 --version
```

All three should report `0.1.1`. Keep the executables together in this directory,
or copy `flagd` and `flagctl` to a directory already on your `PATH`.

## Run the service and CLI

Start the service in the extracted directory:

```sh
./flagd --data-dir data
```

In another terminal in that same directory:

```sh
curl --noproxy '*' --fail http://127.0.0.1:8080/healthz
./flagctl flags create checkout_v2 --env dev --description "New checkout"
./flagctl flags toggle checkout_v2 --env dev --enabled=true
./flagctl flags list --env dev
./flagctl flags get checkout_v2 --env dev --output json
```

The service listens on loopback and creates a private SQLite database. Stop it
with Ctrl+C and start it again with the same data directory: `checkout_v2` stays
enabled. Delete the example when finished:

```sh
./flagctl flags delete checkout_v2 --env dev
```

For another port, start with `--listen 127.0.0.1:8081` and set
`FLAGCTL_SERVER=http://127.0.0.1:8081` in the client terminal. Follow
[secure access](security.md) for verified HTTPS and token files.

## Install the bundled Terraform provider

The provider is distributed through GitHub. It is not on the Terraform Registry.
A filesystem mirror lets `terraform init` select the versioned binary and record
its checksum normally. This example uses Python 3, Terraform, and the extracted
release directory. Run it there, on the machine matching your downloaded archive:

```sh
python3 - <<'PY'
import json
import os
from pathlib import Path
import platform
import shutil

os.umask(0o077)
root = Path.cwd()
version = "0.1.1"
arch = {"x86_64": "amd64", "arm64": "arm64", "aarch64": "arm64"}[platform.machine()]
target = platform.system().lower() + "_" + arch
mirror = root / "provider-mirror"
install = mirror / "registry.terraform.io/ahmedr1zwan/flagctl" / version / target
install.mkdir(parents=True, exist_ok=True)
name = "terraform-provider-flagctl_v" + version
shutil.copy2(root / name, install / name)
(root / "terraform-release.tfrc").write_text(
    'disable_checkpoint = true\n'
    'provider_installation {\n'
    '  filesystem_mirror {\n'
    '    path = ' + json.dumps(str(mirror)) + '\n'
    '    include = ["registry.terraform.io/ahmedr1zwan/flagctl"]\n'
    '  }\n'
    '}\n'
)
PY
export TF_CLI_CONFIG_FILE="$PWD/terraform-release.tfrc"
export FLAGCTL_SERVER=http://127.0.0.1:8080
umask 077
terraform -chdir=examples/terraform init
terraform -chdir=examples/terraform apply
terraform -chdir=examples/terraform plan
```

Keep the service running. Review the plan and confirm apply. The next plan should
show no changes. The example creates `dev/terraform_checkout`. This configuration
is scoped to the current shell and only installs this provider from the mirror;
it does not modify your global Terraform settings or contact the Registry.
Terraform may warn that the mirror provides checksums for only your current
platform. Teams using multiple platforms should populate their mirrors and lock
files for each target. Pin `version = "0.1.1"` in `required_providers.flagctl` when
adding other versions to the mirror.

See the [Terraform guide](terraform.md) for updates, import, and CLI drift.
Clean up the example while the service is still running:

```sh
terraform -chdir=examples/terraform destroy
unset TF_CLI_CONFIG_FILE FLAGCTL_SERVER
```

## Build and verify a snapshot

From a source checkout with Go 1.27.1, Git, Python 3, Terraform 1.16.1, and
[GoReleaser](https://goreleaser.com/install/) **2.18.1**:

```sh
goreleaser check
goreleaser release --snapshot --clean
version="0.0.0-dev.$(git rev-parse --short HEAD)"
python3 scripts/verify-release.py dist "$version"
```

Snapshots build all twelve binaries and four archives without uploading anything.
`--clean` replaces the generated `dist/` directory. The verifier checks the native
archive checksum and all three embedded versions, then exercises CLI operations,
environment isolation, persistence across restart, and the released provider's
mirror installation, apply, no-op plan, update, drift reconciliation, import,
and destroy. It creates and removes only its own temporary files and service.
Source builds without release flags report `dev`.

## Publish a version

The [release workflow](../.github/workflows/release.yml) handles stable semantic
version tags (`vMAJOR.MINOR.PATCH`) whose commits belong to `main`:

1. Update the installation version and release documentation, run the snapshot
   verifier, and commit/push the changes to `main`.
2. Create an annotated tag at the intended commit and push that tag, for example
   `git tag -a v0.1.1 -m 'Release v0.1.1'` then `git push origin v0.1.1` for this
   release. Use a new unused version for later releases; never move a published tag.
3. The workflow runs all four CI jobs against the tag, including Terraform, Docker,
   and vulnerability checks. GoReleaser 2.18.1 then uploads a **draft** with four
   archives and `checksums.txt`.
4. Separate native Linux/macOS Intel/ARM runners download the draft assets and run
   the same verifier. Only after all four pass does the workflow publish the draft.
5. Download the public release and rerun the verifier to check the published path.

CI uses read-only tokens. Draft upload, draft download, and final publish
jobs receive `contents: write`: GitHub exposes draft releases only to callers
with push access. Checkout never persists these credentials. No personal token, signing key, external storage,
or Terraform Registry credentials are required. Actions are pinned to commits.

A failed artifact check leaves the release as a draft. Inspect its logs, fix the
problem, and validate again before publishing. Rerun only failed verification
jobs for transient failures; the workflow does not overwrite existing assets or
move tags. If the workflow itself needs correction, push the fix to `main` and
resume an existing draft with:

```sh
gh workflow run release.yml --ref main -f release_tag=v0.1.1
```

This reruns all CI checks against the original tag, skips rebuilding/uploading,
then verifies all four existing draft archives and publishes only after success.
It rejects published releases and tags outside `main`. Changes to application
source require a new commit and version.

The `v0.1.0` tag did not produce a release: its vulnerability gate detected
new gRPC advisories. The fixed build uses `v0.1.1`; the earlier tag is retained
without published assets.

Release configuration follows GoReleaser's [archive](https://goreleaser.com/customization/package/archives/)
and [draft release](https://goreleaser.com/customization/publish/scm/) documentation.

The [successful v0.1.1 verification/publication run](https://github.com/ahmedr1zwan/flagctl/actions/runs/35178030769)
used the recovery path after correcting draft-download permissions. All four
native targets passed the full artifact verifier before publication. The public
macOS ARM download and installation commands above passed afterward. The tag's
bundled documentation predates the workflow-permission correction; use this
current guide for release-maintainer recovery instructions.
