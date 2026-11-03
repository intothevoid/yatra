# Milestones 0 and 1 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and validate the three architecture probes (OMLX vision, HEIC handling, Ghost gallery API), then deliver a runnable vertical slice: server-side import of notes + photos, VLM analysis, canonical document persistence, all testable against a fake server.

**Architecture:** Single Go binary with embedded HTTP server, one in-process worker, SQLite (modernc.org/sqlite) for persistence, adapter interfaces for extensible external services, full fake server that reproduces real behaviors including failure modes.

**Tech Stack:** Go 1.22+, modernc.org/sqlite, chi (HTTP router), exiftool (CLI), vips (CLI), OMLX at `http://127.0.0.1:8000`, local sample photos.

**Spec:** `docs/superpowers/specs/2025-11-01-travel-diary-m0-m1-design.md` (which implements spec `travel-diary-ai-spec-v1.md`)

## Global Constraints

- Backend in Go; single binary (server + workers).
- SQLite WAL, with foreign keys and transactions.
- Configurable VLM + writer adapters against a generic OpenAI-compatible contract.
- Deterministic content identity: SHA-256 hashes (content, not filename).
- Canonical JSON article format, not HTML or Markdown.
- Tested against a fake server; real services opt-in via config.
- Never change originals.

## Review Focus

1. **Path traversal in imports** — a malicious `../` in `import-path` must never read outside the configured allowlist; the handler returns 400 and logs a warning.
2. **HEIC without codec** — when libvips or exiftool cannot decode HEIC, the importer records the file as `unsupported` without crashing; a status flag indicates which files couldn't be processed.
3. **OMLX vision unavailability** — when the VLM is unreachable or reports no supported image model, the analysis job transitions to `failed` with an actionable error; completed photo metadata from EXIF is preserved.
4. **Identical filenames from separate trips** — two trips each have a `sample.jpg`; content hashes must distinguish them, and the same file imported twice records a single `source_files` row with a count.
5. **Concurrent worker leases on crash** — if the server crashes mid-job, the next restart reclaims the lease; no duplicate analysis runs execute for the same photo.

---

### Task 0: Architecture probes (OMLX vision, HEIC, Ghost gallery)

**Objective:** Validate the three external integrations before any application code is written. Results feed directly into the design decisions for the fake server and the vertical slice.

**Files:**
- Write: `docs/decisions/001-omlx-vision-support.md` (results of probing `http://127.0.0.1:8000`)
- Write: `docs/decisions/002-ghost-gallery-support.md` (results of probing `https://travel.karan.myds.me`)
- Write: `docs/decisions/003-heic-support.md` (results of probing local sample photos for HEIC support)

**Interfaces:** N/A (no code produced).

- [ ] **Step 1: Probe OMLX vision** — Send `sample-1.jpg`, `sample-2.jpg`, and `sample-3.jpg` to `http://127.0.0.1:8000/v1/chat/completions` with an image content part. Check: model name, JSON mode support, response schema, max tokens. Record results in 001.
  - (Run `curl -X POST http://127.0.0.1:8000/v1/models` to see available models.)
  - (Run `curl -X POST ...` with a base64-encoded preview of one photo to test image understanding.)
  - (Verify `/v1/chat/completions` accepts `response_format` with JSON Schema.)
  - Record response size, latency, schema validation result.

- [ ] **Step 2: Probe Ghost gallery** — Connect to `https://travel.karan.myds.me/ghost/api/admin`. Upload a test image (`POST /ghost/api/admin/images/upload/`). Create a draft with that image. Test whether native gallery endpoint exists (`POST /ghost/api/admin/posts/`). Record results in 002.

- [ ] **Step 3: Probe HEIC support** — Search the local sample folder for `.heic` files. Attempt conversion with `vips` CLI. Record results in 003.

- [ ] **Step 4: Write ADRs** — Create `docs/decisions/001-omlx-vision-support.md`, `002-ghost-gallery-support.md`, `003-heic-support.md` with concrete findings. Include fallback decisions based on results.

