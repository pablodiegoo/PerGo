package i18n

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestI18n_BasicLookup(t *testing.T) {
	ctx := context.Background()

	// Default en-US
	if got := T(ctx, "sidebar.overview"); got != "Overview" {
		t.Errorf("expected 'Overview', got '%s'", got)
	}

	// pt-BR context
	ptCtx := WithLocale(ctx, "pt-BR")
	if got := T(ptCtx, "sidebar.overview"); got != "Visão Geral" {
		t.Errorf("expected 'Visão Geral', got '%s'", got)
	}

	// Normalized locale "pt_br"
	ptCtx2 := WithLocale(ctx, "pt_BR")
	if got := T(ptCtx2, "sidebar.overview"); got != "Visão Geral" {
		t.Errorf("expected 'Visão Geral', got '%s'", got)
	}
}

func TestI18n_FallbackToEnUS(t *testing.T) {
	engine := NewEngine()

	// Add an en-US key that is missing in pt-BR
	engine.catalogs[LocaleEnUS] = map[string]string{
		"only.in.en": "English Only Text",
		"both":       "Both EN",
	}
	engine.catalogs[LocalePtBR] = map[string]string{
		"both": "Both PT",
	}

	// 1. Existing key in pt-BR
	if got := engine.Translate("pt-BR", "both"); got != "Both PT" {
		t.Errorf("expected 'Both PT', got '%s'", got)
	}

	// 2. Missing key in pt-BR falls back to en-US
	if got := engine.Translate("pt-BR", "only.in.en"); got != "English Only Text" {
		t.Errorf("expected fallback 'English Only Text', got '%s'", got)
	}

	// 3. Unknown locale falls back to en-US
	if got := engine.Translate("de-DE", "only.in.en"); got != "English Only Text" {
		t.Errorf("expected fallback 'English Only Text', got '%s'", got)
	}

	// 4. Key missing in both returns [key]
	if got := engine.Translate("pt-BR", "totally.nonexistent.key"); got != "[totally.nonexistent.key]" {
		t.Errorf("expected '[totally.nonexistent.key]', got '%s'", got)
	}
	if got := engine.Translate("en-US", "totally.nonexistent.key"); got != "[totally.nonexistent.key]" {
		t.Errorf("expected '[totally.nonexistent.key]', got '%s'", got)
	}
}

func TestI18n_VariableInterpolation(t *testing.T) {
	ctx := context.Background()

	// Test {var} with map[string]any
	res1 := T(ctx, "connections.connected_as", map[string]any{"name": "WhatsApp Prod"})
	if res1 != "Connected as WhatsApp Prod" {
		t.Errorf("expected 'Connected as WhatsApp Prod', got '%s'", res1)
	}

	// Test {var} with M helper
	res2 := T(ctx, "connections.connected_as", M{"name": "Telegram Bot"})
	if res2 != "Connected as Telegram Bot" {
		t.Errorf("expected 'Connected as Telegram Bot', got '%s'", res2)
	}

	// Test {var} with key-value pairs
	res3 := T(ctx, "connections.connected_as", "name", "WhatsApp Dev")
	if res3 != "Connected as WhatsApp Dev" {
		t.Errorf("expected 'Connected as WhatsApp Dev', got '%s'", res3)
	}

	// Test multi-variable with key-value pairs
	res4 := T(ctx, "campaigns.sent_count", "sent", 42, "total", 100)
	if res4 != "42 of 100 sent" {
		t.Errorf("expected '42 of 100 sent', got '%s'", res4)
	}

	// Test pt-BR multi-variable with M helper
	ptCtx := WithLocale(ctx, "pt-BR")
	res5 := T(ptCtx, "campaigns.sent_count", M{"sent": 42, "total": 100})
	if res5 != "42 de 100 enviados" {
		t.Errorf("expected '42 de 100 enviados', got '%s'", res5)
	}

	// Test single positional arg matching single {var}
	res6 := T(ctx, "badges.failed_count", 5)
	if res6 != "5 failed" {
		t.Errorf("expected '5 failed', got '%s'", res6)
	}

	// Test %s sprintf formatting
	customEngine := NewEngine()
	customEngine.catalogs[LocaleEnUS] = map[string]string{
		"fmt.test": "Processing item %d of %s",
	}
	res7 := customEngine.Translate("en-US", "fmt.test", 3, "Batch A")
	if res7 != "Processing item 3 of Batch A" {
		t.Errorf("expected 'Processing item 3 of Batch A', got '%s'", res7)
	}
}

