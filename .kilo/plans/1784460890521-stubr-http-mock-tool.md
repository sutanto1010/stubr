# stubr — HTTP Request Mock/Stub Tool

## Goal

A Go-based HTTP mock server that maps incoming requests to response files via convention-based directory routing and optional YAML config, with support for post-response actions (commands, webhooks, scripts).

## Decisions (Resolved)

| Decision | Choice |
|---|---|
| Language | Go (single binary, minimal Docker image, strong stdlib) |
| Mapping | Convention-based directory structure + optional `stubr.yaml` config for overrides |
| Post-response actions | Shell commands, HTTP webhooks, inline scripts (async, fire-and-forget) |
| Config format | YAML (`stubr.yaml`) |
| HTTP framework | `net/http` stdlib (no external router dependency) |
| Template engine | `text/template` for action bodies and dynamic config fields |

---

## Architecture

### Directory Layout

```
stubr/
├── cmd/stubr/main.go            # Entrypoint, CLI flags, server startup
├── internal/
│   ├── config/
│   │   └── config.go            # YAML config struct, loader, validation
│   ├── router/
│   │   ├── router.go            # HTTP handler, route matching logic
│   │   └── matcher.go           # Convention-based path matching with :param support
│   ├── responder/
│   │   └── responder.go         # Read response file, apply headers/status, write to client
│   ├── actions/
│   │   └── actions.go           # Async action runner (command, webhook, script)
│   └── contenttype/
│       └── contenttype.go       # File extension → Content-Type mapping
├── stubs/                        # Example stub files
│   └── api/
│       └── users/
│           └── GET.json
├── stubr.yaml                    # Example config
├── Dockerfile
├── Makefile
├── go.mod
└── README.md
```

### Data Flow

```
HTTP Request
  │
  ▼
┌─────────────┐    ┌───────────────┐    ┌──────────────┐
│   Router     │───▶│  Responder    │───▶│  HTTP Client  │
│ (match route)│    │ (read file,   │    │ (response     │
│              │    │  set headers, │    │  sent back)   │
│              │    │  set status)  │    │               │
└─────────────┘    └───────────────┘    └───────┬───────┘
                                                │ (async goroutine)
                                                ▼
                                         ┌──────────────┐
                                         │ Actions Runner│
                                         │ (command/     │
                                         │  webhook/     │
                                         │  script)      │
                                         └──────────────┘
```

### Route Matching Logic (Priority Order)

1. **Exact config match:** Check `stubr.yaml` routes for matching `path` + `method`
2. **Convention-based match:** Walk `stubs_dir` directory tree:
   - Static directories = literal path segments
   - Directories named `:param` or `{param}` = dynamic path segments
   - Files named `<METHOD>.<ext>` = method-specific response (e.g., `GET.json`, `POST.xml`)
   - A file named `default.<ext>` = fallback for any unmatched method on that path
3. **No match:** Return 404 with JSON body listing available stub paths

### Config Schema (`stubr.yaml`)

```yaml
port: 8080                    # default: 8080
host: "0.0.0.0"              # default: 0.0.0.0
stubs_dir: "./stubs"         # default: ./stubs
global_delay: 0              # default: 0 (ms delay before every response)
verbose: false               # default: false (log request details)
disable_convention: false    # default: false (disable convention-based routing)

# Global response headers (merged with route-specific, route takes precedence)
headers:
  X-Stubr: "true"

# Explicit route definitions
routes:
  - path: /api/users
    method: GET
    response:
      file: ./stubs/api/users/GET.json   # optional if convention match exists
      status: 200                         # default: 200
      headers:
        Content-Type: application/json
      delay: 500                          # ms, overrides global_delay for this route
    actions:
      - type: command
        command: "notify.sh user_listed"
        timeout: 10s                      # default: 30s
      - type: webhook
        url: "https://hooks.example.com/audit"
        method: POST
        headers:
          Authorization: "Bearer {{.Env.WEBHOOK_TOKEN}}"
        body: |
          {"event": "route_hit", "path": "{{.Request.Path}}"}
        timeout: 30s
        retry: 3
      - type: script
        inline: |
          #!/bin/bash
          echo "Request: {{.Request.Path}} at $(date)" >> /tmp/stubr.log
        timeout: 10s
```

### Convention-Based Routing Examples

Given `stubs_dir: ./stubs`:

| Request | Resolves To |
|---|---|
| `GET /api/users` | `stubs/api/users/GET.json` |
| `POST /api/users` | `stubs/api/users/POST.json` |
| `GET /api/users/42` | `stubs/api/users/:id/GET.json` |
| `DELETE /api/users/42` | `stubs/api/users/:id/DELETE.json` or `default.json` if no DELETE file |
| `GET /health` | `stubs/health/GET.json` |
| `GET /api/items` | `stubs/api/items/GET.json` (if exists), else `stubs/api/items/default.json`, else 404 |

Dynamic segments use `:param` or `{param}` directory naming. Multiple params per path are supported (e.g., `stubs/api/:org/:repo/GET.json`).

### Response File Handling

- **Any format:** JSON, XML, HTML, plain text, YAML, binary (images, PDFs, etc.)
- **Content-Type inference:** From file extension using a mapping table (`.json` → `application/json`, `.html` → `text/html`, `.png` → `image/png`, etc.), falling back to `application/octet-stream` for unknown extensions
- **Streaming:** Files read via `os.Open` and streamed via `io.Copy` to client — no memory limit
- **Status code:** Default 200, overridable in config route or via a convention-based `_status` prefix in filenames (e.g., `201_GET.json`, `404_default.json`) — **optional stretch feature**

### Post-Response Actions

