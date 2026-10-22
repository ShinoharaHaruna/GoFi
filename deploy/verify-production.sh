#!/bin/sh
set -eu

usage() {
    echo "Usage: $0 [--stopped|--permissions-ready]" >&2
    exit 2
}

mode=resolved
if [ "$#" -gt 1 ]; then
    usage
fi
if [ "$#" -eq 1 ]; then
    case "$1" in
        --stopped|--permissions-ready) mode=${1#--} ;;
        *) usage ;;
    esac
fi

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

require_line() {
    haystack=$1
    needle=$2
    description=$3
    matches=$(printf '%s\n' "$haystack" | grep -Fxc "$needle" || true)
    [ "$matches" -eq 1 ] || fail "$description (expected one '$needle', got $matches)"
}

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
project_root=$(CDPATH= cd -- "$script_dir/.." && pwd -P)
expected_project=gofi
expected_container=gofi-db-1
expected_volume=gofi_gofi_db_data
expected_db_image=postgres:17-bookworm
expected_postgres=${GOFI_EXPECTED_POSTGRES_VERSION:-17.6}
expected_config=$(realpath "$project_root/deploy/config.toml") || fail "cannot resolve the container config path"
expected_data=$(realpath "$project_root/data") || fail "cannot resolve the upload directory path"

[ -f "$expected_config" ] || fail "container config is missing: $expected_config; create it from deploy/config.toml.template"
[ -d "$expected_data" ] || fail "upload directory is missing: $expected_data"
[ -d "$expected_data/public" ] || fail "public upload directory is missing: $expected_data/public"
[ -d "$expected_data/private" ] || fail "private upload directory is missing: $expected_data/private"

[ "${GOFI_CONFIG_FILE:-}" = "$expected_config" ] || fail "GOFI_CONFIG_FILE must be exported as the canonical resolved path $expected_config"
[ "${GOFI_DATA_DIR:-}" = "$expected_data" ] || fail "GOFI_DATA_DIR must be exported as the canonical resolved path $expected_data"
for variable in GOFI_APP_PUBLISH GOFI_DB_PUBLISH; do
    [ -z "$(printenv "$variable" 2>/dev/null || true)" ] || fail "$variable must be unset; production must resolve to the documented public app port and loopback DB port"
done

expected_revision=$(git -C "$project_root" rev-parse --verify --short HEAD) || fail "cannot resolve the checked-out production commit"
expected_image=gofi:$expected_revision
[ "${GOFI_IMAGE:-}" = "$expected_image" ] || fail "GOFI_IMAGE must be exactly $expected_image for this checkout; do not retag a different image"

git -C "$project_root" diff --quiet || fail "tracked working-tree changes found; source/image revision cannot be trusted"
git -C "$project_root" diff --cached --quiet || fail "staged changes found; source/image revision cannot be trusted"
unexpected_source=$(git -C "$project_root" status --porcelain --untracked-files=all)
[ -z "$unexpected_source" ] || fail "uncommitted source tree entries found:\n$unexpected_source"

rendered=$(docker compose --env-file /dev/null -p "$expected_project" \
    -f "$project_root/docker-compose.yml" \
    -f "$project_root/deploy/compose.production.yml" config) || fail "production Compose model did not resolve"
app_model=$(printf '%s\n' "$rendered" | sed -n '/^  app:$/,/^  db:$/p' | sed '$d')
db_model=$(printf '%s\n' "$rendered" | sed -n '/^  db:$/,/^networks:$/p' | sed '$d')
volume_model=$(printf '%s\n' "$rendered" | sed -n '/^volumes:$/,$p')

require_line "$rendered" "name: $expected_project" "resolved project name changed"
require_line "$app_model" "    image: $expected_image" "app image does not match the checked-out commit"
require_line "$db_model" "    image: $expected_db_image" "database image changed"

mount_value() {
    target=$1
    printf '%s\n' "$app_model" | awk -v wanted="$target" '
        $1 == "-" && $2 == "type:" {
            in_mount = 1; source = ""; target = ""; read_only = "false"; create_host_path = ""
        }
        in_mount && $1 == "source:" { source = $2 }
        in_mount && $1 == "target:" { target = $2 }
        in_mount && $1 == "read_only:" { read_only = $2 }
        in_mount && $1 == "create_host_path:" {
            create_host_path = $2
            if (target == wanted) {
                printf "%s|%s|%s", source, read_only, create_host_path
                found = 1
            }
            in_mount = 0
        }
        END { if (!found) exit 1 }
    '
}

config_mount=$(mount_value /app/config.toml) || fail "resolved app config mount is missing"
[ "$config_mount" = "$expected_config|true|false" ] || fail "resolved app config mount differs: $config_mount"
data_mount=$(mount_value /app/data) || fail "resolved app data mount is missing"
[ "$data_mount" = "$expected_data|false|false" ] || fail "resolved app data mount differs: $data_mount"

