# Run flagd with Docker

The container runs the same Go service with TLS and bearer authentication.
Compose publishes HTTPS only on host loopback and stores SQLite in a named
volume. The CLI and Terraform provider can connect from the host as before.

Use a local Docker Engine 28 or newer with BuildKit and Docker Compose 2.20 or
newer (Docker Desktop includes these). The walkthrough also uses OpenSSL 3 and
Go 1.27.1 to build the host CLI. Run it as a non-root macOS/Linux user with Docker
access. Windows containers and remote Docker daemons are not supported by this
local credential-mount setup.

## Start the service

From the repository root, create fresh development credentials outside the
checkout. No third-party keys, cloud accounts, or Docker Hub login are required
for the public base image.

```sh
umask 077
export FLAGCTL_SECRETS_DIR="$(mktemp -d)"
openssl rand -hex 32 > "$FLAGCTL_SECRETS_DIR/api.token"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
  -noenc -days 7 -subj '/CN=localhost' \
  -addext 'subjectAltName=DNS:localhost,IP:127.0.0.1,IP:::1' \
  -keyout "$FLAGCTL_SECRETS_DIR/server.key" \
  -out "$FLAGCTL_SECRETS_DIR/server.crt"

export FLAGCTL_UID="$(id -u)"
export FLAGCTL_GID="$(id -g)"
export FLAGCTL_TOKEN_FILE="$FLAGCTL_SECRETS_DIR/api.token"
export FLAGCTL_TLS_CERT_FILE="$FLAGCTL_SECRETS_DIR/server.crt"
export FLAGCTL_TLS_KEY_FILE="$FLAGCTL_SECRETS_DIR/server.key"
export FLAGCTL_CA_FILE="$FLAGCTL_SECRETS_DIR/server.crt"
export FLAGCTL_PORT=8443
export FLAGCTL_SERVER="https://localhost:$FLAGCTL_PORT"

docker compose up --build --wait --wait-timeout 90
docker compose ps
```

Expect `flagd` to be healthy. The first build downloads the pinned Go builder and
module dependencies; later builds use Docker's caches. The 90-second wait starts
after the build. If the port is occupied, choose a free `FLAGCTL_PORT`, update
`FLAGCTL_SERVER`, and run Compose again. Keep these variables in the same shell
for subsequent Compose commands. Only file paths are in the environment; token
contents never belong in Compose YAML, Docker build arguments, or shell commands.

The temporary credential directory remains until you remove it or the operating
system cleans it up. Keep its path to reuse this demo, and use durable private
storage and managed certificates for a persistent deployment. The example
certificate expires in seven days. See [secure access](security.md) for rotation
and the token's full-access scope.

## Use the CLI and check persistence

```sh
go build -o bin/flagctl ./cmd/flagctl
curl --noproxy '*' --cacert "$FLAGCTL_CA_FILE" "$FLAGCTL_SERVER/healthz"
./bin/flagctl flags create checkout_v2 --env dev --description "Container demo"
./bin/flagctl flags toggle checkout_v2 --env dev --enabled=true
./bin/flagctl flags list --env dev --output json
```

The CLI reads the server, token-file, and CA-file variables above. Health returns
`{"status":"ok"}` and the list contains the enabled flag. To check authentication:

```sh
./bin/flagctl --token-file= flags list --env dev
```

Expect exit status 1 and HTTP 401. The same provider configuration/environment
works with Terraform; follow the [local provider guide](terraform.md).

Restart the process, then recreate the container entirely:

```sh
docker compose restart flagd
docker compose up --wait --wait-timeout 60
./bin/flagctl flags get checkout_v2 --env dev --output json

docker compose down
docker compose up --wait --wait-timeout 60
./bin/flagctl flags get checkout_v2 --env dev --output json
```

Both reads should return the same enabled flag and timestamps. `down` removes the
container and network, but keeps the project's `flag_data` volume. Keep the same
Compose project name to reuse it; renaming the project selects another volume.
Do not run multiple service instances against this SQLite volume.

Stop the demo when finished:

```sh
docker compose down
```

To deliberately delete this demo's stored flags, use `docker compose down --volumes`.
That removes the project's database volume. It does not remove host credential
files. Neither command prunes unrelated Docker resources.

## Files, identity, and access

Compose file secrets are read-only bind mounts. Docker does not remap their
UID/GID or apply the `mode` field for file-backed secrets. The image therefore
builds with your non-root UID/GID so it can read your mode-600 token/key files.
The builder rejects UID/GID zero. The default standalone image user is 65532;
Compose supplies the IDs explicitly. This behavior follows Docker's
[file-secret handling](https://docs.docker.com/reference/compose-file/services/#secrets).

The image seeds a mode-700 data directory owned by that user. Docker initializes
an empty named volume from it, and the service creates its private database
subdirectory/files there. An existing volume keeps its existing ownership. If
switching user IDs, migrate the volume deliberately; do not fix permissions with
`chmod 777` or run the service as root. Bind-mounting an arbitrary host data
directory instead of the supplied volume also requires matching private ownership.

The final image is built from `scratch`: it contains the static service binary,
a public CA bundle, and the empty data directory. It has no shell, package manager,
Go compiler, source code, or credentials. The builder image is pinned by digest.
The build-context allowlist excludes caches, Git history, tests, databases, and
credential files, including credentials nested in source directories.

Compose configures a read-only root filesystem, drops all Linux capabilities,
disables privilege escalation, and limits memory, CPU, processes, and log size.
The database volume remains writable. It mounts only the four selected files;
it never mounts the repository, home directory, or Docker socket into the service.
Docker administrators still control containers and volumes; named volumes are
not application-level encryption or backups.

## Health and troubleshooting

The service binary includes `flagd healthcheck`. Compose runs it against the
container's loopback listener while preserving the public Host and verifying
the certificate against the mounted CA. This supports a host port different from
8443 without disabling TLS checks. The probe has a two-second timeout, rejects
redirects, and needs no token or private key. It is a liveness probe, not an
ongoing database readiness test.

```sh
docker compose ps
docker compose logs --tail 50 flagd
docker compose exec flagd /flagd healthcheck \
  --server "$FLAGCTL_SERVER" --connect 127.0.0.1:8443 \
  --ca-file /run/secrets/tls_ca
```

If startup fails, check the credential paths, certificate validity, and file
ownership/modes. Keep token/key files at 600 and their host directory at 700.
The certificate must cover `localhost` for this Compose file. For a private CA,
set `FLAGCTL_CA_FILE` to the issuer's CA bundle, not an unrelated certificate.
Do not use an insecure TLS bypass. Compose keeps the host port on `127.0.0.1`;
changing it to a wildcard is outside this local quickstart.

If Docker Desktop is installed but `docker` is missing from PATH on macOS, its
bundled CLI is in `/Applications/Docker.app/Contents/Resources/bin`. Add that
directory to the PATH of the current shell so Docker can also find its credential
helper. No application keys need to be shared with this project.

## Automated container verification

```sh
FLAGCTL_DOCKER_TEST=1 go test -v -count=1 ./tests/docker -timeout=15m
```

This opt-in test creates temporary certificates, a unique Compose project, a
private database volume, and a host CLI binary. It verifies build-context
exclusions, TLS health, anonymous rejection, CLI operations, environment isolation,
private database permissions, restart/recreation persistence, and clean shutdown.
Cleanup removes only its own containers, network, volume, generated image tag,
and temporary files. Downloaded layers and build caches remain reusable. Ordinary
`go test ./...` skips this Docker test; it never starts your demo implicitly.
