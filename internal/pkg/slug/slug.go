package slug

import (
	"regexp"
	"strings"
)

var (
	nonAlphaNumRegex = regexp.MustCompile(`[^a-z0-9-]+`)
	multiHyphenRegex = regexp.MustCompile(`-+`)
)

// Generate converts a human-readable name into a URL-friendly slug.
// It converts text to lowercase, replaces spaces and underscores with hyphens,
// removes non-alphanumeric characters (except hyphens), and deduplicates consecutive hyphens.
func Generate(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	s = nonAlphaNumRegex.ReplaceAllString(s, "")
	s = multiHyphenRegex.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
