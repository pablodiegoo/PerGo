## Summary of Changes

<!-- Briefly describe the purpose of this pull request and the problems it solves. -->

## Related Issue(s)

<!-- Link any related issues or tracking tickets (e.g. Fixes #123, Part of #45). -->
Fixes #

## Type of Change

- [ ] `feat`: New feature or capability
- [ ] `fix`: Bug fix
- [ ] `refactor`: Code refactoring without behavioral change
- [ ] `docs`: Documentation updates
- [ ] `test`: Adding or updating tests
- [ ] `chore`: Tooling, build system, or dependencies

---

## Visual UI Gate (UI & Template Changes)

<!-- PerGo enforces a visual UI gate for changes touching templ files, CSS, Tailwind, or frontend components. -->
- [ ] **Visual Verification**: I have attached Before / After screenshots or a screen recording demonstrating the UI behavior.
- [ ] **N/A**: This PR does not touch templates, CSS, or user-facing UI.

### Screenshots / Recordings (if applicable)

| Before | After |
| ------ | ----- |
| <!-- Image / GIF / None --> | <!-- Image / GIF / None --> |

---

## Pre-flight Contributor Checklist

Please verify each item before requesting review:

- [ ] **Tests with Race Detector**: All tests pass locally with race detection (`make test-race`).
- [ ] **Linting & Quality**: Linter runs clean without issues (`make lint`).
- [ ] **Formatting**: Formatted all Go files (`go fmt`) and templ components (`templ fmt` / `make generate`).
- [ ] **Conventional Commits**: Commit messages follow the [Conventional Commits](https://www.conventionalcommits.org/) format (e.g., `feat(channel): add discord adapter`).
- [ ] **Documentation**: Updated relevant documentation in `docs/` or `README.md` if behavior or environment variables changed.
- [ ] **Visual UI Gate**: UI screenshot validation completed (or marked N/A).
