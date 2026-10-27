#!/usr/bin/env python3
"""Check the base Compose deployment using only disposable fixtures.

Usage: python3 scripts/test-compose.py --image gofi:local [--audit-image]

Requires a prebuilt local application image, Docker Compose v2 with --wait,
Linux bind mounts, and a daemon whose bind paths and published loopback ports
are accessible from this host. DOCKER_HOST and other Docker settings remain
inherited. Only the development PostgreSQL image may be pulled; nothing is
built, pushed, or run with the production overlay.

Limitations: /health is liveness, not database/storage readiness. Recreation
covers only app replacement, not database recovery, host restarts, TLS, or
production hardening. Log rotation assertions cover app, not the uncapped db
service in the base Compose file. The optional audit checks filesystem names
in the merged image and all final-image layers, not file contents, builder
stages, or external build caches; system files and CA certificates are normal.
Oversized audits are explicitly skipped. Cleanup handles ordinary failures and
SIGINT/SIGTERM, but SIGKILL, power loss, or an unavailable daemon can leave the
reported disposable project/fixture for manual cleanup.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import posixpath
import re
import secrets
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import urllib.error
import urllib.parse
import urllib.request
import uuid


APP_ENTRYPOINT = ["/app/GoFi", "-config", "/app/config.toml"]
DB_IMAGE = "postgres:17-bookworm"
DB_URL = "postgres://gofi_user:gofi_local_dev@db:5432/gofi_db?sslmode=disable"
PROJECT_LABEL = "com.docker.compose.project"
AUX_LABEL = "io.gofi.integration-project"
MIB = 1024 * 1024


class CheckError(Exception):
    pass


class Interrupted(CheckError):
    def __init__(self, signum):
        super().__init__("Interrupted by signal {}".format(signum))
        self.signum = signum


def require(condition, message):
    # Assertions must remain active when the caller uses python -O.
    if not condition:
        raise CheckError(message)


def passed(message):
    print("PASS " + message, flush=True)


class NoRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, url):
        # A redirect must not leak a fixture token or hide an incorrect route.
        return None


class Checker:
    def __init__(self, args):
        self.args = args
        self.repo = Path(__file__).resolve().parent.parent
        self.project = "gofi-test-" + str(uuid.uuid4())
        self.volume_name = self.project + "_gofi_db_data"
        self.network_name = self.project + "_default"
        self.env = {
            key: value for key, value in os.environ.items()
            if not key.startswith(("GOFI_", "COMPOSE_"))
        }
        self.keys = {kind: secrets.token_hex(32) for kind in ("upload", "download", "shorten", "list", "delete")}
        self.wrong_key = secrets.token_hex(32)
        self.redactions = list(self.keys.values()) + [self.wrong_key, "gofi_local_dev"]
        self.temporary = None
        self.fixture = None
        self.fixture_owner = None
        self.host_owner = None
        self.compose_prefix = None
        self.auxiliary = set()
        self.logs_shown = False
        self.failures = []
        self.image = None
        self.image_id = None
        self.base_url = None
        # Environment proxies must not receive HTTP requests carrying test keys.
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirects())

    def redact(self, value):
        if isinstance(value, bytes):
            value = value.decode("utf-8", errors="replace")
        for secret in self.redactions:
            value = value.replace(secret, "<redacted>")
        return value

    def command(self, argv, *, stdin=None, timeout=60, check=True):
        try:
            result = subprocess.run(
                argv, input=stdin, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                text=True, encoding="utf-8", errors="replace", env=self.env,
                cwd=str(self.repo), timeout=timeout, check=False,
            )
        except subprocess.TimeoutExpired as error:
            output = self.redact(error.stdout or "") + self.redact(error.stderr or "")
            raise CheckError("Command timed out: {}\n{}".format(" ".join(argv), output[-12000:])) from None
        except OSError as error:
            raise CheckError("Cannot run {}: {}; check Docker/Compose installation".format(argv[0], error)) from None
        if check and result.returncode:
            output = self.redact(result.stdout + result.stderr)
            raise CheckError("Command failed ({}): {}\n{}".format(
                result.returncode, " ".join(argv), output[-12000:],
            ))
        return result

    def compose(self, *args, **kwargs):
        require(self.compose_prefix is not None, "Fixture environment is not initialized")
        require(re.fullmatch(r"gofi-test-[0-9a-f-]{36}", self.project), "Unsafe project name")
        return self.command(self.compose_prefix + list(args), **kwargs)

    def inspect(self, kind, name):
        result = self.command(["docker", kind, "inspect", name])
        objects = json.loads(result.stdout)
        require(isinstance(objects, list) and len(objects) == 1, "Expected one inspected " + kind)
        return objects[0]

    def inspect_image(self):
        # This is deliberately the first Docker command: --image never triggers a pull/build.
        self.image = self.inspect("image", self.args.image)
        self.image_id = self.image.get("Id", "")
        require(re.fullmatch(r"sha256:[0-9a-f]{64}", self.image_id), "Invalid local image ID")
        config = self.image.get("Config", {})
        require(config.get("User") == "10001:10001", "Image must default to USER 10001:10001")
        require(config.get("Entrypoint") == APP_ENTRYPOINT, "Image entrypoint must use the fixed config flag")
        require(not config.get("Cmd"), "Image must not append application arguments")
        require(not config.get("Volumes"), "Image-declared volumes could hide files from the image audit")
        revision = (config.get("Labels") or {}).get("org.opencontainers.image.revision")
        require(isinstance(revision, str) and revision, "Image lacks a build revision label")
        expected_version = (self.repo / "VERSION").read_text().strip() + "-" + revision
        missing_config = "/app/definitely-missing-config.toml"
        result = self.command([
            "docker", "run", "--rm", "--pull", "never", "--network", "none",
            "--entrypoint", "/app/GoFi", self.image_id, "-config", missing_config,
        ], check=False)
        require(result.returncode != 0 and "Starting GoFi, version: " + expected_version in result.stderr,
                "Binary version does not match VERSION plus the image revision label")
        passed("prebuilt local image inspected; application image ID, non-root defaults, and version injection pinned")

    def make_fixture(self):
        # Ignore TMPDIR so caller-controlled environment cannot place writes in repository state.
        temp_base = Path("/tmp").resolve()
        require(temp_base != self.repo and self.repo not in temp_base.parents, "System temp directory is inside the repository")
        self.temporary = tempfile.TemporaryDirectory(prefix=self.project + "-", dir=str(temp_base))
        self.fixture = Path(self.temporary.name).resolve()
        stat = self.fixture.stat()
        self.host_owner = (stat.st_uid, stat.st_gid)
        self.template = (self.repo / "deploy/config.toml.template").read_bytes()
        text = self.template.decode("utf-8")
        defaults = {
            "GIN_MODE": "release", "GOFI_PORT": "8080",
            "GOFI_BASE_DIR": "/app/data", "DATABASE_URL": DB_URL,
        }
        for key, value in defaults.items():
            matches = re.findall(r'^\s*' + key + r'\s*=\s*"([^"\n]*)"\s*(?:#.*)?$', text, re.MULTILINE)
            require(matches == [value], "Template no longer has the expected development default for " + key)
        (self.fixture / "config.toml").write_bytes(self.template)
        for relative in ("data", "data/public", "data/private"):
            (self.fixture / relative).mkdir(mode=0o750)
        # Pinning the inspected ID prevents tag races and Compose's special latest-tag pull behavior.
        self.env.update({
            "GOFI_IMAGE": self.image_id,
            "GOFI_CONFIG_FILE": str(self.fixture / "config.toml"),
            "GOFI_DATA_DIR": str(self.fixture / "data"),
            "GOFI_APP_PUBLISH": "127.0.0.1:0",
            "GOFI_DB_PUBLISH": "127.0.0.1:0",
        })
        self.compose_prefix = [
            "docker", "compose", "--env-file", "/dev/null", "--project-name", self.project,
            "--project-directory", str(self.repo), "-f", str(self.repo / "docker-compose.yml"),
        ]

    def helper(self, script, *args):
        require(self.fixture is not None, "No temporary fixture to mount")
        name = self.project + "-fixture"
        self.auxiliary.add(name)
        result = self.command([
            "docker", "run", "--rm", "--pull", "never", "--network", "none", "--user", "0:0",
            "--name", name, "--label", AUX_LABEL + "=" + self.project,
            "--entrypoint", "/bin/sh", "--mount", "type=bind,source={},target=/fixture".format(self.fixture),
            self.image_id, "-ec", script, "fixture-helper",
        ] + list(args))
        self.auxiliary.discard(name)
        return result.stdout.strip()

    def prepare_fixture(self):
        # The helper's view, not host numeric IDs, is what chown needs under rootless Docker.
        owner = self.helper("stat -c '%u:%g' /fixture")
        require(re.fullmatch(r"[0-9]+:[0-9]+", owner), "Cannot determine fixture ownership inside Docker")
        self.fixture_owner = owner
        self.helper(
            "chown 10001:10001 /fixture/config.toml /fixture/data /fixture/data/public /fixture/data/private\n"
            "chmod 0640 /fixture/config.toml\n"
            "chmod 0750 /fixture/data /fixture/data/public /fixture/data/private"
        )
        passed("temporary config/data prepared for UID/GID 10001; original namespace ownership recorded")

    def validate_model(self):
        model = json.loads(self.compose("config", "--format", "json").stdout)
        require(model.get("name") == self.project, "Compose project name changed")
        require(set(model.get("services", {})) == {"app", "db"}, "Expected only base app/db services")
        require(not model.get("secrets") and not model.get("configs"), "Unexpected external Compose configuration")
        app, db = model["services"]["app"], model["services"]["db"]
        require(app.get("image") == self.image_id and db.get("image") == DB_IMAGE, "Unexpected service images")
        require(not app.get("command") and not app.get("entrypoint"), "Compose overrides the fixed entrypoint")
        require(not app.get("environment"), "Base app must read only the fixture configuration")
        require(app.get("user", "10001:10001") == "10001:10001", "Compose overrides the non-root image user")
        for service, target in ((app, 8080), (db, 5432)):
            ports = service.get("ports", [])
            require(len(ports) == 1 and ports[0].get("host_ip") == "127.0.0.1"
                    and str(ports[0].get("published")) == "0" and ports[0].get("target") == target,
                    "Compose must publish only an ephemeral loopback port")
            require(not service.get("container_name") and not service.get("network_mode"), "Service bypasses project isolation")
            require(not service.get("privileged"), "Unexpected privileged service")
        mounts = app.get("volumes", [])
        require(len(mounts) == 2, "App must have only fixture config/data mounts")
        for target, source, read_only in (
            ("/app/config.toml", self.fixture / "config.toml", True),
            ("/app/data", self.fixture / "data", False),
        ):
            matching = [mount for mount in mounts if mount.get("target") == target]
            require(len(matching) == 1, "Missing fixture mount: " + target)
            mount = matching[0]
            require(mount.get("type") == "bind" and mount.get("source") == str(source)
                    and bool(mount.get("read_only", False)) == read_only
                    and mount.get("bind", {}).get("create_host_path") is False,
                    "Unsafe fixture mount: " + target)
        require(db.get("environment") == {
            "POSTGRES_DB": "gofi_db", "POSTGRES_USER": "gofi_user", "POSTGRES_PASSWORD": "gofi_local_dev",
        }, "Database development defaults changed")
        db_mounts = db.get("volumes", [])
        require(len(db_mounts) == 1 and db_mounts[0].get("type") == "volume"
                and db_mounts[0].get("source") == "gofi_db_data"
                and db_mounts[0].get("target") == "/var/lib/postgresql/data", "Unexpected database storage")
        volumes = model.get("volumes", {})
        require(set(volumes) == {"gofi_db_data"} and volumes["gofi_db_data"].get("name") == self.volume_name
                and not volumes["gofi_db_data"].get("external"), "Database volume is not project-scoped")
        networks = model.get("networks", {})
        require(set(networks) == {"default"} and networks["default"].get("name") == self.network_name
                and not networks["default"].get("external"), "Network is not project-scoped")
        passed("base Compose resolves only fixture mounts, ephemeral loopback ports, and project storage")

    def start(self):
        result = self.command(["docker", "image", "inspect", DB_IMAGE], check=False)
        if result.returncode:
            require("No such image" in result.stderr, "Cannot inspect PostgreSQL image: " + self.redact(result.stderr))
            self.compose("pull", "db", timeout=600)
        # Explicit db-only pulling keeps the application off every registry path.
        self.compose("up", "-d", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "120", timeout=180)
        passed("isolated app/db started without building or pulling the application")

    def service_id(self, service):
        ids = self.compose("ps", "--all", "--quiet", service).stdout.split()
        require(len(ids) == 1 and re.fullmatch(r"[0-9a-f]{12,64}", ids[0]), "Expected one " + service + " container")
        return ids[0]

    def check_container(self, service):
        container_id = self.service_id(service)
        info = self.inspect("container", container_id)
        labels = info.get("Config", {}).get("Labels", {})
        require(labels.get(PROJECT_LABEL) == self.project and labels.get("com.docker.compose.service") == service,
                "Container does not belong to the isolated project")
        require(info.get("State", {}).get("Running") is True, service + " is not running")
        require(info.get("State", {}).get("Health", {}).get("Status") == "healthy", service + " is not healthy")
        require(set(info.get("NetworkSettings", {}).get("Networks", {})) == {self.network_name}, "Unexpected container network")
        return container_id, info

    def published_port(self, info, port):
        bindings = info.get("NetworkSettings", {}).get("Ports", {}).get(str(port) + "/tcp", [])
        require(len(bindings) == 1 and bindings[0].get("HostIp") == "127.0.0.1", "Unexpected published address")
        published = bindings[0].get("HostPort", "")
        require(published.isdigit() and 0 < int(published) <= 65535, "No ephemeral published port")
        return int(published)

    def inspect_deployment(self):
        app_id, app = self.check_container("app")
        db_id, db = self.check_container("db")
        require(app.get("Image") == self.image_id, "App does not use the inspected image")
        config, host = app["Config"], app["HostConfig"]
        require(config.get("User") == "10001:10001", "App user is not fixed to 10001:10001")
        require(config.get("Entrypoint") == APP_ENTRYPOINT and not config.get("Cmd")
                and app.get("Path") == APP_ENTRYPOINT[0] and app.get("Args") == APP_ENTRYPOINT[1:],
                "App startup flags changed")
        require(not host.get("Privileged") and "ALL" in host.get("CapDrop", [])
                and "no-new-privileges:true" in host.get("SecurityOpt", []), "App security restrictions changed")
        log = host.get("LogConfig", {})
        require(log.get("Type") == "json-file" and log.get("Config", {}).get("max-size") == "10m"
                and log.get("Config", {}).get("max-file") == "3", "App logs are not bounded to 10m x 3")
        mounts = app.get("Mounts", [])
        require(len(mounts) == 2, "Unexpected runtime app mounts")
        for destination, source, writable in (
            ("/app/config.toml", self.fixture / "config.toml", False),
            ("/app/data", self.fixture / "data", True),
        ):
            matches = [mount for mount in mounts if mount.get("Destination") == destination]
            require(len(matches) == 1 and matches[0].get("Type") == "bind"
                    and matches[0].get("Source") == str(source) and matches[0].get("RW") is writable,
                    "Runtime mount differs from fixture: " + destination)
        for flag in ("-u", "-g"):
            value = self.command(["docker", "exec", app_id, "id", flag]).stdout.strip()
            require(value == "10001", "Running app UID/GID differs from 10001")
        ownership = self.command([
            "docker", "exec", app_id, "stat", "-c", "%u:%g:%a",
            "/app/config.toml", "/app/data", "/app/data/public", "/app/data/private",
        ]).stdout.splitlines()
        require(ownership == ["10001:10001:640"] + ["10001:10001:750"] * 3, "Fixture ownership/modes changed")
        digest = self.command(["docker", "exec", app_id, "sha256sum", "/app/config.toml"]).stdout.split()
        require(digest and digest[0] == hashlib.sha256(self.template).hexdigest(), "Mounted config differs from the unchanged template")
        db_mounts = db.get("Mounts", [])
        require(len(db_mounts) == 1 and db_mounts[0].get("Type") == "volume"
                and db_mounts[0].get("Name") == self.volume_name
                and db_mounts[0].get("Destination") == "/var/lib/postgresql/data",
                "Database is not using the isolated project volume")
        volume = self.inspect("volume", self.volume_name)
        labels = volume.get("Labels") or {}
        require(volume.get("Name") == self.volume_name and labels.get(PROJECT_LABEL) == self.project
                and labels.get("com.docker.compose.volume") == "gofi_db_data", "Database volume labels do not match the project")
        app_port = self.published_port(app, 8080)
        require(app_port != self.published_port(db, 5432), "App/db host ports collided")
        self.base_url = "http://127.0.0.1:" + str(app_port)
        passed("runtime user/config/flags, fixture binds, app log limits, and unique database volume verified")
        return app_id, db_id

    def psql(self, sql):
        # SQL stays on stdin, never in process arguments or normal output.
        return self.compose(
            "exec", "-T", "db", "psql", "-X", "--quiet", "--no-align", "--tuples-only",
            "--set", "ON_ERROR_STOP=1", "--username", "gofi_user", "--dbname", "gofi_db", stdin=sql,
        ).stdout.strip()

    def seed_keys(self):
        rows = ["(NOW(), NOW(), '{}', '{}', TRUE)".format(key, kind) for kind, key in self.keys.items()]
        self.psql('BEGIN;\nINSERT INTO api_keys (created_at, updated_at, "key", type, is_enabled) VALUES\n'
                  + ",\n".join(rows) + ";\nCOMMIT;\n")
        self.check_keys()
        passed("randomized upload/download/shorten/list/delete keys seeded only in the disposable database")

    def check_keys(self):
        conditions = ["(\"key\" = '{}' AND type = '{}')".format(key, kind) for kind, key in self.keys.items()]
        found = self.psql("SELECT type FROM api_keys WHERE deleted_at IS NULL AND is_enabled = TRUE AND ("
                          + " OR ".join(conditions) + ") ORDER BY type;\n")
        require(found.splitlines() == ["delete", "download", "list", "shorten", "upload"],
                "Seeded API keys did not persist unchanged")

    def request(self, method, path, expected, label, *, key=None, body=None, content_type=None, headers=None):
        require(path.startswith("/") and not path.startswith("//"), "Unsafe response path")
        parts = urllib.parse.urlsplit(path)
        require(not parts.scheme and not parts.netloc and not parts.fragment, "Response path is not local")
        request_headers = dict(headers or {})
        if key is not None:
            request_headers["Authorization"] = "Bearer " + key
        if content_type:
            request_headers["Content-Type"] = content_type
        request = urllib.request.Request(self.base_url + path, data=body, headers=request_headers, method=method)
        try:
            try:
                response = self.http.open(request, timeout=10)
            except urllib.error.HTTPError as error:
                response = error
            with response:
                status = response.code
                data = response.read(MIB + 1)
        except (urllib.error.URLError, OSError) as error:
            raise CheckError(label + ": HTTP request failed: " + self.redact(str(error))) from None
        require(status == expected, "{}: expected HTTP {}, got {}".format(label, expected, status))
        require(len(data) <= MIB, label + ": unexpected oversized response")
        return data

    def json_request(self, method, path, expected, label, *, payload=None, **kwargs):
        if payload is not None:
            kwargs.update(body=json.dumps(payload).encode("utf-8"), content_type="application/json")
        body = self.request(method, path, expected, label, **kwargs)
        try:
            result = json.loads(body)
        except (ValueError, UnicodeError):
            raise CheckError(label + ": expected a JSON response") from None
        require(isinstance(result, dict), label + ": expected a JSON object")
        return result

    def health(self):
        result = self.json_request("GET", "/health", 200, "health")
        require(result == {"status": "UP"}, "Health body must be exactly the UP status object")

    def upload(self, filename, data, *, key, public=False, expected=200):
        boundary = "gofi-" + uuid.uuid4().hex
        body = ("--{}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{}\"\r\n"
                "Content-Type: application/octet-stream\r\n\r\n".format(boundary, filename)).encode("ascii")
        body += data + ("\r\n--{}--\r\n".format(boundary)).encode("ascii")
        headers = {"X-GoFi-Target-Dir": "public"} if public else {}
        result = self.json_request(
            "POST", "/upload", expected, "public upload" if public else "default-private upload",
            key=key, body=body, content_type="multipart/form-data; boundary=" + boundary, headers=headers,
        )
        if expected == 200:
            require(result == {"download_path": "/" + filename}, "Upload returned an unexpected download path")
        return "/" + filename

    def check_download(self, path, data, *, private):
        if not private:
            require(self.request("GET", path, 200, "anonymous public download") == data, "Public bytes differ")
            return
        for key in (None, self.wrong_key, self.keys["upload"], self.keys["shorten"]):
            self.json_request("GET", path, 401, "private download rejects missing/wrong/type token", key=key)
        invalid_query = path + "?" + urllib.parse.urlencode({"token": self.wrong_key})
        self.json_request("GET", invalid_query, 401, "private download rejects wrong query token")
        require(self.request("GET", path, 200, "private header download", key=self.keys["download"]) == data,
                "Private header download bytes differ")
        query = path + "?" + urllib.parse.urlencode({"token": self.keys["download"]})
        require(self.request("GET", query, 200, "private query download") == data, "Private query download bytes differ")

    def shorten(self, filename, key, expected=200):
        result = self.json_request("POST", "/shorten", expected, "shorten authorization",
                                   payload={"filename": filename}, key=key)
        if expected != 200:
            return None
        path = result.get("short_url_path")
        require(isinstance(path, str) and re.fullmatch(r"/s/[A-Za-z0-9]+", path), "Invalid local short URL path")
        return path

    def toggle_short(self, path, data, *, private):
        endpoint = "/shorten/" + path.rsplit("/", 1)[1]
        for method, action, expected_message in (
            ("DELETE", endpoint, "Short link disabled"),
            ("POST", endpoint + "/enable", "Short link enabled"),
        ):
            for key in (None, self.wrong_key, self.keys["upload"], self.keys["download"]):
                self.json_request(method, action, 401, "short toggle rejects missing/wrong/type token", key=key)
            result = self.json_request(method, action, 200, "authorized short toggle", key=self.keys["shorten"])
            require(result == {"message": expected_message}, "Unexpected short toggle result")
            if method == "DELETE":
                result = self.json_request("GET", path, 404, "disabled short download", key=self.keys["download"])
                require(result == {"error": "Short link is disabled"}, "Disabled short link was not retained")
        self.check_download(path, data, private=private)

    def check(self, label, operation):
        # Behavioral mismatches must not hide independent persistence or liveness failures.
        try:
            operation()
        except Interrupted:
            raise
        except Exception as error:
            self.failures.append(self.redact(label + ": " + str(error)))
            print("FAIL " + self.failures[-1], file=sys.stderr, flush=True)
            return False
        passed(label)
        return True

    def exercise_api(self):
        self.check("/health returns exactly HTTP 200 with status UP", self.health)
        self.files = {}
        for private in (False, True):
            def upload_case(private=private):
                filename = ("private-" if private else "public-") + uuid.uuid4().hex + ".bin"
                data = b"GoFi isolated fixture\n\x00\xff" + secrets.token_bytes(128)
                for key in (None, self.wrong_key, self.keys["download"], self.keys["shorten"]):
                    self.upload(filename, data, key=key, public=not private, expected=401)
                self.upload(filename, data, key=self.keys["upload"], public=not private)
                self.files[private] = (filename, data)
                self.check_download("/" + filename, data, private=private)
            self.check("private upload/authenticated header/query download" if private else "public upload/anonymous download", upload_case)
        self.shorts = {}
        for private, (filename, data) in self.files.items():
            label = "private" if private else "public"
            def short_case(private=private, filename=filename, data=data):
                for key in (None, self.wrong_key, self.keys["upload"], self.keys["download"]):
                    self.shorten(filename, key, expected=401)
                path = self.shorten(filename, self.keys["shorten"])
                self.shorts[private] = path
                shortcode = path.rsplit("/", 1)[1]
                stored = self.psql(
                    "SELECT CASE WHEN is_private THEN 'private' ELSE 'public' END "
                    "FROM short_links WHERE short_code = '{}';\n".format(shortcode)
                )
                require(stored == ("private" if private else "public"),
                        "Persisted short-link privacy differs from the uploaded file")
                self.check_download(path, data, private=private)
            self.check(label + " short-link authorization/download", short_case)
            if private in self.shorts:
                self.check(label + " shorten-only disable/re-enable", lambda private=private, data=data:
                           self.toggle_short(self.shorts[private], data, private=private))
        self.check("list key returns a JSON file listing with dir filter", self.list_case)
        self.check("delete key removes files and disables their short links", self.delete_case)

    def list_case(self):
        for key in (None, self.wrong_key, self.keys["upload"], self.keys["download"],
                    self.keys["shorten"], self.keys["delete"]):
            self.json_request("GET", "/files", 401, "list rejects missing/wrong/type token", key=key)
        result = self.json_request("GET", "/files", 200, "authorized list", key=self.keys["list"])
        files = result.get("files")
        require(isinstance(files, list), "List response lacks a files array")
        found = {(entry.get("name"), entry.get("dir")) for entry in files if isinstance(entry, dict)}
        expected = {(name, "private" if private else "public") for private, (name, _) in self.files.items()}
        require(expected <= found, "List response is missing uploaded files")
        for entry in files:
            require(isinstance(entry.get("size"), int) and isinstance(entry.get("mod_time"), str),
                    "File entry lacks size or mod_time")
        filtered = self.json_request("GET", "/files?dir=public", 200, "public-only list", key=self.keys["list"])
        require(all(entry.get("dir") == "public" for entry in filtered["files"]),
                "?dir=public returned non-public entries")
        self.json_request("GET", "/files?dir=bogus", 400, "invalid dir rejected", key=self.keys["list"])

    def delete_case(self):
        missing = "missing-" + uuid.uuid4().hex + ".bin"
        for key in (None, self.wrong_key, self.keys["upload"], self.keys["download"],
                    self.keys["shorten"], self.keys["list"]):
            self.json_request("DELETE", "/" + missing, 401, "delete rejects missing/wrong/type token", key=key)
        self.json_request("DELETE", "/" + missing, 404, "delete reports a missing file", key=self.keys["delete"])

        # The same name in both buckets is ambiguous without an explicit ?dir=.
        name = "ambiguous-" + uuid.uuid4().hex + ".bin"
        data = secrets.token_bytes(64)
        self.upload(name, data, key=self.keys["upload"], public=True)
        self.upload(name, data, key=self.keys["upload"], public=False)
        self.json_request("DELETE", "/" + name, 409, "ambiguous delete rejected", key=self.keys["delete"])
        result = self.json_request("DELETE", "/" + name + "?dir=public", 200, "delete public copy",
                                   key=self.keys["delete"])
        require(result.get("dir") == "public" and result.get("filename") == name,
                "Delete reported the wrong bucket or filename")
        self.json_request("GET", "/" + name, 401, "only the private copy remains")
        result = self.json_request("DELETE", "/" + name, 200, "unambiguous delete omits dir",
                                   key=self.keys["delete"])
        require(result.get("dir") == "private", "Delete reported the wrong bucket")
        self.json_request("GET", "/" + name, 404, "deleted file is gone")
        self.json_request("DELETE", "/" + name, 404, "re-delete reports a missing file", key=self.keys["delete"])

        # Deleting a file soft-disables the enabled short links that pointed at it.
        for private in (False, True):
            fname = ("delete-private-" if private else "delete-public-") + uuid.uuid4().hex + ".bin"
            self.upload(fname, data, key=self.keys["upload"], public=not private)
            path = self.shorten(fname, self.keys["shorten"])
            bucket = "private" if private else "public"
            result = self.json_request("DELETE", "/" + fname + "?dir=" + bucket, 200,
                                       "delete file", key=self.keys["delete"])
            require(result.get("disabled_short_links") == 1,
                    "Expected exactly one disabled short link")
            self.json_request("GET", path, 404, "short link disabled after file deletion",
                              key=self.keys["download"])

    def recreate_app(self, original_app_id, original_db_id):
        self.compose("up", "-d", "--no-deps", "--no-build", "--pull", "never", "--force-recreate",
                     "--wait", "--wait-timeout", "120", "app", timeout=180)
        app_id, db_id = self.inspect_deployment()
        require(app_id != original_app_id, "App container ID did not change on force-recreate")
        require(db_id == original_db_id, "App-only recreation replaced the database")
        passed("app container ID changed; database container and project volume retained")
        self.check("health after app recreation", self.health)
        self.check("same randomized API keys persisted", self.check_keys)
        for private, (filename, data) in self.files.items():
            label = "private" if private else "public"
            self.check(label + " uploaded bytes/auth persisted", lambda private=private, filename=filename, data=data:
                       self.check_download("/" + filename, data, private=private))
            if private in self.shorts:
                self.check(label + " original short URL/auth persisted", lambda private=private, data=data:
                           self.check_download(self.shorts[private], data, private=private))
                self.check(label + " persisted shorten key disables/re-enables original URL", lambda private=private, data=data:
                           self.toggle_short(self.shorts[private], data, private=private))
        # A new upload proves the original upload key works, rather than just existing on disk.
        def upload_again():
            filename, data = "after-recreate-" + uuid.uuid4().hex + ".bin", secrets.token_bytes(128)
            path = self.upload(filename, data, key=self.keys["upload"])
            self.check_download(path, data, private=True)
        self.check("persisted upload/download keys authorize a new private file", upload_again)

    def stop_db_check_liveness(self, db_id):
        self.compose("stop", "--timeout", "10", "db", timeout=45)
        db = self.inspect("container", db_id)
        require(db.get("State", {}).get("Running") is False, "Isolated database did not stop")
        self.health()
        passed("/health stays HTTP 200 UP with db stopped: liveness, not readiness")

    @staticmethod
    def forbidden_image_name(name, is_directory):
        parts = PurePosixPath(name).parts
        if ".." in parts:
            return True
        parts = tuple(part for part in parts if part not in ("/", "."))
        if not parts:
            return False
        # Whiteouts count too: deleting a secret in a later layer does not remove it from earlier layers.
        parts = tuple(part[4:] if part.startswith(".wh.") and part != ".wh..wh..opq" else part for part in parts)
        path = "/".join(parts)
        basename = parts[-1]
        if any(part in {".git", ".gitignore", ".gitattributes", ".gitmodules", "go-build", "node_modules", "__pycache__"}
               for part in parts):
            return True
        if basename in {"go.mod", "go.sum", "go.work", "go.work.sum", "go", "gofmt"} or basename.endswith(".go"):
            return True
        if basename == "config.toml" or basename.startswith("config.toml."):
            return True
        if parts[0] == "app":
            allowed_dirs = {"app", "app/data", "app/data/public", "app/data/private"}
            return not ((is_directory and path in allowed_dirs) or (not is_directory and path == "app/GoFi"))
        if parts[0] in {"src", "source", "workspace", "build", "out", "go", "data", "uploads"}:
            return True
        prefixes = ("usr/local/go", "usr/lib/go", "root/go", "root/.cache", "home/gofi/.cache", "tmp/go-build", "usr/src/")
        if any(path == prefix or path.startswith(prefix.rstrip("/") + "/") for prefix in prefixes):
            return True
        if path.startswith("usr/lib/go-") or "pkg/mod" in path or "uploads" in parts:
            return True
        return False

    def scan_archive(self, archive, label, budget):
        for member in archive:
            budget[0] += max(member.size, 0)
            require(budget[0] <= self.args.audit_max_mib * MIB, label + ": uncompressed audit limit exceeded")
            require(not self.forbidden_image_name(member.name, member.isdir()),
                    label + ": forbidden image artifact: " + member.name)
            if member.issym() or member.islnk():
                target = member.linkname
                if not target.startswith("/") and member.issym():
                    target = posixpath.join(posixpath.dirname(member.name), target)
                target = posixpath.normpath(target)
                require(not self.forbidden_image_name(target, member.isdir()),
                        label + ": link targets a forbidden image artifact: " + member.name)

    def audit_image(self):
        if not self.args.audit_image:
            return
        limit = self.args.audit_max_mib * MIB
        size = self.image.get("Size")
        require(isinstance(size, int) and size > 0, "Image has no usable size for the audit budget")
        if size + 16 * MIB > limit or shutil.disk_usage(self.fixture).free < size + 128 * MIB:
            print("SKIP image audit: archive budget/free space insufficient; increase --audit-max-mib or free disk space", flush=True)
            return
        name = self.project + "-audit"
        self.auxiliary.add(name)
        self.command([
            "docker", "create", "--pull", "never", "--network", "none", "--name", name,
            "--label", AUX_LABEL + "=" + self.project, self.image_id,
        ])
        archive_path = self.fixture / "merged-image.tar"
        try:
            self.command(["docker", "export", "--output", str(archive_path), name], timeout=300)
            require(archive_path.stat().st_size <= limit, "Merged image archive exceeded --audit-max-mib")
            with tarfile.open(str(archive_path), mode="r|*") as archive:
                self.scan_archive(archive, "merged filesystem", [0])
        finally:
            if archive_path.exists():
                archive_path.unlink()
            self.remove_auxiliary(name)
        passed("merged final-image filesystem names audited without starting the app")
        archive_path = self.fixture / "final-image-layers.tar"
        try:
            self.command(["docker", "image", "save", "--output", str(archive_path), self.image_id], timeout=300)
            require(archive_path.stat().st_size <= limit, "Saved final-image archive exceeded --audit-max-mib")
            with tarfile.open(str(archive_path), mode="r:*") as saved:
                with saved.extractfile("manifest.json") as manifest_file:
                    manifest = json.load(manifest_file)
                require(isinstance(manifest, list) and len(manifest) == 1, "Expected exactly one saved final image")
                manifest = manifest[0]
                with saved.extractfile(manifest["Config"]) as config_file:
                    config = json.load(config_file)
                diff_ids = self.image.get("RootFS", {}).get("Layers", [])
                layers = manifest.get("Layers", [])
                require(diff_ids and config.get("rootfs", {}).get("diff_ids") == diff_ids
                        and len(layers) == len(diff_ids), "Saved layers do not match every inspected final-image layer")
                budget = [0]
                for index, layer in enumerate(layers, 1):
                    with saved.extractfile(layer) as layer_file:
                        with tarfile.open(fileobj=layer_file, mode="r|*") as archive:
                            self.scan_archive(archive, "final layer {}".format(index), budget)
            passed("all {} final-image layers audited by name; system files/CAs allowed".format(len(layers)))
        finally:
            if archive_path.exists():
                archive_path.unlink()

    def remove_auxiliary(self, name):
        require(name in {self.project + "-fixture", self.project + "-audit"}, "Unsafe auxiliary container name")
        result = self.command(["docker", "container", "inspect", name], check=False)
        if result.returncode:
            require("No such" in result.stderr, "Cannot inspect disposable helper: " + self.redact(result.stderr))
        else:
            objects = json.loads(result.stdout)
            require(len(objects) == 1 and (objects[0].get("Config", {}).get("Labels") or {}).get(AUX_LABEL) == self.project,
                    "Refusing to remove an auxiliary container without the fixture label")
            self.command(["docker", "rm", "--force", "--volumes", objects[0]["Id"]])
        self.auxiliary.discard(name)

    def show_logs(self):
        if self.logs_shown or self.compose_prefix is None:
            return
        self.logs_shown = True
        print("--- isolated Compose logs (fixture tokens redacted) ---", file=sys.stderr, flush=True)
        try:
            result = self.compose("logs", "--no-color", "--timestamps", "--tail", "120", "app", "db", timeout=30, check=False)
            print(self.redact((result.stdout + result.stderr).strip()) or "(no Compose logs)", file=sys.stderr, flush=True)
        except Exception as error:
            print("Cannot capture isolated logs: " + self.redact(str(error)), file=sys.stderr, flush=True)

    def cleanup(self):
        errors = []

        def attempt(label, operation):
            try:
                operation()
            except Exception as error:
                errors.append(label + ": " + self.redact(str(error)))
                print("CLEANUP FAILED " + errors[-1], file=sys.stderr, flush=True)
                self.show_logs()

        if self.compose_prefix is not None:
            attempt("Compose down", lambda: self.compose("down", "--volumes", "--remove-orphans", "--timeout", "15", timeout=120))
        for name in list(self.auxiliary):
            attempt("remove disposable helper", lambda name=name: self.remove_auxiliary(name))
        if self.fixture_owner is not None:
            # -h/-R prevents an unexpected fixture symlink from changing ownership outside the bind mount.
            attempt("restore fixture ownership", lambda: self.helper('chown -h -R -- "$1" /fixture', self.fixture_owner))
        for name in list(self.auxiliary):
            attempt("remove ownership helper", lambda name=name: self.remove_auxiliary(name))
        if self.fixture is not None and self.fixture.exists():
            def verify_owner():
                stat = (self.fixture / "data").stat() if (self.fixture / "data").exists() else self.fixture.stat()
                require((stat.st_uid, stat.st_gid) == self.host_owner, "Host ownership was not restored; do not chown to host numeric 10001")
            attempt("verify host ownership", verify_owner)
        if self.temporary is not None:
            attempt("remove temporary fixture", self.temporary.cleanup)
        if self.compose_prefix is not None:
            for kind, command in (
                ("containers", ["docker", "ps", "--all", "--quiet", "--filter", "label=" + PROJECT_LABEL + "=" + self.project]),
                ("volumes", ["docker", "volume", "ls", "--quiet", "--filter", "label=" + PROJECT_LABEL + "=" + self.project]),
                ("networks", ["docker", "network", "ls", "--quiet", "--filter", "label=" + PROJECT_LABEL + "=" + self.project]),
            ):
                def verify_removed(command=command, kind=kind):
                    require(not self.command(command).stdout.strip(), "Disposable project still has " + kind)
                attempt("verify project " + kind + " removed", verify_removed)
        if errors:
            print("Cleanup requires attention: project {}; fixture {}. Restore Docker access and remove only these disposable resources."
                  .format(self.project, self.fixture), file=sys.stderr, flush=True)
        elif self.temporary is not None:
            passed("isolated project containers/volumes/network removed; fixture ownership restored and directory deleted")
        return not errors

    def run(self):
        status = 0
        try:
            self.inspect_image()
            self.make_fixture()
            self.validate_model()
            self.prepare_fixture()
            if self.args.audit_image:
                self.check("optional final-image audit completed (see PASS/SKIP milestones)", self.audit_image)
            self.start()
            app_id, db_id = self.inspect_deployment()
            self.seed_keys()
            self.exercise_api()
            self.check("app-only recreation checks completed", lambda: self.recreate_app(app_id, db_id))
            self.check("stopped-database liveness", lambda: self.stop_db_check_liveness(db_id))
        except (Exception, KeyboardInterrupt) as error:
            status = 128 + error.signum if isinstance(error, Interrupted) else 1
            self.failures.append(self.redact(str(error) or "Interrupted"))
            print("FAIL " + self.failures[-1], file=sys.stderr, flush=True)
        finally:
            # A second interrupt must not cut ownership restoration short.
            for signum in (signal.SIGINT, signal.SIGTERM):
                signal.signal(signum, signal.SIG_IGN)
            if self.failures:
                status = status or 1
                print("FAIL {} check(s); independent checks were not suppressed".format(len(self.failures)), file=sys.stderr, flush=True)
                self.show_logs()
            if not self.cleanup():
                status = 1
        return status


def positive_int(value):
    try:
        number = int(value)
    except ValueError:
        raise argparse.ArgumentTypeError("must be a positive integer") from None
    if number <= 0:
        raise argparse.ArgumentTypeError("must be a positive integer")
    return number


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--image", required=True, help="prebuilt local application image reference; never built or pulled")
    parser.add_argument("--audit-image", action="store_true", help="audit merged filesystem and every final-image layer by name")
    parser.add_argument("--audit-max-mib", type=positive_int, default=1024, help="maximum bytes per audit archive/scan in MiB (default: 1024)")
    args = parser.parse_args()
    if not args.image or args.image.startswith("-") or any(character.isspace() for character in args.image):
        parser.error("--image must be a single local image reference")

    def interrupt(signum, frame):
        raise Interrupted(signum)

    previous = {signum: signal.signal(signum, interrupt) for signum in (signal.SIGINT, signal.SIGTERM)}
    try:
        return Checker(args).run()
    finally:
        for signum, handler in previous.items():
            signal.signal(signum, handler)


if __name__ == "__main__":
    sys.exit(main())