- [ ] **Step 5: Commit probes and ADRs** (if probes are successful) or (if OMLX probe fails, record fallback in 001 and proceed with fake server for milestone 1 regardless).

```bash
git add docs/decisions/
git commit -m "Probes 001-003: OMLX vision, Ghost gallery, HEIC support"
```

---

### Task 1: Go module scaffold, configuration loader, and domain models

**Objective:** Initialize the Go module, define core domain types, and implement a config loader that supports both environment variables and a config file.

**Files:**
- Create: `go.mod` (module `github.com/karan/travel-diary`)
- Create: `go.sum`
- Create: `docs/travel-diary.md` (module documentation)
- Create: `.env.example` (documented env vars)
- Create: `internal/config/config.go` (config loader)
- Create: `internal/domain/trip.go` (Trip value object)
- Create: `internal/domain/source_file.go` (SourceFile value object)
- Create: `internal/domain/photo.go` (Photo value object)
- Create: `internal/domain/article.go` (ArticleDocument with block types)
- Create: `internal/domain/job.go` (Job value object)

**Interfaces:**
- Consumes: none
- Produces: exported structs `Trip`, `SourceFile`, `Photo`, `ArticleDocument`, `ArticleRevision`, `Job` (with full field lists as in the spec)

```go
// trip.go
type Trip struct {
    ID        string
    Name      string
    SourcePath string
    NotesHash string
    Status    TripStatus
    CreatedAt time.Time
    UpdatedAt time.Time
}

type TripStatus string
// Values: "IMPORTED" | "ANALYSING" | "PHOTO_REVIEW" | "PHOTOS_APPROVED" | "GENERATING" | "EDITING" | "ARTICLE_APPROVED" | "EXPORTED_DRAFT"
```

```go
// source_file.go
type SourceFile struct {
    ID          string
    TripID      string
    RelativePath string
    MIME        string
    Bytes       int64
    SHA256      string
    CaptureTime *time.Time
    ScanError   string
}
```

```go
// photo.go
type Photo struct {
    ID             string
    SourceFileID   string
    EXIF           string  // JSON string from exiftool
    ThumbnailPath  string  // path to 400px thumbnail
    PreviewPath    string  // path to 1200px preview
    ExportPath     string  // path to 2560px export
    VLMResult      string  // JSON from VLM adapter (schema_version, photo_id, observed, hypotheses, quality_flags, safety_flags)
    Status         PhotoStatus
    RankingReasons string  // JSON: ranking explanation
}

type PhotoStatus string
// Values: "unknown" | "recommended" | "review" | "not_selected"
```

```go
// article.go
type ArticleDocument struct {
    SchemaVersion  int
    ArticleID      string
    Title          string
    Slug           string
    Excerpt        string
    FeaturePhotoID string
    Tags           []string
    Blocks         []ArticleBlock
    Approved       bool
}

type ArticleBlock struct {
    ID          string
    Type        string  // "heading" | "paragraph" | "image" | "gallery" | "quote"
    Text        string  // optional
    Level       int     // for heading
    PhotoIDs    []string // for gallery
    Caption     string  // for gallery
    EvidenceIDs []string // for paragraph
}
```

- [ ] **Step 1: Initialize Go module** with `go mod init github.com/karan/travel-diary`.

- [ ] **Step 2: Implement domain models** — Define structs with all fields from the spec (see above). Include constants for statuses and block types.

- [ ] **Step 3: Write domain model tests** (no real integration tests yet — unit tests for struct validation, status encoding).

```go
// Example domain test
func TestTripStatusTransitions(t *testing.T) {
    var current TripStatus = "IMPORTED"
    assert.Equal(t, "IMPORTED", current.String())
    // Test valid and invalid transitions
}

func TestArticleDocumentValidation(t *testing.T) {
    doc := ArticleDocument{SchemaVersion: 1, Title: "test"}
    assert.Equal(t, "test", doc.Slug)  // default slug generation
}
```

- [ ] **Step 4: Implement config loader** — `internal/config/config.go` loads from file or env:

