# ADR 003: HEIC Codec Support

**Date:** 2025-11-01
**Status:** Accepted

## Context

The local sample folder contains JPEG files. We need to verify whether the application's image processing stack (libvips + exiftool) can handle HEIC files.

## Decision

The sample folder contains only JPEG files (confirmed via `file` output: all 6048x4032 JPEG). However, the spec requires HEIC support as a future capability.

For the current version:
- The import process marks files with unsupported extensions as `unsupported` with a scan error, without crashing.
- The application must document that HEIC support depends on the Docker base image's libvips and libheif packages.
- If the built image does not include libheif, HEIC files appear in the "Unsupported" tab of the photo review grid.

## Consequences

- The vertical slice works without HEIC support (the test data is JPEG).
- HEIC support adds complexity to the Dockerfile (extra packages).
- Document the dependency clearly in deployment.md so users know they may need to rebuild for HEIC.
