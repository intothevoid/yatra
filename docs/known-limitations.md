# Travel Diary AI — Known Limitations

## v1 Limitations

### Vision Model
- No VLM models currently loaded on OMLX server. Vision analysis uses a fake adapter that returns deterministic results.
- To enable real vision analysis, load a vision-capable model (e.g., LLaVA, BakLLaVA) in OMLX.

### Ghost Integration
- JWT authentication implemented but not tested against a live Ghost instance (auth format may need adjustment).
- Gallery cards use simple HTML divs; native Ghost gallery cards require Lexical JSON which is not yet implemented.
- Image upload to Ghost implemented but not tested end-to-end.

### Image Processing
- HEIC support depends on `libheif` being installed. Not included in default Docker image.
- Perceptual hashing uses a simple average hash (aHash). More sophisticated methods (pHash, dHash) not implemented.
- No automatic image enhancement (sharpening, color correction).

### Editor
- Block reordering (drag-and-drop) UI is present but not fully wired.
- No inline formatting (bold, italic, links) — plain text only.
- No image alt text editing in the gallery blocks.

### Writing
- Style profile import from Ghost not tested (depends on Ghost integration).
- Content relevance scoring uses simple keyword matching, not semantic similarity.
- No automatic fact-checking beyond evidence traceability.

### Performance
- No caching of VLM responses (re-analyses on every run).
- No concurrent photo processing (single worker).
- No pagination for large photo sets.

### Security
- No authentication/authorization (assumes local/trusted network).
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
