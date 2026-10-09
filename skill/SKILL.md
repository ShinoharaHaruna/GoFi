---
name: gofi-client
description: Upload, download, and share files on a GoFi server, and manage its short links and API keys. Use whenever the user mentions GoFi, a GoFi URL or /s/ short link, asks to upload or fetch a file from a self-hosted file server, wants a shareable link for a file, or wants to create or revoke GoFi keys, even when they only say "share this file" or "put it on the server" in a context where GoFi is the sharing service.
---

# GoFi client

GoFi is a self-hosted file-sharing service with a plain HTTP REST API. Uploaded files land in one of two buckets: `public` (anyone can download) or `private` (download requires a key). `curl` covers every operation.

This skill is for talking to a running GoFi instance. For building or deploying the server itself, see the repo's README.md and docker-compose.yml.

## Credentials

`credentials.env`, next to this SKILL.md, holds the base URL and API keys. If it does not exist yet, copy the template and ask the user to fill it in:

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
```

Each key has exactly one type and only works on endpoints that accept that type. A disabled key behaves like a missing one. If the value you need is empty, ask the user for it; do not invent keys.

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
| Download | `GET /{filename}` | none for public files, download for private |
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

Valid types: `upload`, `download`, `shorten`, `api`. Keys are UUIDv4 strings generated server-side.

If no `api` key exists yet, seed one directly in Postgres, then save it to credentials.env:

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

- No listing endpoint. The API cannot enumerate files, so track the filenames you upload.
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
| 400 | Missing `file` field, malformed JSON, invalid key type |
| 401 | Missing, wrong-type, or disabled key |
| 404 | File, short link, or key not found; also disabled short links |
| 500 | Server-side failure |

A 401 on an endpoint you expected to be open usually means the file sits in `private/` and needs the download key.
