package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Flags
// ---------------------------------------------------------------------------

var (
	feLocale       string
	feTZ           string
	feHours        float64
	feCommodities  []string
	feFocus        string
	feMaxItems     int
	fePageSize     int
	feMaxPages     int
	feTags         []string
	feJSON         bool
	feOutputFile   string
	feFullText     bool
	feMaxTextChars int
)

// ---------------------------------------------------------------------------
// Analysis types
// ---------------------------------------------------------------------------

// enrichMeta is the metadata block for the enrich payload.
type enrichMeta struct {
	Now    string  `json:"now"`
	Cutoff *string `json:"cutoff"`
	Hours  *float64 `json:"hours"`
	TZ     string  `json:"tz"`
	Locale string  `json:"locale"`
}

// commodityAnalysis holds the per-commodity analysis output.
type commodityAnalysis struct {
	Slug         string          `json:"slug"`
	Name         string          `json:"name"`
	Last         *float64        `json:"last"`
	Change       *float64        `json:"change"`
	Pct          *float64        `json:"pct"`
	LastUpdate   *string         `json:"lastUpdate"`
	ArticleCount int             `json:"articleCount"`
	BullScore    int             `json:"bullScore"`
	BearScore    int             `json:"bearScore"`
	GeoScore     int             `json:"geoScore"`
	Outlook      string          `json:"outlook"`
	Confidence   string          `json:"confidence"`
	TopArticles  []topArticleRef `json:"topArticles"`
}

// topArticleRef is a lightweight article reference for the analysis output.
type topArticleRef struct {
	ID         int     `json:"id"`
	Title      string  `json:"title"`
	Slug       *string `json:"slug"`
	Type       *string `json:"type"`
	ISO        string  `json:"iso"`
	Author     *string `json:"author"`
	ArticleURL *string `json:"articleUrl"`
	FullURL    *string `json:"fullUrl"`
	Takeaway   string  `json:"takeaway"`
}

// enrichPayload is the JSON output envelope for fxempire-enrich.
type enrichPayload struct {
	Meta           enrichMeta          `json:"meta"`
	RatesURL       string              `json:"ratesUrl"`
	Prices         []fxPrice           `json:"prices"`
	PricesError    *string             `json:"pricesError"`
	Articles       []Article           `json:"articles"`
	Analysis       []commodityAnalysis `json:"analysis"`
	ReportMarkdown string              `json:"reportMarkdown"`
	ReportFile     *string             `json:"reportFile,omitempty"`
}

// ---------------------------------------------------------------------------
// Keyword scoring helpers
// ---------------------------------------------------------------------------

// scoreKeywords counts occurrences of keyword substrings in text.
// Mirrors scoreKeywords() in fxempire_enrich.mjs.
func scoreKeywords(text string, words []string) int {
	t := strings.ToLower(text)
	score := 0
	for _, word := range words {
		score += strings.Count(t, word)
	}
	return score
}

var (
	bullWords = []string{"rise", "rally", "gain", "upside", "support", "higher", "boost", "bull"}
	bearWords = []string{"fall", "drop", "dive", "fade", "downside", "pressure", "lower", "selloff", "bear"}
	geoWords  = []string{"iran", "hormuz", "middle east", "strike", "military", "tension", "war"}
)

// outlookLabel returns a human-readable outlook label from price and narrative signals.
// Mirrors outlookLabel() in fxempire_enrich.mjs.
func outlookLabel(pct *float64, bull, bear int) string {
	switch {
	case pct != nil && *pct >= 1 && bull >= bear:
		return "Bullish (momentum + narrative aligned)"
	case pct != nil && *pct <= -1 && bear >= bull:
		return "Bearish (momentum + narrative aligned)"
	case bull-bear >= 3:
		return "Bullish bias (narrative-led)"
	case bear-bull >= 3:
		return "Bearish bias (narrative-led)"
	case pct != nil && *pct > 0.4:
		return "Mild bullish bias (price-led)"
	case pct != nil && *pct < -0.4:
		return "Mild bearish bias (price-led)"
	default:
		return "Neutral / mixed"
	}
}

// confidenceLevel computes a confidence label from article count and signal strength.
// Mirrors confidenceLevel() in fxempire_enrich.mjs.
func confidenceLevel(articleCount int, pct *float64, bull, bear int) string {
	score := 1
	if articleCount < 3 {
		score += articleCount
	} else {
		score += 3
	}
	pctAbs := 0.0
	if pct != nil {
		pctAbs = *pct
		if pctAbs < 0 {
			pctAbs = -pctAbs
		}
	}
	if pctAbs < 3 {
		score += int(pctAbs)
	} else {
		score += 3
	}
	diff := bull - bear
	if diff < 0 {
		diff = -diff
	}
	if diff < 3 {
		score += diff
	} else {
		score += 3
	}
	switch {
	case score >= 7:
		return "High"
	case score >= 4:
		return "Medium"
	default:
		return "Low"
	}
}

