---
status: complete
date: 2026-06-28
description: Global rename of the project from PerGo to PerGo to prevent trademark and copyright conflicts.
---

# Quick Task: renomear-projeto-para-pergo - Summary

## Work Done
1. **Module Name & Code Imports**:
   - Updated `go.mod` module name to `github.com/pablojhp.pergo`.
   - Updated all import definitions in Go source and test files to refer to the new module path.
2. **Directories & Files renamed**:
   - Renamed `cmd/pergo` to `cmd/pergo`.
   - Renamed `docs/PRD PerGo.md` to `docs/PRD PerGo.md`.
3. **Configuration & Environment variables**:
   - Updated `.air.toml` build output to `pergo`.
   - Updated `Makefile` targets, build/dev paths, and binary outputs.
   - Updated environment variable prefixes from `PERGO_` to `PERGO_` in all `.env` files, configs, and codebase references.
4. **Docs & Planning**:
   - Replaced all textual occurrences of `PerGo` and `pergo` with `PerGo` and `pergo` respectively in markdown documentation and plannings.
5. **Verification**:
   - Regenerated HTML template Go files with `make generate`.
   - Ran `make test` unit test suite and verified that 100% of tests pass under the new name.
