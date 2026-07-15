---
quick_id: "260715-f3q"
status: complete
---

# Summary: Review and Update Gitattributes and Gitignore for Agent Files

We have reviewed and updated the `.gitattributes` and `.gitignore` configurations to properly handle agent files.

## Actions taken:
1. **Task 1: Update `.gitattributes` ([.gitattributes](file:///home/pablo/Coding/OmniGo/.gitattributes))**
   - Configured `.agents/**`, `.opencode/**`, `.planning/**`, `AGENTS.md`, `CONTEXT.md`, `GEMINI.md`, and `skills-lock.json` with the `export-ignore` attribute.
   - This ensures that when the repository is packaged for distribution (using `git archive` or GitHub Release downloads), these agentic and planning files/directories are omitted from the archive.
2. **Task 2: Update `.gitignore` ([.gitignore](file:///home/pablo/Coding/OmniGo/.gitignore))**
   - Added `/graphify-out/` and `/.gemini/` to ensure local agent caches and graph output files do not get tracked in git.
3. **Task 3: Verification**
   - Verified that git archive respects `export-ignore` and correctly omits these files.

## Commits:
- `8e2a253` - Task 1: Add export-ignore attribute to agent configurations and planning files in gitattributes
- `3b28734` - Task 2: Ignore agentic outputs and caches in gitignore
- `6cd6db4` - Task 1 (amendment): Exclude skills-lock.json from git archive exports
