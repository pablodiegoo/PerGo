package webhook

import (
	"path"
)

// MatchEvent matches an incoming event (e.g. "message.received") against a pattern (e.g. "message.*" or "*").
func MatchEvent(pattern, eventName string) bool {
	if pattern == "*" {
		return true
	}
	matched, err := path.Match(pattern, eventName)
	if err != nil {
		return pattern == eventName // Fallback to exact match
	}
	return matched
}

// MatchesAny returns true if the eventName matches any of the patterns.
func MatchesAny(patterns []string, eventName string) bool {
	for _, pattern := range patterns {
		if MatchEvent(pattern, eventName) {
			return true
		}
	}
	return false
}
