# gothub

AI-first code hosting with structural diffs, structural merge preview, entity lineage, and built-in code intelligence.

Contributor workflow: see [CONTRIBUTING.md](CONTRIBUTING.md).

## Quick start (Docker)

```bash
cp .env.example .env
docker compose up --build
```

Open `http://localhost:3000`.

Default compose settings enable password auth for local development and set a non-empty dev JWT secret. Change these values before any shared deployment.

## Quick start (local)

```bash
go test ./...
cd frontend && npm ci && npm run build && cd ..
go run ./cmd/gothub serve
```

## Environment variables

### Core

- `GOTHUB_HOST`: bind host (default `0.0.0.0`)
- `GOTHUB_PORT`: bind port (default `3000`)
- `GOTHUB_DB_DRIVER`: `sqlite` or `postgres` (default `sqlite`)
- `GOTHUB_DB_DSN`: DB DSN/file path
- `GOTHUB_STORAGE_PATH`: repository storage root
- `GOTHUB_JWT_SECRET`: JWT signing secret (required; at least 16 chars)
- `GOTHUB_ENABLE_PASSWORD_AUTH`: enable password login (`true`/`false`)

### Auth/WebAuthn

- `GOTHUB_WEBAUTHN_ORIGIN`: RP origin (for passkeys)
- `GOTHUB_WEBAUTHN_RPID`: RP ID (for passkeys)

### Ops/observability

- `GOTHUB_TRUSTED_PROXIES`: comma-separated trusted proxy CIDRs/IPs for `X-Forwarded-For` (default loopback only)
- `GOTHUB_TRUST_PROXY`: legacy trust-all proxy mode (`true`/`false`), only used when `GOTHUB_TRUSTED_PROXIES` is unset
- `GOTHUB_CORS_ALLOW_ORIGINS`: comma-separated allowlist for CORS origins (default `*`)
- `GOTHUB_ENABLE_ADMIN_HEALTH`: expose `/admin/health` (`true`/`false`)
- `GOTHUB_ENABLE_PPROF`: expose `/debug/pprof/*` (`true`/`false`)
- `GOTHUB_ADMIN_ALLOWED_CIDRS`: comma-separated CIDRs allowed for admin routes
- `GOTHUB_OTEL_EXPORTER_OTLP_ENDPOINT`: OTLP endpoint
- `GOTHUB_OTEL_EXPORTER_OTLP_INSECURE`: OTLP insecure transport (`true`/`false`)
- `GOTHUB_OTEL_SERVICE_NAME`: override service name for traces/metrics

## Development notes

- Password auth is disabled by default in code config, but enabled in `docker-compose.yml` for local bootstrap.
- Magic-link auth is available in local/dev mode without external email delivery (token returned/logged for dev flows).
- Passkeys require properly configured origin/RP ID and browser support.

## License

MIT
