# Unstrange Go API

Go 1.25+ / Gin API with PostgreSQL for the Unstrange mobile app. It implements phone OTP sessions, profiles, photo/video posts, stories, follows, comments, plans and host controls, conversations, event spaces, reports, blocking, and manual identity review.

## Run on an Ubuntu VM with Neon

The Go binary runs directly under systemd. Neon hosts PostgreSQL; Caddy provides HTTPS. The instructions target Ubuntu 24.04 LTS with sudo access. No local PostgreSQL server or container runtime is required.

### 1. Prepare the VM

Point `api.example.com` to the VM's public IP. Allow inbound TCP 80/443 and SSH from your IP in the cloud firewall. Allow outbound PostgreSQL to Neon on 5432 and HTTPS to external providers. Port 8080 stays bound to localhost. If an AAAA record exists, it must point to a working IPv6 address on the VM.

```sh
sudo apt-get update
sudo apt-get install -y git curl ca-certificates openssl jq
```

Install a current stable Go release, at least 1.25, using the [official Go instructions](https://go.dev/doc/install). On a fresh VM without `/usr/local/go`, this Bash block selects the correct architecture and verifies the official download checksum:

```bash
(
  set -euo pipefail
  test ! -e /usr/local/go || { echo 'Go already exists; check go version before upgrading.'; exit 1; }
  unstrange_arch=$(dpkg --print-architecture)
  unstrange_release=$(curl -fsSL 'https://go.dev/dl/?mode=json' | jq -er --arg arch "$unstrange_arch" '[.[] | select(.stable) | .files[] | select(.os == "linux" and .arch == $arch and .kind == "archive")][0] | [.filename, .sha256] | @tsv')
  read -r unstrange_archive unstrange_checksum <<< "$unstrange_release"
  unstrange_tmp=$(mktemp -d)
  curl -fsSL "https://go.dev/dl/$unstrange_archive" -o "$unstrange_tmp/go.tar.gz"
  printf '%s  %s\n' "$unstrange_checksum" "$unstrange_tmp/go.tar.gz" | sha256sum --check
  sudo tar -C /usr/local -xzf "$unstrange_tmp/go.tar.gz"
  rm "$unstrange_tmp/go.tar.gz"
  rmdir "$unstrange_tmp"
)
export PATH=/usr/local/go/bin:$PATH
go version
```

### 2. Clone and build

```sh
git clone https://github.com/nirmalparmar/unstrange-go.git
cd unstrange-go
mkdir -p bin
CGO_ENABLED=0 go build -trimpath -o bin/server ./cmd/server
```

If GitHub requires authentication, use your Git credential manager or an SSH deploy key, rather than embedding a token in the URL.

### 3. Install the service and configuration

Run from the repository root. These initial setup commands create a dedicated service account and a root-owned environment file:

```sh
id unstrange >/dev/null 2>&1 || sudo useradd --system --user-group --home-dir /opt/unstrange --shell /usr/sbin/nologin unstrange
sudo install -d -m 755 /opt/unstrange
sudo install -d -m 700 /etc/unstrange
sudo install -m 755 bin/server /opt/unstrange/server
sudo test -e /etc/unstrange/backend.env || sudo install -m 600 deploy/.env.example /etc/unstrange/backend.env
sudo nano /etc/unstrange/backend.env
```

Set the following values:

| Variable | Value |
| --- | --- |
| `DATABASE_URL` | Your direct Neon URL, keeping `sslmode=require&channel_binding=require` |
| `DATABASE_URL_POOLED` | Your Neon pooler URL, keeping the same TLS parameters |
| `JWT_SECRET` | Generate with `openssl rand -hex 32` |
| `PRIVATE_MEDIA_KEY` | Generate with `openssl rand -base64 32` |
| `APP_URL` | `https://api.example.com`, using your API hostname |
| `FRONTEND_URL` | Your actual web frontend origin, if applicable |
| `MSG91_AUTH_TOKEN`, `MSG91_WIDGET_ID` | Your configured MSG91 OTP widget credentials |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | Credentials with `s3:PutObject` on the media bucket prefixes |
| `AWS_REGION`, `S3_BUCKET_NAME` | Region and bucket for public photos/videos |
| `ASSET_BASE_URL` | Public asset origin, e.g. `https://dt9dx2ny2cwkb.cloudfront.net` |

Keep `ENV=production` and `TEST_OTP` empty. Until SMS is configured, set `OTP_MODE=mock`, `MOCK_OTP=123456`, and `MOCK_OTP_PHONES` to a comma-separated list of dedicated test phone numbers with country codes (for example `+919999999999`). Production mock mode requires this list and rejects other numbers. The app displays the code with a **Use test code** action. Switch `OTP_MODE` to `msg91` when real SMS is ready. The env file uses plain `KEY=value` entries; do not add `export`. Preserve the generated JWT and private-media keys across deployments. Store a secure backup of `PRIVATE_MEDIA_KEY` separately from Neon: replacing it makes existing encrypted chat/identity images unreadable.

The media service returns `ASSET_BASE_URL/KEY` for every S3 upload: profile photos, posts, stories, reels, activity/event covers, and general uploads. Set `ASSET_BASE_URL=https://dt9dx2ny2cwkb.cloudfront.net` to deliver all these assets through CloudFront. The distribution must point at the configured bucket and have read access through Origin Access Control; the bucket can stay private. Optional base paths are supported and trailing slashes are removed. URLs must include `https://` in production. Changing this variable affects new upload URLs; already saved URLs are preserved. The Flutter app uses the returned URLs without needing a rebuild.

When `ASSET_BASE_URL` is empty, delivery falls back to `https://BUCKET.s3.REGION.amazonaws.com/KEY`, which requires public read access to the social media prefixes. Local development without AWS credentials continues to use `/uploads/`. Private chat and identity images stay in encrypted, authenticated database storage and never use the public asset domain. With `OTP_MODE=msg91`, real sign-in requires MSG91; without S3, public uploads fail.

Optional AI, moderation, weather and reviewer configuration is in `deploy/.env.example`. `ADMIN_USER_IDS` accepts existing user UUIDs.

```sh
sudo install -m 644 deploy/unstrange.service /etc/systemd/system/unstrange.service
sudo systemctl daemon-reload
sudo systemctl enable --now unstrange
sudo systemctl status unstrange --no-pager
sudo journalctl -u unstrange -n 100 --no-pager
curl --fail http://127.0.0.1:8080/health
```

Expected result: `{"status":"ok"}`. Startup applies the schema through the direct Neon endpoint, then uses the pooled endpoint for API traffic. No sample records are inserted. The service runs as an unprivileged user, restarts after failure, and starts automatically on boot. Its filesystem is read-only except for its private temporary directory.

`/health` checks the API process, not SMS or storage integrations. Complete an SMS sign-in with `OTP_MODE=msg91` and an upload before inviting public users. Mock mode is for dedicated preview accounts and does not prove phone ownership.

### 4. Enable HTTPS

Install Caddy using its [official Ubuntu package instructions](https://caddyserver.com/docs/install#debian-ubuntu-raspbian):

```sh
sudo apt-get install -y debian-keyring debian-archive-keyring apt-transport-https gnupg
curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/gpg.key | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo chmod o+r /usr/share/keyrings/caddy-stable-archive-keyring.gpg /etc/apt/sources.list.d/caddy-stable.list
sudo apt-get update
sudo apt-get install -y caddy
sudo install -m 644 deploy/Caddyfile /etc/caddy/Caddyfile
sudo nano /etc/caddy/Caddyfile
```

Replace `api.example.com` and `you@example.com` with your hostname and certificate contact email. The template is for a fresh Caddy installation; merge its site block if the VM already serves other sites.

```sh
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl enable --now caddy
sudo systemctl reload caddy
curl --fail https://api.example.com/health
```

Use your hostname in the last command. Caddy obtains and renews certificates through [automatic HTTPS](https://caddyserver.com/docs/automatic-https). The API trusts forwarding headers only from loopback, where the Caddy proxy connects; it does not trust Internet callers' arbitrary forwarding headers.

### 5. Connect the Flutter app

In the separate mobile repository, use the API origin without an `/api/v1` suffix:

```sh
flutter build apk --release --dart-define=API_BASE_URL=https://api.example.com
```

Android release signing must be configured. Set the same define for the iOS build.

### Updates

Take a Neon backup or branch before schema-changing deployments. Startup runs idempotent SQL migrations; review migration changes and do not assume rolling back code reverses database changes.

From the backend checkout:

```sh
git pull --ff-only origin main
CGO_ENABLED=0 go build -trimpath -o bin/server ./cmd/server
sudo install -m 755 bin/server /opt/unstrange/server.next
sudo mv /opt/unstrange/server.next /opt/unstrange/server
sudo systemctl restart unstrange
sudo journalctl -u unstrange -n 100 --no-pager
curl --fail https://api.example.com/health
```

Keep `/etc/unstrange/backend.env` during updates; it never belongs in Git. Review service-file changes separately and run `systemctl daemon-reload` after installing an updated unit. Application logs go to the system journal.

## Local development

With Go and PostgreSQL CLI tools installed, `./scripts/dev.sh` starts an isolated local database on `127.0.0.1:55439` and the API on `127.0.0.1:8080`, using OTP `123456`. It ignores cloud credentials. This helper is only for local development; the VM uses Neon.

`make run` reads the ignored `.env` file. Environment variables take precedence. `DATABASE_URL_POOLED` is optional; without it, the API uses the direct endpoint throughout.

## Code layout and current limits

- `cmd/server`: startup, migrations and graceful shutdown.
- `internal/auth`, `middleware`: OTP, JWT sessions, authorization, request limits and moderation.
- `internal/community`: plans, people, chat, events, safety and identity review.
- `internal/post`, `story`, `profile`, `social`: feeds and social content.
- `internal/db`: connection pools and SQL schema.
- `internal/media`: public S3 uploads; private media uses authenticated encrypted database storage.

Chat refreshes in the foreground; background pushes, offline chat, automatic identity verification, media moderation and complete list pagination are not implemented. Public media URLs remain accessible to anyone who already knows the URL even if API visibility later changes. Identity review is manual.
