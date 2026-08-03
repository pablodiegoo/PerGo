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

  // Prevent replay attacks (5 minutes tolerance)
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

### 2.2. Python (Flask / FastApi)

```python
import hmac
import hashlib
import time

def verify_pergo_signature(raw_body: bytes, signature_header: str, secret: str) -> bool:
    if not signature_header:
        return False
    
    parts = dict(part.split('=', 1) for part in signature_header.split(',') if '=' in part)
    timestamp = parts.get('t')
    expected_sig = parts.get('v1')
    
    if not timestamp or not expected_sig:
        return False
        
    # Prevent replay attacks (5 minutes tolerance)
    if abs(time.time() - int(timestamp)) > 300:
        return False
        
    # Compute HMAC signature
    message = f"{timestamp}.".encode('utf-8') + raw_body
    computed_sig = hmac.new(secret.encode('utf-8'), message, hashlib.sha256).hexdigest()
    
    return hmac.compare_digest(computed_sig, expected_sig)
```

### 2.3. Go (net/http / Echo)

```go
package webhookverifier

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func VerifyPerGoSignature(rawBody []byte, signatureHeader string, secret string) bool {
	if signatureHeader == "" {
		return false
	}

	var timestamp, expectedSig string
	parts := strings.Split(signatureHeader, ",")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			if kv[0] == "t" {
				timestamp = kv[1]
			} else if kv[0] == "v1" {
				expectedSig = kv[1]
			}
		}
	}

	if timestamp == "" || expectedSig == "" {
		return false
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Now().Unix()-ts > 300 || ts-time.Now().Unix() > 300 {
		return false // Replay protection (5 minutes tolerance)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(rawBody)
	computedSig := hex.EncodeToString(mac.Sum(nil))

	return subtle.ConstantTimeCompare([]byte(computedSig), []byte(expectedSig)) == 1
}
```
