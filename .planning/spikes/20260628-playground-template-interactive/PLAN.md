---
status: complete
date: 2026-06-28
description: Spike for interactive template selection and media upload in Developer Playground
---

# Plan - Interactive Playground WABA Templates & Media Uploads

Enhance the Developer Playground to support:
1. Dynamic retrieval of WABA templates for the selected workspace.
2. A clean dropdown list showing the templates.
3. Rendering complete metadata fields defensively (loop-based rendering for varied data formats).
4. Showing the template message body text with active variables.
5. Creating input boxes dynamically for every body variable.
6. Support for header media inputs (IMAGE/VIDEO/DOCUMENT) with a local S3 upload button.
7. Real-time client-side preview rendering.

## Tasks
- Add new endpoints in `PlaygroundHandler`:
  - `GET /admin/playground/templates` to return the template selector dropdown.
  - `GET /admin/playground/templates/details` to return the template details, preview, and input fields.
  - `POST /admin/playground/upload` to store any local files securely onto the MinIO bucket and return the media proxy path.
- Add `s3Client` and `templatesRepo` to `PlaygroundHandler`.
- Inject `s3Client` and `wabaTemplateRepo` into `NewPlaygroundHandler` in `main.go`.
- Implement dynamic template rendering in `playground.templ`.
- Add real-time javascript preview sync logic.
- Update `Send` handler to parse custom template components dynamically.
