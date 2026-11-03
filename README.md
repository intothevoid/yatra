# yatra

A travel diary that turns your photos into a blog post, powered by Ghost CMS and LLMs.

## What it does

1. **Import** — point it at a folder of photos (and optionally a notes file)
2. **Analyse** — extracts metadata, generates thumbnails, and uses a VLM model to describe each photo
3. **Rank** — selects the best shots for your story
4. **Generate** — writes a full travel article using an OpenAI-compatible API
5. **Edit** — tweak the article block-by-block (paragraphs, images, headings, etc.)

## Getting started

### Quick start

```bash
# 1. Copy and configure the environment
cp .env.example .env
# Edit .env — set OMLX_API_KEY, OMLX_BASE_URL, and OMLX_WRITER_MODEL

# 2. Start with Docker
docker compose up --build -d

# 3. Open the UI on this computer
open http://localhost:8080
```

To use it from a phone on the same Wi-Fi, open `http://<computer-ip>:8080` in the phone browser. The computer's firewall must allow connections on port 8080.

### Configuration

Copy `.env.example` to `.env` and edit these values:

| Variable | Description | Example |
|---|---|---|
| `DATA_DIR` | SQLite DB + generated assets | `./data` (local) or `/data` (Docker) |
| `IMPORT_ROOTS` | Allowed source directories (comma-separated) | `./imports` (local) or `/imports` (Docker) |
| `OMLX_BASE_URL` | OpenAI-compatible API endpoint; use the Docker host address for Compose | `http://host.docker.internal:8000/v1` |
| `OMLX_API_KEY` | Your API key (required for article generation) | `your-key-here` |
| `OMLX_WRITER_MODEL` | Model for writing articles | `gemma-4-e4b-it-4bit` |
| `OMLX_VISION_MODEL` | Optional vision model; defaults to `OMLX_WRITER_MODEL` | `llava` |
| `LOCATION_WEB_SEARCH` | Search the web for destination context (disabled by default) | `false` |
| `BRAVE_SEARCH_API_KEY` | Required when location web search is enabled | `your-key-here` |
| `MAX_UPLOAD_FILES` | Maximum photos allowed in one browser upload | `200` |
| `PORT` | Server port | `8080` |

### Using with Docker

The Docker setup maps these directories:

| Host → Container | Purpose |
|---|---|
| `./data` → `/data` | SQLite database and generated assets |
| `./trips` → `/imports:ro` | Optional server-side source folders |

The browser workflow uploads selected photos directly, so it does not need a mounted source folder. The optional path-import API uses the **container path**:

```
Source path: /imports/my-trip
```

This maps to your local folder `trips/my-trip`.

## Workflow

### Create and edit a post

1. Enter a title, destination, and the details you remember.
2. Choose multiple photos from the device. The app uploads them and prepares thumbnails.
3. Describe the photos, then select or deselect images in the review grid.
4. Generate a first-person draft. Generation runs in the background and reports progress.
5. Edit the draft in the Ghost Koenig editor. Changes save automatically and the editor works in desktop and mobile browsers.
6. Choose **Send draft to Ghost** when you are ready. The app creates a Ghost draft; it does not publish the post.

When `LOCATION_WEB_SEARCH=false`, the configured writer model supplies general destination context. Web search is optional; enable it with `LOCATION_WEB_SEARCH=true` and provide a Brave Search API key. Search result sources are shown with the draft's destination notes.

### Status progression

```
IMPORTED → (Analyse) → PHOTO_REVIEW → (Rank) → PHOTOS_APPROVED
  → (Generate) → EDITING → (Approve) → ARTICLE_APPROVED → (Export) → EXPORTED_DRAFT
```

## API Reference

### Pages (HTMX UI)

| Method | Endpoint | Description |
|---|---|---|
| GET | `/` | Main page (HTMX) |
| GET | `/api/v1/page/trips` | Trip list (HTMX fragment) |
| GET | `/api/v1/page/trip/{id}` | Trip detail (HTMX fragment) |
| GET | `/api/v1/page/article/{id}` | Article editor (HTMX fragment) |
| POST | `/api/v1/page/import` | Import a trip (form: `source_path`) |
| POST | `/api/v1/page/analyse/{id}` | Start analysis |
| POST | `/api/v1/page/rank/{id}` | Rank photos |
| POST | `/api/v1/page/generate/{id}` | Generate article |

### API (JSON)

| Method | Endpoint | Description |
|---|---|---|
| GET | `/health/live` | Process is running |
| GET | `/health/ready` | Database is accessible |
| GET | `/api/v1/trips` | List trips |
| POST | `/api/v1/trips/import-path` | Import a trip (JSON body: `{source_path}`) |
| GET | `/api/v1/trips/{id}` | Get trip details |
| GET | `/api/v1/trips/{id}/photos` | List trip photos |
| PATCH | `/api/v1/trips/{id}/photos/{photoId}` | Update photo selection |
| GET | `/api/v1/articles/{id}` | Get article (JSON) |
| PUT | `/api/v1/articles/{id}` | Update article (JSON body) |

## Story file format

You can include a `story.txt` in your trip folder (at `trips/{tripname}/story.txt`).

Lines are parsed to extract structured experiences:

- **Location lines**: `Location: <name>` (extracted as `location: <name>`)
- **Food lines**: `Food: <name>` (extracted as `food: <name>`)
- **Activity lines**: `Activity: <name>` (extracted as `activity: <name>`)
- **Date lines**: `Date: <date>` (extracted as `date: <date>`)
- **General notes**: Any line is treated as a memory with `note: <content>`

Example:
```
Location: Santorini, Greece
Date: 15 June 2025
Food: Fresh seafood at Oxygène
Activity: Boat tour around the caldera
Visited the Blue Domes and the ancient fortress
```
