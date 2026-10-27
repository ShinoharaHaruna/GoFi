---
name: gofi-client
description: Upload, download, list, and delete files on a GoFi server, and manage its short links and API keys. Use whenever the user mentions GoFi, a GoFi URL or /s/ short link, asks to upload or fetch a file from a self-hosted file server, wants a shareable link for a file, or wants to create or revoke GoFi keys, even when they only say "share this file" or "put it on the server" in a context where GoFi is the sharing service. Also use when the task is to deploy, upgrade, back up, or provision keys on a GoFi server — server-side knowledge lives in server/README.md and is loaded on demand.
---

# GoFi client

GoFi is a self-hosted file-sharing service with a plain HTTP REST API. Uploaded files land in one of two buckets: `public` (anyone can download) or `private` (download requires a key). `curl` covers every operation.

This skill is primarily a **client** guide for talking to a running GoFi instance. When the task is server-side (deployment, upgrades, backups, seeding API keys on a host), read `server/README.md` first — it holds the operations knowledge, lazily, so it never crowds the client path.

## When to reach for GoFi

GoFi is a **bridge, not a pipe**. Use it only when the direct path between two parties lacks the bandwidth or connectivity to move the file — machines that cannot reach each other, or links too slow for the file at hand. When a direct route exists (`scp`, `rsync`, a shared mount, a fast LAN), take it; routing bytes through a small shared server is the fallback, not the default.

## Credentials

Two credential stores exist, checked in this order:

**1. `~/.config/gofi/<server>/<key-type>` — the client-side key ring.** One directory per server, named by its base URL minus scheme and trailing slash (`https://g.zheyo.ng` → `g.zheyo.ng`; `http://localhost:8080` → `localhost:8080`). Inside it, one raw-value file per key type: `upload`, `download`, `shorten`, `list`, `delete`, `api`. This layout exists so plain `cat` feeds curl directly:

```sh
curl -X POST "https://g.zheyo.ng/upload" \
  -H "Authorization: Bearer $(cat ~/.config/gofi/g.zheyo.ng/upload)" \
  -F "file=@/local/path/report.pdf"
```

**2. `credentials.env`, next to this SKILL.md** — the repo-side store, mainly for development. If it does not exist yet, copy the template and ask the user to fill it in:

```sh
cp credentials.env.template credentials.env
```

Read it before any request and never write its values into other files. It is gitignored; keep it that way. Only `credentials.env.template` belongs in git.

```sh
GOFI_BASE_URL=http://localhost:8080   # no trailing slash
GOFI_UPLOAD_KEY=      # type "upload": POST /upload
GOFI_DOWNLOAD_KEY=    # type "download": private downloads
GOFI_SHORTEN_KEY=     # type "shorten": create/enable/disable short links
GOFI_API_KEY=         # type "api": create/enable/disable keys
GOFI_LIST_KEY=        # type "list": GET /files
GOFI_DELETE_KEY=      # type "delete": DELETE /{filename}
```

Resolving a request: take the base URL from `GOFI_BASE_URL` (or the single directory under `~/.config/gofi/` if exactly one exists, defaulting to `https://`; localhost gets `http://`). For each key type, prefer `~/.config/gofi/<server>/<type>`; fall back to the matching `GOFI_*_KEY` variable.

Each key has exactly one type and only works on endpoints that accept that type. A disabled key behaves like a missing one. If the value you need is empty and no key file exists, ask the user for it; do not invent keys.

## Authentication

Send the key as a Bearer token:

```sh
-H "Authorization: Bearer $GOFI_UPLOAD_KEY"
```

A `?token=<key>` query parameter works too and is checked only when the header is absent. Prefer the header, since query tokens end up in logs and shared URLs. Missing, wrong-type, or disabled keys get a 401.

## Endpoint quick reference