```go
type Config struct {
    DataDir        string
    ImportRoots    []string
    OMLXBaseURL    string
    OMLXVisionModel string
    OMLXWriterModel string
    OMLXTimeoutSec int
    OMLXConcurrency int
    LogLevel       string
}

func LoadConfig() (*Config, error)
func (c *Config) Validate() error
```

- [ ] **Step 5: Write config loader tests** — test env override precedence, file parsing, validation of required fields (DataDir, ImportRoots non-empty).

- [ ] **Step 6: Add dependencies** — `modernc.org/sqlite`, `github.com/go-chi/chi/v5`, `github.com/santhosh-tekuri/jsonschema`, `golang.org/x/crypto/bcrypt` (for auth in later milestones).

- [ ] **Step 7: Run all domain and config tests**, commit.

```bash
git add go.mod go.sum internal/config/config.go internal/domain/*.go docs/travel-diary.md .env.example
git commit -m "domain models, config loader, module scaffold"
```

---

### Task 2: SQLite store (migrations, trip/photo/article persistence)

**Objective:** Implement the SQLite-backed store with schema, migrations, and core CRUD operations for trips, source_files, photos, and articles.

**Files:**
- Create: `internal/store/schema.sql` (migrations)
- Create: `internal/store/store.go` (database connection and transaction helpers)
- Create: `internal/store/migrator.go` (migration versioning)
- Create: `internal/store/trip_store.go` (trip and source_file CRUD)
- Create: `internal/store/photo_store.go` (photo CRUD)
- Create: `internal/store/article_store.go` (article CRUD + revisions)
- Create: `internal/store/job_store.go` (job CRUD)

**Interfaces:**
- Consumes: domain structs (from Task 1)
- Produces: interfaces (store.TripStore, store.PhotoStore, store.ArticleStore, store.JobStore)

```sql
-- schema.sql (migrations/v1.sql)
CREATE TABLE IF NOT EXISTS trips (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    source_path TEXT,
    notes_hash TEXT,
    status TEXT NOT NULL DEFAULT 'IMPORTED',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS source_files (
    trip_id TEXT NOT NULL REFERENCES trips(id),
    relative_path TEXT NOT NULL,
    mime_type TEXT,
    file_size INTEGER,
    sha256 TEXT,
    capture_time TEXT,
    scan_error TEXT,
    PRIMARY KEY (trip_id, relative_path)
);
-- (and photos, articles, article_revisions, jobs as in spec)
```

- [ ] **Step 1: Write migration file** (migrations/v1.sql) with all tables from the spec (trips, source_files, photos, photo_groups, experiences, photo_matches, style_profiles, articles, article_revisions, jobs, ghost_exports).

- [ ] **Step 2: Implement migrator** — `MigrateTo(ctx, version)` checks sqlite_master for migration table, runs needed scripts, records version.

- [ ] **Step 3: Implement store.Open()** — opens sqlite with WAL, runs migrator on first call, returns a session with methods.

- [ ] **Step 4: Implement TripStore** — `Save(ctx, trip)`, `Get(ctx, id)`, `List(ctx)`, with transaction handling and WAL compatibility.

- [ ] **Step 5: Implement PhotoStore** — `Upsert(ctx, photo)`, `GetByTrip(ctx, tripID)`, `UpdateSelection(ctx, photoID, status)`.

- [ ] **Step 6: Implement ArticleStore** — `SaveRevision(ctx, doc)`, `GetLatest(ctx, articleID)`, `GetRevision(ctx, revisionID)`.

- [ ] **Step 7: Implement JobStore** — `Create(ctx, job)`, `Claim(ctx, jobType)`, `Update(ctx, jobID, fn)`, with lease and heartbeat.

- [ ] **Step 8: Write integration tests** — use `t.Fatal()` with in-memory tmpdir + `os.Remove` cleanup. Test trip import, photo update, article revision, job claim/complete.

```go
func TestTripImport(t *testing.T) {
    db := setup(t)
    ctx := context.Background()
    trip := Trip{ID: "t1", Name: "Japan 2025"}
    assert.NoError(t, store.SaveTrip(ctx, trip))
    fetched, err := store.GetTrip(ctx, "t1")
    assert.NoError(t, err)
    assert.Equal(t, "Japan 2025", fetched.Name)
}
```

