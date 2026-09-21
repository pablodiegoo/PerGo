// Package i18n provides a thread-safe, embedded localization engine for PerGo.
package i18n

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"sync"

	"github.com/pablojhp.pergo/locales"
)

const (
	// LocaleEnUS is the canonical base locale (English).
	LocaleEnUS = "en-US"
	// LocalePtBR is the Brazilian Portuguese locale.
	LocalePtBR = "pt-BR"
)

var (
	supportedLocales = []string{LocaleEnUS, LocalePtBR}
	placeholderRegex = regexp.MustCompile(`\{([a-zA-Z0-9_-]+)\}`)
)

// M is a convenience alias for a map of template variables.
type M map[string]any

type contextKey struct{}

var localeContextKey = contextKey{}

// SupportedLocales returns a copy of the supported locale codes.
func SupportedLocales() []string {
	out := make([]string, len(supportedLocales))
	copy(out, supportedLocales)
	return out
}

// DefaultLocale returns the canonical base locale.
func DefaultLocale() string {
	return LocaleEnUS
}

// NormalizeLocale standardizes language codes to canonical supported tags.
// Returns an empty string if the locale is not supported.
func NormalizeLocale(loc string) string {
	loc = strings.TrimSpace(loc)
	loc = strings.ReplaceAll(loc, "_", "-")
	lower := strings.ToLower(loc)
	switch {
	case lower == "en" || lower == "en-us" || strings.HasPrefix(lower, "en-"):
		return LocaleEnUS
	case lower == "pt" || lower == "pt-br" || strings.HasPrefix(lower, "pt-"):
		return LocalePtBR
	default:
		return ""
	}
}

// IsValidLocale checks whether a locale string is supported.
func IsValidLocale(locale string) bool {
	return NormalizeLocale(locale) != ""
}

// WithLocale returns a copy of parent context with the normalized locale attached.
func WithLocale(ctx context.Context, locale string) context.Context {
	norm := NormalizeLocale(locale)
	if norm == "" {
		norm = DefaultLocale()
	}
	return context.WithValue(ctx, localeContextKey, norm)
}

// GetLocale retrieves the locale from context, returning DefaultLocale() if not set or invalid.
func GetLocale(ctx context.Context) string {
	if ctx == nil {
		return DefaultLocale()
	}
	val, ok := ctx.Value(localeContextKey).(string)
	if !ok || val == "" {
		return DefaultLocale()
	}
	norm := NormalizeLocale(val)
	if norm == "" {
		return DefaultLocale()
	}
	return norm
}

// Engine holds compiled translation catalogs for thread-safe concurrent lookups.
type Engine struct {
	mu       sync.RWMutex
	catalogs map[string]map[string]string // locale -> (dotKey -> translation)
}

// NewEngine creates an empty translation Engine.
func NewEngine() *Engine {
	return &Engine{
		catalogs: make(map[string]map[string]string),
	}
}

// LoadFS loads all .json translation files from the provided filesystem.
func (e *Engine) LoadFS(fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}

		localeCode := strings.TrimSuffix(d.Name(), ".json")
		normLocale := NormalizeLocale(localeCode)
		if normLocale == "" {
			normLocale = localeCode
		}

		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("failed to unmarshal %s: %w", path, err)
		}

		flat := make(map[string]string)
		flatten("", raw, flat)

		e.mu.Lock()
		e.catalogs[normLocale] = flat
		e.mu.Unlock()

		return nil
	})
}

// Lookup finds a raw translated string in the given locale catalog.
func (e *Engine) Lookup(locale, key string) (string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	cat, ok := e.catalogs[locale]
	if !ok {
		return "", false
	}
	val, ok := cat[key]
	return val, ok
}

