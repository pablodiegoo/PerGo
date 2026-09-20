# Outbound Webhook Security Signatures (HMAC-SHA256)

PerGo secures all outbound HTTP webhooks delivered to subscribers using an HMAC-SHA256 signature header (`X-PerGo-Signature`). This allows your application server to cryptographically verify that webhook payloads originated from your PerGo gateway and were not tampered with in transit.

---

## 1. Signature Header Format

The `X-PerGo-Signature` header uses the standard timestamped format:

```http
X-PerGo-Signature: t=1754179200,v1=a1b2c3d4e5f67890abcdef1234567890abcdef1234567890abcdef1234567890
```

Where:
- `t`: Unix timestamp (in seconds) when the signature was computed.
- `v1`: Hex-encoded HMAC-SHA256 digest of `${timestamp}.${raw_payload_bytes}` using your workspace or subscription secret key.

---

## 2. Verification Code Examples

### 2.1. Node.js (TypeScript / Express)

```javascript
const crypto = require('crypto');

function verifyPerGoSignature(rawBody, signatureHeader, secret) {
  if (!signatureHeader) return false;

  const parts = signatureHeader.split(',');
  let timestamp = '';
  let expectedSignature = '';

  for (const part of parts) {
    const [key, value] = part.split('=');
    if (key === 't') timestamp = value;
    if (key === 'v1') expectedSignature = value;
  }

  if (!timestamp || !expectedSignature) return false;

  // Prevent replay attacks (5 minutes tolerance window)
  const now = Math.floor(Date.now() / 1000);
  if (Math.abs(now - parseInt(timestamp, 10)) > 300) return false;

  // Compute HMAC signature
  const hmac = crypto.createHmac('sha256', secret);
  hmac.update(`${timestamp}.`);
  hmac.update(rawBody);
  const computedSignature = hmac.digest('hex');

  return crypto.timingSafeEqual(
    Buffer.from(computedSignature, 'utf8'),
    Buffer.from(expectedSignature, 'utf8')
  );
}
```

### 2.2. Python (FastAPI / Flask)

```python
import hmac
import hashlib
import time

def verify_pergo_signature(raw_body: bytes, signature_header: str, secret: str) -> bool:
    if not signature_header:
        return False

    parts = dict(part.split('=', 1) for part in signature_header.split(','))
    timestamp = parts.get('t')
    expected_signature = parts.get('v1')

    if not timestamp or not expected_signature:
        return False

    # Prevent replay attacks (5 minutes tolerance window)
    now = int(time.time())
    if abs(now - int(timestamp)) > 300:
        return False

    # Compute HMAC-SHA256 signature
    payload = f"{timestamp}.".encode('utf-8') + raw_body
    computed_signature = hmac.new(
        secret.encode('utf-8'),
        payload,
        hashlib.sha256
    ).hexdigest()

    return hmac.compare_digest(computed_signature, expected_signature)
```

### 2.3. Go

```go
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func VerifySignature(rawBody []byte, header string, secret string) bool {
	var timestamp, signature string
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			timestamp = kv[1]
		case "v1":
			signature = kv[1]
		}
	}

	if timestamp == "" || signature == "" {
		return false
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(ts, 0)).Abs() > 5*time.Minute {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%s.", timestamp)))
	mac.Write(rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expected))
}
```
