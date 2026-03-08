package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// flexibleDate – regression for numeric date form from forecasts endpoint
// ---------------------------------------------------------------------------

func TestFlexibleDate_NumericMilliseconds(t *testing.T) {
	// The live FXEmpire forecasts endpoint returns date as a numeric millisecond
	// timestamp (e.g. 1705276800000 = 2024-01-15T00:00:00.000Z).
	// Reproduces the bug reported in issue #65 comment 2900883790.
	type wrapper struct {
		Date flexibleDate `json:"date"`
	}
	var got wrapper
	if err := json.Unmarshal([]byte(`{"date":1705276800000}`), &got); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	wantMs := int64(1705276800000)
	if got.Date.ms != wantMs {
		t.Errorf("flexibleDate(numeric) = %d, want %d", got.Date.ms, wantMs)
	}
}

func TestFlexibleDate_RFC3339String(t *testing.T) {
	type wrapper struct {
		Date flexibleDate `json:"date"`
	}
	var got wrapper
	if err := json.Unmarshal([]byte(`{"date":"2024-01-15T00:00:00Z"}`), &got); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	want := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	if got.Date.ms != want {
		t.Errorf("flexibleDate(string) = %d, want %d", got.Date.ms, want)
	}
}

func TestFlexibleDate_AbsentField(t *testing.T) {
	type wrapper struct {
		Date flexibleDate `json:"date"`
	}
	var got wrapper
	if err := json.Unmarshal([]byte(`{}`), &got); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if got.Date.ms != 0 {
		t.Errorf("flexibleDate(absent) = %d, want 0", got.Date.ms)
	}
}

func TestFlexibleDate_NullField(t *testing.T) {
	type wrapper struct {
		Date flexibleDate `json:"date"`
	}
	var got wrapper
	if err := json.Unmarshal([]byte(`{"date":null}`), &got); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if got.Date.ms != 0 {
		t.Errorf("flexibleDate(null) = %d, want 0", got.Date.ms)
	}
}

func TestRawArticleTimestamp_NumericDate(t *testing.T) {
	// Regression: forecasts endpoint returns numeric ms in "date" field.
	// rawArticleTimestamp must yield the correct ms value.
	raw := rawHubArticle{
		ID:   1,
		Date: flexibleDate{ms: 1705276800000},
	}
	if got := rawArticleTimestamp(raw); got != 1705276800000 {
		t.Errorf("rawArticleTimestamp(numeric date) = %d, want 1705276800000", got)
	}
}

func TestRawArticleTimestamp_TimestampFieldTakesPriority(t *testing.T) {
	// When both `timestamp` and `date` are present, `timestamp` wins.
	raw := rawHubArticle{
		ID:        1,
		Timestamp: "9999999999999",
		Date:      flexibleDate{ms: 1705276800000},
	}
	got := rawArticleTimestamp(raw)
	if got != 9999999999999 {
		t.Errorf("rawArticleTimestamp should prefer timestamp field: got %d", got)
	}
}

// ---------------------------------------------------------------------------
// windowHoursForDate
// ---------------------------------------------------------------------------

func TestWindowHoursForDate_Weekday(t *testing.T) {
	// Monday 2024-01-15 at noon UTC
	monday := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	got := windowHoursForDate(monday, "UTC")
	if got != 24 {
		t.Errorf("windowHoursForDate(Monday) = %v, want 24", got)
	}
}

func TestWindowHoursForDate_Saturday(t *testing.T) {
	// Saturday 2024-01-13
	saturday := time.Date(2024, 1, 13, 12, 0, 0, 0, time.UTC)
	got := windowHoursForDate(saturday, "UTC")
	if got != 48 {
		t.Errorf("windowHoursForDate(Saturday) = %v, want 48", got)
	}
}

func TestWindowHoursForDate_Sunday(t *testing.T) {
	// Sunday 2024-01-14
	sunday := time.Date(2024, 1, 14, 12, 0, 0, 0, time.UTC)
	got := windowHoursForDate(sunday, "UTC")
	if got != 72 {
		t.Errorf("windowHoursForDate(Sunday) = %v, want 72", got)
	}
}

