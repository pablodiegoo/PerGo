# Contributing to PerGo

We welcome and appreciate contributions to PerGo! Whether you are reporting a bug, proposing a new messaging channel, improving documentation, or submitting code changes, this guide will help you get started.

---

## Community Governance & Health

PerGo adheres to standard open-source governance guidelines:

* **Code of Conduct**: All contributors, maintainers, and community members are expected to uphold our [Code of Conduct](CODE_OF_CONDUCT.md) (Contributor Covenant v2.1). Violations can be reported directly to `pablodiegoo@gmail.com`.
* **Security Policy**: If you discover a potential vulnerability, please follow our [Security Policy](SECURITY.md) to report it via GitHub Private Vulnerability Reporting or email rather than opening a public issue.

---

## Getting Started & Local Setup

1. **Prerequisites**: Go 1.25+, Docker & Docker Compose, and Make.
2. **First Run**: Review [GETTING-STARTED.md](docs/context/GETTING-STARTED.md) for quick installation and setup.
3. **Local Development**:
   - Start shared infrastructure (Postgres, NATS, Redis, MinIO, Mailpit):
     ```bash
     make infra
     ```
   - Run PerGo with live-reload (Air + Templ):
     ```bash
     make dev
     ```
   - For detailed architecture and configuration, consult [DEVELOPMENT.md](docs/context/DEVELOPMENT.md).

---

## Issue Reporting

All issue tracking takes place on the [GitHub Issues](https://github.com/pablodiegoo/PerGo/issues) tracker. Please use the appropriate structured issue form:

* **[Bug Report](https://github.com/pablodiegoo/PerGo/issues/new?template=bug_report.yml)**: Include OS, Go version, affected messaging channel, logs with redacted PII, and reproduction steps.
* **[Feature Request](https://github.com/pablodiegoo/PerGo/issues/new?template=feature_request.yml)**: Capture your use cases, technical design proposals, and community impact.
* **[Channel Request](https://github.com/pablodiegoo/PerGo/issues/new?template=channel_request.yml)**: Propose new messaging providers (Discord, Instagram, RCS, Email, etc.).
* **Questions & Support**: Please use [GitHub Discussions](https://github.com/pablodiegoo/PerGo/discussions) for open-ended questions, configuration help, and architecture ideas.

---

## Coding Standards

To maintain consistency and code quality across the codebase:

* **Formatting**: Format Go code with `go fmt` and templ templates with `templ fmt` (or `make generate`).
* **Static Analysis**: All code must pass `golangci-lint` without warnings:
  ```bash
  make lint
  ```
* **Testing & Concurrency**: All tests must pass with race detection enabled:
  ```bash
  make test-race
  ```

---

## Pull Request Workflow

1. **Branching**:
   - Always branch off the latest `main` branch.
   - Use descriptive branch prefixes:
     - `feat/<short-name>`: New features or channel adapters
     - `fix/<short-name>`: Bug fixes
     - `refactor/<short-name>`: Refactoring without behavior change
     - `docs/<short-name>`: Documentation improvements
     - `test/<short-name>`: Test improvements
2. **Commit Messages**:
   - Follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:
     ```text
     feat(channel): add discord adapter
     fix(inbound): handle nil contact gracefully
     docs(readme): update quickstart instructions
     ```
3. **Visual UI Gate**:
   - When modifying frontend templates (`templates/`), CSS, or UI views, attach Before / After screenshots or recordings to the pull request as guided in `.github/pull_request_template.md`.
4. **Submitting**:
   - Open a PR targeting the `main` branch of [https://github.com/pablodiegoo/PerGo](https://github.com/pablodiegoo/PerGo).
   - Fill out the provided [Pull Request Template](.github/pull_request_template.md), confirming that all pre-flight checklist items are checked.
5. **Review & Merge**:
   - CI runs `make lint` and `make test-race` automatically on all pull requests.
   - PRs require approval before being squashed and merged into `main`.
