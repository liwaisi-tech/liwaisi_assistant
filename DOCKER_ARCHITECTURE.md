# Docker Compose Architecture

## Topology: 3-Service Stack

```
                    Host :3000
                        │
                   ┌────▼─────┐
                   │ frontend  │  nginx:1.27-alpine
                   │  :80      │  Static SPA + reverse proxy
                   └────┬─────┘
                        │ /api/* → http://backend:8080
                   ┌────▼─────┐
                   │ backend   │  alpine:3.21 (Go binary)
                   │  :8080    │  liwaisi-server
                   └──────────┘
                        │
              Docker network: liwaisi-net (bridge)
```

## Services

### 1. backend

| Aspect | Decision |
|---|---|
| **Dockerfile** | Multi-stage: `golang:1.25-alpine` (build) → `alpine:3.21` (runtime) |
| **Build context** | `back/go-assistant` |
| **Binary** | `CGO_ENABLED=0 go build -ldflags "-s -w -X ..." -o /app/liwaisi-server ./cmd/server` |
| **Runtime image** | `alpine:3.21` — includes `ca-certificates` for TLS to OpenRouter API |
| **Port** | `8080` (internal only, not exposed to host) |
| **User** | Non-root `appuser` (UID 10001) |
| **Health check** | `wget --spider http://localhost:8080/api/v1/health` every 10s |
| **Env vars** | `OPENROUTER_API_KEY` (required, from `.env`), `CORS_ORIGINS`, `DEFAULT_MODEL`, `LOG_LEVEL`, `LISTEN_ADDR` |

### 2. frontend

| Aspect | Decision |
|---|---|
| **Dockerfile** | Multi-stage: `node:22-alpine` (build) → `nginx:1.27-alpine` (runtime) |
| **Build context** | `front/react-assistant` |
| **Build step** | `npm ci && npm run build` → `dist/` |
| **Runtime image** | `nginx:1.27-alpine` serving static files |
| **Port** | `80` internal, mapped to host `3000` |
| **Depends on** | `backend` (healthy) |

### 3. Network: liwaisi-net

- Bridge network connecting backend and frontend
- Backend resolves as hostname `backend` from frontend's nginx

## Nginx Configuration

Key concerns:

```nginx
server {
    listen 80;

    # SPA static files
    location / {
        root /usr/share/nginx/html;
        try_files $uri $uri/ /index.html;
    }

    # API reverse proxy
    location /api/ {
        proxy_pass http://backend:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # SSE: disable buffering for event streams
        proxy_buffering off;
        proxy_cache off;
        proxy_set_header Connection '';
        proxy_http_version 1.1;
        chunked_transfer_encoding off;

        # SSE connections are long-lived (120s idle in Go server)
        proxy_read_timeout 300s;
    }
}
```

**SSE handling**: All `/api/` routes get `proxy_buffering off`. The Go server already sets `X-Accel-Buffering: no` on SSE responses, and the nginx config reinforces this by disabling buffering at the proxy level. `proxy_read_timeout` is set to 300s to exceed the Go server's 120s idle timeout.

## File Layout

```
├── back/go-assistant/
│   ├── Dockerfile              # Multi-stage Go build
│   └── .dockerignore
├── front/react-assistant/
│   ├── Dockerfile              # Multi-stage Node build + nginx
│   ├── .dockerignore
│   └── nginx.conf              # Custom nginx config
├── docker-compose.yml          # Service orchestration
├── .env.example                # Documented env vars (committed)
└── .env                        # Actual secrets (gitignored)
```

## Environment & Secrets

`.env.example` documents all variables:

```env
# Required — OpenRouter API key for LLM calls
OPENROUTER_API_KEY=your-key-here

# Optional — defaults shown
CORS_ORIGINS=*
DEFAULT_MODEL=anthropic/claude-sonnet-4-6
LOG_LEVEL=info
LISTEN_ADDR=:8080
```

`.env` contains actual values and is gitignored.

## Key Design Decisions

1. **alpine over distroless** for backend runtime — allows `wget` for health checks without adding a separate binary
2. **Frontend port 3000** (not 80) on host — avoids privilege requirements and port conflicts
3. **Backend not exposed to host** — all traffic goes through nginx reverse proxy, simplifying CORS and providing a single entry point
4. **SSE buffering disabled globally on /api/** — simpler than path-specific rules; the performance impact on non-SSE API calls is negligible since responses are small
5. **Non-root user in backend** — security best practice for the long-running Go process
6. **nginx handles SPA routing** — `try_files` ensures client-side routing works with direct URL access