func TestWindowHoursForDate_InvalidTZ(t *testing.T) {
	// Invalid timezone should fall back to UTC.
	monday := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	got := windowHoursForDate(monday, "Not/AValid/Zone")
	if got != 24 {
		t.Errorf("invalid TZ fallback: got %v, want 24", got)
	}
}

// ---------------------------------------------------------------------------
// resolveArticleURL
// ---------------------------------------------------------------------------

func TestResolveArticleURL_FullHTTPS(t *testing.T) {
	u := resolveArticleURL("https://www.fxempire.com/news/article/foo-123", "", "", 0, "news")
	if u == nil || *u != "https://www.fxempire.com/news/article/foo-123" {
		t.Errorf("unexpected URL: %v", u)
	}
}

func TestResolveArticleURL_RelativePath(t *testing.T) {
	u := resolveArticleURL("/news/article/foo-123", "", "", 0, "news")
	if u == nil || *u != "https://www.fxempire.com/news/article/foo-123" {
		t.Errorf("unexpected URL: %v", u)
	}
}

func TestResolveArticleURL_FullURLFallback(t *testing.T) {
	u := resolveArticleURL("", "https://example.com/article", "", 0, "news")
	if u == nil || *u != "https://example.com/article" {
		t.Errorf("unexpected URL: %v", u)
	}
}

func TestResolveArticleURL_SlugAndID(t *testing.T) {
	u := resolveArticleURL("", "", "gold-price-rises", 42, "news")
	if u == nil || *u != "https://www.fxempire.com/news/article/gold-price-rises-42" {
		t.Errorf("unexpected URL: %v", u)
	}
}

func TestResolveArticleURL_Forecasts(t *testing.T) {
	u := resolveArticleURL("", "", "gold-forecast", 99, "forecasts")
	if u == nil || *u != "https://www.fxempire.com/forecasts/article/gold-forecast-99" {
		t.Errorf("unexpected URL: %v", u)
	}
}

func TestResolveArticleURL_NilWhenNoInfo(t *testing.T) {
	u := resolveArticleURL("", "", "", 0, "news")
	if u != nil {
		t.Errorf("expected nil URL when no info, got: %v", *u)
	}
}

func TestResolveArticleURL_UnknownTypeFallsThrough(t *testing.T) {
	// Unknown article type with no articleUrl or fullUrl → nil.
	u := resolveArticleURL("", "", "some-slug", 10, "unknown")
	if u != nil {
		t.Errorf("expected nil for unknown type, got: %v", *u)
	}
}

// ---------------------------------------------------------------------------
// normalizeHubArticle
// ---------------------------------------------------------------------------

func TestNormalizeHubArticle_Basic(t *testing.T) {
	raw := rawHubArticle{
		ID:          42,
		Title:       "Gold Rises on Safe Haven Demand",
		Slug:        "gold-rises-123",
		Description: "Gold prices moved higher.",
		Type:        "news",
		Timestamp:   "1705276800000", // 2024-01-15T00:00:00.000Z
		Author:      rawArticleAuth{Name: "John Doe"},
		ArticleURL:  "/news/article/gold-rises-123-42",
	}

	a := normalizeHubArticle(raw, "news", "co-gold", "gold")

	if a.ID != 42 {
		t.Errorf("ID = %d, want 42", a.ID)
	}
	if a.Title != "Gold Rises on Safe Haven Demand" {
		t.Errorf("Title = %q", a.Title)
	}
	if a.Commodity != "gold" {
		t.Errorf("Commodity = %q, want gold", a.Commodity)
	}
	if a.Type != "news" {
		t.Errorf("Type = %q, want news", a.Type)
	}
	if a.Timestamp == 0 {
		t.Error("Timestamp should be non-zero")
	}
	if a.ISO == "" {
		t.Error("ISO should be non-empty")
	}
	if a.Author == nil || *a.Author != "John Doe" {
		t.Errorf("Author = %v", a.Author)
	}
	if a.FullURL == nil {
		t.Error("FullURL should be resolved")
	}
}

