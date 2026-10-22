#!/bin/sh
# Prove that a failed production gate prevents every later operation in the
# documented command chain. This uses only temporary stubs; it does not need
# Docker, a database, or production files.
set -eu

fixture=$(mktemp -d "${TMPDIR:-/tmp}/gofi-gate-test.XXXXXX")
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
mkdir -p "$fixture/deploy"

cat > "$fixture/deploy/verify-production.sh" <<'EOF'
#!/bin/sh
exit 42
EOF
chmod 0755 "$fixture/deploy/verify-production.sh"

marker=$fixture/stubs-invoked
backup_stub() { printf 'backup\n' >> "$marker"; }
chown_stub() { printf 'chown\n' >> "$marker"; }
pc() { printf 'pc %s\n' "$*" >> "$marker"; }

set +e
(
    cd "$fixture" &&
    deploy/verify-production.sh --permissions-ready &&
      backup_stub &&
      chown_stub &&
      pc up -d --no-deps --no-build --pull never --wait --wait-timeout 120 app
)
status=$?
set -e

[ "$status" -eq 42 ] || {
    echo "FAIL: gate chain returned $status, expected 42" >&2
    exit 1
}
[ ! -e "$marker" ] || {
    echo "FAIL: stubs ran after gate failure:" >&2
    cat "$marker" >&2
    exit 1
}

echo "PASS failed gate skipped backup, chown, and app-start stubs"