| Operation | Endpoint | Key type |
|---|---|---|
| Health check | `GET /health` | none |
| Random UUID | `GET /uuid` | none |
| Upload | `POST /upload` | upload |
| List files | `GET /files` | list |
| Download | `GET /{filename}` | none for public files, download for private |
| Delete file | `DELETE /{filename}` | delete |
| Create short link | `POST /shorten` | shorten |
| Download via short link | `GET /s/{shortcode}` | none for public files, download for private |
| Disable short link | `DELETE /shorten/{shortcode}` | shorten |
| Enable short link | `POST /shorten/{shortcode}/enable` | shorten |
| Create API key | `POST /api-keys` | api |
| Disable API key | `DELETE /api-keys/{key}` | api |
| Enable API key | `POST /api-keys/{key}/enable` | api |

Responses carry paths only, never full URLs: `{"download_path":"/report.pdf"}` or `{"short_url_path":"/s/6f3a9b2c41"}`. Join them onto `GOFI_BASE_URL` yourself. Interactive docs live at `$GOFI_BASE_URL/swagger/index.html`.

## Recipes

### Upload a file

Uploads default to `private`. Add `X-GoFi-Target-Dir: public` for a public file.

```sh
curl -X POST "$GOFI_BASE_URL/upload" \
  -H "Authorization: Bearer $GOFI_UPLOAD_KEY" \
  -F "file=@/local/path/report.pdf"
# -> {"download_path":"/report.pdf"}
```

### Download a file

```sh
# public file
curl -o report.pdf "$GOFI_BASE_URL/report.pdf"

# private file
curl -o report.pdf \
  -H "Authorization: Bearer $GOFI_DOWNLOAD_KEY" \
  "$GOFI_BASE_URL/report.pdf"
```

### List files

`GET /files` returns every stored file as JSON; `?dir=public` or `?dir=private` restricts it to one bucket.

```sh
curl -H "Authorization: Bearer $GOFI_LIST_KEY" "$GOFI_BASE_URL/files"
# -> {"files":[{"name":"report.pdf","dir":"private","size":1048576,"mod_time":"2026-01-01T12:00:00Z"}]}
```

### Delete a file

Deletion removes the file and soft-disables every enabled short link that pointed at it. If the same name exists in both buckets, the server answers 409 and you must pass `?dir=`.

```sh
curl -X DELETE -H "Authorization: Bearer $GOFI_DELETE_KEY" \
  "$GOFI_BASE_URL/report.pdf"
# -> {"message":"File deleted","filename":"report.pdf","dir":"private","disabled_short_links":1}

# ambiguous name: pick a bucket explicitly
curl -X DELETE -H "Authorization: Bearer $GOFI_DELETE_KEY" \
  "$GOFI_BASE_URL/report.pdf?dir=private"
```

### Short links

The file must already exist on the server; `/shorten` only mints an alias.

```sh
curl -X POST "$GOFI_BASE_URL/shorten" \
  -H "Authorization: Bearer $GOFI_SHORTEN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"filename":"report.pdf"}'
# -> {"short_url_path":"/s/6f3a9b2c41"}

curl -o report.pdf "$GOFI_BASE_URL/s/6f3a9b2c41"                 # public source file
curl -o report.pdf -H "Authorization: Bearer $GOFI_DOWNLOAD_KEY" \
  "$GOFI_BASE_URL/s/6f3a9b2c41"                                  # private source file
```

Disable and re-enable:

```sh
curl -X DELETE "$GOFI_BASE_URL/shorten/6f3a9b2c41" \
  -H "Authorization: Bearer $GOFI_SHORTEN_KEY"
curl -X POST "$GOFI_BASE_URL/shorten/6f3a9b2c41/enable" \
  -H "Authorization: Bearer $GOFI_SHORTEN_KEY"
```

### Manage API keys

