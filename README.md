# GoFi - Simple & Secure File Sharing

[![Go Version](https://img.shields.io/badge/Go-1.26.10+-blue.svg)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

[简体中文](README.zh-CN.md)

GoFi is a lightweight, self-hosted file sharing service with public/private uploads, token-protected private downloads, short links, and Swagger documentation. PostgreSQL stores API keys and short links; uploaded bytes live in the host's `./data` directory.

## Container quick start

Use Docker Engine with a recent Compose plugin (v2 or later). The Dockerfile uses Go **1.26.10**, a supported toolchain containing the [eMudhra-related `crypto/x509` fix](https://groups.google.com/g/golang-announce/c/66pojueaQek). Keep the pinned toolchain updated as security releases arrive. No application dependencies or database schema are changed by this deployment recipe.

### 1. Configure a fresh installation

```sh
git clone https://github.com/ShinoharaHaruna/GoFi.git
cd GoFi
if [ ! -e deploy/config.toml ]; then
  (umask 027; cp deploy/config.toml.template deploy/config.toml)
fi
sudoedit deploy/config.toml
```

The container configuration is a **file**, not an environment-only configuration:

```toml
GIN_MODE = "release"
GOFI_PORT = "8080"
GOFI_BASE_DIR = "/app/data"
DATABASE_URL = "postgres://gofi_user:gofi_local_dev@db:5432/gofi_db?sslmode=disable"
```

These are the unchanged Compose **development defaults**, not a recommendation for new public deployments. Use the database's actual credentials, URL-encoding special characters in the DSN. Configure credentials before initializing a new public installation; credential rotation for an existing database is a separate operation. Changing `POSTGRES_PASSWORD` does not rotate an initialized database's password. Container-to-container connections use **`db:5432`**, not `localhost:54320`.

`deploy/config.toml` is gitignored and mounted read-only at `/app/config.toml`. The existing root `config.toml` is not replaced; it remains available for host development and rollback. Do not put secrets into build arguments or image tags.

### 2. Prepare permissions

The image runs as fixed **UID:GID `10001:10001`**, without a root startup script. For a **fresh installation with rootful Docker and no user-namespace remapping**, prepare the bind mount explicitly:

```sh
sudo install -d -o 10001 -g 10001 -m 0750 ./data ./data/public ./data/private
sudo chown root:10001 deploy/config.toml
sudo chmod 0640 deploy/config.toml
```

These commands are repeatable and do not overwrite the configuration. They do not repair existing files: if `data` already contains uploads, use the stop/backup/permission steps in [migration](#first-migration-from-screen-existing-production). Never use `chmod 777`. Rootless Docker or `userns-remap` maps numeric IDs differently; do not apply these host IDs blindly. Prepare the mapped ownership in that daemon's namespace instead. The isolated test below handles both cases using a one-off fixture helper, never a root application entrypoint.

### 3. Build and start the whole stack

```sh
export GOFI_VCS_REF="$(git describe --always --dirty)"
docker compose -p gofi build app
docker compose -p gofi up -d --no-build --wait --wait-timeout 120
docker compose -p gofi ps
curl --fail http://127.0.0.1:8080/health
```

The default mapping remains **`8080:8080` on all host interfaces**; this change does not restrict the existing public exposure. PostgreSQL remains at **`127.0.0.1:54320`**. Create API keys before testing authenticated operations. Do not run a second app against a production database, even on another HTTP port: startup calls **AutoMigrate before binding the port**.

The default recipe creates the logical volume `gofi_db_data` automatically, so fresh installations need no pre-created volume. With project `gofi`, its physical name is **`gofi_gofi_db_data`**. Other project names get independent volumes. The [production overlay](deploy/compose.production.yml) instead requires that exact existing volume and fails if it is absent; it is **not** for fresh installs.

## Configuration and isolation overrides

All four TOML keys should be present. Keep the container's internal port at `8080` so publishing and the image healthcheck agree.

| TOML key | Container value | Purpose |
| --- | --- | --- |
| `GIN_MODE` | `release` | Gin mode |
| `GOFI_PORT` | `8080` | Internal HTTP port |
| `GOFI_BASE_DIR` | `/app/data` | Host bind-mounted uploads |
| `DATABASE_URL` | `postgres://…@db:5432/…` | Compose database |

Viper currently adds a `GOFI` prefix even to keys already named `GOFI_*`, and `AutomaticEnv` alone does not reliably supply all keys to `Unmarshal`. Do not rely on the old environment-variable table or an env-only deployment. This recipe deliberately uses an explicit TOML file without refactoring configuration behavior.

The following variables are **Compose interpolation inputs**, not application configuration environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `GOFI_IMAGE` | `gofi:local` | Built/loaded app image |
| `GOFI_VCS_REF` | `unknown` | Build-time revision suffix; no `.git` is copied |
| `GOFI_CONFIG_FILE` | `./deploy/config.toml` | Read-only host configuration path |
| `GOFI_DATA_DIR` | `./data` | Host upload directory |
| `GOFI_APP_PUBLISH` | `8080` | Host `[IP:]port`, e.g. `127.0.0.1:18080` |
| `GOFI_DB_PUBLISH` | `127.0.0.1:54320` | Host `[IP:]port` for PostgreSQL, e.g. `127.0.0.1:54321` |

For an isolated instance, override **all state inputs**, not just ports: use a different `-p` project, configuration file, data directory, and database volume. Do not use the production overlay. `scripts/test-compose.py` automates this with temporary state and random host ports. Bind sources must already exist; Compose will not silently create a directory in place of a missing configuration file.

## Host development: start only PostgreSQL

In a separate local development checkout, use Go 1.26.10 or a newer supported, patched release. `go.mod` retains the existing minimum language version; no business dependencies are upgraded here.

```sh
GOFI_DB_PUBLISH=127.0.0.1:54321 docker compose -p gofi-dev up -d db
if [ ! -e config.toml ]; then
  (umask 027; cp config.toml.template config.toml)
fi
mkdir -p ./data/public ./data/private
```

Keep any existing host configuration. For a new development configuration, set:

```toml
GIN_MODE = "debug"
GOFI_PORT = "8080"
GOFI_BASE_DIR = "./data"
DATABASE_URL = "postgres://gofi_user:gofi_local_dev@localhost:54321/gofi_db?sslmode=disable"
```

```sh
go run ./cmd/gofi -config ./config.toml
```

Only `db` starts with `up -d db`; plain `up -d` now starts **both** services. A host process uses `localhost` and the published database port; a container uses `db:5432`. Do not share production storage/database with development or run both host and container apps against the same production database.

## API usage and initial keys

Swagger UI: <http://localhost:8080/swagger/index.html>.

| Endpoint | Access |
| --- | --- |
| `GET /health` | Unauthenticated HTTP liveness only |
| `POST /upload` | `upload` token; `X-GoFi-Target-Dir: public` opts into public storage, otherwise private |
| `GET /:filename` | Public: anonymous; private: `download` token |
| `POST /shorten` | `shorten` token; JSON `{"filename":"example.txt"}` |
| `GET /s/:shortcode` | Public: anonymous; private: `download` token |
| `DELETE /shorten/:shortcode` | `shorten` token; disables the link |
| `POST /shorten/:shortcode/enable` | `shorten` token; re-enables the link |

After the first app startup initializes tables, open a database session and replace the placeholders with distinct strong keys (do not paste real keys into documentation or shell history):

```sh
docker compose -p gofi exec db psql -U gofi_user -d gofi_db
```

```sql
INSERT INTO api_keys (key, type, is_enabled)
VALUES
  ('<your-upload-key>', 'upload', true),
  ('<your-download-key>', 'download', true),
  ('<your-shorten-key>', 'shorten', true);
```

Use `Authorization: Bearer <key>`; private downloads also accept `?token=<key>`, but query strings can leak into access logs. Existing production API keys and links remain in the existing database; do not reseed or replace them during migration.

## Operations

For the existing production installation, use `pc` from the migration section instead of plain `docker compose -p gofi` in **every** command, so the external-volume guard stays active.

```sh
pc logs --tail 100 -f app db
pc stop app       # Stops the application, keeps PostgreSQL running.
pc stop           # Stops both services, retains all state.
pc down           # Removes containers/network, retains named volume and bind data.
```

**Never run `docker compose down -v` in production.** It can delete Compose-managed database volumes. An external volume adds a guard, not a replacement for backups or protection from explicit `docker volume rm`/prune operations. Deleting `./data` still destroys uploads.

### Updates and images

Build on a local/CI machine, not on the approximately 1 GB production VPS. Compilation uses `CGO_ENABLED=0`, serial package builds, `GOMAXPROCS=2`, and a soft Go memory target, **not a hard memory cap**. Transfer a prebuilt image for the VPS's architecture after acceptance; do not add `--build` on the VPS.

```sh
# First run the normal dirty-tree verification. Then commit the reviewed changes.
git status --short

docker compose -p gofi build app &&
  python3 scripts/test-compose.py --image gofi:local --audit-image

# After the commit, rebuild and re-run the checks against an image derived from that commit.
release_ref="$(git rev-parse --short HEAD)" &&
  export GOFI_VCS_REF="$release_ref" &&
  export GOFI_IMAGE="gofi:${release_ref}" &&
  docker compose -p gofi build --no-cache app &&
  python3 scripts/test-compose.py --image "$GOFI_IMAGE" --audit-image &&
  docker image save --output "gofi-${release_ref}.tar" "$GOFI_IMAGE" &&
  git push origin main
```

For source archives without Git, set `GOFI_VCS_REF` explicitly; otherwise the suffix is `unknown`. The binary's startup version is `<VERSION>-<revision>`, matching the existing build convention. The final image contains only the runtime OS, CA certificates, curl, and the binary, not the build stage, module cache, source, uploads, or a configuration file. Both the build context allowlist and explicit source-only `COPY` paths exclude local secrets. When adding a Go package, update those lists.

Do not retag a dirty or pre-commit validation image for release: its binary version and `org.opencontainers.image.revision` label already contain the old revision. The required order is **revise and verify → commit → rebuild from that commit → re-verify that exact image → push → VPS `git pull --ff-only` → transfer and load the same image**. Preserve the previous image and host binary. After a stop-and-backup window:

```sh
# Production, with pc defined below and the existing db confirmed healthy.
project_root="$(pwd -P)" &&
  resolved_config="$(realpath "$project_root/deploy/config.toml")" &&
  resolved_data="$(realpath "$project_root/data")" &&
  export GOFI_CONFIG_FILE="$resolved_config" &&
  export GOFI_DATA_DIR="$resolved_data" &&
  release_ref="$(git rev-parse --short HEAD)" &&
  export GOFI_IMAGE="gofi:${release_ref}" &&
  docker image load --input /path/to/gofi-image.tar &&
  deploy/verify-production.sh &&
  pc up -d --no-deps --no-build --pull never --wait --wait-timeout 120 app &&
  pc logs --tail 100 app
```

This replaces only `app`; it neither pulls nor recreates PostgreSQL. The database tag remains `postgres:17-bookworm`; do not pull/recreate `db` as part of this migration, since a mutable tag can change the running patch version. PostgreSQL upgrades and password rotation are separate changes.

### Consistent backups

Back up **both** PostgreSQL (keys and links) and `./data` (file bytes), plus both configuration files, the old binary/image reference, and ownership metadata. Keep backups outside the repository with restrictive permissions. During a write-free window, with **all** app writers stopped (including any old screen process):

```sh
project_root="$(pwd -P)" &&
  resolved_config="$(realpath "$project_root/deploy/config.toml")" &&
  resolved_data="$(realpath "$project_root/data")" &&
  export GOFI_CONFIG_FILE="$resolved_config" &&
  export GOFI_DATA_DIR="$resolved_data" &&
  release_ref="$(git rev-parse --short HEAD)" &&
  export GOFI_IMAGE="gofi:${release_ref}" &&
  deploy/verify-production.sh &&
  umask 077 &&
  backup_dir="$(mktemp -d "${HOME}/gofi-backup.XXXXXX")" &&
  # pc stop app is not sufficient if the old screen process is still running.
  pc stop app &&
  pc exec -T db pg_dump -U gofi_user -d gofi_db --format=custom > "$backup_dir/database.dump" &&
  sudo tar --numeric-owner --acls --xattrs -C "$resolved_data" -cpf "$backup_dir/uploads.tar" . &&
  sudo cp -p config.toml "$backup_dir/host-config.toml" &&
  sudo cp -p "$resolved_config" "$backup_dir/container-config.toml" &&
  sudo cp -p build/GoFi "$backup_dir/GoFi.previous"
```

Verify the dump (`pg_restore --list`), archive inventory, and recovery procedure before relying on them. Do not copy a live PostgreSQL data directory as a logical backup. Restart only the selected application after the database and file snapshot are both complete; keep writers stopped throughout a migration or restore. Backups include secrets and private files; protect and store them off-host appropriately. Later container-only installations may no longer have `build/GoFi`; retain their prior image instead.

## First migration from screen (existing production)

**Operator runbook for after acceptance only. This implementation does not authorize or perform VPS access.** Expected existing state: project `gofi`, container `gofi-db-1`, PostgreSQL 17.6 from `postgres:17-bookworm`, volume `gofi_gofi_db_data` mounted at `/var/lib/postgresql/data`, host DB port `127.0.0.1:54320`, uploads at `/root/GoFi/data`, and a root-owned app under screen. Stop and investigate any mismatch; do not initialize an empty replacement database.

1. Prepare the reviewed source/image without starting another app. Preserve root `config.toml` and `build/GoFi`. After explicit release approval, synchronize source using `git pull --ff-only`; do not reset a dirty checkout. Load the locally built image of the correct architecture. Create `deploy/config.toml` only if absent, using the real existing database credentials with **`db:5432`**, `release`, port `8080`, and `/app/data`. Keep the old host configuration's `localhost:54320` connection unchanged. Do not pre-test this app against production: AutoMigrate runs before any HTTP port conflict can protect the database.

2. Use the production override on every operation:

   ```sh
   cd /root/GoFi &&
     project_root="$(pwd -P)" &&
     resolved_config="$(realpath "$project_root/deploy/config.toml")" &&
     resolved_data="$(realpath "$project_root/data")" &&
     export GOFI_CONFIG_FILE="$resolved_config" &&
     export GOFI_DATA_DIR="$resolved_data" &&
     release_ref="$(git rev-parse --short HEAD)" &&
     export GOFI_IMAGE="gofi:${release_ref}" &&
     pc() { docker compose --env-file /dev/null -p gofi -f docker-compose.yml -f deploy/compose.production.yml "$@"; } &&
     deploy/verify-production.sh &&
     db_container_before="$(docker inspect --format '{{.Id}}' gofi-db-1)"
   ```

   `name: gofi` plus `-p gofi` preserves the network/project. The logical key stays `gofi_db_data`; the overlay declares physical `gofi_gofi_db_data` external, so a missing volume fails instead of silently creating an empty database. Do not create it merely to satisfy this check.

3. Begin a maintenance/stop-write window. Let active uploads complete and stop clients/writers. Attach to the existing screen (`screen -r 2817.gofi`, or its verified current session), stop the old `build/GoFi` with Ctrl-C, and confirm that process and the `:8080` listener are gone. The app has no request-level graceful shutdown; stopping it can interrupt in-flight requests. Keep PostgreSQL running, then fail fast before any backup or permission change:

   ```sh
   deploy/verify-production.sh --stopped
   ```

   Take and verify the database/file/config/binary backups above **after** all writers stop.

4. In this stopped window, prepare existing uploads for the fixed non-root ID. These commands are repeatable, do not follow symlinks, and preserve file contents; backup metadata first:

   ```sh
   deploy/verify-production.sh --stopped &&
     sudo install -d -o 10001 -g 10001 -m 0750 "$resolved_data" "$resolved_data/public" "$resolved_data/private" &&
     sudo find "$resolved_data" -xdev -type d -exec chown 10001:10001 {} + -exec chmod 0750 {} + &&
     sudo find "$resolved_data" -xdev -type f -exec chown 10001:10001 {} + -exec chmod 0640 {} + &&
     sudo chown root:10001 "$resolved_config" &&
     sudo chmod 0640 "$resolved_config" &&
     deploy/verify-production.sh --permissions-ready
   ```

   This is an explicit one-time host preparation, not an application startup action. Inspect unexpected symlinks, special files, separate mounts, or ACL requirements before proceeding. If Docker uses user-namespace remapping, adapt the IDs first. A bind mount hides the image directory's permissions, so image ownership alone cannot fix host `root:root`/`0750` directories.

5. Start **only** the app, with the database already verified healthy:

   ```sh
   deploy/verify-production.sh --permissions-ready &&
     pc up -d --no-deps --no-build --pull never --wait --wait-timeout 120 app &&
     pc ps &&
     pc logs --tail 100 app &&
     curl --fail http://127.0.0.1:8080/health &&
     test "$(docker inspect --format '{{.Id}}' gofi-db-1)" = "$db_container_before"
   ```

   `--no-deps` deliberately leaves the existing DB untouched and skips dependency startup checks, hence the explicit health check in step 2. Verify old public/private downloads and short links, then an authorized upload/download with the operator's existing keys and a disposable file. `/health` alone is insufficient. Confirm the same DB volume and host uploads remain in use, then reopen writes and end the maintenance window. No containerized test suite may target production.

### Rollback to screen

1. Stop writes again; finish or account for active requests. Run `pc stop app` and `pc rm -f app` so a restart policy cannot bring up a competing app. **Do not stop/delete `db`, delete the volume, or run `down -v`.**
2. Take another paired database/file snapshot if the container accepted writes. The default rollback reuses the current DB and uploads, including new files; do not blindly restore the old file archive over newer data. This change introduces no schema changes, but startup still executes AutoMigrate. If a database restore is necessary, reconcile new writes and restore the database and corresponding file snapshot together while all writers remain stopped.
3. For the documented original root-run deployment, restore data ownership to `root:root`, including files created since migration:

   ```sh
   resolved_data="$(realpath ./data)" &&
     sudo find "$resolved_data" -xdev -type d -exec chown 0:0 {} + -exec chmod 0750 {} + &&
     sudo find "$resolved_data" -xdev -type f -exec chown 0:0 {} + -exec chmod 0640 {} +
   ```

   Use the saved metadata instead if the original installation had different owners/modes/ACLs. Do not restore the file archive solely to change permissions. Keep the container config protected; the old app uses the untouched root `config.toml` with `localhost:54320`.
4. Restore the saved binary if needed, open a new screen as the original root user (`screen -S gofi`), and run `./build/GoFi -config /root/GoFi/config.toml` from `/root/GoFi`. Confirm that only this instance is running. Check logs, downloads, private authentication, and short links before reopening writes. Retain backups and the approved image for diagnosis.

## Health and shutdown limitations

- Compose waits for the database's `pg_isready` before initially starting `app`. This does not guarantee ongoing database availability or validate app credentials/schema.
- `/health` returns only `{"status":"UP"}`. The image healthcheck is HTTP **liveness**, not full business readiness: it does not query the database or check storage permissions/capacity.
- Docker marking a container **unhealthy does not restart it**. `restart: unless-stopped` restarts an exited app process (and allows startup after daemon/host restart unless explicitly stopped), not an unhealthy but running one. Monitoring/operator action is still needed.
- The app has no `http.Server.Shutdown` or request-level graceful draining. An exec-form entrypoint delivers container signals directly, but containerization does not add graceful request completion.
- Historical rows created by the previous public-short-link defect may still have `is_private=true` even though the file lives under `public/`. Anonymous access returns 401, while a valid `download` token then reaches the private-path lookup and returns 404. New links now persist the correct value; no existing records are rewritten automatically. Repair an affected row only after reviewing it, for example by setting `is_private=false` for one verified short code.

## Local verification

Build and test only on a development machine with no production configuration mounted:

```sh
GOTOOLCHAIN=go1.26.10 go test ./...
scripts/test-production-gate.sh
docker compose --env-file /dev/null config -q
docker compose --env-file /dev/null -p gofi -f docker-compose.yml -f deploy/compose.production.yml config -q
GOFI_VCS_REF="$(git describe --always --dirty)" GOFI_IMAGE=gofi:verify \
  docker compose --env-file /dev/null -p gofi-build build app
python3 scripts/test-compose.py --image gofi:verify --audit-image
```

The Python 3 standard-library test uses an internally generated project name, temporary TOML/storage, an independent named DB volume, and random loopback ports. `scripts/test-production-gate.sh` is a stub-only negative test proving that a failed gate skips the documented backup/chown/app-start chain. It checks the non-root identity, mounts, health, public/private auth, uploads/downloads, the persisted short-link privacy value, app-recreation persistence, and final image contents. A one-off root fixture helper adjusts **only temporary test files** for rootful/rootless ID mapping. The script removes only its own generated project and test volume; this is not a production cleanup recipe. If Docker is unavailable or inaccessible, image building and integration remain **unverified** even if Go tests and Compose parsing pass.

## License

[MIT](LICENSE).
