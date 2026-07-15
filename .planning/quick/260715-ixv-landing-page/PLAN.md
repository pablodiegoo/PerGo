---
wave: 1
depends_on: []
files_modified:
  - cmd/pergo/main.go
  - templates/pages/landing.templ
autonomous: true
---

# Plan: Landing Page for PerGo

## Objective
Create a beautiful static landing page served at the root route `/` of the application. The page will introduce PerGo, show a quick cURL code integration example for `POST /api/v1/messages`, and provide links to the Admin Console, documentation, and the code repository.

## Tasks

<tasks>
<task id="T1">
  <read_first>
    - templates/layout/base.templ
  </read_first>
  <acceptance_criteria>
    - `templates/pages/landing.templ` is created.
    - Landing page is styled using Tailwind CSS and DaisyUI.
    - Page includes CTA buttons linking to "/admin/", "https://github.com/pablodiegoo/OmniGo" (or a repository URL), and internal documentation paths.
    - Page displays a code integration snippet demonstrating:
      ```bash
      curl -X POST http://localhost:8080/api/v1/messages \
        -H "Authorization: Bearer <API_KEY>" \
        -H "Content-Type: application/json" \
        -d '{
          "channel": "whatsapp",
          "to": "+5511999999999",
          "body": "Hello from PerGo!"
        }'
      ```
  </acceptance_criteria>
  <action>
    Create the template file `templates/pages/landing.templ`. Design a modern landing page using custom gradients, dark mode features, and interactive buttons for the CTAs.
  </action>
</task>

<task id="T2">
  <read_first>
    - cmd/pergo/main.go
  </read_first>
  <acceptance_criteria>
    - `e.GET("/", ...)` is registered in `cmd/pergo/main.go`.
    - Accessing the root path `/` renders the newly created landing page template.
  </acceptance_criteria>
  <action>
    Modify `cmd/pergo/main.go` to import the new page template and register the root handler mapping `/` to render the landing template.
  </action>
</task>

<task id="T3">
  <read_first>
    - Makefile
  </read_first>
  <acceptance_criteria>
    - Code generation via `templ generate` compiles without errors.
    - The server compiles and runs without issues.
    - `go test ./...` passes.
  </acceptance_criteria>
  <action>
    Run `templ generate` to compile the templates, and then verify with `go test ./...`.
  </action>
</task>
</tasks>

## Must-Haves
- [ ] Accessing `GET /` returns the HTML landing page with 200 OK.
- [ ] All CTAs function and link correctly.
- [ ] Code compiles and tests pass.

## Artifacts this phase produces
- New template `templates/pages/landing.templ`.
- Root route handler for `/` in `cmd/pergo/main.go`.