// Translate performs locale lookup, transparent fallback to en-US, variable interpolation,
// and returns "[key]" if the translation is not found in the base catalog.
func (e *Engine) Translate(locale, key string, args ...any) string {
	norm := NormalizeLocale(locale)
	if norm == "" {
		norm = DefaultLocale()
	}

	// 1. Try requested locale
	val, ok := e.Lookup(norm, key)
	if ok && val != "" {
		return interpolate(val, args...)
	}

	// 2. Transparent fallback to canonical en-US if requested locale differs
	if norm != DefaultLocale() {
		val, ok = e.Lookup(DefaultLocale(), key)
		if ok && val != "" {
			return interpolate(val, args...)
		}
	}

	// 3. Missing from canonical base -> return [key]
	return fmt.Sprintf("[%s]", key)
}

// Catalog returns a copy of the flattened translation map for a given locale.
func (e *Engine) Catalog(locale string) map[string]string {
	norm := NormalizeLocale(locale)
	if norm == "" {
		norm = locale
	}
	e.mu.RLock()
	defer e.mu.RUnlock()

	cat, ok := e.catalogs[norm]
	if !ok {
		return nil
	}
	out := make(map[string]string, len(cat))
	for k, v := range cat {
		out[k] = v
	}
	return out
}

var (
	defaultEngine *Engine
	defaultOnce   sync.Once
)

// DefaultEngine returns the singleton Engine initialized with embedded locales.
func DefaultEngine() *Engine {
	defaultOnce.Do(func() {
		defaultEngine = NewEngine()
		if err := defaultEngine.LoadFS(locales.FS); err != nil {
			// Embedded catalogs are verified by unit tests; fallback to empty if error occurs.
			_ = err
		}
	})
	return defaultEngine
}

// T translates key for the locale resolved from ctx, interpolating any provided args.
func T(ctx context.Context, key string, args ...any) string {
	loc := GetLocale(ctx)
	return DefaultEngine().Translate(loc, key, args...)
}

// flatten recursively converts a nested map into a dot-notation key-value map.
func flatten(prefix string, src map[string]any, dst map[string]string) {
	for k, v := range src {
		fullKey := k
		if prefix != "" {
			fullKey = prefix + "." + k
		}
		switch val := v.(type) {
		case string:
			dst[fullKey] = val
		case map[string]any:
			flatten(fullKey, val, dst)
		default:
			if val != nil {
				dst[fullKey] = fmt.Sprint(val)
			}
		}
	}
}

// interpolate replaces {var} named placeholders, %s printf specifiers, or positional args.
func interpolate(tmpl string, args ...any) string {
	if len(args) == 0 {
		return tmpl
	}

	// Case 1: Single map passed
	if len(args) == 1 {
		switch m := args[0].(type) {
		case map[string]any:
			return replaceMap(tmpl, m)
		case map[string]string:
			conv := make(map[string]any, len(m))
			for k, v := range m {
				conv[k] = v
			}
			return replaceMap(tmpl, conv)
		case M:
			return replaceMap(tmpl, m)
		}
	}

	// Case 2: Key-value pairs ("key", val, "key2", val2)
	if len(args) >= 2 && len(args)%2 == 0 {
		isKVPairs := true
		m := make(map[string]any, len(args)/2)
		for i := 0; i < len(args); i += 2 {
			k, ok := args[i].(string)
			if !ok {
				isKVPairs = false
				break
			}
			m[k] = args[i+1]
		}
		if isKVPairs {
			return replaceMap(tmpl, m)
		}
	}

	// Case 3: Printf format string with % verbs
	if strings.Contains(tmpl, "%") {
		formatted := fmt.Sprintf(tmpl, args...)
		if !strings.Contains(formatted, "%!(") {
			return formatted
		}
	}

	// Case 4: Positional interpolation into {var} placeholders
	matches := placeholderRegex.FindAllStringSubmatch(tmpl, -1)
	if len(matches) > 0 {
		// Replace in order of appearance
		res := tmpl
		for i, m := range matches {
			if i < len(args) {
				res = strings.Replace(res, m[0], fmt.Sprint(args[i]), 1)
			}
		}
		return res
	}

	return tmpl
}

func replaceMap(tmpl string, m map[string]any) string {
	return placeholderRegex.ReplaceAllStringFunc(tmpl, func(matched string) string {
		varName := matched[1 : len(matched)-1]
		if val, ok := m[varName]; ok {
			return fmt.Sprint(val)
		}
		return matched
	})
}
