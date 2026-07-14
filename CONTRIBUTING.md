<!-- generated-by: gsd-doc-writer -->
# Contributing to PerGo

We welcome and appreciate contributions to PerGo! Whether you are reporting a bug, proposing a new feature, or submitting code changes, this guide will help you get started.

---

## Development Setup

Before you start writing code, please refer to the following guides to set up your local development environment:

* **[GETTING-STARTED.md](docs/GETTING-STARTED.md)** — Core prerequisites, infrastructure dependencies (Postgres & NATS), and running the application for the first time.
* **[DEVELOPMENT.md](docs/DEVELOPMENT.md)** — Detailed repository directory layout, build commands, and detailed instructions for modifying UI components.

---

## Coding Standards

To maintain consistency and code quality across the codebase, please ensure your changes follow these standards:

* **Code Formatting**: Format all Go code using `go fmt` and template files using `templ fmt` before submitting.
* **Linter Compliance**: Run static analysis checks locally to verify that your code adheres to standard Go best practices. All code must pass the project linter:
  ```bash
  make lint
  ```
* **Test Coverage**: We encourage writing table-driven tests for new features. Ensure existing tests pass and run concurrency validation checks using:
  ```bash
  make test-race
  ```

---

## Branch and Commit Conventions

We follow structured guidelines for branch naming and commit messages to keep the git history clean and understandable:

* **Branch Naming**: Create a new branch from `master` using the following prefixes:
  - `feat/<description>` for new features
  - `fix/<description>` for bug fixes
  - `refactor/<description>` for structural improvements without logical changes
  - `docs/<description>` for documentation updates
* **Commit Messages**: We follow the **Conventional Commits** specification:
  - Format: `<type>(<scope>): <short summary>`
  - Examples:
    - `feat(channel): add discord adapter`
    - `fix(session): resolve connection race condition on startup`
    - `docs(api): update rate limiting headers documentation`

---

## Pull Request Guidelines

When you are ready to submit your contributions, please follow this process:

1. **Verify Locally**: Ensure that all unit tests pass, the race detector reports no issues (`make test-race`), and the linter is green (`make lint`).
2. **Open a PR**: Submit a Pull Request pointing to the `master` branch.
3. **Describe Changes**: Provide a clear description of the problem you are solving, the changes you made, and any environment or database schema migrations introduced.
4. **Code Review**: At least one reviewer must approve the PR before it is merged.
5. **Integration**: Approved PRs are merged into `master` using the **Squash and Merge** strategy.

---

## Issue Reporting

If you encounter bugs, performance regressions, or security issues, please report them using our issue tracker:

* **Bug Reports**: Provide a clear description of the bug, steps to reproduce, the expected behavior, and details about your running environment (OS, Go version, Postgres version).
* **Feature Requests**: Describe the use case, why this feature would be valuable to the community, and any initial thoughts on how it could be designed or structured.
