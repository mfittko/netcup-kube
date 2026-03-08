package main

import (
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// scoreKeywords
// ---------------------------------------------------------------------------

func TestScoreKeywords_Empty(t *testing.T) {
	if got := scoreKeywords("", bullWords); got != 0 {
		t.Errorf("empty text: got %d, want 0", got)
	}
}

func TestScoreKeywords_BullWords(t *testing.T) {
	text := "Gold will rise and rally higher on strong support, bullish outlook."
	score := scoreKeywords(text, bullWords)
	if score == 0 {
		t.Error("expected non-zero bull score for bullish text")
	}
}

func TestScoreKeywords_BearWords(t *testing.T) {
	text := "Oil prices could fall and drop under pressure from the downside."
	score := scoreKeywords(text, bearWords)
	if score == 0 {
		t.Error("expected non-zero bear score for bearish text")
	}
}

func TestScoreKeywords_CountsOccurrences(t *testing.T) {
	// "rise" appears 3 times.
	text := "prices rise rise rise today"
	score := scoreKeywords(text, []string{"rise"})
	if score != 3 {
		t.Errorf("expected 3 occurrences, got %d", score)
	}
}

func TestScoreKeywords_CaseInsensitive(t *testing.T) {
	text := "RISE Rise rise"
	score := scoreKeywords(text, []string{"rise"})
	if score != 3 {
		t.Errorf("expected 3 case-insensitive matches, got %d", score)
	}
}

// ---------------------------------------------------------------------------
// outlookLabel
// ---------------------------------------------------------------------------

func TestOutlookLabel_BullishMomentumAligned(t *testing.T) {
	pct := 2.0
	got := outlookLabel(&pct, 5, 2)
	if got != "Bullish (momentum + narrative aligned)" {
		t.Errorf("unexpected outlook: %q", got)
	}
}

func TestOutlookLabel_BearishMomentumAligned(t *testing.T) {
	pct := -2.0
	got := outlookLabel(&pct, 1, 5)
	if got != "Bearish (momentum + narrative aligned)" {
		t.Errorf("unexpected outlook: %q", got)
	}
}

func TestOutlookLabel_BullishNarrativeLed(t *testing.T) {
	// bull-bear >= 3, but pct does not qualify for full bullish
	pct := 0.1
	got := outlookLabel(&pct, 5, 1)
	if got != "Bullish bias (narrative-led)" {
		t.Errorf("unexpected outlook: %q", got)
	}
}

func TestOutlookLabel_BearishNarrativeLed(t *testing.T) {
	pct := -0.1
	got := outlookLabel(&pct, 1, 5)
	if got != "Bearish bias (narrative-led)" {
		t.Errorf("unexpected outlook: %q", got)
	}
}

func TestOutlookLabel_MildBullish(t *testing.T) {
	pct := 0.6
	got := outlookLabel(&pct, 2, 2)
	if got != "Mild bullish bias (price-led)" {
		t.Errorf("unexpected outlook: %q", got)
	}
}

func TestOutlookLabel_MildBearish(t *testing.T) {
	pct := -0.6
	got := outlookLabel(&pct, 2, 2)
	if got != "Mild bearish bias (price-led)" {
		t.Errorf("unexpected outlook: %q", got)
	}
}

func TestOutlookLabel_Neutral(t *testing.T) {
	pct := 0.1
	got := outlookLabel(&pct, 2, 2)
	if got != "Neutral / mixed" {
		t.Errorf("unexpected outlook: %q", got)
	}
}

func TestOutlookLabel_NilPct(t *testing.T) {
	// With nil pct, falls through to narrative-led or neutral.
	got := outlookLabel(nil, 4, 0)
	if got != "Bullish bias (narrative-led)" {
		t.Errorf("unexpected outlook with nil pct: %q", got)
	}
}

// ---------------------------------------------------------------------------
// confidenceLevel
// ---------------------------------------------------------------------------

func TestConfidenceLevel_High(t *testing.T) {
	pct := 5.0
	got := confidenceLevel(3, &pct, 4, 1)
	if got != "High" {
		t.Errorf("expected High confidence, got %q", got)
	}
}

func TestConfidenceLevel_Low(t *testing.T) {
	got := confidenceLevel(0, nil, 0, 0)
	if got != "Low" {
		t.Errorf("expected Low confidence, got %q", got)
	}
}

func TestConfidenceLevel_Medium(t *testing.T) {
	pct := 1.0
	got := confidenceLevel(2, &pct, 2, 1)
	if got != "Medium" {
		t.Errorf("expected Medium confidence, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// normalizeTakeaway
// ---------------------------------------------------------------------------

func TestNormalizeTakeaway_Short(t *testing.T) {
	text := "Gold prices rise."
	got := normalizeTakeaway(text)
	if got != text {
		t.Errorf("short text should be unchanged: %q", got)
	}
}

func TestNormalizeTakeaway_Empty(t *testing.T) {
	if got := normalizeTakeaway(""); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestNormalizeTakeaway_Long(t *testing.T) {
	// Build a string longer than 700 chars.
	base := "Gold prices continue to rise due to safe haven demand and geopolitical tensions. "
	var long string
	for len(long) < 800 {
		long += base
	}
	got := normalizeTakeaway(long)
	if len(got) > 750 {
		t.Errorf("takeaway too long: %d chars", len(got))
	}
}

// ---------------------------------------------------------------------------
// buildCommodityAnalysis
// ---------------------------------------------------------------------------

func TestBuildCommodityAnalysis_NoData(t *testing.T) {
	a := buildCommodityAnalysis("gold", nil, nil)
	if a.Slug != "gold" {
		t.Errorf("Slug = %q, want gold", a.Slug)
	}
	if a.ArticleCount != 0 {
		t.Errorf("ArticleCount should be 0 with no articles")
	}
	if a.Outlook == "" {
		t.Error("Outlook should be non-empty")
	}
	if a.Confidence == "" {
		t.Error("Confidence should be non-empty")
	}
}

func TestBuildCommodityAnalysis_WithData(t *testing.T) {
	last := 2050.5
	pct := 1.5
	prices := []fxPrice{
		{Slug: "gold", Name: "Gold", Last: &last, Pct: &pct},
	}
	snippet := "Gold continues to rise strongly on bullish demand."
	articles := []Article{
		{ID: 1, Title: "Gold Rises", Commodity: "gold", Type: "news", Timestamp: 1000, TextSnippet: &snippet},
		{ID: 2, Title: "Gold Outlook", Commodity: "gold", Type: "forecasts", Timestamp: 900},
		{ID: 3, Title: "Silver Falls", Commodity: "silver", Type: "news", Timestamp: 800}, // different commodity
	}
	a := buildCommodityAnalysis("gold", prices, articles)

	if a.Name != "Gold" {
		t.Errorf("Name = %q, want Gold", a.Name)
	}
	if a.Last == nil || *a.Last != 2050.5 {
		t.Errorf("Last = %v, want 2050.5", a.Last)
	}
	if a.ArticleCount != 2 {
		t.Errorf("ArticleCount = %d, want 2 (only gold articles)", a.ArticleCount)
	}
	if a.BullScore == 0 {
		t.Error("expected non-zero bull score from bullish snippet")
	}
	if len(a.TopArticles) == 0 {
		t.Error("expected non-empty topArticles")
	}
}

// ---------------------------------------------------------------------------
// buildEnrichMarkdown
// ---------------------------------------------------------------------------

func TestBuildEnrichMarkdown_ContainsExpectedSections(t *testing.T) {
	last := 2050.5
	pct := 1.2
	hours := 24.0
	cutoff := "2024-01-14T10:00:00Z"
	payload := enrichPayload{
		Meta: enrichMeta{
			Now:    "2024-01-15T10:00:00.000Z",
			Cutoff: &cutoff,
			Hours:  &hours,
			TZ:     "UTC",
			Locale: "en",
		},
		Prices:   []fxPrice{{Slug: "gold", Name: "Gold", Last: &last, Pct: &pct}},
		Articles: nil,
	}
	analyses := []commodityAnalysis{
		{
			Slug:        "gold",
			Name:        "Gold",
			Last:        &last,
			Pct:         &pct,
			Outlook:     "Bullish (momentum + narrative aligned)",
			Confidence:  "Medium",
			TopArticles: nil,
		},
	}
	out := buildEnrichMarkdown(payload, analyses)

	if !strings.Contains(out, "# Commodity Market Analysis") {
		t.Errorf("missing heading: %q", out[:min(len(out), 200)])
	}
	if !strings.Contains(out, "Market Snapshot") {
		t.Errorf("missing market snapshot section")
	}
	if !strings.Contains(out, "Gold") {
		t.Errorf("missing Gold commodity")
	}
	if !strings.Contains(out, "Bullish") {
		t.Errorf("missing outlook in output")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestBuildAnalysisOrder_FocusIncluded(t *testing.T) {
	order := buildAnalysisOrder([]string{"gold", "silver", "brent-crude-oil"}, "silver")
	want := []string{"silver", "gold", "brent-crude-oil"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("unexpected order: got %v want %v", order, want)
	}
}

func TestBuildAnalysisOrder_FocusNotIncluded(t *testing.T) {
	order := buildAnalysisOrder([]string{"gold", "silver"}, "brent-crude-oil")
	want := []string{"gold", "silver"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("unexpected order: got %v want %v", order, want)
	}
}
