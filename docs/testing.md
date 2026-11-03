# Travel Diary AI — Testing Guide

## Running Tests

```bash
# All tests
go test ./...

# Verbose
go test ./... -v

# Single package
go test ./internal/store/... -v
```

## Test Coverage

| Package | Tests | Coverage |
|---------|-------|----------|
| `internal/domain` | 16 | Status transitions, slug generation, validation |
| `internal/config` | 6 | Env loading, path allowlist, validation |
| `internal/store` | 11 | CRUD operations, migrations, job leases |
| `internal/adapters` | 11 | Fake AI, fake server, image processor |
| `internal/server` | 13 | HTTP handlers, upload security, SSE |
| `internal/services` | 10 | Clustering, ranking, evidence extraction |

## Test Strategy

### Unit Tests
- Domain model validation (status transitions, slug generation)
- Config loading and validation
- Store CRUD operations with temporary SQLite databases
- Adapter interfaces with fake implementations

### Integration Tests
- Full import → analyse → generate pipeline
- HTTP handler tests with httptest server
- Job progress streaming via SSE

### Security Tests
- Path traversal prevention in imports and zip extraction
- MIME validation (rejects HTML files with .jpg extension)
- Zip bomb protection (size limits, entry count)
- Symlink rejection in zip files

## Fake Services

### FakeVLMAdapter
Returns deterministic VisionResult for testing. Each call increments a counter to produce unique results.

### FakeTextAdapter
Returns a simple text response for narrative generation testing.

### FakeServer
HTTP server simulating OMLX at a random port. Supports:
- `/v1/models` — returns fake model list
- `/v1/chat/completions` — returns vision or text responses based on request content

### FakeImageProcessor
Creates minimal JPEG files for thumbnail/preview/export testing. Returns deterministic EXIF data.

## Writing Tests

### Store Tests
Use `setup(t)` to create a temporary database:
```go
func TestSomething(t *testing.T) {
    store := setup(t)
    // ... test code
}
```

### HTTP Handler Tests
Use `setupApp(t)` to create a test server:
```go
func TestSomething(t *testing.T) {
    _, srv := setupApp(t)
    resp, _ := srv.Client().Get(srv.URL + "/api/v1/trips")
    // ... assertions
}
```

## Continuous Integration

Tests run on every commit. All tests must pass before merging.

## Ghost rendering regression checks

Run `npm ci` in `frontend` first, then from the repository root:

```sh
npm --prefix frontend test
npm --prefix frontend run test:ghost:browser
```

The browser command uses an installed Google Chrome. Set
`YATRA_CHROME_CHANNEL=chromium` if using a Playwright-managed Chromium instead.
Go and Node are required; Node 24 is recommended by the Ghost test packages.

These checks use synthetic data and do not require an LLM, API keys, Ghost, or
photo uploads. They exercise the actual editor serialization and Go export
function, then Ghost's official HTML importer and Lexical renderer. The fixture
contains 11 photos (one header plus ten body images), the market and dinner
headings, full width images, and four-photo groups. Checks assert every photo
URL survives exactly once, headings survive editor save and both export paths,
related photos stay in their section, and four-photo groups render as two rows
of two. A negative test reproduces Ghost dropping galleries without dimensions.
Go also tests the HTTP 422 fallback against a fake Ghost server.

The browser test checks desktop (1440px) and phone (390px) rendering: all images
load and have visible dimensions, headings are larger than body text, gallery
rows align, and the document has no horizontal overflow. It writes an offline
HTML preview and screenshots to `frontend/test-results/ghost-rendering/`, which
is ignored by Git. Images in this report are labeled placeholders.

To use the blog's public theme CSS instead of the basic test stylesheet, set
`YATRA_GHOST_SCREEN_CSS` and `YATRA_GHOST_CARDS_CSS` to local copies of
`/assets/built/screen.css` and `/public/cards.min.css` before the browser command.
These tests verify pinned Ghost rendering packages, not the installed server's
storage, theme templates, or image upload API. A live integration check remains
useful when changing those components.
