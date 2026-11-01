# Milestone 2 — Photo Ingestion/Curation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans.

**Goal:** Add duplicate detection, photo ranking, browser upload (multi-file + zip), job progress tracking, and security hardening.

**Architecture:** Extend existing services with clustering/ranking logic; add upload handler with validation; add SSE endpoint for job progress.

**Tech Stack:** Go stdlib, `modernc.org/sqlite`, `chi`, `archive/zip`, `hash` for perceptual hashing.

**Spec:** `travel-diary-ai-spec-v1.md` §4, §6, §12, §14

## Global Constraints

- Never change originals.
- SHA-256 for content identity.
- Detect exact duplicates with hashes, near duplicates with perceptual hashing.
- Validate MIME from file content, not just extension.
- Reject zip bombs (expansion size limit, path traversal prevention).
- Preserve user overrides on reruns.

## Review Focus

1. **Zip bomb** — a zip containing a single file that expands to 10GB must be rejected before extraction.
2. **Path traversal in zip** — `../../../etc/passwd` inside a zip must not escape the upload directory.
3. **MIME spoofing** — a file named `photo.jpg` containing HTML must be rejected or flagged.
4. **Duplicate import** — importing the same photo twice must detect it via SHA-256 and not create a second record.
5. **Concurrent uploads** — two simultaneous uploads to the same trip must not corrupt state.

---

### Task 1: Duplicate detection and perceptual hashing

**Files:**
- Create: `internal/services/clustering.go` (duplicate/similar photo clustering)
- Create: `internal/services/clustering_test.go`

**Interfaces:**
- Consumes: `Photo` (domain), `PhotoStore` (store)
- Produces: `Clusterer` interface, `PhotoGroup` struct

```go
type Clusterer interface {
    // Cluster finds duplicate and similar photos in a trip.
    Cluster(ctx context.Context, tripID string) ([]PhotoGroup, error)
}

type PhotoGroup struct {
    ID     string   `json:"id"`
    TripID string   `json:"trip_id"`
    Type   string   `json:"type"` // "duplicate" or "similar"
    Photos []string `json:"photo_ids"`
}
```

- [ ] **Step 1: Implement perceptual hash** — use average hash (aHash) via vips CLI: resize to 8x8, convert to grayscale, threshold at mean. Returns a 64-bit hash.

- [ ] **Step 2: Implement exact duplicate detection** — group photos by SHA-256. Photos with identical hashes are exact duplicates.

- [ ] **Step 3: Implement near-duplicate detection** — compute perceptual hash for each photo, group by Hamming distance ≤ 10 bits.

- [ ] **Step 4: Implement Clusterer** — runs both exact and perceptual clustering, stores results in `photo_groups` table, returns groups.

- [ ] **Step 5: Write tests** — test exact duplicate detection with same-hash photos, test near-duplicate with perceptual hash, test that groups are stored correctly.

- [ ] **Step 6: Commit.**

```bash
git add internal/services/clustering.go internal/services/clustering_test.go
git commit -m "duplicate detection: exact SHA-256 + perceptual hash clustering"
```

---

### Task 2: Photo ranking with explainable factors

**Files:**
- Create: `internal/services/ranker.go` (ranking algorithm)
- Create: `internal/services/ranker_test.go`

**Interfaces:**
- Consumes: `Photo` (domain), `VisionResult` (domain), `Trip` (domain)
- Produces: `Ranker` interface, ranked photos with reasons

```go
type Ranker interface {
    // Rank evaluates photos and sets recommended/review/not_selected status.
    Rank(ctx context.Context, tripID string) error
}

type RankReason struct {
    Factor  string  `json:"factor"`   // "content_relevance", "image_quality", "variety", "coverage"
    Weight  float64 `json:"weight"`   // 0.0-1.0 contribution
    Detail  string  `json:"detail"`   // human-readable explanation
}
```

- [ ] **Step 1: Implement ranking algorithm** — score each photo on: content relevance (VLM subjects match notes keywords), image quality (EXIF dimensions, no blur flags), variety (unique categories), coverage (timestamp spread). Return weighted score + reasons.

- [ ] **Step 2: Set photo status** — score ≥ 0.7 → recommended, 0.4-0.7 → review, < 0.4 → not_selected. Store ranking reasons as JSON in `ranking_reasons` column.

- [ ] **Step 3: Preserve user overrides** — if a photo has `user_override=1`, skip ranking for that photo and preserve its status.

- [ ] **Step 4: Write tests** — test ranking with mock photos, test that overrides are preserved, test reason serialization.

- [ ] **Step 5: Commit.**

