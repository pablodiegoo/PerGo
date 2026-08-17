#!/usr/bin/env bash
#
# A wizard — walks a human through setting up messaging channels (WABA, Telegram, WhatsApp Web)
# and configuring environment variables in .env / .env.seed.
#

set -euo pipefail

# ──────────────────────────────────────────────────────────────────────────
# Wizard library — delightful, consistent UX. Identical across every wizard.
# ──────────────────────────────────────────────────────────────────────────

if [[ -t 1 ]] && command -v tput >/dev/null 2>&1 && [[ "$(tput colors 2>/dev/null || echo 0)" -ge 8 ]]; then
  BOLD=$(tput bold); DIM=$(tput dim); RESET=$(tput sgr0)
  BLUE=$(tput setaf 4); GREEN=$(tput setaf 2); YELLOW=$(tput setaf 3); RED=$(tput setaf 1)
else
  BOLD=""; DIM=""; RESET=""; BLUE=""; GREEN=""; YELLOW=""; RED=""
fi

TOTAL_STAGES=3

_STAGE_INDEX=0
ENV_FILE="${ENV_FILE:-.env}"
WRITTEN_ENV=()
WRITTEN_SECRET=()
SKIPPED=()

DEFAULT_WORKSPACE_ID="a0000000-0000-0000-0000-000000000001"

_clear() {
  [[ -t 1 ]] || return 0
  if command -v tput >/dev/null 2>&1; then tput clear; else printf '\033[2J\033[3J\033[H'; fi
}

banner() {
  _clear
  printf '\n%s%s  %s%s\n' "$BOLD" "$BLUE" "$1" "$RESET"
  printf '%s  %s stages%s\n\n' "$DIM" "$TOTAL_STAGES" "$RESET"
  printf '%s  You drive the browser; this wizard tells you exactly what to do and\n' "$DIM"
  printf '  captures the values you copy back. Stop any time with Ctrl-C and re-run\n'
  printf '  later — it remembers values already saved.%s\n' "$RESET"
  pause "Ready to start?"
}

stage() {
  _clear
  _STAGE_INDEX=$((_STAGE_INDEX + 1))
  printf '\n%s%s▸ Stage %s/%s · %s%s\n' \
    "$BOLD" "$BLUE" "$_STAGE_INDEX" "$TOTAL_STAGES" "$1" "$RESET"
}

say()  { printf '  %s\n' "$1"; }
step() { printf '  %s•%s %s\n' "$BLUE" "$RESET" "$1"; }
note() { printf '  %s%s%s\n' "$DIM" "$1" "$RESET"; }
warn() { printf '  %s⚠ %s%s\n' "$YELLOW" "$1" "$RESET"; }

open_url() {
  local url="$1"
  printf '  %s↗ opening%s %s\n' "$GREEN" "$RESET" "$url"
  { if   command -v wslview     >/dev/null 2>&1; then wslview "$url"
    elif command -v explorer.exe >/dev/null 2>&1; then explorer.exe "$url"
    elif command -v xdg-open    >/dev/null 2>&1; then xdg-open "$url"
    elif command -v open        >/dev/null 2>&1; then open "$url"
    else warn "couldn't open a browser — visit it manually: $url"; fi
  } >/dev/null 2>&1 || warn "couldn't open a browser — visit it manually: $url"
}

pause() {
  printf '  %s%s%s ' "$DIM" "${1:-Press Enter to continue}" "$RESET"
  read -r _ || true
}

confirm() {
  local reply=""
  printf '  %s? %s [y/N] ' "$YELLOW" "$1"
  read -r reply || true
  [[ "$reply" =~ ^[Yy] ]]
}

_existing() {
  [[ -f "$ENV_FILE" ]] || return 1
  local line; line=$(grep -E "^${1}=" "$ENV_FILE" | tail -n1) || return 1
  printf '%s' "${line#*=}"
}

ask() {
  local key="$1" prompt="$2" current input
  current=$(_existing "$key" || true)
  if [[ -n "$current" ]]; then
    printf '  %s%s%s %s[Enter keeps current]%s ' "$BOLD" "$prompt" "$RESET" "$DIM" "$RESET"
  else
    printf '  %s%s%s ' "$BOLD" "$prompt" "$RESET"
  fi
  read -r input || true
  [[ -z "$input" && -n "$current" ]] && input="$current"
  printf -v "$key" '%s' "$input"
}

