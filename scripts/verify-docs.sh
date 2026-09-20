#!/usr/bin/env bash
set -euo pipefail

# ─────────────────────────────────────────────────────────────
# PerGo — Repository Health & Documentation Verification Script
# Validates governance files, forbidden legacy strings, and
# Markdown relative link integrity.
# ─────────────────────────────────────────────────────────────

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "=== [1/3] Checking Governance and Community Health Files ==="
GOVERNANCE_FILES=(
    "CODE_OF_CONDUCT.md"
    "SECURITY.md"
    "CONTRIBUTING.md"
    ".github/pull_request_template.md"
)

# Expand all .github/ISSUE_TEMPLATE/*.yml templates dynamically via glob
shopt -s nullglob
ISSUE_TEMPLATES=(.github/ISSUE_TEMPLATE/*.yml)
shopt -u nullglob

if [ ${#ISSUE_TEMPLATES[@]} -eq 0 ]; then
    echo "✗ No issue templates found matching .github/ISSUE_TEMPLATE/*.yml"
    GOV_ERRORS=1
else
    for tmpl in "${ISSUE_TEMPLATES[@]}"; do
        GOVERNANCE_FILES+=("$tmpl")
    done
    GOV_ERRORS=0
fi

for f in "${GOVERNANCE_FILES[@]}"; do
    if [ ! -f "$f" ]; then
        echo "✗ Missing governance file: $f"
        GOV_ERRORS=$((GOV_ERRORS + 1))
    elif [ ! -s "$f" ]; then
        echo "✗ Governance file is empty (0 bytes): $f"
        GOV_ERRORS=$((GOV_ERRORS + 1))
    else
        echo "✓ Found: $f ($(wc -c < "$f") bytes)"
    fi
done

if [ "$GOV_ERRORS" -gt 0 ]; then
    echo "ERROR: Governance file verification failed ($GOV_ERRORS errors)."
    exit 1
fi

echo ""
echo "=== [2/3] Checking for Forbidden Legacy Strings ==="
# Construct forbidden search strings dynamically to avoid self-match
LEGACY_NAME="Omni""Go"
LOCAL_PATH="file://""/home/pablo/"

EXCLUDES=(
    ":!scripts/verify-docs.sh"
    ":!cmd/pergo/documentation_test.go"
    ":!cmd/pergo/community_governance_test.go"
    ":!cmd/pergo/repository_health_test.go"
)

LEGACY_MATCHES=$(git grep -n -E "${LEGACY_NAME}|${LOCAL_PATH}" -- "${EXCLUDES[@]}" || true)

if [ -n "$LEGACY_MATCHES" ]; then
    echo "✗ Forbidden legacy strings detected:"
    echo "$LEGACY_MATCHES"
    echo "ERROR: Found references to legacy name or local absolute paths."
    exit 1
else
    echo "✓ Zero occurrences of legacy '$LEGACY_NAME' or local path '$LOCAL_PATH'."
fi

echo ""
echo "=== [3/3] Verifying Markdown Relative Link Integrity ==="
LINK_ERRORS=0

# Collect markdown files: README.md, CONTRIBUTING.md, and docs/**/*.md
MD_FILES=("README.md" "CONTRIBUTING.md" "CODE_OF_CONDUCT.md" "SECURITY.md")
while IFS= read -r file; do
    MD_FILES+=("$file")
done < <(find docs -type f -name "*.md" | sort)

for md_file in "${MD_FILES[@]}"; do
    [ -f "$md_file" ] || continue
    file_dir="$(dirname "$md_file")"

    # Extract markdown links [text](target)
    # Using grep and sed to parse link targets
    while IFS= read -r target; do
        [ -z "$target" ] && continue

        # Ignore external URLs, mailto, and internal page anchors
        if [[ "$target" =~ ^https?:// ]] || [[ "$target" =~ ^mailto: ]] || [[ "$target" =~ ^# ]]; then
            continue
        fi

        # Strip optional title e.g. [label](target "title")
        clean_target="${target%% *}"
        clean_target="${clean_target%\"}"
        clean_target="${clean_target#\"}"
        clean_target="${clean_target%\'}"
        clean_target="${clean_target#\'}"

        # Strip fragment anchor (#section)
        clean_target="${clean_target%%#*}"
        [ -z "$clean_target" ] && continue

        # Resolve path
        if [[ "$clean_target" == /* ]]; then
            resolved="$REPO_ROOT$clean_target"
        else
            resolved="$file_dir/$clean_target"
        fi

        # Normalize path
        if [ ! -e "$resolved" ]; then
            echo "✗ Dead link in $md_file: target '$target' -> '$resolved' does not exist"
            LINK_ERRORS=$((LINK_ERRORS + 1))
        fi
    done < <(grep -o -E '\[[^]]+\]\([^)]+\)' "$md_file" | sed -E 's/.*\]\(([^)]+)\)/\1/' || true)
done

if [ "$LINK_ERRORS" -gt 0 ]; then
    echo "ERROR: Found $LINK_ERRORS dead Markdown links."
    exit 1
else
    echo "✓ All relative Markdown links resolve to valid files."
fi

echo ""
echo "======================================================="
echo "✓ All repository health and documentation checks passed!"
echo "======================================================="
exit 0
