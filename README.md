# Prophecy AniZone Sub Server (Go Port)

Standalone high-performance Go provider API for Prophecy Backend.

## Features
- **High Performance & Low Memory**: Written in idiomatic Go using standard library HTTP routing and net/http.
- **Provider Protocol**: Full compatibility with Prophecy Provider v1 protocol.
- **Full Capabilities**:
  - Anime Search (`/api/v1/anime/search`)
  - Series Details (`/api/v1/anime/{slug}`)
  - Episode Listing (`/api/v1/anime/{slug}/episodes`)
  - Source Extraction (`/api/v1/anime/{slug}/episodes/{episode_id}/sources`)
  - Source Resolution (`/api/v1/anime/{slug}/episodes/{episode_id}/resolve`)
  - Subtitle Fetching (`/api/v1/anime/{slug}/episodes/{episode_id}/subtitles`)
  - Provider Engine Execution (`/api/v1/providers/anizone/execute`)
  - Health & Manifest Endpoints (`/health`, `/api/v1/manifest`)

## Configuration
Configure via environment variables:
| Variable | Default | Description |
|---|---|---|
| `PORT` | `8000` | HTTP Server Port |
| `HOST` | `0.0.0.0` | HTTP Server Host |
| `ANIZONE_ENABLED` | `true` | Enable/Disable AniZone provider |
| `ANIZONE_BASE_URL` | `https://anizone.to` | AniZone Base URL |
| `REQUEST_TIMEOUT` | `20.0` | Upstream request timeout (seconds) |
| `REQUEST_RETRIES` | `2` | Upstream request retries |
| `MAX_RESPONSE_BYTES` | `4194304` | 4MB maximum response limit |
| `MAX_PAGINATION_PAGES` | `64` | Maximum pagination limit |

## Run Locally
```bash
go run ./cmd/server
```

## Run Tests
```bash
go test -v ./...
```

## Docker Build & Run
```bash
docker build -t anizone-prophecy-sub-server .
docker run -p 8000:8000 anizone-prophecy-sub-server
```
