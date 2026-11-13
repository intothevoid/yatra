# Travel Diary AI — Deployment Guide

## Prerequisites

- Go 1.26.4+ (for native builds)
- Docker + Docker Compose (for container deployment)
- FFmpeg, FFprobe, and libvips WebP support (for native video processing)
- OMLX server running locally or on LAN
- Ghost instance (v5+) for publishing

## Native Build (macOS/Linux)

```bash
# Install dependencies
brew install exiftool vips ffmpeg  # macOS
# or
sudo apt install libimage-exiftool-perl libvips  # Debian/Ubuntu

# Build
go build ./cmd/travel-diary

# Run
./travel-diary
```

## Docker Deployment

```bash
# Build and run
docker compose up -d

# View logs
docker compose logs -f travel-diary
```

## Configuration

Copy `.env.example` to `.env` and edit:

```bash
cp .env.example .env
```

### Required Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `DATA_DIR` | Directory for SQLite DB and assets | `./data` |
| `IMPORT_ROOTS` | Comma-separated import directories | `./imports` |
| `OMLX_BASE_URL` | OMLX API base URL (Docker Desktop host address) | `http://host.docker.internal:8000/v1` |
| `OMLX_API_KEY` | OMLX API key | (none) |
| `OMLX_WRITER_MODEL` | Text model ID | `gemma-4-e4b-it-4bit` |

### Optional Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | HTTP server port | `8080` |
| `GHOST_URL` | Ghost Admin API base URL | (none) |
| `GHOST_ADMIN_API_KEY` | Ghost Admin API key | (none) |
| `MAX_FILE_SIZE_MB` | Max upload file size (MB) | `50` |
| `MAX_UPLOAD_SIZE_MB` | Max total upload size (MB) | `500` |
| `MAX_UPLOAD_FILES` | Max photos per browser upload | `200` |
| `VIDEO_ENABLED` | Enable local FFmpeg processing | `true` |
| `FFMPEG_PATH` / `FFPROBE_PATH` / `VIPS_PATH` | Tool executable paths | `ffmpeg` / `ffprobe` / `vips` |
| `VIDEO_DEFAULT_PROFILE` | Default encoding profile | `standard` |
| `VIDEO_DEFAULT_DURATION_SECONDS` | Default trim duration (user can override) | `6` |
| `VIDEO_PROCESSING_TIMEOUT_SECONDS` | Per-clip timeout | `300` |
| `VIDEO_MAX_SOURCE_SIZE_MB` | Maximum video source size | `500` |
| `VIDEO_POSTER_QUALITY` | WebP poster quality | `80` |
| `VIDEO_STANDARD_*` | Width, height, fps, CRF, preset | `1920`, `1080`, `25`, `24`, `slow` |
| `VIDEO_LIGHTWEIGHT_*` | Width, height, fps, CRF, preset | `1280`, `720`, `25`, `26`, `slow` |

## Security

- The server binds to `127.0.0.1` by default. Configure `PORT` with a different bind address for LAN access.
- API keys are read from environment variables only, never stored in the database.
- Import paths are validated against the `IMPORT_ROOTS` allowlist.
- Zip uploads are validated for path traversal, symlinks, and size limits.

## Backup

The SQLite database and derived assets are stored in `DATA_DIR`:

```bash
# Backup
tar czf backup.tar.gz data/

# Restore
tar xzf backup.tar.gz -C /path/to/app
```

## Troubleshooting

### OMLX Connection Issues

- Verify the OMLX server is running: `curl http://127.0.0.1:8000/v1/models`
- Check the API key is correct in `.env`
- Ensure the model ID matches an exposed model in OMLX

### Ghost Connection Issues

- Verify the Ghost URL is accessible from the server
- Check the Admin API key format (should be `content_key:admin_key`)
- Ensure the Ghost instance is v5+

### Image Processing Issues

- Verify `exiftool` and `vips` are installed
- Check that the data directory is writable
- For HEIC support, ensure `libheif` is installed

### Video processing issues

- Install FFmpeg/FFprobe and verify both are on `PATH`; set `FFMPEG_PATH` and `FFPROBE_PATH` when they are elsewhere.
- WebP posters are written with libvips (`vips`). Install `vips` on macOS with Homebrew; the Docker image includes `libvips-tools`.
- If conversion reports an encoder or dimension error, update FFmpeg and check that its build includes `libx264`. The encoder rounds scaled dimensions down to even values while preserving source aspect ratio.
- Original videos remain in `DATA_DIR/assets/<trip>/source-video/`; MP4s and WebP posters are stored in `DATA_DIR/assets/<trip>/videos/`.
- To pause off-screen videos and respect reduced-motion preferences on the public Ghost site, copy `docs/video-playback.js` into your Ghost theme footer (or equivalent theme asset). The local editor does not supply scripts to the Ghost theme.
