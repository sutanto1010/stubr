# stubr

[GitHub](https://github.com/sutanto1010/stubr)

HTTP mock/stub server with convention-based directory routing, per-directory config (`_stubr.yaml`), query string matching, and post-response actions.

## Quickstart

```sh
# Start the server (zero config — just needs a stubs directory)
mkdir -p stubs/api/users
echo '{"users": [{"id":1, "name":"Alice"}]}' > stubs/api/users/GET.json
go run ./cmd/stubr
```

```sh
curl http://localhost:8080/api/users
# → {"users": [{"id":1, "name":"Alice"}]}
```

## Installation

```sh
make build              # → bin/stubr
sudo make install       # → /usr/local/bin/stubr
```

Docker:

```sh
make docker-build
make docker-run         # mounts ./stubs and ./stubr.yaml into container
```

Docker Compose:

```yaml
# docker-compose.yml
services:
  stubr:
    image: sutanto/stubr:4ae42e2
    ports:
      - "8080:8080"
    volumes:
      - ./stubs:/stubs
      - ./stubr.yaml:/etc/stubr.yaml
    command: -config /etc/stubr.yaml -dir /stubs
```

```sh
docker compose up -d
```

## Project Structure

```
stubs/
├── _stubr.yaml              # root defaults (headers, etc.)
├── health/
│   └── GET.json
├── api/
│   ├── _stubr.yaml          # api-level config
│   └── users/
│       ├── _stubr.yaml      # route config, query matches, method overrides
│       ├── GET.json
│       ├── POST.json
│       ├── admin.json       # served when ?role=admin
│       └── :id/
│           └── GET.json     # dynamic path segment
```

## How It Works

### Convention-Based Routing

Stubr maps HTTP requests to files by walking the `stubs_dir` directory tree:

| Request | Resolves To |
|---|---|
| `GET /api/users` | `stubs/api/users/GET.json` |
| `POST /api/users` | `stubs/api/users/POST.json` |
| `GET /api/users/42` | `stubs/api/users/:id/GET.json` |
| `DELETE /api/users/42` | `stubs/api/users/:id/DELETE.json` or `default.json` |
| `GET /health` | `stubs/health/GET.json` |

Dynamic segments use `:param` or `{param}` directory names. If no method-specific file exists, `default.<ext>` is used as a fallback. Trailing slashes are normalized before matching.

### Directory Config (`_stubr.yaml`)

Place a `_stubr.yaml` file in any stubs subdirectory to set status codes, delays, headers, actions, and query-based routing for all requests under that path.

Configs are **inherited** bottom-up: `/_stubr.yaml` → `/api/_stubr.yaml` → `/api/users/_stubr.yaml`. Child fields override parent. Per-method blocks (`methods:`) are applied after directory-level defaults.

```yaml
# stubs/api/users/_stubr.yaml
status: 200
delay: 50
headers:
  X-Resource: users

# Query-based routing: checked before convention files.
# All listed params must be present and match (case-sensitive). First match wins.
query_match:
  - params:
      role: admin
    status: 200
    file: ./admin.json     # relative to this directory
    delay: 100
    headers:
      X-Role: admin
    actions:
      - type: command
        command: "echo 'admin accessed users'"

  - params:
      role: user
      page: "1"
    file: ./page1.json

# Per-method overrides
methods:
  GET:
    delay: 30
    headers:
      Cache-Control: max-age=60
  POST:
    status: 201
    delay: 100
```

```sh
curl "http://localhost:8080/api/users?role=admin"
# → 200 OK, serves admin.json, X-Role: admin header

curl -X POST http://localhost:8080/api/users
# → 201 Created, no response file needed (uses convention POST.json)
```

### Config File (`stubr.yaml`)

Optional. Define server settings and explicit routes that override convention matching:

```yaml
port: 8080
host: "0.0.0.0"
stubs_dir: "./stubs"
verbose: true
global_delay: 0          # ms delay before every response

routes:
  - path: /api/health
    method: GET
    response:
      file: ./stubs/health/GET.json
      status: 200
      headers:
        Content-Type: application/json
      delay: 0

routes:
  - path: /webhook-target
    method: POST
    response:
      status: 204
    actions:
      - type: webhook
        url: "https://hooks.example.com/audit"
        method: POST
        body: |
          {"event": "route_hit", "path": "{{.Request.Path}}"}

      - type: command
        command: "echo '{{.Request.Path}} hit at {{.Timestamp}}'"

      - type: script
        inline: |
          #!/bin/sh
          echo "Request: {{.Request.Path}}" >> /tmp/stubr.log
```

### Post-Response Actions

Actions run **asynchronously** after the response is sent. Failures are logged but never affect the HTTP response. Actions can be defined in both `stubr.yaml` routes and directory `_stubr.yaml` files.

| Type | Config Fields | Behavior |
|---|---|---|
| `command` | `command`, `timeout`, `env` | Executed via `sh -c` |
| `webhook` | `url`, `method` (default POST), `headers`, `body`, `timeout`, `retry` | HTTP request with exponential backoff (1s, 2s, 4s) |
| `script` | `inline`, `timeout` | Written to temp file, executed via `sh`, cleaned up after |

**Template context** available in all action fields:

```
{{.Request.Method}}    {{.Request.Path}}    {{.Request.Headers}}    {{.Request.Query}}    {{.Request.Body}}
{{.Response.Status}}   {{.Response.Headers}}   {{.Response.Body}}
{{.Env.VAR_NAME}}      {{.Timestamp}}
```

### Precedence

When resolving a response, sources are applied in this order (higher wins):

| Priority | Source |
|---|---|
| 1 | `stubr.yaml` explicit route |
| 2 | Directory `_stubr.yaml` method-level `query_match` |
| 3 | Directory `_stubr.yaml` top-level `query_match` |
| 4 | Directory `_stubr.yaml` method defaults (`methods: METHOD`) |
| 5 | Directory `_stubr.yaml` defaults |
| 6 | Convention file match (`GET.json`, etc.) |

Headers are merged across all layers (root config → dir config ancestors → dir config → method overrides → query match). Non-header fields (status, delay, file) use the highest-priority non-zero value.

## CLI Flags

```
-config string   Path to YAML config file (default "stubr.yaml")
-port int        Override listen port
-host string     Override listen host
-dir string      Override stubs directory
-verbose         Enable verbose request logging
```

## Supported Content Types

JSON, XML, HTML, plain text, YAML, CSV, PNG, JPEG, GIF, SVG, PDF, MP4, MP3, JS, CSS, WOFF2, WOFF, ICO, WebP, WebM, AVIF. Unknown extensions fall back to `application/octet-stream`.

## Makefile Targets

```
make build         Build binary to bin/
make run           Start with go run
make test          Run tests with -race
make lint          Run go vet
make fmt           Format all Go source files
make tidy          Tidy go modules
make install       Install binary to /usr/local/bin
make docker-build  Build Docker image
make docker-run    Run container with volume mounts
make clean         Remove bin/ and clean go cache
```
