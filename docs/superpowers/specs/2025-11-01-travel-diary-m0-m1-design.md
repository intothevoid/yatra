# Design: Travel Diary AI — Milestones 0 and 1

**Created:** 2025-11-01
**Author:** Pi (based on spec v1.0)
**Approved by:** TBD

## Intent

Build and validate (a) the architecture probes that prove the external integrations — OMLX vision model, HEIC handling, Ghost Gallery API — and (b) the first complete vertical slice: server-side import of notes + photos, analysis, a single prose section, matched images, and a persisted canonical document. No Ghost integration in Milestone 1.

## Goals

- Probe OMLX vision support with real images from `~/Downloads/Nikon Zf Photos/` and record results in an ADR.
- Probe Ghost Admin API gallery support (native edit, single-image fallback).
- Bootstrap a Go module with SQLite (entropy), fake services, and a minimal CLI/HTTP boundary.
- Produce a runnable demo: `travel-diary run` imports ~5 real photos + a stubbed `story.txt`, analyses them, and writes an article with matching images.
- All external dependencies (VLM, Ghost) are mockable by default; real services wired by config.

## Non-goals (for milestones 0 + 1)

- Browser UI (Milestone 4)
- Ghost integration (Milestone 5)
- HEIC support (depends on Docker/dependency probe result; stub for now)
- Full photo curation grid (Milestone 2)
- Style-profile import (Milestone 3)
- Docker packaging (Milestone 6)
- Authentication (Milestone 5+).

## Anti-goals

- Any cloud inference (spec forbids it).
- Storing credentials in the SQLite database.
- Automatically publishing a Ghost draft (spec forbids).
- Overly complex infrastructure (no Redis, no microservices).

## Constraints (from spec)

- Backend in Go. Single binary (server + workers).
- SQLite WAL, with foreign keys and transactions.
- Configurable VLM + writer adapters against a generic OpenAI-compatible contract.
- Deterministic content identity: SHA-256 hashes.
- Canonical JSON article format, not HTML or Markdown.
- Tested against a fake server; real services opt-in via config.
- No change to originals.

## Approaches (previously reviewed)

Three approaches evaluated. Selected: **Approach B with probe run**.

1. Probe-first, then build — build the vertical slice only after confirming real services. Risk: probe failures block progress.
2. Fake-first, verify later — build against a fake server that reproduces the probe findings, wire real services as a config toggle.
3. Parallel spike and build — spike probes in parallel with scaffold; use results to inform decisions.

**Selection: Approach 2.** The three probes will run immediately (the OMLX at `http://127.0.0.1:8000`, the three sample photos, and a stubbed Ghost test); their results feed directly into the fake server fixture and the design choices for the vertical slice. This gives a green, testable milestone without depending on OMLX availability and gives a test fixture that doubles as a contract for Milestone 0+ outputs.

## Technical decisions (settled)

| Decision | Choice | Rationale |
|----------|--------|-----------|
| SQLite driver | `modernc.org/sqlite` (pure Go) | No CGO in Docker; supports WAL and concurrency. |
| JSON schema validation | `github.com/santhosh-tekuri/jsonschema` | Validates spec output at parse time; versioned schemas. |
| HTTP framework | Go stdlib `net/http` + `chi` router | Simple, no runtime, sufficient for v1. |
| VLM adapter contract | Generic interface + `fakeAI` impl | Per spec testing requirements. Fake server emits schema-verified responses matching the spec's output structure (schema_version, photo_id, observed/hypotheses/quality_flags/safety_flags). |
| Image library | Call `vips` CLI via adapter. | Spec allows either. CLI is more portable for Docker build. |
| EXIF library | Call `exiftool` CLI via adapter. | Spec allows either. More portable than CGO bindings. |
| Config | Config file (TOML or YAML) + env overrides. | Spec says env vars for secrets. Config file for service endpoints, concurrency, timeouts. |
| Fixture data | 3 real JPEGs from `~/Downloads/Nikon Zf Photos/` (DSC_0089, DSC_0102, DSC_0105) | Real content gives real test value. |
| Go module path | `github.com/karan/travel-diary` | Per GitHub conventions (repo `karan/travel-diary`). |
| Fake server | Implement full `/v1/models` and `/v1/chat/completions` routes with configurable latency, failure modes, and schema-verified responses. | Allows testing timeout, 429, 5xx, partial responses, and cancellation — all listed in spec. |

## Plan outline (to be expanded by writing-plans)

### Probe phase

1. **VLM probe** — Send three photos (DSC_0089, DSC_0102, DSC_0105) to `http://127.0.0.1:8000/v1/chat/completions` with image content part. Check: model name, JSON mode support, response schema, max tokens. Write ADR.
2. **Ghost probe** — Create a draft with single image, test native gallery endpoint. Write ADR.
3. **HEIC probe** — Search a few `.heic` images in the same folder, attempt conversion with libvips/CLI. Record status.

### Scaffold

4. Init Go module (`github.com/karan/travel-diary`); write module docs.
5. Define core domain types (`Trip`, `SourceFile`, `Photo`, `ArticleDocument`) as plain Go structs.
6. Define SQLite migration set (trips, source_files, photos, articles, jobs) with version tracking.
7. Define `AIAdapter` interface + `VLMAdapter` + `TextAdapter` + `FakeAI` implementation.
8. Define `ImageProcessor` interface + `ExifToolAdapter` + `VipsProcessor` + `FakeProcessor`.

### Vertical-slice integration

9. Server-side import handler: accept a path, enumerate files, compute SHA-256, parse EXIF, create previews.
10. Worker pool: single in-process worker that executes jobs (analysis, thumbnails) with lease/heartbeat.
11. Minimal HTTP server: `/health`, `/api/v1/trips`, `/api/v1/trips/import-path`, `/api/v1/trips/{id}/analyse`.
12. Fake VLM fixture for end-to-end test (produce schema-verified response).
13. End-to-end test: import path → analyse → store photos, store results (no UI).

### Document outputs (Milestone 1 acceptance)

- ADR for VLM probe (`docs/decisions/001-omlx-vision-support.md`).
- ADR for Ghost gallery probe (`docs/decisions/002-ghost-gallery-support.md`).
- ADR for HEIC handling (`docs/decisions/003-heic-support.md`).
- All unit/integration tests passing (green CI on branch).
- Observable demo: `go run ./cmd/travel-diary run --trip /path/to/fake-trip`.

# Decision Records

## 001 — Vision model probe result (see docs/decisions/001-omlx-vision-support.md)

Pending: write after running the three-photo probe against `http://127.0.0.1:8000`.

## 002 — Ghost gallery API support (see docs/decisions/002-ghost-gallery-support.md)

Pending: write after testing gallery endpoints on `https://travel.karan.myds.me`.

## 003 — HEIC codec support (see docs/decisions/003-heic-support.md)

Pending: write after probing HEIC files in the sample folder and confirming libvips conversion.
