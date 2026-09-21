// Package locales embeds the JSON translation catalogs for PerGo.
package locales

import "embed"

// FS embeds all JSON translation catalogs.
//
//go:embed *.json
var FS embed.FS