func TestNormalizeHubArticle_DateFallback(t *testing.T) {
	// Timestamp field is missing; fall back to Date (RFC3339 string form).
	raw := rawHubArticle{
		ID:    1,
		Title: "Test",
		Date:  flexibleDate{ms: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC).UnixMilli()},
	}
	a := normalizeHubArticle(raw, "news", "co-gold", "gold")
	if a.Timestamp == 0 {
		t.Error("expected timestamp from date fallback")
	}
}

// ---------------------------------------------------------------------------
// deduplicateArticles
// ---------------------------------------------------------------------------

func TestDeduplicateArticles(t *testing.T) {
	articles := []Article{
		{ID: 1, Type: "news"},
		{ID: 1, Type: "news"},   // duplicate
		{ID: 1, Type: "forecasts"}, // different type — not a duplicate
		{ID: 2, Type: "news"},
	}
	got := deduplicateArticles(articles)
	if len(got) != 3 {
		t.Errorf("deduplicateArticles: got %d, want 3", len(got))
	}
}

func TestDeduplicateArticles_Empty(t *testing.T) {
	got := deduplicateArticles(nil)
	if len(got) != 0 {
		t.Errorf("expected empty result for nil input")
	}
}

// ---------------------------------------------------------------------------
// capArticlesByTypeAndCommodity
// ---------------------------------------------------------------------------

func TestCapArticlesByTypeAndCommodity(t *testing.T) {
	articles := []Article{
		{ID: 1, Commodity: "gold", Type: "news"},
		{ID: 2, Commodity: "gold", Type: "news"},
		{ID: 3, Commodity: "gold", Type: "news"},   // 3rd → over maxItems=2
		{ID: 4, Commodity: "gold", Type: "forecasts"}, // different type → allowed
		{ID: 5, Commodity: "silver", Type: "news"},  // different commodity → allowed
	}
	got := capArticlesByTypeAndCommodity(articles, 2)
	if len(got) != 4 {
		t.Errorf("cap: got %d items, want 4", len(got))
	}
	// Third gold/news article should be dropped.
	for _, a := range got {
		if a.ID == 3 {
			t.Error("article ID=3 should have been capped out")
		}
	}
}

// ---------------------------------------------------------------------------
// sortArticlesByTimestamp
// ---------------------------------------------------------------------------

func TestSortArticlesByTimestamp(t *testing.T) {
	articles := []Article{
		{ID: 1, Timestamp: 100},
		{ID: 2, Timestamp: 300},
		{ID: 3, Timestamp: 200},
	}
	sortArticlesByTimestamp(articles)
	if articles[0].ID != 2 || articles[1].ID != 3 || articles[2].ID != 1 {
		t.Errorf("sort order wrong: got IDs %d,%d,%d", articles[0].ID, articles[1].ID, articles[2].ID)
	}
}

// ---------------------------------------------------------------------------
// buildTagMap
// ---------------------------------------------------------------------------

func TestBuildTagMap_Defaults(t *testing.T) {
	m := buildTagMap(nil)
	if m["gold"] != "co-gold" {
		t.Errorf("expected co-gold for gold, got %q", m["gold"])
	}
	if m["bitcoin"] != "cc-bitcoin" {
		t.Errorf("expected cc-bitcoin for bitcoin, got %q", m["bitcoin"])
	}
}

func TestBuildTagMap_Overrides(t *testing.T) {
	m := buildTagMap([]string{"gold=custom-gold", "silver=custom-silver"})
	if m["gold"] != "custom-gold" {
		t.Errorf("expected custom-gold override, got %q", m["gold"])
	}
	if m["silver"] != "custom-silver" {
		t.Errorf("expected custom-silver override, got %q", m["silver"])
	}
	// Unoverridden default should still be present.
	if m["bitcoin"] != "cc-bitcoin" {
		t.Errorf("default bitcoin tag should remain")
	}
}