All actions run **asynchronously** after the response is sent. Failures are logged but never affect the HTTP response.

**Template context** available in action fields (`command`, webhook `url`/`body`/`headers`, script `inline`):
- `{{.Request.Method}}`, `{{.Request.Path}}`, `{{.Request.Headers}}` (map), `{{.Request.Query}}` (map), `{{.Request.Body}}` (string)
- `{{.Response.Status}}`, `{{.Response.Headers}}` (map), `{{.Response.Body}}` (string)
- `{{.Env.VAR_NAME}}` — environment variables
- `{{.Timestamp}}` — RFC3339 timestamp of request

**Action types:**

| Type | Config Fields | Behavior |
|---|---|---|
| `command` | `command` (shell string), `timeout`, `env` (map) | Executed via `sh -c`, stdout/stderr logged |
| `webhook` | `url`, `method` (default POST), `headers` (map), `body` (string), `timeout`, `retry` | HTTP request sent after template rendering; scheduled retries with exponential backoff (1s, 2s, 4s) |
| `script` | `inline` (multi-line script), `timeout` | Written to temp file, executed via `sh`, temp file cleaned up after |

### CLI Flags

```
stubr [flags]

Flags:
  -config string    Path to YAML config file (default "stubr.yaml")
  -port int         Override listen port from config
  -host string      Override listen host from config
  -dir string       Override stubs directory from config
  -verbose          Enable verbose request logging
```

### Error Handling

| Scenario | HTTP Status | Body |
|---|---|---|
| No matching route | 404 | `{"error":"no stub for GET /path","available":["/api/users","/health"]}` |
| Response file not found | 500 | `{"error":"stub file not found","file":"stubs/api/users/GET.json"}` |
| File read error | 500 | `{"error":"failed to read stub file","detail":"permission denied"}` |
| Invalid config | Startup exit(1) | Stderr: "invalid config: field X: reason" |
| Action failure | N/A (post-response) | Logged to stderr |

### Dockerfile (Multi-Stage)

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /stubr ./cmd/stubr

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /stubr /usr/local/bin/stubr
EXPOSE 8080
ENTRYPOINT ["stubr"]
```

Image size target: < 15 MB.

### Makefile

```makefile
.PHONY: build run test lint docker-build docker-run docker-push clean

APP_NAME   := stubr
BIN_DIR    := bin
IMAGE_NAME := stubr
VERSION    ?= latest

build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BIN_DIR)/$(APP_NAME) ./cmd/$(APP_NAME)

run:
	go run ./cmd/$(APP_NAME)

test:
	go test -v -race -count=1 ./...

lint:
	go vet ./...

docker-build:
	docker build -t $(IMAGE_NAME):$(VERSION) .

docker-run:
	docker run --rm -p 8080:8080 -v $(PWD)/stubs:/stubs -v $(PWD)/stubr.yaml:/etc/stubr.yaml $(IMAGE_NAME):$(VERSION) -config /etc/stubr.yaml -dir /stubs

docker-push:
	docker push $(IMAGE_NAME):$(VERSION)

clean:
	rm -rf $(BIN_DIR)
	go clean -cache
```

### Dependencies (go.mod)

Only external dependency: `gopkg.in/yaml.v3` for config parsing. Everything else uses Go stdlib.

---

## Implementation Tasks (Ordered)

1. **Initialize Go module and scaffold** — `go mod init`, create directory structure, write `go.mod` with `yaml.v3` dependency
2. **config package** — Define `Config`, `Route`, `ResponseConfig`, `Action` structs; implement YAML loader with validation; implement CLI flag merging (CLI overrides config)
3. **contenttype package** — Extension-to-MIME mapping table; fallback via `http.DetectContentType` for binary files
4. **responder package** — `Respond(filePath, status, headers) http.HandlerFunc`; stream file to response; apply headers/status; handle file-not-found errors
5. **matcher package** — Convention-based path matcher: walk stubs dir, tokenize path, match static + `:param` segments, resolve to file path + detected params
6. **router package** — Orchestrate config-route matching then convention matching; call responder; trigger post-response actions
7. **actions package** — `Run(ctx, actions)` goroutine; template rendering with request/response context; command execution (sh -c with timeout); webhook send (HTTP client with retry + backoff); script execution (temp file + sh)
8. **main.go** — Wire everything together: parse flags, load config, build router, start `http.Server`, graceful shutdown on SIGINT/SIGTERM
9. **Dockerfile** — Multi-stage build as designed above
10. **Makefile** — As designed above
11. **Example stubs + config** — Create `stubr.yaml` and sample `stubs/` directory for demonstration

---

## Validation Plan

- `make build` compiles without errors
- `make lint` (go vet) passes
- `make test` passes all unit tests
- `make docker-build && make docker-run` starts and serves stubs
- Manual smoke test: `curl localhost:8080/api/users` returns content from `stubs/api/users/GET.json`
- Config override test: define route in `stubr.yaml` with custom status/headers, verify via curl
- Post-response action test: configure a webhook action, verify webhook URL is called (using a local listener or mock)
- Convention fallback test: request a path not in config but exists in stubs/ → returns file content
- 404 test: request a path with no match → returns JSON error with available paths list
- Binary file test: put a `.png` in stubs, curl returns correct Content-Type and binary content

---

## Open Questions / Future Enhancements (Out of Scope)

- Recording mode (proxy + record responses to files)
- Request body matching (match different response files based on request body/content)
- GraphQL support
- Stateful stubs (sequence-based responses: first call returns A, second call returns B)
- Hot reload on config/file changes
- Web UI / admin API for managing stubs
- Regex-based path matching in config routes
- gRPC support
