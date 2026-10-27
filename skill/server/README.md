# GoFi server operations

Read this file only when the task involves running, deploying, or provisioning a GoFi **server** — for calling a running instance as a client, stay in the parent `../SKILL.md`.

The canonical runbook is the repository's `README.md` (deployment, backup, migration, verification) — this file is the orientation map, not a copy of it.

## Architecture in one paragraph

GoFi is a single Go binary plus PostgreSQL. Uploaded bytes live on the host filesystem under `GOFI_BASE_DIR/{public,private}`; the database stores only `api_keys` and `short_links`. Six key types exist — `upload`, `download`, `shorten`, `api`, `list`, `delete` — each scoped to its endpoint group, all soft-disabled via `is_enabled`. A request is authorized by a Bearer token (or `?token=` query fallback) matched on key value + type + enabled flag.

## Deployment shape

Docker Compose is the documented path: an `app` container (non-root UID 10001, image built from the repo Dockerfile) and a `db` container (`postgres:17-bookworm`) with a named volume. `deploy/config.toml` holds the four TOML keys (`GIN_MODE`, `GOFI_PORT`, `GOFI_BASE_DIR`, `DATABASE_URL`) and is bind-mounted read-only. `deploy/compose.production.yml` is an overlay for an existing production volume — never for fresh installs. `deploy/verify-production.sh` is the precondition gate for every production touch.

Host-run deployments (screen/systemd) are also supported: `SIGINT`/`SIGTERM` drain requests for up to 30 s, and `SIGHUP` performs a zero-downtime re-exec that hands the listener to a new process. Inside a container, redeploy by replacing the container — the parent's exit ends it.

## Provisioning API keys

Two paths, no third:

1. `POST /api-keys` with an existing `api`-type key — the normal route once an admin key exists.
2. Direct `psql` insert — the bootstrap path when no `api` key exists yet:

   ```sql
   INSERT INTO api_keys (key, type, is_enabled)
   VALUES ('<new-random-key>', '<type>', true);
   ```

Deliver generated keys to the user's key ring (`~/.config/gofi/<server>/<type>`, mode 600, no trailing newline) and never into committed files or logs.

## Instance knowledge is machine-local

Which hosts run GoFi, under which SSH alias, repo path, and project name, is per-machine knowledge that must not be committed. When the user keeps it, it lives at `~/.config/gofi/<server>/SERVER.md`. If that file is missing, ask the user where the instance is deployed instead of guessing.
