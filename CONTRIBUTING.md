<!-- generated-by: gsd-doc-writer -->
# Contributing to PerGo

We welcome and appreciate contributions to PerGo! Whether you are reporting a bug, proposing a new feature, or submitting code changes, this guide will help you get started.

---

## Development Setup

See [GETTING-STARTED.md](docs/GETTING-STARTED.md) for prerequisites and first-run instructions, and [DEVELOPMENT.md](docs/DEVELOPMENT.md) for local development setup.

---

## Coding Standards

To maintain consistency and code quality across the codebase, please ensure your changes follow these standards:

* **Code Formatting**: Format all Go source files using `go fmt` and template files using `templ fmt` before submitting.
* **Linter Compliance**: All code must pass static analysis using `golangci-lint`. Run `make lint` to verify locally. This is enforced by CI on every push and pull request.
* **Testing**: Run tests with race detection using `make test-race` to ensure no concurrency issues. CI runs `go test -race` on all submissions.

---

## Pull Request Guidelines

When you are ready to submit your contributions, please follow these guidelines:

* **Branch Naming**: Create a new branch from `master` using prefixes like `feat/`, `fix/`, `refactor/`, or `docs/`.
* **Commit Messages**: Follow the **Conventional Commits** specification (e.g., `feat(channel): add discord adapter`).
* **Verification**: Ensure all tests pass (`make test-race`) and the linter is green (`make lint`) before submitting.
* **Pull Request**: Open a PR targeting the `master` branch. Provide a clear description of the problem solved, the changes made, and any new env variables or schema migrations.
* **Review & Merge**: At least one reviewer must approve the PR. Approved PRs are merged into `master` using the **Squash and Merge** strategy.

---

## Issue Reporting

If you encounter bugs, performance regressions, or security issues, please report them using the [GitHub Issues](https://github.com/pablodiegoo/OmniGo/issues) tracker.

* **Bug Reports**: Provide a description of the issue, clear steps to reproduce, the expected vs. actual behavior, and details about your environment (OS, Go version, Postgres version).
* **Feature Requests**: Describe the use case, the value to the community, and any initial design ideas.