- [ ] **Step 9: Run tests, commit.**

```bash
git add internal/store/
git commit -m "SQLite store: schema, migrations, CRUD for trips, photos, articles, jobs"
```

---

### Task 3: Image processor adapters (EXIF + vips) and fake image processor

**Objective:** Implement the `ImageProcessor` interface with real adapters (call `exiftool` and `vips` as CLIs) and a fake implementation that produces test-verified output.

**Files:**
- Create: `internal/adapters/image_processor.go` (interface definition)
- Create: `internal/adapters/exif_tool.go` (exiftool CLI adapter)
- Create: `internal/adapters/vips_processor.go` (vips CLI adapter)
- Create: `internal/adapters/fake_image_processor.go` (fake implementation)

**Interfaces:**
- Consumes: `Photo` (from Task 1)
- Produces: `ImageProcessor` interface, `ImageResult` (thumbnail/preview/export paths, metadata map)

```go
type ImageProcessor interface {
    ProcessImage(ctx context.Context, srcPath string) (*ImageResult, error)
    GenerateThumbnail(ctx context.Context, srcPath string, destPath string) error
    GeneratePreview(ctx context.Context, srcPath string, destPath string) error
    GenerateExport(ctx context.Context, srcPath string, destPath string) error
    ExtractEXIF(ctx context.Context, srcPath string) (map[string]string, error)
    // HEIC support:
    CanProcessHEIC(ctx context.Context) bool
}
```

- [ ] **Step 1: Define interface** with all methods from the spec (thumbnail 400px, preview 1200px, export 2560px, EXIF extraction).

- [ ] **Step 2: Implement ExifToolAdapter** — calls `exiftool -json` to parse, returns structured map. Handle missing exiftool error gracefully (return empty map + error).

- [ ] **Step 3: Implement VipsProcessor** — calls `vips` CLI with `--size`, `--quality`, orientation fix, ICC-to-sRGB conversion. Strip GPS metadata from export derivatives.

- [ ] **Step 4: Implement FakeImageProcessor** — produces deterministic paths (`testdata/fixtures/DSC_xxx/thumb.jpg`) and returns EXIF maps containing known test data. Used by all tests.

- [ ] **Step 5: Write processor tests** — test thumbnail produces image at correct resolution, test that EXIF maps include known fields, test HEIC handling.

- [ ] **Step 6: Commit.**

```bash
git add internal/adapters/
git commit -m "image processor: interface, exiftool + vips adapters, fake implementation"
```

---

### Task 4: AI adapter interfaces + VLMAdapter + TextAdapter + fakeAI + fake server

**Objective:** Implement the adapter contract for both vision and text models, plus the fake server that simulates the OMLX interface for testing.

**Files:**
- Create: `internal/adapters/vlm_adapter.go` (interface + real adapter)
- Create: `internal/adapters/text_adapter.go` (interface + real adapter)
- Create: `internal/adapters/fake_ai.go` (fake implementation)
- Create: `internal/server/fake_server.go` (fake HTTP server at `/v1/...`)
- Create: `internal/server/fake_server_test.go`

**Interfaces:**
- Consumes: `Config.OMLXBaseURL`, `Config.OMLXVisionModel`, etc.
- Produces: interfaces `VLMAdapter` and `TextAdapter`.

```go
type VLMAdapter interface {
    Analyze(ctx context.Context, imageBytes []byte) (*VisionResult, error)
    ModelSupportsImages(ctx context.Context) (bool, error)
}

type TextAdapter interface {
    Generate(ctx context.Context, notes string, photos []Photo) ([]ArticleBlock, error)
}

type VisionResult struct {
    SchemaVersion int
    PhotoID       string
    Observed      ObservedResult  // description, subjects, category, visible_text
    Hypotheses    []Hypothesis    // {Type, Value, Confidence}
    QualityFlags  []string
    SafetyFlags   []string
}

type ObservedResult struct {
    Description  string
    Subjects     []string
    Category     string
    VisibleText  []string
}

type Hypothesis struct {
    Type       string  // "place", "food", "landmark"
    Value      string
    Confidence float32 // heuristic, not calibrated probability
}
```

