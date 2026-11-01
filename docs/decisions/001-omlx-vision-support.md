# ADR 001: VLM Support on OMLX Server

**Date:** 2025-11-01
**Status:** Accepted

## Context

The application requires a configurable vision-language model endpoint to analyse photos. The deployed OMLX instance is at `http://127.0.0.1:8000`.

## Decision

OMLX **does support image understanding** via `/v1/chat/completions`. Evidence from the OpenAPI spec:

- `/v1/chat/completions` accepts `content` arrays with an `InputItem` that has a field `image` (a base64 data URI).
- The `InputItem` type supports an `image` field: `"type": "string", "title": "Image"`.
- `response_format` supports `json_schema` for structured output.

However, the default API key is required (no unauthenticated inference). Configuration notes:

- Use env `OMLX_API_KEY` for the API key (or `skip_api_key_verification` to disable).
- The `v1/models` endpoint lists loaded models; the model's settings include `expose_as_model: true` to expose a model via the `/v1/` endpoints.
- The VLM model may show as "hidden" in the dashboard but is accessible via the API if `expose_as_model` is true.

## Consequences

- Configure the application's VLM model ID to match an exposed vision model.
- The API key must be provided (from environment variable).
- If the configured model does not support image input, the adapter must return an actionable error.
- The spec's `json_schema` mode for output is supported and recommended.

## Reference

- OMLX OpenAPI at `http://127.0.0.1:8000/openapi.json`
- Spec section 5: AI provider contract
