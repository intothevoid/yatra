# Travel Diary AI — Known Limitations

## Current Limitations

### Vision Model
- Real photo analysis requires a vision-capable model configured in OMLX and selected with `OMLX_VISION_MODEL` (or `OMLX_WRITER_MODEL` as fallback).
- If no API key/model is configured, the development fake adapters are used.

### Ghost Integration
- Ghost Admin API authentication and draft creation still need end-to-end verification against the target Ghost site.

### Image Processing
- HEIC support depends on `libheif` being installed. Not included in default Docker image.
- Perceptual hashing uses a simple average hash (aHash). More sophisticated methods (pHash, dHash) not implemented.
- No automatic image enhancement (sharpening, color correction).

### Editor
- The browser editor uses Ghost's Koenig editor. Image upload and gallery editing inside a saved article are still being refined.

### Writing
- Style profile import from Ghost not tested (depends on Ghost integration).
- Content relevance scoring uses simple keyword matching, not semantic similarity.
- No automatic fact-checking beyond evidence traceability.

### Performance
- No caching of VLM responses (re-analyses on every run).
- Photo processing uses a single worker.
- Photo grids are not paginated.

### Security
- No authentication/authorization (assumes local/trusted network). The default Compose port binding makes the app reachable by devices on the host's network.
- No rate limiting on API endpoints.
- No CSRF protection (HTMX requests from same origin).

## Future Improvements

- [ ] Load a vision model in OMLX for real photo analysis
- [ ] Test Ghost integration end-to-end with a live instance
- [ ] Implement native Ghost gallery cards (Lexical JSON)
- [ ] Add HEIC support to Docker image
- [ ] Implement block reordering with drag-and-drop
- [ ] Add inline formatting support
- [ ] Implement VLM response caching
- [ ] Add concurrent photo processing
- [ ] Add authentication for LAN deployment
- [ ] Add rate limiting and CSRF protection