- [ ] **Step 1: Define adapter interfaces** — `VLMAdapter`, `TextAdapter`, with methods matching spec contract.

- [ ] **Step 2: Implement real VLMAdapter** — sends `POST /v1/chat/completions` with base64 image content part. Respects JSON schema mode. Handles 429 with jitter backoff.

- [ ] **Step 3: Implement real TextAdapter** — sends `POST /v1/chat/completions` with model ID. Writes sections in first-person.

- [ ] **Step 4: Implement FakeAI** — returns `VisionResult` with pre-stated subjects and categories for known test photos. Also handles configurable timeout, 429, 5xx.

- [ ] **Step 5: Implement FakeServer** — a standalone http.Server that listens on a port, serves `/v1/models` (list models) and `/v1/chat/completions` (accept image, return VisionResult).

- [ ] **Step 6: Write fake server tests** — send real image, verify JSON schema of response, verify unsupported model returns actionable error.

```go
func TestFakeVisionServesSchema(t *testing.T) {
    srv := StartFakeServer()
    defer srv.Close()
    resp := srv.Post("/v1/chat/completions", imageBytes)
    assert.Equal(t, `schema_version: 1`, resp.SchemaVersion)
}
```

- [ ] **Step 7: Commit.**

```bash
git add internal/adapters/* internal/server/fake_server.go internal/server/fake_server_test.go
git commit -m "AI adapters: VLM + Text contracts, fake AI, fake server"
```

---

### Task 5: Import service, worker pool, and HTTP handlers (vertical slice)

**Objective:** Tie together domain, store, adapters, and worker pool into a working import → analyse → persist pipeline. This is the full vertical slice for Milestone 1.

**Files:**
- Create: `internal/services/importer.go` (handles import flow: enumerate, hash, scan, create Trip + SourceFiles)
- Create: `internal/services/worker.go` (single in-process worker pool with queue, lease, heartbeat)
- Create: `internal/services/genie.go` (analysis service: runs VLM + ImageProcessor on a Trip)
- Create: `internal/server/handler.go` (HTTP handlers: `/health`, `/api/v1/trips`, `/api/v1/trips/import-path`, `/api/v1/trips/{id}/analyse`)
- Create: `internal/server/handler_test.go`

**Interfaces:**
- Consumes: `Trip`, `SourceFile`, `Photo`, `ArticleDocument` (domain), `TripStore`, `PhotoStore` (store), `VLMAdapter`, `ImageProcessor` (adapters)
- Produces: `TripImporter`, `WorkerPool`, `Genie`, `Server`

```go
type Importer interface {
    ImportTrip(ctx context.Context, tripID string, sourcePath string) error
}

type WorkerPool interface {
    Enqueue(ctx context.Context, jobType string, payload interface{}) (string, error)
    Complete(ctx context.Context, jobID string, fn func(*Job) error) error
    Cancel(ctx context.Context, jobID string) error
}

type Server interface {
    Listen(ctx context.Context, port int) error
}
```

- [ ] **Step 1: Implement Importer** — enumerate files in sourcePath, compute SHA-256, extract EXIF, create `Trip` and `SourceFiles` rows in SQLite. Return error if traversal detected (path outside ImportRoots).

- [ ] **Step 2: Implement WorkerPool** — channel-backed queue, single goroutine per pool, Claim + Update with 30s lease/heartbeat timeout. Reclaim expired leases on Start().

- [ ] **Step 3: Implement Genie (analysis service)** — reads photos for a trip, calls ImageProcessor + VLMAdapter for each, writes results to PhotoStore. Writes "ANALYSING" status to Trip.

- [ ] **Step 4: Implement ImportHandler** — POST `/api/v1/trips/import-path` with JSON body `{trip_id, source_path}`. Validates allowlist, calls Importer, returns trip ID.