// minSentenceBreakIndex is the minimum character position in normalizeTakeaway
// before which sentence boundaries are not considered for truncation.
const minSentenceBreakIndex = 280

// minTakeawayTruncationPoint is the minimum character position accepted as a
// truncation point when no sentence boundary or space boundary is found.
const minTakeawayTruncationPoint = 200

// normalizeTakeaway trims and sentence-breaks a text snippet to ≤700 chars.
// Mirrors normalizeTakeaway() in fxempire_enrich.mjs.
func normalizeTakeaway(text string) string {
	s := strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if s == "" {
		return ""
	}
	const maxChars = 700
	if len(s) <= maxChars {
		return s
	}
	window := s[:maxChars]
	// Find last sentence boundary ≥minSentenceBreakIndex chars in.
	lastBoundary := -1
	for i, r := range window {
		if i < minSentenceBreakIndex {
			continue
		}
		if r == '.' || r == '!' || r == '?' {
			lastBoundary = i
		}
	}
	if lastBoundary > 0 {
		return window[:lastBoundary+1] + "…"
	}
	lastSpace := strings.LastIndex(window, " ")
	cut := lastSpace
	if cut < minTakeawayTruncationPoint {
		cut = maxChars
	}
	return window[:cut] + "…"
}

// articleText returns the best available text for an article (textFull > textSnippet > description > excerpt).
func articleText(a Article) string {
	if a.TextFull != nil {
		return *a.TextFull
	}
	if a.TextSnippet != nil {
		return *a.TextSnippet
	}
	if a.Description != nil {
		return *a.Description
	}
	if a.Excerpt != nil {
		return *a.Excerpt
	}
	return ""
}

// ---------------------------------------------------------------------------
// Analysis builder
// ---------------------------------------------------------------------------

// buildCommodityAnalysis computes the commodity analysis for a single slug.
// Mirrors buildCommodityAnalysis() in fxempire_enrich.mjs.
func buildCommodityAnalysis(slug string, prices []fxPrice, articles []Article) commodityAnalysis {
	// Find price data for this slug.
	var price fxPrice
	for _, p := range prices {
		if p.Slug == slug {
			price = p
			break
		}
	}

	// Find articles for this slug, sorted descending by timestamp.
	var items []Article
	for _, a := range articles {
		if a.Commodity == slug {
			items = append(items, a)
		}
	}
	sortArticlesByTimestamp(items)

	// Build combined body text from top 3 articles.
	var bodyParts []string
	for i := 0; i < len(items) && i < 3; i++ {
		bodyParts = append(bodyParts, articleText(items[i]))
	}
	body := strings.Join(bodyParts, " ")

	bull := scoreKeywords(body, bullWords)
	bear := scoreKeywords(body, bearWords)
	geo := scoreKeywords(body, geoWords)

	// Build top article references.
	topN := 3
	if len(items) < topN {
		topN = len(items)
	}
	topArticles := make([]topArticleRef, topN)
	for i := 0; i < topN; i++ {
		a := items[i]
		ref := topArticleRef{
			ID:         a.ID,
			Title:      a.Title,
			ISO:        a.ISO,
			Author:     a.Author,
			ArticleURL: a.ArticleURL,
			FullURL:    a.FullURL,
			Takeaway:   normalizeTakeaway(func() string {
				if a.TextSnippet != nil {
					return *a.TextSnippet
				}
				if a.Description != nil {
					return *a.Description
				}
				if a.Excerpt != nil {
					return *a.Excerpt
				}
				return ""
			}()),
		}
		if a.Slug != "" {
			s := a.Slug
			ref.Slug = &s
		}
		if a.Type != "" {
			t := a.Type
			ref.Type = &t
		}
		topArticles[i] = ref
	}

	name := price.Name
	if name == "" {
		name = slug
	}

	return commodityAnalysis{
		Slug:         slug,
		Name:         name,
		Last:         price.Last,
		Change:       price.Change,
		Pct:          price.Pct,
		LastUpdate:   price.LastUpdate,
		ArticleCount: len(items),
		BullScore:    bull,
		BearScore:    bear,
		GeoScore:     geo,
		Outlook:      outlookLabel(price.Pct, bull, bear),
		Confidence:   confidenceLevel(len(items), price.Pct, bull, bear),
		TopArticles:  topArticles,
	}
}

// ---------------------------------------------------------------------------
// Markdown report builder
// ---------------------------------------------------------------------------