app_ports=$(printf '%s\n' "$app_model" | awk '
    /^    ports:/ { in_ports = 1; next }
    in_ports && /^      -/ { host_ip = ""; target = ""; published = "" }
    in_ports && $1 == "host_ip:" { host_ip = $2 }
    in_ports && $1 == "target:" { target = $2 }
    in_ports && $1 == "published:" {
        gsub(/"/, "", $2); published = $2
        printf "%s|%s|%s", host_ip, target, published
    }
')
[ "$app_ports" = "|8080|8080" ] || fail "resolved app publication is not public 8080:8080: $app_ports"

db_ports=$(printf '%s\n' "$db_model" | awk '
    /^    ports:/ { in_ports = 1; next }
    in_ports && /^      -/ { host_ip = ""; target = ""; published = "" }
    in_ports && $1 == "host_ip:" { host_ip = $2 }
    in_ports && $1 == "target:" { target = $2 }
    in_ports && $1 == "published:" {
        gsub(/"/, "", $2); published = $2
        printf "%s|%s|%s", host_ip, target, published
    }
')
[ "$db_ports" = "127.0.0.1|5432|54320" ] || fail "resolved database publication is not loopback 54320:5432: $db_ports"

require_line "$volume_model" "  gofi_db_data:" "production logical volume key changed"
require_line "$volume_model" "    name: $expected_volume" "resolved physical database volume changed"
require_line "$volume_model" "    external: true" "database volume is not external"
! printf '%s\n' "$app_model" | grep -Eq '^[[:space:]]+host_ip:' || fail "app publication unexpectedly includes a host bind address"

grep -Eq '^[[:space:]]*GIN_MODE[[:space:]]*=[[:space:]]*"release"' "$expected_config" || fail "container config must set GIN_MODE=release"
grep -Eq '^[[:space:]]*GOFI_PORT[[:space:]]*=[[:space:]]*"8080"' "$expected_config" || fail "container config must keep internal GOFI_PORT=8080"
grep -Eq '^[[:space:]]*GOFI_BASE_DIR[[:space:]]*=[[:space:]]*"/app/data"' "$expected_config" || fail "container config must use /app/data"
grep -Eq '^[[:space:]]*DATABASE_URL[[:space:]]*=.*".*@db:5432/' "$expected_config" || fail "container DATABASE_URL must target db:5432"
! grep -Eiq '@(localhost|127\.0\.0\.1)(:[0-9]+)?/' "$expected_config" || fail "container DATABASE_URL must not use localhost inside the app container"

docker volume inspect "$expected_volume" >/dev/null || fail "existing database volume is missing: $expected_volume"
actual_mount=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}' "$expected_container") || fail "cannot inspect $expected_container"
[ "$actual_mount" = "$expected_volume" ] || fail "$expected_container uses volume '$actual_mount', expected '$expected_volume'"
health_status=$(docker inspect --format '{{.State.Health.Status}}' "$expected_container") || fail "cannot inspect $expected_container health"
[ "$health_status" = healthy ] || fail "$expected_container is not healthy ($health_status)"
postgres_version=$(docker exec "$expected_container" postgres --version) || fail "cannot read PostgreSQL version"
printf '%s\n' "$postgres_version" | grep -F "PostgreSQL) $expected_postgres" >/dev/null || fail "unexpected PostgreSQL version: $postgres_version (expected $expected_postgres)"

image_user=$(docker image inspect --format '{{.Config.User}}' "$expected_image") || fail "image is not loaded locally: $expected_image"
[ "$image_user" = "10001:10001" ] || fail "image user changed: $image_user"
image_revision=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$expected_image") || fail "cannot inspect image revision: $expected_image"
[ "$image_revision" = "$expected_revision" ] || fail "image revision '$image_revision' does not match HEAD '$expected_revision'; rebuild after commit, never retag"
image_entrypoint=$(docker image inspect --format '{{json .Config.Entrypoint}}' "$expected_image") || fail "cannot inspect image entrypoint: $expected_image"
[ "$image_entrypoint" = '["/app/GoFi","-config","/app/config.toml"]' ] || fail "image entrypoint changed: $image_entrypoint"

if [ "$mode" != resolved ]; then
    app_containers=$(docker ps -aq --filter "label=com.docker.compose.project=$expected_project" --filter "label=com.docker.compose.service=app") || fail "cannot list existing app containers"
    [ -z "$app_containers" ] || fail "a Compose app container already exists; remove it explicitly before migration"
    command -v ss >/dev/null || fail "ss is required to verify host port 8080 is free"
    ! ss -H -ltn | awk '{ split($4, address, ":"); if (address[length(address)] == "8080") found = 1 } END { exit found ? 0 : 1 }' \
        || fail "host port 8080 is still listening; stop the old screen process and all writers first"
fi

if [ "$mode" = permissions-ready ]; then
    unexpected=$(find "$expected_data" -xdev \( ! -user 10001 -o ! -group 10001 \) -print -quit)
    [ -z "$unexpected" ] || fail "unexpected owner below data directory: $unexpected"
    unexpected=$(find "$expected_data" -xdev ! -type f ! -type d -print -quit)
    [ -z "$unexpected" ] || fail "special file below data directory is not supported: $unexpected"
    unexpected=$(find "$expected_data" -xdev -type d ! -perm 0750 -print -quit)
    [ -z "$unexpected" ] || fail "unexpected directory mode: $unexpected"
    unexpected=$(find "$expected_data" -xdev -type f ! -perm 0640 -print -quit)
    [ -z "$unexpected" ] || fail "unexpected file mode: $unexpected"
    [ "$(stat -c '%u:%g:%a' "$expected_config")" = "0:10001:640" ] || fail "container config must be root:10001 mode 0640"
fi

printf 'PASS production inputs resolved: project=%s image=%s revision=%s config=%s data=%s volume=%s db=%s (%s)\n' \
    "$expected_project" "$expected_image" "$expected_revision" "$expected_config" "$expected_data" \
    "$expected_volume" "$expected_container" "$postgres_version"
