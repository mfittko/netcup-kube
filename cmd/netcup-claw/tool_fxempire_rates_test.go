package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// classifyInstrument
// ---------------------------------------------------------------------------

func TestClassifyInstrument_Commodities(t *testing.T) {
	cases := []string{
		"brent-crude-oil", "gold", "silver", "natural-gas",
		"copper", "corn", "wheat", "platinum", "unknown-slug",
	}
	for _, slug := range cases {
		if got := classifyInstrument(slug); got != "commodities" {
			t.Errorf("classifyInstrument(%q) = %q, want %q", slug, got, "commodities")
		}
	}
}

func TestClassifyInstrument_Crypto(t *testing.T) {
	cases := []string{"bitcoin", "ethereum", "litecoin", "ripple", "dogecoin", "solana"}
	for _, slug := range cases {
		if got := classifyInstrument(slug); got != "crypto" {
			t.Errorf("classifyInstrument(%q) = %q, want %q", slug, got, "crypto")
		}
	}
}

func TestClassifyInstrument_Indices(t *testing.T) {
	cases := []string{"spx", "dji", "nasdaq", "ftse", "dax", "nikkei"}
	for _, slug := range cases {
		if got := classifyInstrument(slug); got != "indices" {
			t.Errorf("classifyInstrument(%q) = %q, want %q", slug, got, "indices")
		}
	}
}

func TestClassifyInstrument_Currencies(t *testing.T) {
	cases := []string{"eur-usd", "gbp-usd", "usd-jpy", "aud-usd", "nzd-usd"}
	for _, slug := range cases {
		if got := classifyInstrument(slug); got != "currencies" {
			t.Errorf("classifyInstrument(%q) = %q, want %q", slug, got, "currencies")
		}
	}
}

func TestClassifyInstrument_CaseInsensitive(t *testing.T) {
	if got := classifyInstrument("Bitcoin"); got != "crypto" {
		t.Errorf("classifyInstrument(%q) = %q, want %q", "Bitcoin", got, "crypto")
	}
	if got := classifyInstrument("EUR-USD"); got != "currencies" {
		t.Errorf("classifyInstrument(%q) = %q, want %q", "EUR-USD", got, "currencies")
	}
	if got := classifyInstrument("SPX"); got != "indices" {
		t.Errorf("classifyInstrument(%q) = %q, want %q", "SPX", got, "indices")
	}
}

func TestClassifyInstrument_Whitespace(t *testing.T) {
	if got := classifyInstrument("  bitcoin  "); got != "crypto" {
		t.Errorf("classifyInstrument with whitespace = %q, want %q", got, "crypto")
	}
}

// ---------------------------------------------------------------------------
// formatMarkdown
// ---------------------------------------------------------------------------

func TestFormatMarkdown_Empty(t *testing.T) {
	out := formatMarkdown(nil)
	if !strings.Contains(out, "No rates") {
		t.Errorf("empty formatMarkdown should contain 'No rates', got: %q", out)
	}
}

func TestFormatMarkdown_SingleCategory(t *testing.T) {
	rates := []RateEntry{
		{Slug: "brent-crude-oil", Name: "Brent Crude Oil", Category: "commodities", Price: 75.5, Change: -0.32, ChangePct: -0.42},
		{Slug: "gold", Name: "Gold", Category: "commodities", Price: 1985.3, Change: 12.1, ChangePct: 0.61},
	}
	out := formatMarkdown(rates)

	if !strings.Contains(out, "## Commodities") {
		t.Errorf("expected Commodities header, got:\n%s", out)
	}
	if !strings.Contains(out, "Brent Crude Oil") {
		t.Errorf("expected Brent Crude Oil row, got:\n%s", out)
	}
	if !strings.Contains(out, "75.50") {
		t.Errorf("expected formatted price 75.50, got:\n%s", out)
	}
	if !strings.Contains(out, "-0.42%") {
		t.Errorf("expected changePct -0.42%%, got:\n%s", out)
	}
}

func TestFormatMarkdown_MultiCategory(t *testing.T) {
	rates := []RateEntry{
		{Slug: "brent-crude-oil", Name: "Brent Crude Oil", Category: "commodities", Price: 75.5, Change: -0.32, ChangePct: -0.42},
		{Slug: "eur-usd", Name: "EUR/USD", Category: "currencies", Price: 1.085, Change: 0.002, ChangePct: 0.18},
		{Slug: "bitcoin", Name: "Bitcoin", Category: "crypto", Price: 42000.0, Change: -500, ChangePct: -1.18},
		{Slug: "spx", Name: "S&P 500", Category: "indices", Price: 4800.0, Change: 25.0, ChangePct: 0.52},
	}
	out := formatMarkdown(rates)

	for _, header := range []string{"## Commodities", "## Currencies", "## Crypto", "## Indices"} {
		if !strings.Contains(out, header) {
			t.Errorf("expected header %q in output:\n%s", header, out)
		}
	}
}

func TestFormatMarkdown_TableStructure(t *testing.T) {
	rates := []RateEntry{
		{Slug: "gold", Name: "Gold", Category: "commodities", Price: 1985.3, Change: 12.1, ChangePct: 0.61},
	}
	out := formatMarkdown(rates)

	// Must have the Markdown table header.
	if !strings.Contains(out, "| Instrument | Price | Change | Change % |") {
		t.Errorf("expected table header row, got:\n%s", out)
	}
	if !strings.Contains(out, "|---|---|---|---|") {
		t.Errorf("expected table separator row, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// JSON output structure (via json.Marshal on []RateEntry)
// ---------------------------------------------------------------------------

func TestRateEntry_JSONFields(t *testing.T) {
	entry := RateEntry{
		Slug:      "bitcoin",
		Name:      "Bitcoin",
		Category:  "crypto",
		Price:     42000.0,
		Change:    -500.0,
		ChangePct: -1.18,
	}

	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	for _, key := range []string{"slug", "name", "category", "price", "change", "changePct"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q to be present", key)
		}
	}
}
