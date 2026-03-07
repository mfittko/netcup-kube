package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// classifyInstrument – must mirror the JS sets + heuristic exactly
// ---------------------------------------------------------------------------

func TestClassifyInstrument_ExplicitCommodities(t *testing.T) {
	for _, slug := range []string{"brent-crude-oil", "wti-crude-oil", "natural-gas", "gold", "silver", "platinum"} {
		if got := classifyInstrument(slug); got != "commodities" {
			t.Errorf("classifyInstrument(%q) = %q, want commodities", slug, got)
		}
	}
}

func TestClassifyInstrument_ExplicitIndices(t *testing.T) {
	for _, slug := range []string{"spx", "tech100-usd", "us30-usd", "de30-eur", "uk100-gbp", "jp225-usd", "fr40-eur", "vix"} {
		if got := classifyInstrument(slug); got != "indices" {
			t.Errorf("classifyInstrument(%q) = %q, want indices", slug, got)
		}
	}
}

func TestClassifyInstrument_ExplicitCurrencies(t *testing.T) {
	for _, slug := range []string{"eur-usd", "usd-jpy", "gbp-usd", "usd-chf", "usd-cad", "aud-usd", "nzd-usd"} {
		if got := classifyInstrument(slug); got != "currencies" {
			t.Errorf("classifyInstrument(%q) = %q, want currencies", slug, got)
		}
	}
}

func TestClassifyInstrument_CryptoCoin_NoHyphen(t *testing.T) {
	for _, slug := range []string{"bitcoin", "ethereum", "solana", "dogecoin"} {
		if got := classifyInstrument(slug); got != "crypto-coin" {
			t.Errorf("classifyInstrument(%q) = %q, want crypto-coin", slug, got)
		}
	}
}

func TestClassifyInstrument_CryptoCoin_MultiHyphen(t *testing.T) {
	// Three parts (two hyphens) → NOT the single-hyphen heuristic → crypto-coin.
	for _, slug := range []string{"bitcoin-cash-abc", "some-multi-part"} {
		if got := classifyInstrument(slug); got != "crypto-coin" {
			t.Errorf("classifyInstrument(%q) = %q, want crypto-coin", slug, got)
		}
	}
}

func TestClassifyInstrument_SingleHyphenHeuristic(t *testing.T) {
	// Unknown slug with exactly one hyphen → currencies heuristic.
	if got := classifyInstrument("foo-bar"); got != "currencies" {
		t.Errorf("classifyInstrument(%q) = %q, want currencies", "foo-bar", got)
	}
}

func TestClassifyInstrument_CaseInsensitive(t *testing.T) {
	if got := classifyInstrument("GOLD"); got != "commodities" {
		t.Errorf("classifyInstrument(GOLD) = %q, want commodities", got)
	}
	if got := classifyInstrument("EUR-USD"); got != "currencies" {
		t.Errorf("classifyInstrument(EUR-USD) = %q, want currencies", got)
	}
	if got := classifyInstrument("Bitcoin"); got != "crypto-coin" {
		t.Errorf("classifyInstrument(Bitcoin) = %q, want crypto-coin", got)
	}
}

func TestClassifyInstrument_Whitespace(t *testing.T) {
	if got := classifyInstrument("  gold  "); got != "commodities" {
		t.Errorf("classifyInstrument with whitespace = %q, want commodities", got)
	}
}

// ---------------------------------------------------------------------------
// formatMarkdown – mirrors JS bullet-list output
// ---------------------------------------------------------------------------

func TestFormatMarkdown_Empty(t *testing.T) {
	payload := fxPayload{Prices: nil}
	out := formatMarkdown(payload)
	// Should at least emit the heading.
	if !strings.Contains(out, "## FXEmpire rates") {
		t.Errorf("expected FXEmpire rates heading, got: %q", out)
	}
}

func TestFormatMarkdown_SingleInstrument(t *testing.T) {
	last := 75.5
	change := -0.32
	pct := -0.42
	lu := "2024-01-01T00:00:00Z"
	payload := fxPayload{
		Prices: []fxPrice{
			{Slug: "brent-crude-oil", Name: "Brent Crude Oil", Last: &last, Change: &change, Pct: &pct, LastUpdate: &lu},
		},
	}
	out := formatMarkdown(payload)

	if !strings.Contains(out, "## FXEmpire rates") {
		t.Errorf("missing heading:\n%s", out)
	}
	if !strings.Contains(out, "**Brent Crude Oil**") {
		t.Errorf("missing bold name:\n%s", out)
	}
	if !strings.Contains(out, "(brent-crude-oil)") {
		t.Errorf("missing slug in parens:\n%s", out)
	}
	if !strings.Contains(out, "75.5") {
		t.Errorf("missing price 75.5:\n%s", out)
	}
	if !strings.Contains(out, "-0.42%") {
		t.Errorf("missing pct -0.42%%:\n%s", out)
	}
	if !strings.Contains(out, "lastUpdate") {
		t.Errorf("missing lastUpdate:\n%s", out)
	}
}