// buildEnrichMarkdown builds the detailed analysis Markdown report.
// Mirrors buildDetailedMarkdown() in fxempire_enrich.mjs.
func buildEnrichMarkdown(payload enrichPayload, analyses []commodityAnalysis) string {
	lines := []string{"# Commodity Market Analysis (FXEmpire)", ""}
	lines = append(lines,
		fmt.Sprintf("- Generated: %s", payload.Meta.Now),
		fmt.Sprintf("- Window: last %.0fh (%s)", func() float64 {
			if payload.Meta.Hours != nil {
				return *payload.Meta.Hours
			}
			return 24
		}(), payload.Meta.TZ),
		fmt.Sprintf("- Locale: %s", payload.Meta.Locale),
		"",
	)

	lines = append(lines, "## Market Snapshot", "")
	lines = append(lines, "| Commodity | Last | Change | % | Outlook | Confidence |")
	lines = append(lines, "|---|---:|---:|---:|---|---|")
	for _, a := range analyses {
		lines = append(lines, fmt.Sprintf("| %s | %s | %s | %s | %s | %s |",
			mdEnrichEscape(a.Name),
			fmtOrNA(a.Last),
			fmtOrNA(a.Change),
			pctOrNA(a.Pct),
			mdEnrichEscape(a.Outlook),
			a.Confidence,
		))
	}

	for _, a := range analyses {
		lines = append(lines, "",
			fmt.Sprintf("## %s (%s)", mdEnrichEscape(a.Name), a.Slug),
			"",
			fmt.Sprintf("- Price action: %s (%s, %s)", fmtOrNA(a.Last), fmtOrNA(a.Change), pctOrNA(a.Pct)),
			fmt.Sprintf("- Narrative signals: bull=%d, bear=%d, geopolitics=%d", a.BullScore, a.BearScore, a.GeoScore),
			fmt.Sprintf("- Outlook: **%s**", mdEnrichEscape(a.Outlook)),
			fmt.Sprintf("- Confidence: **%s**", a.Confidence),
		)

		if len(a.TopArticles) > 0 {
			lines = append(lines, "", "### Supporting Articles")
			for _, item := range a.TopArticles {
				when := strings.ReplaceAll(item.ISO, "T", " ")
				when = strings.ReplaceAll(when, "+00:00", "Z")

				var linkLabel string
				if item.FullURL != nil {
					title := strings.ReplaceAll(strings.ReplaceAll(item.Title, "[", "\\["), "]", "\\]")
					u := strings.ReplaceAll(strings.ReplaceAll(*item.FullURL, "(", "%28"), ")", "%29")
					linkLabel = fmt.Sprintf("[%s](%s)", title, u)
				} else {
					linkLabel = mdEnrichEscape(item.Title)
				}

				var meta string
				if when != "" {
					meta = " (" + when
					if item.Author != nil {
						meta += ", " + mdEnrichEscape(*item.Author)
					}
					meta += ")"
				} else if item.Author != nil {
					meta = " (" + mdEnrichEscape(*item.Author) + ")"
				}

				var suffix string
				if item.FullURL == nil {
					suffix = " — link unavailable"
				}

				lines = append(lines, fmt.Sprintf("- %s%s%s", linkLabel, meta, suffix))
				if item.Takeaway != "" {
					lines = append(lines, fmt.Sprintf("  - %s", mdEnrichEscape(item.Takeaway)))
				}
			}
		}
	}

	return strings.Join(lines, "\n") + "\n"
}

