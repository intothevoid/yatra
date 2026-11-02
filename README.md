# yatra

A travel diary AI that turns your photos into a blog post, powered by Ghost CMS and LLMs.

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
# Edit .env — set your OMLX_API_KEY and optionally OMLX_BASE_URL

# 2. Prepare your trip folder
mkdir -p trips/my-trip
cp /path/to/photos/*.jpg trips/my-trip/
# Optional: add trip descriptions (helps the AI)
echo "Trip to Kyoto, March 2025" > trips/my-trip/story.txt

# 3. Start with Docker
docker compose up --build -d

# 4. Open the UI
open http://localhost:8080
```

### Configuration

Copy `.env.example` to `.env` and edit these values:

| Variable | Description | Example |
|---|---|---|
| `DATA_DIR` | SQLite DB + generated assets | `./data` (local) or `/data` (Docker) |
| `IMPORT_ROOTS` | Allowed source directories (comma-separated) | `./imports` (local) or `/imports` (Docker) |
| `OMLX_BASE_URL` | OpenAI-compatible API endpoint | `http://localhost:8000/v1` |
| `OMLX_API_KEY` | Your API key (required for article generation) | `your-key-here` |
| `OMLX_WRITER_MODEL` | Model for writing articles | `gemma-4-e4b-it-4bit` |
| `PORT` | Server port | `8080` |

### Using with Docker

The Docker setup maps these directories:

| Host → Container | Purpose |
|---|---|
| `./data` → `/data` | SQLite database and generated assets |
| `./trips` → `/imports:ro` | Your source photos (read-only) |

When importing through the UI, use the **container path**:

```
Source path: /imports/my-trip
```

This maps to your local folder `trips/my-trip`.

## Workflow

### Step 1 — Import

On the home page, enter the source path and click **Import Trip**.

- The importer copies photos to `data/assets/{trip_id}/source/`
- Extracts EXIF metadata (date, location, camera)
- Reads `story.txt` if present (used by the AI for article generation)
- Creates a trip record with status `IMPORTED`

### Step 2 — Analyse

Click **Analyse Photos** on the trip page. The system:

1. Generates thumbnails (400px default) and previews (1200px default)
2. Sends each photo to a VLM (Vision Language Model) for description
3. Extracts text from signs, labels, and signage (via Tesseract)
4. Sets the trip status to `PHOTO_REVIEW`

### Step 3 — Rank

Click **Rank Photos**. The system:

1. Scores each photo based on VLM descriptions, metadata, and quality
2. Selects the top N photos (configurable; default is 50)
3. Sets the trip status to `PHOTOS_APPROVED`

### Step 4 — Generate Article

Click **Generate Article** (requires `OMLX_API_KEY`). The system:

1. Combines VLM descriptions, metadata, and your notes
2. Invokes the LLM to write a full travel article with structure
3. Extracts "experiences" (events, locations, foods) from your notes
4. Sets the trip status to `EDITING`

### Step 5 — Edit

A link like **Edit Article** appears when status = `EDITING`. You can:

- Edit any block (paragraph, heading, image)
- Move blocks up or down
- Delete blocks
- Save changes (stored as new revisions)

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