```sh
curl -X POST "$GOFI_BASE_URL/api-keys" \
  -H "Authorization: Bearer $GOFI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"type":"download"}'
# -> 201 {"key":"...","type":"download","is_enabled":true}

curl -X DELETE "$GOFI_BASE_URL/api-keys/<key>" \
  -H "Authorization: Bearer $GOFI_API_KEY"
curl -X POST "$GOFI_BASE_URL/api-keys/<key>/enable" \
  -H "Authorization: Bearer $GOFI_API_KEY"
```

Valid types: `upload`, `download`, `shorten`, `api`, `list`, `delete`. Keys are UUIDv4 strings generated server-side.

If no `api` key exists yet, seed one directly in Postgres, then save it to credentials.env (repeat for `list`/`delete` as needed):

```sql
INSERT INTO api_keys (key, type, is_enabled)
VALUES ('<new-random-key>', 'api', true);
```

### Share a file end to end

```sh
# upload as public, mint a short link, hand out the URL
curl -X POST "$GOFI_BASE_URL/upload" \
  -H "Authorization: Bearer $GOFI_UPLOAD_KEY" \
  -H "X-GoFi-Target-Dir: public" \
  -F "file=@report.pdf"

curl -X POST "$GOFI_BASE_URL/shorten" \
  -H "Authorization: Bearer $GOFI_SHORTEN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"filename":"report.pdf"}'

# share: $GOFI_BASE_URL/s/<shortcode>
```

A private share still needs a download key at fetch time. For recipients who cannot send headers, the link form is `$GOFI_BASE_URL/s/<code>?token=<download-key>`. Treat that URL as a secret.

## Behavior worth knowing

- Small host, small payloads. Typical GoFi deployments are tiny VPS boxes whose disk is shared with the OS — treat storage as scarce: upload only what the recipient needs, and delete the file once it has been picked up.
- HTTP, not a bulk protocol. Uploads are a single multipart POST: no resume, no chunking, no progress stream. Large files can additionally hit reverse-proxy body limits and squeeze a small host's memory, so keep payloads modest.
- `GET /files` shadows a filename. A file literally named `files` (likewise `health`, `uuid`, `s`, `swagger`) cannot be fetched via `GET /{filename}`; mint a short link for it instead.
- Deletion is final on disk but soft on links. The file is gone; short links pointing at it are disabled, not removed, and a re-upload under the same name does not revive them.
- Basenames only. The server strips directories from the upload filename (`dir/a.pdf` arrives as `a.pdf`). There is no folder structure.
- Overwrite on collision. Uploading a name that already exists in that bucket silently replaces the file.
- Public wins on direct download. `GET /{filename}` checks `public/` first, so if the same name sits in both buckets the public copy is served.
- Short links freeze privacy. `/shorten` records whether the file was private at creation and serves from that bucket.
- A private short link still needs a key. The short code is an alias, not a capability; whoever fetches `/s/{code}` for a private file must present a download key.
- Disabling is soft-deletion. Disabled links and keys return 404 or 401 until re-enabled; the records stay in the database.
- Short codes are 10 lowercase hex characters, for example `/s/6f3a9b2c41`.

## Errors

| Status | Meaning |
|---|---|
| 400 | Missing `file` field, malformed JSON, invalid key type or `dir` value |
| 401 | Missing, wrong-type, or disabled key |
| 404 | File, short link, or key not found; also disabled short links |
| 409 | `DELETE /{filename}` without `?dir=` when the name exists in both buckets |
| 500 | Server-side failure |

A 401 on an endpoint you expected to be open usually means the file sits in `private/` and needs the download key.

A 404 on `GET /files` or a hangup on `DELETE /{filename}` against a server you do not control usually means it runs an older build that predates these endpoints — the feature list is version-dependent, so check the server version before assuming a client-side bug.

## Server operations

For deploying, upgrading, backing up, or provisioning keys on a GoFi **server**, read `server/README.md` in this skill directory. It covers the architecture, the Compose stack, key seeding, and where per-instance knowledge lives. The repo's own `README.md` remains the canonical runbook.