func mdEnrichEscape(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

func fmtOrNA(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return fmt.Sprintf("%g", *v)
}

func pctOrNA(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.2f%%", *v)
}

func buildAnalysisOrder(commodities []string, focus string) []string {
	seen := map[string]bool{}
	order := make([]string, 0, len(commodities))
	if focus != "" {
		for _, slug := range commodities {
			if slug == focus {
				order = append(order, focus)
				seen[focus] = true
				break
			}
		}
	}
	for _, slug := range commodities {
		if !seen[slug] {
			order = append(order, slug)
			seen[slug] = true
		}
	}
	return order
}

// ---------------------------------------------------------------------------
// Cobra command
// ---------------------------------------------------------------------------

var fxempireEnrichCmd = &cobra.Command{
	Use:   "fxempire-enrich",
	Short: "Fetch and enrich FXEmpire market data with article analysis",
	Long: `Fetch FXEmpire market rates and news/forecast articles, combine them, and
produce a per-commodity analysis with outlook, confidence score, and
top supporting articles.

The command fetches:
  - Rates from the FXEmpire rates API (same as fxempire-rates)
  - Articles from the FXEmpire hub API (same as fxempire-articles)

It then computes bull/bear/geo keyword scores from article body text and
produces an outlook and confidence label per commodity.

Examples:
  netcup-claw tool fxempire-enrich --json
  netcup-claw tool fxempire-enrich --commodities brent-crude-oil,gold --json
  netcup-claw tool fxempire-enrich --focus gold --hours 48 --json
  netcup-claw tool fxempire-enrich --full-text --max-text-chars 6000 --json`,
	RunE: runFXEmpireEnrich,
}

func runFXEmpireEnrich(cmd *cobra.Command, _ []string) error {
	commodities := feCommodities
	if len(commodities) == 0 {
		commodities = []string{"brent-crude-oil", "natural-gas", "gold", "silver"}
	}

	var hoursOverride *float64
	if cmd.Flags().Changed("hours") && feHours > 0 {
		h := feHours
		hoursOverride = &h
	}

	// Fetch rates and articles sequentially (can be parallelized in the future if needed).
	ratesPayload := computeFXEmpireRates(feLocale, commodities)
	articlesPayload := fetchArticlesPayload(feLocale, feTZ, hoursOverride, commodities, feTags,
		feMaxItems, fePageSize, feMaxPages, feFullText, feMaxTextChars)

	// Build the combined payload meta.
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	metaNow := articlesPayload.Meta.Now
	if metaNow == "" {
		metaNow = ratesPayload.Meta.Now
	}
	if metaNow == "" {
		metaNow = now
	}

	var cutoffPtr *string
	if articlesPayload.Meta.Cutoff != "" {
		s := articlesPayload.Meta.Cutoff
		cutoffPtr = &s
	}

	var hoursPtr *float64
	h := articlesPayload.Meta.Hours
	if h > 0 {
		hoursPtr = &h
	}

	// Determine analysis order: focus slug first (if selected), then remaining commodities.
	order := buildAnalysisOrder(commodities, feFocus)

	// Build per-commodity analysis.
	analyses := make([]commodityAnalysis, 0, len(order))
	for _, slug := range order {
		analyses = append(analyses, buildCommodityAnalysis(slug, ratesPayload.Prices, articlesPayload.Articles))
	}

	payload := enrichPayload{
		Meta: enrichMeta{
			Now:    metaNow,
			Cutoff: cutoffPtr,
			Hours:  hoursPtr,
			TZ:     feTZ,
			Locale: feLocale,
		},
		RatesURL:    ratesPayload.RatesURL,
		Prices:      ratesPayload.Prices,
		PricesError: ratesPayload.PricesError,
		Articles:    articlesPayload.Articles,
		Analysis:    analyses,
	}

	reportMD := buildEnrichMarkdown(payload, analyses)
	payload.ReportMarkdown = reportMD

	// Optionally write report to file.
	if feOutputFile != "" {
		if err := os.WriteFile(feOutputFile, []byte(reportMD), 0o644); err != nil {
			return fmt.Errorf("writing output file %s: %w", feOutputFile, err)
		}
		s := feOutputFile
		payload.ReportFile = &s
	}

	if feJSON {
		b, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding JSON output: %w", err)
		}
		_, err = fmt.Fprintln(os.Stdout, string(b))
		return err
	}

	fmt.Print(reportMD)
	return nil
}

func init() {
	fxempireEnrichCmd.Flags().StringVar(&feLocale, "locale", "en", "API locale (e.g. en, de)")
	fxempireEnrichCmd.Flags().StringVar(&feTZ, "tz", "Europe/Berlin", "Timezone for time-window calculation (IANA, e.g. Europe/Berlin)")
	fxempireEnrichCmd.Flags().Float64Var(&feHours, "hours", 0, "Override look-back window in hours (auto-detects from weekday if omitted)")
	fxempireEnrichCmd.Flags().StringSliceVar(&feCommodities, "commodities", nil,
		"Comma-separated commodity slugs (default: brent-crude-oil,natural-gas,gold,silver)")
	fxempireEnrichCmd.Flags().StringVar(&feFocus, "focus", "brent-crude-oil", "Slug to appear first in the analysis output")
	fxempireEnrichCmd.Flags().IntVar(&feMaxItems, "max-items", 6, "Max articles per commodity per type")
	fxempireEnrichCmd.Flags().IntVar(&fePageSize, "page-size", 50, "Articles per page when fetching")
	fxempireEnrichCmd.Flags().IntVar(&feMaxPages, "max-pages", 10, "Max pages to fetch per tag/type")
	fxempireEnrichCmd.Flags().StringSliceVar(&feTags, "tags", nil,
		"Override slug→tag mapping as slug=tag pairs (e.g. gold=co-gold)")
	fxempireEnrichCmd.Flags().BoolVar(&feJSON, "json", false, "Output as JSON instead of Markdown")
	fxempireEnrichCmd.Flags().StringVar(&feOutputFile, "output-file", "", "Write Markdown report to this file path")
	fxempireEnrichCmd.Flags().BoolVar(&feFullText, "full-text", true, "Fetch full article text; use --full-text=false to disable")
	fxempireEnrichCmd.Flags().IntVar(&feMaxTextChars, "max-text-chars", 12000, "Max characters of full text to include")
}