```bash
git add internal/services/ranker.go internal/services/ranker_test.go
git commit -m "photo ranking: content relevance, quality, variety, coverage with explainable reasons"
```

---

### Task 3: Browser upload (multi-file + zip) with security hardening

**Files:**
- Create: `internal/server/upload_handler.go` (browser upload endpoints)
- Create: `internal/server/upload_handler_test.go`
- Modify: `internal/config/config.go` (add upload limits)

**Interfaces:**
- Consumes: `Importer` (services), `Config` (config)
- Produces: `UploadHandler` with multi-file and zip endpoints

```go
type UploadHandler struct {
    MaxFileSize   int64 // default 50MB
    MaxTotalSize  int64 // default 500MB
    MaxFiles      int   // default 200
    MaxZipSize    int64 // default 1GB
    ExtractDir    string
}
```

- [ ] **Step 1: Add upload config** — `MAX_FILE_SIZE`, `MAX_TOTAL_SIZE`, `MAX_FILES`, `MAX_ZIP_SIZE` env vars with safe defaults.

- [ ] **Step 2: Implement MIME validation** — read first 512 bytes, use `http.DetectContentType`, compare against supported MIME types. Reject mismatches.

- [ ] **Step 3: Implement multi-file upload** — `POST /api/v1/trips/import-upload` with `multipart/form-data`. Validate each file's size, MIME, and name. Reject path traversal in filenames.

- [ ] **Step 4: Implement zip upload** — `POST /api/v1/trips/import-upload` with `.zip` file. Validate zip structure before extraction: check total uncompressed size, reject paths with `..`, reject symlinks, reject absolute paths. Extract to temp dir, then import.

- [ ] **Step 5: Implement zip bomb protection** — count total uncompressed size during header reading, reject if exceeds `MaxZipSize`. Limit entries to 10000.

- [ ] **Step 6: Write tests** — test MIME validation rejects spoofed files, test path traversal rejection, test zip bomb rejection, test successful multi-file upload, test successful zip upload.

- [ ] **Step 7: Commit.**

```bash
git add internal/server/upload_handler.go internal/server/upload_handler_test.go internal/config/config.go
git commit -m "browser upload: multi-file + zip with MIME validation, zip bomb protection, path traversal prevention"
```

---

### Task 4: Job progress tracking via SSE

**Files:**
- Create: `internal/server/progress.go` (SSE endpoint)
- Create: `internal/server/progress_test.go`
- Modify: `internal/store/job_store.go` (add progress field)

**Interfaces:**
- Consumes: `Job` (domain), `JobStore` (store)
- Produces: SSE stream at `GET /api/v1/jobs/{id}/progress`

- [ ] **Step 1: Add progress to Job** — `Progress` field (percentage 0-100), `CurrentStep` (human-readable), `TotalItems` / `CompletedItems` for batch jobs.

- [ ] **Step 2: Implement SSE endpoint** — `GET /api/v1/jobs/{id}/progress` streams `text/event-stream` with JSON data. Polls the job store every 500ms.

- [ ] **Step 3: Wire progress updates in Genie** — when analysing photos, update job progress after each photo (step N/M, percentage).

- [ ] **Step 4: Write tests** — test SSE stream emits events, test progress updates are visible, test cancellation stops the stream.

- [ ] **Step 5: Commit.**

```bash
git add internal/server/progress.go internal/server/progress_test.go internal/store/job_store.go internal/services/genie.go
git commit -m "job progress: SSE streaming, percentage tracking, step updates"
```

---

### Task 5: Integration test and demo

**Files:**
- No new files (testing only)

- [ ] **Step 1: Create test fixture** — 5 photos with 2 exact duplicates, 1 near-duplicate, 1 unique.

- [ ] **Step 2: Run end-to-end test** — import → analyse → cluster → rank → verify groups and rankings.

- [ ] **Step 3: Test browser upload** — upload zip with 3 photos, verify import.

- [ ] **Step 4: Verify security** — test zip bomb rejection, path traversal rejection, MIME spoofing rejection.

- [ ] **Step 5: Commit.**

```bash
git add -A
git commit -m "Milestone 2 complete: duplicate clustering, ranking, browser upload, job progress"
```

---

## Summary

| Task | Files | Purpose |
|------|-------|---------|
| 1 | 2 | Duplicate detection (SHA-256 + perceptual hash) |
| 2 | 2 | Photo ranking with explainable factors |
| 3 | 2 + 1 mod | Browser upload (multi-file + zip) with security |
| 4 | 2 + 2 mod | Job progress via SSE |
| 5 | 0 | Integration test and demo |
