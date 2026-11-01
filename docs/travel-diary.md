# Travel Diary AI

A self-hosted application that turns a directory of travel notes and photos into an illustrated Ghost blog draft, using locally hosted AI models.

## Quick start

```bash
cp .env.example .env
# Edit .env with your OMLX API key and model IDs
go run ./cmd/travel-diary run
```

## Architecture

- **Backend:** Go, single binary with embedded HTTP server and in-process workers
- **Storage:** SQLite (pure Go, no CGO) for metadata, filesystem for derived assets
- **AI:** Configurable OpenAI-compatible endpoints (OMLX) for vision and text
- **Frontend:** Browser-based block editor (Milestone 4)
- **Publishing:** Ghost Admin API draft creation only (Milestone 5)

See `docs/decisions/` for architecture decision records.
