# 06 — Make wsRepo a required parameter in NewTagAdminHandler

**What to build:** `NewTagAdminHandler` currently accepts `wsRepo` as a variadic `...*repository.WorkspaceRepository`, hiding a required dependency. The only call site in `main.go` always passes it. Change the signature to accept `wsRepo *repository.WorkspaceRepository` as a regular parameter. Update `RedirectToWorkspaceTags` to remove the nil-check on `h.wsRepo` (it's now guaranteed non-nil). Update all call sites and tests.

**Blocked by:** None — can start immediately.

**Status:** ready-for-agent

- [ ] `NewTagAdminHandler` signature: `func NewTagAdminHandler(tagRepo *repository.TagRepository, contactRepo *repository.ContactRepository, wsRepo *repository.WorkspaceRepository) *TagAdminHandler`
- [ ] `RedirectToWorkspaceTags` removes the `h.wsRepo != nil` guard (wsRepo is always set)
- [ ] `main.go` call site compiles without changes (already passes wsRepo)
- [ ] `tag_test.go` updated to pass a workspace repository in all `NewTagAdminHandler` calls
- [ ] All existing tag handler tests pass
