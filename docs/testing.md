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
