# ADR 002: Ghost Gallery API Support

**Date:** 2025-11-01
**Status:** Probing deferred (see notes)

## Context

We need to verify whether the user's Ghost installation (`https://travel.karan.myds.me/`) supports native gallery cards, and whether we can create a draft with a gallery and edit it successfully.

## Decision

**Deferred** — The user requested focus on milestones 0 and 1. The Ghost integration (Milestone 5) does not block the vertical slice. When this probe runs:

- Test `POST /ghost/api/admin/images/upload/` with a test image (2560px max).
- Test creating a draft with a gallery card and reading it back.
- If the gallery card fails, fall back to uploading images individually.

## Notes

- The spec's "native gallery round-trip" is a probe target; this ADR records whether it passes and what fallback to use.
- Per spec: "When the gallery card fails, upload images individually and write caption text."