// ---------------------------------------------------------------------------
// Hub URL builders
// ---------------------------------------------------------------------------

func TestHubNewsURL(t *testing.T) {
	u := hubNewsURL("https://www.fxempire.com/api/v1/en", "co-gold", 50, 1)
	if !strings.Contains(u, "hub/news") {
		t.Errorf("expected hub/news in URL: %s", u)
	}
	if !strings.Contains(u, "tag=co-gold") {
		t.Errorf("expected tag param: %s", u)
	}
	if !strings.Contains(u, "size=50") {
		t.Errorf("expected size param: %s", u)
	}
}

func TestHubForecastsURL(t *testing.T) {
	u := hubForecastsURL("https://www.fxempire.com/api/v1/en", "co-gold", 50, 2)
	if !strings.Contains(u, "hub/forecasts") {
		t.Errorf("expected hub/forecasts in URL: %s", u)
	}
	if !strings.Contains(u, "page=2") {
		t.Errorf("expected page=2 param: %s", u)
	}
}

func TestArticleDetailsURL(t *testing.T) {
	u := articleDetailsURL("https://www.fxempire.com/api/v1/en", []int{1, 2, 3})
	if !strings.Contains(u, "articles?ids=1,2,3") {
		t.Errorf("unexpected URL: %s", u)
	}
}

// ---------------------------------------------------------------------------
// HTML stripping helpers
// ---------------------------------------------------------------------------

func TestStripHTMLText_RemovesTags(t *testing.T) {
	html := `<p>Hello <b>World</b></p><script>var x=1;</script>`
	got := stripHTMLText(html)
	if strings.Contains(got, "<") || strings.Contains(got, ">") {
		t.Errorf("HTML tags should be stripped: %q", got)
	}
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") {
		t.Errorf("expected text content preserved: %q", got)
	}
}

func TestTrimBoilerplate(t *testing.T) {
	// "Important Disclaimers" must appear at index > 200 to be trimmed.
	prefix := strings.Repeat("x", 210)
	text := prefix + " Important Disclaimers and more boilerplate text."
	got := trimBoilerplate(text)
	if strings.Contains(got, "Important Disclaimers") {
		t.Errorf("boilerplate should be trimmed: %q", got)
	}
}

func TestFirstNonNil(t *testing.T) {
	a := "hello"
	b := "world"
	if got := firstNonNil(&a, &b); *got != "hello" {
		t.Errorf("firstNonNil should return first: %v", *got)
	}
	if got := firstNonNil(nil, &b); *got != "world" {
		t.Errorf("firstNonNil should return second when first nil: %v", *got)
	}
	if got := firstNonNil(nil, nil); got != nil {
		t.Errorf("firstNonNil(nil, nil) should be nil")
	}
}

// ---------------------------------------------------------------------------
// Markdown output
// ---------------------------------------------------------------------------

func TestArticlesMarkdown_Empty(t *testing.T) {
	payload := articlesPayload{
		Meta:     ArticlesMeta{Hours: 24, TZ: "UTC", Commodities: []string{"gold"}},
		Articles: nil,
	}
	out := articlesMarkdown(payload)
	if !strings.Contains(out, "FXEmpire articles") {
		t.Errorf("expected heading: %q", out)
	}
}

func TestArticlesMarkdown_WithArticles(t *testing.T) {
	fullURL := "https://www.fxempire.com/news/article/gold-rises-42"
	payload := articlesPayload{
		Meta: ArticlesMeta{Hours: 24, TZ: "UTC", Commodities: []string{"gold"}},
		Articles: []Article{
			{
				ID:        42,
				Title:     "Gold Rises",
				Type:      "news",
				Commodity: "gold",
				ISO:       "2024-01-15T10:00:00Z",
				FullURL:   &fullURL,
			},
		},
	}
	out := articlesMarkdown(payload)
	if !strings.Contains(out, "gold") {
		t.Errorf("expected commodity section: %q", out)
	}
	if !strings.Contains(out, "Gold Rises") {
		t.Errorf("expected article title: %q", out)
	}
}
