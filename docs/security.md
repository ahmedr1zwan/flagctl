# Secure access

`flagd` has two explicit modes. The default is HTTP on a literal loopback IP,
without authentication, for local development. Secure mode uses TLS 1.3 and a
bearer token loaded from a private file. Network listeners require secure mode
plus `--allow-network` and `--public-origin`.

## Try secure mode locally

No cloud account or third-party API key is needed. These commands create a fresh,
short-lived development certificate and random token with OpenSSL 3. Keep the
terminal in the repository directory. The credentials live in a new private
temporary directory outside the repository; the command prints no secret values.

```sh
go build -o bin/flagd ./cmd/flagd
go build -o bin/flagctl ./cmd/flagctl

umask 077
export FLAGCTL_DEMO_DIR="$(mktemp -d)"
openssl rand -hex 32 > "$FLAGCTL_DEMO_DIR/api.token"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
  -noenc -days 7 -subj '/CN=localhost' \
  -addext 'subjectAltName=DNS:localhost,IP:127.0.0.1,IP:::1' \
  -keyout "$FLAGCTL_DEMO_DIR/server.key" \
  -out "$FLAGCTL_DEMO_DIR/server.crt"

export FLAGCTL_SERVER=https://127.0.0.1:8443
export FLAGCTL_TOKEN_FILE="$FLAGCTL_DEMO_DIR/api.token"
export FLAGCTL_CA_FILE="$FLAGCTL_DEMO_DIR/server.crt"

./bin/flagd --listen 127.0.0.1:8443 \
  --data-dir "$FLAGCTL_DEMO_DIR/data" \
  --token-file "$FLAGCTL_TOKEN_FILE" \
  --tls-cert-file "$FLAGCTL_CA_FILE" \
  --tls-key-file "$FLAGCTL_DEMO_DIR/server.key" &
FLAGCTL_DEMO_PID=$!
```

Wait for `flagd listening`. In the same terminal:

```sh
curl --noproxy '*' --cacert "$FLAGCTL_CA_FILE" "$FLAGCTL_SERVER/healthz"
./bin/flagctl flags create checkout_v2 --env dev
./bin/flagctl flags toggle checkout_v2 --env dev --enabled=true
./bin/flagctl flags list --env dev --output json
```

Health returns `{"status":"ok"}`. The JSON list contains `checkout_v2` with
`enabled: true`. Authentication is required for every flag operation:

```sh
./bin/flagctl --token-file= flags list --env dev
```

Expect exit status 1 and an HTTP 401 authentication error. An explicit empty
option clears the environment fallback. `--ca-file=` restores system trust;
this self-signed demonstration certificate should then fail verification.
Do not use `curl -k` or install this certificate into system trust.

Stop the background service after the demo:

```sh
kill -TERM "$FLAGCTL_DEMO_PID"
wait "$FLAGCTL_DEMO_PID"
unset FLAGCTL_SERVER FLAGCTL_TOKEN_FILE FLAGCTL_CA_FILE
```

The private temporary directory retains the demo database and credentials until
you delete it. The certificate expires after seven days. Use managed certificates
and durable private credential storage for a persistent deployment.

## Service configuration

| Option | Requirement |
| --- | --- |
| `--token-file` | Private regular file containing 64 lowercase hexadecimal characters (32 random bytes), optionally followed by LF or CRLF. |
| `--tls-cert-file` | PEM server certificate chain, leaf first. |
| `--tls-key-file` | Private PEM key matching the leaf certificate. |
| `--public-origin` | HTTPS origin that clients use, such as `https://flags.example.com:8443`. Must match the certificate's hostname/IP. Required with `--allow-network`. |
| `--allow-network` | Explicitly permits non-loopback or wildcard listen IPs. Requires all three files and a public origin, even if the chosen listener is loopback. |

The three credential/certificate options must be supplied together. Invalid
configuration fails before a listener opens or the database is created. Without
an explicit public origin, secure loopback mode uses the actual bound IP and
port, including a port assigned for `--listen 127.0.0.1:0`.

The public origin controls the accepted HTTP Host, including its port. Port 443
may be omitted. It is independent of the bind address so container forwarding
can use a different external port. It does not configure DNS or issue a
certificate. Example: a future container may listen on `0.0.0.0:8443` while
accepting `https://localhost:9443`, with a certificate covering `localhost`.
Container packaging is the next checkpoint.

Secure mode serves HTTPS directly. It rejects plaintext HTTP and does not trust
`X-Forwarded-Proto` as proof of TLS. Any proxy in front must preserve the
configured Host and use verified HTTPS to `flagd`. `/healthz` accepts anonymous
GET/HEAD over TLS; it is a liveness check, not a continuous database readiness
probe. Other routes require exactly one valid Authorization bearer header.
Tokens in URLs or cookies do not authenticate requests.

## CLI and Terraform configuration

The CLI accepts `--server`, `--token-file`, and `--ca-file`. Their environment
fallbacks are `FLAGCTL_SERVER`, `FLAGCTL_TOKEN_FILE`, and `FLAGCTL_CA_FILE`.
Explicit options win. Only file paths go in these variables, never token values.
Help does not display their environment values.

Terraform uses the same client and environment variables:

```hcl
provider "flagctl" {
  server     = "https://127.0.0.1:8443"
  token_file = "/private/path/api.token"
  ca_file    = "/private/path/server.crt"
}
```

Alternatively omit these attributes to use the environment from the demo.
The [Terraform guide](terraform.md) explains the local provider installation.
Provider configuration accepts credential **paths**, with no raw-token attribute.
An explicit empty `token_file` clears the environment token; an empty `ca_file`
restores system trust. The token contents are not added to resource state.
Terraform state and plans still contain flag names, descriptions, and values.

HTTP origins are limited to loopback and reject token/CA configuration. Remote
HTTPS origins require an explicit token file. Certificate and hostname checks
stay enabled; custom CA bundles replace system roots for that client. Neither
client follows redirects or uses proxy environment variables. Request errors
and provider diagnostics omit raw response bodies, remote messages, tokens, and
credential file paths. There is no raw-token command-line option or insecure TLS
verification switch.

## Credential handling and limits

- Store tokens and private keys in trusted directories outside the checkout.
  Use directory mode 700 and file mode 600. The loader rejects symlinks,
  non-regular files, oversized files, and group/other permissions on secrets;
  it does not change your existing permissions. Protect parent directories from
  replacement by other users as well.
- File privacy currently uses POSIX permissions. Secure mode is supported on
  macOS/Linux; secret loading fails closed on Windows until ACL-aware validation
  is implemented. Windows binaries have only been cross-compiled, not runtime-tested.
- Certificates and tokens are read once at service startup/client configuration.
  To rotate, generate a new token file privately, update the service and clients
  to that file, then restart `flagd` and reconfigure clients. Old credentials
  stop authenticating after the service restart. There is no overlapping-token
  rotation window or automatic certificate reload.
- The bearer token grants full read/write access across all environments. There
  are no per-user roles, per-environment grants, token expiry, or audit history.
  Limit access to trusted clients and networks. This is a single-instance service
  with bounded requests, not a hardened public multi-tenant deployment.
- Logs omit request contents and TLS peer-controlled error text. Do not log raw
  HTTP headers yourself or place credentials in feature descriptions. Database
  files and backups are private but are not encrypted by the application.

The [test guide](testing.md) describes the automated rejection, TLS, lifecycle,
and Terraform checks. Authentication transport follows the bearer-token guidance
in [RFC 6750](https://www.rfc-editor.org/rfc/rfc6750.html); TLS uses Go's
[standard library](https://pkg.go.dev/crypto/tls).
