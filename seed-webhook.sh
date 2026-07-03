#!/usr/bin/env bash
# Trigger a WABA inbound webhook to test the inbox toast notification.
#
# Usage:  ./seed-webhook.sh [from_phone]
# Default from_phone: 15559998877 (a NEW contact, distinct from seeded ones)
#
# Prerequisites:
#   - pergo server running on localhost:8080
#   - run `go run ./cmd/pergo-seed/` first (creates workspace + connection)
#
# The workspace_id and display phone are read from the seeded data.
set -euo pipefail

FROM_PHONE="${1:-15559998877}"
WORKSPACE_ID="143cde23-5d03-450e-9d37-08282cd2bf2b"
PHONE_NUMBER_ID="105408512621900"
DISPLAY_PHONE="15551357931"

PAYLOAD=$(cat <<EOF
{
  "object": "whatsapp_business_account",
  "entry": [{
    "id": "102787872887154",
    "changes": [{
      "field": "messages",
      "value": {
        "messaging_product": "whatsapp",
        "metadata": {
          "display_phone_number": "${DISPLAY_PHONE}",
          "phone_number_id": "${PHONE_NUMBER_ID}"
        },
        "contacts": [{
          "profile": { "name": "New Contact" },
          "wa_id": "${FROM_PHONE}"
        }],
        "messages": [{
          "from": "${FROM_PHONE}",
          "id": "wamid.test$(date +%s)",
          "timestamp": "$(date +%s)",
          "type": "text",
          "text": { "body": "Nova mensagem de teste para o toast!" }
        }]
      }
    }]
  }]
}
EOF
)

echo "Triggering WABA webhook for workspace ${WORKSPACE_ID}"
echo "From: ${FROM_PHONE} (display: ${DISPLAY_PHONE})"
echo ""

curl -s -X POST "http://localhost:8080/webhooks/waba/${WORKSPACE_ID}" \
  -H "Content-Type: application/json" \
  -d "${PAYLOAD}" \
  -w "\nHTTP status: %{http_code}\n"

echo ""
echo "==> Now keep a DIFFERENT conversation open in the inbox at"
echo "    http://localhost:8080/admin/inbox"
echo "    The toast should appear top-center within ~3s (poll interval)."
