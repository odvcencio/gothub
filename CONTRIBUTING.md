# Contributing to gothub

## Prerequisites

- Go 1.25 (`go.mod`)
- Node.js + npm (for `frontend/`)
- Docker + Docker Compose (optional, for containerized local setup)

## Setup and run

### Docker quick start

```bash
cp .env.example .env
docker compose up --build
```

Open `http://localhost:3000`.

### Local quick start

```bash
cd frontend && npm ci && cd ..
go test ./...
make build
GOTHUB_JWT_SECRET=dev-jwt-secret-12345 GOTHUB_PORT=3900 go run ./cmd/gothub serve
```

Notes:

- `GOTHUB_JWT_SECRET` must be at least 16 characters.
- `serve` applies DB migrations on startup.
- With no DB env overrides, local runs use SQLite (`gothub.db`) and `data/repos`.

## Dev and test workflow

- Run all backend tests: `go test ./...`
- Build frontend only: `cd frontend && npm run build`
- Run frontend dev server: `cd frontend && npm run dev`
- Build full app (frontend + wasm + embedded assets + binary): `make build`
- Run migrations only: `go run ./cmd/gothub migrate`

## Branching and commits

- Branch from `main` for each change.
- Use short, purpose-based branch names (for example, `feat/auth-passkeys`, `fix/pr-merge`, `docs/contributing`).
- Keep commits and PRs small and single-purpose.
- Before opening a PR, run at least `go test ./...` and `make build`.
- When committing, use this required command style: `buckley commit --yes --minimal-output`.
- Write commit subjects in imperative mood (for example, `add repo policy endpoint`).
