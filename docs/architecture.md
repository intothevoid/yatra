# Travel Diary AI — Architecture

## Overview

Travel Diary AI is a self-hosted Go application that turns travel notes and photos into illustrated Ghost blog drafts. It uses locally hosted AI models (OMLX) for image analysis and narrative generation.

## System Boundaries

```
┌─────────────────────────────────────────────────────────────┐
│                    Travel Diary AI                          │
│                                                             │
│  ┌──────────┐   ┌──────────┐   ┌──────────┐               │
│  │  HTTP    │   │ Services │   │  Store   │               │
│  │ Server   │──▶│          │──▶│ (SQLite) │               │
│  └──────────┘   └──────────┘   └──────────┘               │
│       │                │                                     │
│       │                ▼                                     │
│       │          ┌──────────┐                                │
│       │          │  Worker  │                                │
│       │          │  Pool    │                                │
│       │          └──────────┘                                │
│       │                │                                     │
│       │                ▼                                     │
│       │          ┌──────────┐                                │
│       │          │ Adapters │                                │
│       │          └──────────┘                                │
│                                                             │
└───────────┬─────────────────────────────────────────────────┘
            │
     ┌──────┴───────┐
     │              │
  ┌───────┐    ┌────────┐
  │ OMLX  │    │ Ghost  │
  │ (VLM) │    │ Admin  │
  └───────┘    └────────┘
```

## Components

### HTTP Server (`internal/server`)
- Chi router with CORS, request ID, logging
- REST API endpoints for trips, photos, videos, articles, jobs
- HTMX page fragments for browser UI
- SSE endpoint for job progress streaming

### Services (`internal/services`)
- **Importer**: Server-side directory import with path validation
- **Video processing**: FFprobe metadata extraction, FFmpeg trim/silent H.264 encoding, and libvips WebP posters; original source copies remain immutable
- **Genie**: Analysis pipeline coordinator
- **EvidenceExtractor**: Parses notes into structured experiences
- **PhotoMatcher**: Links photos to experiences by semantic score
- **SectionDrafter**: Generates narrative blocks from evidence
- **GroundingAuditor**: Verifies claims against evidence
- **Clusterer**: Duplicate/similar photo detection
- **Ranker**: Photo ranking with explainable factors
- **GhostExporter**: Creates/updates Ghost drafts
- **StyleProfiler**: Imports writing style from Ghost posts

### Store (`internal/store`)
- SQLite with WAL mode, foreign keys, migrations
- Tables: trips, source_files, photos, videos, photo_groups, experiences, photo_matches, style_profiles, articles, article_revisions, jobs, ghost_exports
- Pure Go SQLite driver (`modernc.org/sqlite`)

### Adapters (`internal/adapters`)
- **VLMAdapter**: Vision-language model interface
- **TextAdapter**: Text generation interface
- **ImageProcessor**: EXIF extraction, thumbnail/preview/export generation
- **FakeAI**: Deterministic responses for testing
- **FakeServer**: HTTP server simulating OMLX for tests

### Domain (`internal/domain`)
- Value objects: Trip, SourceFile, Photo, ArticleDocument, ArticleBlock, Job, StyleProfile
- Status constants and transition validation
- Canonical JSON article format

## Data Flow

1. **Import**: User selects trip media → images are hashed and receive derivatives; videos are preserved, probed, trimmed, transcoded without audio, and given WebP posters
2. **Analyse**: VLM analysis queued → worker processes photos → results stored
3. **Rank**: Photos scored on content relevance, quality, variety, coverage
4. **Generate**: Notes parsed → experiences extracted → photos matched → narrative drafted
5. **Edit**: User edits article blocks → revisions saved
6. **Export**: Article video and poster media are uploaded to Ghost storage, then the Ghost draft is created/updated with public media URLs

## Security

- Path traversal prevention in imports and zip extraction
- MIME validation from file content (not extension)
- Zip bomb protection (size limits, entry count)
- Symlink rejection in zip files
- API keys from environment variables only
- JWT for Ghost Admin API (never exposed to browser)

## Deployment

- Single Go binary with embedded HTTP server
- SQLite for persistence (no external database)
- Docker support with multi-stage build
- Configurable via environment variables and `.env` file
