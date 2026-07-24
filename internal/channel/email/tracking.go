package email

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var hrefRegex = regexp.MustCompile(`(?i)<a\s+([^>]*\s+)?href=["'](https?://[^"']+)["']`)

// GenerateTrackingHMAC generates a SHA256-HMAC signature for email tracking URLs.
func GenerateTrackingHMAC(messageID, payload, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(messageID + ":" + payload))
	return hex.EncodeToString(h.Sum(nil))
}

// VerifyTrackingHMAC verifies a SHA256-HMAC signature.
func VerifyTrackingHMAC(messageID, payload, sig, secret string) bool {
	expected := GenerateTrackingHMAC(messageID, payload, secret)
	return hmac.Equal([]byte(expected), []byte(sig))
}

// InjectOpenPixel injects a 1x1 GIF tracking pixel into the HTML body.
func InjectOpenPixel(htmlBody, messageID, baseURL, secret string) string {
	if htmlBody == "" {
		return ""
	}

	sig := GenerateTrackingHMAC(messageID, "open", secret)
	pixelURL := fmt.Sprintf("%s/v1/webhooks/email/open?m=%s&sig=%s", strings.TrimRight(baseURL, "/"), url.QueryEscape(messageID), sig)
	pixelTag := fmt.Sprintf(`<img src="%s" width="1" height="1" style="display:none !important;" alt="" />`, pixelURL)

	lower := strings.ToLower(htmlBody)
	if idx := strings.LastIndex(lower, "</body>"); idx != -1 {
		return htmlBody[:idx] + pixelTag + "\n" + htmlBody[idx:]
	}

	return htmlBody + "\n" + pixelTag
}

// RewriteClickLinks rewrites all HTTP/HTTPS links in the HTML body for click tracking.
func RewriteClickLinks(htmlBody, messageID, baseURL, secret string) string {
	if htmlBody == "" {
		return ""
	}

	trimmedBase := strings.TrimRight(baseURL, "/")

	return hrefRegex.ReplaceAllStringFunc(htmlBody, func(match string) string {
		submatches := hrefRegex.FindStringSubmatch(match)
		if len(submatches) < 3 {
			return match
		}

		targetURL := submatches[2]
		sig := GenerateTrackingHMAC(messageID, targetURL, secret)
		trackURL := fmt.Sprintf("%s/v1/webhooks/email/click?m=%s&url=%s&sig=%s",
			trimmedBase,
			url.QueryEscape(messageID),
			url.QueryEscape(targetURL),
			sig,
		)

		return strings.Replace(match, targetURL, trackURL, 1)
	})
}