func TestI18n_ContextHelpers(t *testing.T) {
	ctx := context.Background()

	// Default
	if got := GetLocale(ctx); got != "en-US" {
		t.Errorf("expected default en-US, got %s", got)
	}

	// WithLocale valid
	ctxPt := WithLocale(ctx, "pt-BR")
	if got := GetLocale(ctxPt); got != "pt-BR" {
		t.Errorf("expected pt-BR, got %s", got)
	}

	// WithLocale normalized from "pt"
	ctxPtShort := WithLocale(ctx, "pt")
	if got := GetLocale(ctxPtShort); got != "pt-BR" {
		t.Errorf("expected pt-BR, got %s", got)
	}

	// WithLocale invalid falls back to default
	ctxInvalid := WithLocale(ctx, "fr-FR")
	if got := GetLocale(ctxInvalid); got != "en-US" {
		t.Errorf("expected fallback en-US, got %s", got)
	}

	// DefaultLocale and SupportedLocales
	if DefaultLocale() != "en-US" {
		t.Errorf("expected en-US, got %s", DefaultLocale())
	}
	locales := SupportedLocales()
	if len(locales) != 2 || locales[0] != "en-US" || locales[1] != "pt-BR" {
		t.Errorf("unexpected supported locales: %v", locales)
	}
}

func TestI18n_KeyParity(t *testing.T) {
	engine := DefaultEngine()

	enCatalog := engine.Catalog(LocaleEnUS)
	ptCatalog := engine.Catalog(LocalePtBR)

	if len(enCatalog) == 0 {
		t.Fatal("en-US catalog is empty")
	}
	if len(ptCatalog) == 0 {
		t.Fatal("pt-BR catalog is empty")
	}

	// Collect keys
	enKeys := make([]string, 0, len(enCatalog))
	for k := range enCatalog {
		enKeys = append(enKeys, k)
	}
	sort.Strings(enKeys)

	ptKeys := make([]string, 0, len(ptCatalog))
	for k := range ptCatalog {
		ptKeys = append(ptKeys, k)
	}
	sort.Strings(ptKeys)

	// Check 1: All en-US keys must exist in pt-BR
	for _, k := range enKeys {
		if _, ok := ptCatalog[k]; !ok {
			t.Errorf("Key '%s' found in en-US but missing in pt-BR", k)
		}
	}

	// Check 2: All pt-BR keys must exist in en-US
	for _, k := range ptKeys {
		if _, ok := enCatalog[k]; !ok {
			t.Errorf("Key '%s' found in pt-BR but missing in en-US", k)
		}
	}

	// Check 3: Placeholder parity
	varRegex := regexp.MustCompile(`\{[a-zA-Z0-9_-]+\}`)

	for _, k := range enKeys {
		enVal, okEn := enCatalog[k]
		ptVal, okPt := ptCatalog[k]
		if !okEn || !okPt {
			continue
		}

		enPlaceholders := varRegex.FindAllString(enVal, -1)
		ptPlaceholders := varRegex.FindAllString(ptVal, -1)

		sort.Strings(enPlaceholders)
		sort.Strings(ptPlaceholders)

		enJoined := strings.Join(enPlaceholders, ",")
		ptJoined := strings.Join(ptPlaceholders, ",")

		if enJoined != ptJoined {
			t.Errorf("Key '%s' placeholder mismatch: en-US has [%s] but pt-BR has [%s]", k, enJoined, ptJoined)
		}
	}
}
