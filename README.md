# stubr

HTTP mock/stub server with convention-based directory routing and optional YAML config.

## Quickstart

```sh
# Start the server (zero config — just needs a stubs directory)
mkdir -p stubs/api/users
echo '{"users": [{"id":1, "name":"Alice"}]}' > stubs/api/users/GET.json
stubr

# Or with a config file
stubr -config stubr.yaml
```

```sh
curl http://localhost:8080/api/users
# → {"users": [{"id":1, "name":"Alice"}]}
```

## Installation

```sh
make build          # → bin/stubr
```

Docker:

```sh
make docker-build
make docker-run     # mounts ./stubs and ./stubr.yaml into container
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

Dynamic segments use `:param` or `{param}` directory names. If no method-specific file exists, `default.<ext>` is used as a fallback.

### Config File (`stubr.yaml`)

Optional. Override defaults and define explicit routes:

```yaml
port: 8080
host: "0.0.0.0"
stubs_dir: "./stubs"
verbose: true
global_delay: 0          # ms delay before every response

headers:
  X-Stubr: "true"

routes:
  - path: /api/users
    method: GET
    response:
      file: ./stubs/api/users/GET.json
      status: 200
      headers:
        Content-Type: application/json
      delay: 500
    actions:
      - type: webhook
        url: "https://hooks.example.com/audit"
        method: POST
        body: |
          {"event": "route_hit", "path": "{{.Request.Path}}"}
        retry: 2

      - type: command
        command: "echo '{{.Request.Path}} hit at {{.Timestamp}}'"

      - type: script
        inline: |
          #!/bin/sh
          echo "Request: {{.Request.Path}}" >> /tmp/stubr.log
```

### Post-Response Actions

Actions run **asynchronously** after the response is sent. Failures are logged but never affect the HTTP response.

| Type | Description |
|---|---|
| `command` | Shell command executed via `sh -c` |
| `webhook` | HTTP request with configurable method, headers, body, and retry + backoff |
| `script` | Inline script written to a temp file and executed via `sh` |

**Template context** available in all action fields:

```
{{.Request.Method}}  {{.Request.Path}}  {{.Request.Headers}}  {{.Request.Query}}  {{.Request.Body}}
{{.Response.Status}}  {{.Response.Headers}}  {{.Response.Body}}
{{.Env.VAR_NAME}}  {{.Timestamp}}
```

## CLI Flags

```
-config string   Path to YAML config file (default "stubr.yaml")
-port int        Override listen port
-host string     Override listen host
-dir string      Override stubs directory
-verbose         Enable verbose request logging
```

## Supported Content Types

JSON, XML, HTML, plain text, YAML, CSV, PNG, JPEG, GIF, SVG, PDF, MP4, MP3, JS, CSS, WOFF2, and more. Unknown extensions fall back to `application/octet-stream`.

## Makefile Targets

```
make build         Build binary to bin/
make run           Run with go run
make test          Run tests with -race
make lint          Run go vet
make docker-build  Build Docker image
make docker-run    Run container with volume mounts
make clean         Remove bin/ and clean go cache
```