ask_secret() {
  local key="$1" prompt="$2" current input
  current=$(_existing "$key" || true)
  if [[ -n "$current" ]]; then
    printf '  %s%s%s %s[Enter keeps current]%s ' "$BOLD" "$prompt" "$RESET" "$DIM" "$RESET"
  else
    printf '  %s%s%s ' "$BOLD" "$prompt" "$RESET"
  fi
  read -rs input || true
  printf '\n'
  [[ -z "$input" && -n "$current" ]] && input="$current"
  printf -v "$key" '%s' "$input"
}

write_env() {
  local key="$1" value="$2" tmp
  touch "$ENV_FILE"
  tmp=$(mktemp)
  grep -vE "^${key}=" "$ENV_FILE" > "$tmp" || true
  printf '%s=%s\n' "$key" "$value" >> "$tmp"
  mv "$tmp" "$ENV_FILE"
  WRITTEN_ENV+=("$key")
  printf '  %s✓ wrote%s %s → %s\n' "$GREEN" "$RESET" "$key" "$ENV_FILE"
}

finish() {
  _clear
  printf '\n%s%s  ✓ Channels setup complete%s\n' "$BOLD" "$GREEN" "$RESET"
  (( ${#WRITTEN_ENV[@]} )) && note "wrote ${#WRITTEN_ENV[@]} value(s) to $ENV_FILE: ${WRITTEN_ENV[*]}"
  printf '\n'
}

# ──────────────────────────────────────────────────────────────────────────
# STAGES
# ──────────────────────────────────────────────────────────────────────────

banner "PerGo Channels Setup Wizard"

# ── Stage 1: Development Workspace Configuration ──────────────────────────
stage "Default Workspace Configuration"
say "PerGo uses a deterministic UUID for the development workspace."
say "Default UUID: $DEFAULT_WORKSPACE_ID"
ask WS_ID "Confirm or customize DEFAULT_WORKSPACE_ID [Enter keeps default]:"
if [[ -z "${WS_ID:-}" ]]; then
  WS_ID="$DEFAULT_WORKSPACE_ID"
fi
write_env "DEFAULT_WORKSPACE_ID" "$WS_ID"

# ── Stage 2: WhatsApp Cloud API (WABA) ───────────────────────────────────
stage "WhatsApp Cloud API (WABA)"
say "Configure Meta Graph API credentials for WABA integration."
open_url "https://developers.facebook.com/apps"
step "Copy your Meta Graph API System User / Access Token"
ask_secret ACCESS_TOKEN "Paste Meta Access Token:"
step "Copy your Phone Number ID"
ask PHONE_NUMBER_ID "Paste Phone Number ID:"
step "Copy your WhatsApp Business Account ID"
ask WHATSAPP_BUSINESS_ACCOUNT_ID "Paste WABA Account ID:"

if [[ -n "${ACCESS_TOKEN:-}" && -n "${PHONE_NUMBER_ID:-}" && -n "${WHATSAPP_BUSINESS_ACCOUNT_ID:-}" ]]; then
  write_env "ACCESS_TOKEN" "$ACCESS_TOKEN"
  write_env "PHONE_NUMBER_ID" "$PHONE_NUMBER_ID"
  write_env "WHATSAPP_BUSINESS_ACCOUNT_ID" "$WHATSAPP_BUSINESS_ACCOUNT_ID"
  note "WABA credentials saved."
else
  warn "Skipping WABA credentials (one or more fields empty)."
fi

# ── Stage 3: Telegram Bot ────────────────────────────────────────────────
stage "Telegram Bot"
say "Configure Telegram Bot Token and Secret for webhook delivery."
open_url "https://t.me/BotFather"
step "Create or select your bot with @BotFather to get the HTTP API token"
ask_secret TELEGRAM_BOT_TOKEN "Paste Telegram Bot Token (or press Enter to skip):"

if [[ -n "${TELEGRAM_BOT_TOKEN:-}" ]]; then
  ask TELEGRAM_BOT_USERNAME "Paste Telegram Bot Username (e.g. @my_bot):"
  ask_secret TELEGRAM_SECRET_TOKEN "Paste Telegram Secret Token (for webhook verification):"
  write_env "TELEGRAM_BOT_TOKEN" "$TELEGRAM_BOT_TOKEN"
  [[ -n "${TELEGRAM_BOT_USERNAME:-}" ]] && write_env "TELEGRAM_BOT_USERNAME" "$TELEGRAM_BOT_USERNAME"
  [[ -n "${TELEGRAM_SECRET_TOKEN:-}" ]] && write_env "TELEGRAM_SECRET_TOKEN" "$TELEGRAM_SECRET_TOKEN"
  note "Telegram credentials saved."
else
  note "Telegram setup skipped."
fi

finish
