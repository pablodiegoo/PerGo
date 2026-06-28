---
status: in-progress
date: 2026-06-28
description: Rename the PerGo project to PerGo globally across codebase, configs, and documentation.
---

# Plan - Renomear Projeto para PerGo

Globally rename the project from "PerGo" to "PerGo" due to trademark/copyright precautions.

## Tasks
1. **Rename module path**:
   - Update `go.mod` to module `github.com/pablojhp.pergo`.
   - Update all import statements in Go files from `github.com/pablojhp.pergo` to `github.com/pablojhp.pergo`.
2. **Rename directories & files**:
   - Rename directory `cmd/pergo` to `cmd/pergo`.
3. **Update build and dev tooling configuration**:
   - Update `.air.toml` build commands and binary paths.
   - Update `Makefile` references to `pergo` (both as target, run script, build commands, binary name).
   - Update `Dockerfile` to build `./cmd/pergo` and output to `/app/pergo`.
   - Update `docker-compose.yml` service name and env variables where appropriate.
4. **Update environment files**:
   - Update `.env`, `.env.example`, `.env.seed` to rename prefixed env variables if applicable (e.g. `PERGO_` -> `PERGO_`).
     Wait, do we rename environment variables prefix?
     Let's check if the user wanted that: "renomeamos todo o projeto com esse nome". Yes, keeping them as `PERGO_` when the project is called `PerGo` is inconsistent. Let's rename the environment variable prefix from `PERGO_` to `PERGO_` as well, updating `internal/config/config.go` and other files reading them.
5. **Update docs and metadata**:
   - Update `README.md` and `AGENTS.md` and `.planning/` references from "PerGo" to "PerGo".
6. **Verify and build**:
   - Generate template files (`templ generate`).
   - Run tests (`make test`).
   - Ensure the project builds successfully.