- [ ] **Step 5: Implement AnalyseHandler** — POST `/api/v1/trips/{id}/analyse`. Enqueues "analyse" job, returns job ID.

- [ ] **Step 6: Implement health endpoints** — `/health/live` (process liveness), `/health/ready` (database + assets readable).

- [ ] **Step 7: Implement HTTP server with chi** — middleware for JSON error responses `{code, message, details}`, request IDs.

- [ ] **Step 8: Write handler tests** — test 400 on invalid path, 404 on missing trip, 200 on valid import, job queue works end-to-end with fake VLM and fake ImageProcessor.

- [ ] **Step 9: Implement cmd/travel-diary/main.go** — bootstrap, load config, run server.

```go
// main.go
func main() {
    cfg := config.LoadConfig()
    store := store.Open(cfg.DataDir)
    importer := importer.New(cfg)
    pool := worker.NewPool(store, cfg)
    server := handler.NewServer(cfg, store, importer, pool)
    server.Listen(context.Background(), cfg.Port)
}
```

- [ ] **Step 10: Write integration test** — import a trip with the 3 fixture photos + a stub `story.txt`, run analyse, verify photos have VLM result and thumbnails created.

- [ ] **Step 11: Commit.**

```bash
git add internal/services/ internal/server/handler.go internal/server/handler_test.go cmd/travel-diary/main.go
git commit -m "vertical slice: importer, worker pool, Genie, HTTP handlers"
```

---

### Task 6: Observe, demo, and clean up

**Objective:** Run the demo end-to-end, confirm all Milestone 1 acceptance criteria (from spec §14), and clean up the branch.

**Files:**
- No new code (mostly testing and demos).

**Interfaces:**
- Consumes: the complete vertical slice from Task 5.

- [ ] **Step 1: Create test trip** — use 3 local sample photos + a stub `story.txt`.

- [ ] **Step 2: Run end-to-end demo** — `go run ./cmd/travel-diary run --trip /path/to/test/trip`. Verify: Trip created, SourceFiles stored, photos analysed (VLM result), thumbnails created, article generated.

- [ ] **Step 3: Verify Milestone 1 acceptance criteria** from spec §14:
  - A. Import works without changing originals (check with `sha256sum` before/after).
  - B. Stable, schema-valid visual analyses from VLM (from fake).
  - C. Photo selection and override persist correctly.
  - D. Generator creates an article (from fake AI with test data).

- [ ] **Step 4: Commit.**

```bash
git add docs/superpowers/plans/
git commit -m "Final: milestone 0 + 1 validation and demo"
```

---

## Summary of file inventory

| Package | Files | Purpose |
|---------|-------|---------|
| `cmd/travel-diary/` | `main.go` | Entry point |
| `internal/config/` | `config.go` | Config loader (env + file) |
| `internal/domain/` | 5 files | Value objects (Trip, SourceFile, Photo, ArticleDocument, Job) |
| `internal/store/` | 5 files | SQLite migrations + CRUD (trips, photos, articles, jobs) |
| `internal/adapters/` | 5 files | VLM/Text adapters, image processor, fake implementations |
| `internal/services/` | 3 files | Importer, WorkerPool, Genie (analysis service) |
| `internal/server/` | 2 files | fake_server.go (test fixture), handler.go (HTTP routes) |

**Estimated lines of code per package** (rough): domain (300), store (600), adapters (500), services (400), server (300) = ~2100 lines + tests ~1000.

## Notes for the builder

- The spec's section 9 (Persistent state) is very prescriptive about column types and indexes; follow those exactly in `schema.sql`.
- The spec's section 5 (AI provider contract) defines the YAML config format verbatim; use it as-is in the config file.
- The spec's section 8 (Canonical document data format) defines the JSON schema verbatim; use those structs in `article.go`.
- The spec explicitly requires the `fake server` in section 14 (Testing requirements); build it before the real adapters.
- Section 3 (System boundaries) explicitly says "no separate broker, Redis, or distributed queue"; the worker pool is in-process only.
- Section 16 (Open technical validation) lists the 5 probe targets; complete them as Task 0 before proceeding.