func TestFormatMarkdown_NilPriceRendersNull(t *testing.T) {
	payload := fxPayload{
		Prices: []fxPrice{
			{Slug: "gold", Name: "Gold"},
		},
	}
	out := formatMarkdown(payload)
	if !strings.Contains(out, "null") {
		t.Errorf("nil Last should render as 'null', got:\n%s", out)
	}
}

func TestFormatMarkdown_WithError(t *testing.T) {
	errMsg := "network timeout"
	payload := fxPayload{PricesError: &errMsg}
	out := formatMarkdown(payload)

	if !strings.Contains(out, "- ERROR: network timeout") {
		t.Errorf("expected error line, got:\n%s", out)
	}
}

func TestFormatMarkdown_PipeEscaped(t *testing.T) {
	name := "A|B"
	last := 100.0
	payload := fxPayload{
		Prices: []fxPrice{{Slug: "ab", Name: name, Last: &last}},
	}
	out := formatMarkdown(payload)
	if strings.Contains(out, "A|B") && !strings.Contains(out, `A\|B`) {
		t.Errorf("pipe in name should be escaped, got:\n%s", out)
	}
}

func TestFormatMarkdown_NoChangeNoPct_OmitsParens(t *testing.T) {
	last := 75.5
	payload := fxPayload{
		Prices: []fxPrice{
			{Slug: "gold", Name: "Gold", Last: &last},
		},
	}
	out := formatMarkdown(payload)
	// When both change and pct are nil, no parenthesised (change, pct%) block.
	if strings.Contains(out, "(%") || strings.Contains(out, ", %)") {
		t.Errorf("unexpected change/pct block when both nil:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// JSON output structure
// ---------------------------------------------------------------------------

func TestFxPayload_JSONFields(t *testing.T) {
	last := 75.5
	pct := -0.42
	change := -0.32
	lu := "2024-01-01T00:00:00Z"
	payload := fxPayload{
		Meta:     fxMeta{Now: "now", Locale: "en", Commodities: []string{"gold"}},
		RatesURL: "https://example.com",
		Prices: []fxPrice{
			{Slug: "gold", Name: "Gold", Last: &last, Change: &change, Pct: &pct, LastUpdate: &lu},
		},
	}

	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	for _, key := range []string{"meta", "ratesUrl", "prices", "pricesError"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q to be present", key)
		}
	}
}

func TestFxPrice_JSONFields(t *testing.T) {
	last := 75.5
	payload := fxPrice{Slug: "gold", Name: "Gold", Last: &last}

	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	for _, key := range []string{"slug", "name", "last"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q to be present", key)
		}
	}
}

// ---------------------------------------------------------------------------
// coalesce / coalesceStr helpers
// ---------------------------------------------------------------------------

func TestCoalesce_PreferFirst(t *testing.T) {
	a := 1.0
	b := 2.0
	got := coalesce(&a, &b)
	if got == nil || *got != 1.0 {
		t.Errorf("coalesce should prefer first non-nil: got %v", got)
	}
}

func TestCoalesce_FallbackToSecond(t *testing.T) {
	b := 2.0
	got := coalesce(nil, &b)
	if got == nil || *got != 2.0 {
		t.Errorf("coalesce should fall back to second: got %v", got)
	}
}

func TestCoalesce_BothNil(t *testing.T) {
	got := coalesce(nil, nil)
	if got != nil {
		t.Errorf("coalesce(nil, nil) = %v, want nil", got)
	}
}

func TestCoalesceStr(t *testing.T) {
	if got := coalesceStr("a", "b"); got != "a" {
		t.Errorf("coalesceStr(a, b) = %q, want a", got)
	}
	if got := coalesceStr("", "b"); got != "b" {
		t.Errorf("coalesceStr('', b) = %q, want b", got)
	}
	if got := coalesceStr("", ""); got != "" {
		t.Errorf("coalesceStr('', '') = %q, want ''", got)
	}
}
