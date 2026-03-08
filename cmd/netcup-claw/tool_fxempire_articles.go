package main

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/mfittko/netcup-kube/internal/toolutil"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Flags
// ---------------------------------------------------------------------------

var (
	faLocale       string
	faTZ           string
	faHours        float64
	faCommodities  []string
	faMaxItems     int
	faPageSize     int
	faMaxPages     int
	faJSON         bool
	faFullText     bool
	faMaxTextChars int
	faTags         []string
)

// ---------------------------------------------------------------------------
// Default values
// ---------------------------------------------------------------------------

var defaultArticlesCommodities = []string{
	"brent-crude-oil", "wti-crude-oil", "natural-gas", "gold", "silver", "platinum",
	"spx", "tech100-usd", "us30-usd", "eur-usd", "usd-jpy",
	"bitcoin", "ethereum", "solana",
}

// defaultTagMap maps FXEmpire commodity slugs to their hub tag values.
var defaultTagMap = map[string]string{
	"brent-crude-oil": "co-brent-crude-oil",
	"wti-crude-oil":   "co-wti-crude-oil",
	"natural-gas":     "co-natural-gas",
	"gold":            "co-gold",
	"silver":          "co-silver",
	"platinum":        "co-platinum",
	"spx":             "i-spx",
	"tech100-usd":     "i-tech100-usd",
	"us30-usd":        "i-us30-usd",
	"eur-usd":         "c-eur-usd",
	"usd-jpy":         "c-usd-jpy",
	"bitcoin":         "cc-bitcoin",
	"ethereum":        "cc-ethereum",
	"solana":          "cc-solana",
}

// ---------------------------------------------------------------------------
// Data types
// ---------------------------------------------------------------------------

// Article is the normalized article shape output by fxempire-articles.
type Article struct {
	ID          int      `json:"id"`
	Title       string   `json:"title"`
	Slug        string   `json:"slug"`
	Description *string  `json:"description"`
	Excerpt     *string  `json:"excerpt"`
	Tags        []string `json:"tags"`
	Type        string   `json:"type"`
	Tag         string   `json:"tag"`
	Commodity   string   `json:"commodity"`
	Timestamp   int64    `json:"timestamp"`
	ISO         string   `json:"iso"`
	Author      *string  `json:"author"`
	ArticleURL  *string  `json:"articleUrl"`
	FullURL     *string  `json:"fullUrl"`
	TextSnippet *string  `json:"textSnippet"`
	TextFull    *string  `json:"textFull,omitempty"`
}

// ArticlesMeta is the metadata block for the articles payload.
type ArticlesMeta struct {
	Now         string   `json:"now"`
	Cutoff      string   `json:"cutoff"`
	Hours       float64  `json:"hours"`
	TZ          string   `json:"tz"`
	Locale      string   `json:"locale"`
	Commodities []string `json:"commodities"`
}

// articlesPayload is the JSON output envelope for fxempire-articles.
type articlesPayload struct {
	Meta     ArticlesMeta `json:"meta"`
	Articles []Article    `json:"articles"`
}

// rawHubArticle is the per-article shape from the FXEmpire hub API.
type rawHubArticle struct {
	ID          int            `json:"id"`
	Title       string         `json:"title"`
	Slug        string         `json:"slug"`
	Description string         `json:"description"`
	Excerpt     string         `json:"excerpt"`
	Tags        []string       `json:"tags"`
	Type        string         `json:"type"`
	Timestamp   json.Number    `json:"timestamp"`
	Date        string         `json:"date"`
	Author      rawArticleAuth `json:"author"`
	ArticleURL  string         `json:"articleUrl"`
	FullURL     string         `json:"fullUrl"`
}

type rawArticleAuth struct {
	Name string `json:"name"`
}

// rawHubForecasts is the forecasts response envelope (news returns array directly).
type rawHubForecasts struct {
	Articles []rawHubArticle `json:"articles"`
	Paging   struct {
		TotalPages int `json:"totalPages"`
	} `json:"paging"`
}

// rawDetailArticle is the per-article shape from the article detail endpoint.
type rawDetailArticle struct {
	ID          int            `json:"id"`
	ArticleURL  string         `json:"articleUrl"`
	Description string         `json:"description"`
	Excerpt     string         `json:"excerpt"`
	Author      rawArticleAuth `json:"author"`
}

// ---------------------------------------------------------------------------
// Time-window helpers
// ---------------------------------------------------------------------------

// windowHoursForDate returns the look-back window in hours for a given date
// and IANA timezone name. Mirrors windowHoursFor() in fxempire_articles.mjs.
func windowHoursForDate(t time.Time, tzName string) float64 {
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.UTC
	}
	local := t.In(loc)
	switch local.Weekday() {
	case time.Sunday:
		return 72
	case time.Saturday:
		return 48
	default:
		return 24
	}
}

// ---------------------------------------------------------------------------
// URL helpers
// ---------------------------------------------------------------------------

// hubNewsURL builds the FXEmpire hub news URL for a given page and tag.
func hubNewsURL(base, tag string, pageSize, page int) string {
	return fmt.Sprintf("%s/articles/hub/news?size=%d&page=%d&tag=%s",
		base, pageSize, page, url.QueryEscape(tag))
}

// hubForecastsURL builds the FXEmpire hub forecasts URL for a given page and tag.
func hubForecastsURL(base, tag string, pageSize, page int) string {
	return fmt.Sprintf("%s/articles/hub/forecasts?size=%d&page=%d&tag=%s",
		base, pageSize, page, url.QueryEscape(tag))
}

// articleDetailsURL builds the batch article detail endpoint URL.
func articleDetailsURL(base string, ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("%d", id)
	}
	return fmt.Sprintf("%s/articles?ids=%s", base, strings.Join(parts, ","))
}

// ---------------------------------------------------------------------------
// Article URL resolution
// ---------------------------------------------------------------------------

// resolveArticleURL derives the canonical full URL for an article.
// Mirrors resolveArticleUrl() in fxempire_articles.mjs.
func resolveArticleURL(articleURL, fullURL, slug string, id int, articleType string) *string {
	if articleURL != "" {
		var full string
		if strings.HasPrefix(articleURL, "http://") || strings.HasPrefix(articleURL, "https://") {
			full = articleURL
		} else {
			full = "https://www.fxempire.com" + articleURL
		}
		return &full
	}
	if fullURL != "" {
		return &fullURL
	}
	var t string
	switch articleType {
	case "news":
		t = "news"
	case "forecasts":
		t = "forecasts"
	default:
		return nil
	}
	if slug != "" && id != 0 {
		s := fmt.Sprintf("https://www.fxempire.com/%s/article/%s-%d", t, slug, id)
		return &s
	}
	return nil
}

// ---------------------------------------------------------------------------
// Article normalization
// ---------------------------------------------------------------------------

// rawArticleTimestamp extracts a millisecond timestamp from a rawHubArticle.
func rawArticleTimestamp(raw rawHubArticle) int64 {
	if tsFl, err := raw.Timestamp.Float64(); err == nil && tsFl > 0 {
		return int64(tsFl)
	}
	if raw.Date != "" {
		if t, err := time.Parse(time.RFC3339, raw.Date); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

// normalizeHubArticle converts a rawHubArticle into the normalized Article.
func normalizeHubArticle(raw rawHubArticle, articleType, tag, commodity string) Article {
	ts := rawArticleTimestamp(raw)
	var iso string
	if ts > 0 {
		iso = time.UnixMilli(ts).UTC().Format(time.RFC3339)
	}

	fullURL := resolveArticleURL(raw.ArticleURL, raw.FullURL, raw.Slug, raw.ID, articleType)

	a := Article{
		ID:        raw.ID,
		Title:     raw.Title,
		Slug:      raw.Slug,
		Tags:      raw.Tags,
		Type:      articleType,
		Tag:       tag,
		Commodity: commodity,
		Timestamp: ts,
		ISO:       iso,
		FullURL:   fullURL,
	}
	if raw.ArticleURL != "" {
		s := raw.ArticleURL
		a.ArticleURL = &s
	}
	if raw.Description != "" {
		s := raw.Description
		a.Description = &s
	}
	if raw.Excerpt != "" {
		s := raw.Excerpt
		a.Excerpt = &s
	}
	if raw.Author.Name != "" {
		s := raw.Author.Name
		a.Author = &s
	}
	return a
}

// ---------------------------------------------------------------------------
// Article dedup / cap
// ---------------------------------------------------------------------------

// deduplicateArticles removes duplicate articles by id+type composite key.
// Mirrors uniqBy() in fxempire_articles.mjs.
func deduplicateArticles(articles []Article) []Article {
	seen := map[string]bool{}
	out := make([]Article, 0, len(articles))
	for _, a := range articles {
		key := fmt.Sprintf("%d:%s", a.ID, a.Type)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	return out
}

// capArticlesByTypeAndCommodity caps articles per (commodity, type) pair.
// Mirrors the cap loop in fxempire_articles.mjs main().
func capArticlesByTypeAndCommodity(articles []Article, maxItems int) []Article {
	counts := map[string]int{}
	out := make([]Article, 0, len(articles))
	for _, a := range articles {
		key := a.Commodity + ":" + a.Type
		if counts[key] >= maxItems {
			continue
		}
		counts[key]++
		out = append(out, a)
	}
	return out
}

// sortArticlesByTimestamp sorts articles in descending timestamp order (in place).
func sortArticlesByTimestamp(articles []Article) {
	// Insertion sort — article slices are small.
	for i := 1; i < len(articles); i++ {
		for j := i; j > 0 && articles[j].Timestamp > articles[j-1].Timestamp; j-- {
			articles[j], articles[j-1] = articles[j-1], articles[j]
		}
	}
}

// ---------------------------------------------------------------------------
// HTML / text helpers (for --full-text)
// ---------------------------------------------------------------------------

var (
	reScriptStyle  = regexp.MustCompile(`(?is)<(script|style|noscript)\b[^>]*>.*?</(script|style|noscript)>`)
	reBlockClose   = regexp.MustCompile(`(?i)</(p|div|section|article|aside|header|footer|h[1-6]|li|ul|ol|blockquote|pre|table|tr|td)\s*>`)
	reBlockOpen    = regexp.MustCompile(`(?i)<(p|div|section|article|aside|header|footer|h[1-6]|li|ul|ol|blockquote|pre|table|tr|td)\b[^>]*>`)
	reBR           = regexp.MustCompile(`(?i)<br\s*/?>`)
	reAllTags      = regexp.MustCompile(`<[^>]+>`)
	reMultiNewline = regexp.MustCompile(`\n{3,}`)
	reJsonLDScript = regexp.MustCompile(`(?is)<script[^>]+type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	reNextData     = regexp.MustCompile(`(?is)<script[^>]+id=["']__NEXT_DATA__["'][^>]*>(.*?)</script>`)
	reTooManyURLs  = regexp.MustCompile(`https?://`)
	reNavBoiler    = regexp.MustCompile(`(?i)Markets Crypto Forecasts News Education Forex Brokers`)
)

var boilerplateCutMarkers = []string{
	"Important Disclaimers",
	"Risk Disclaimers",
	"FXEmpire is owned and operated",
	"Scan QR code to install app",
}

// minBoilerplateMarkerPosition is the minimum character index at which a
// boilerplate marker must appear before we trim.  Markers earlier than this
// are assumed to be part of legitimate article text.
const minBoilerplateMarkerPosition = 200

// minStructuredBodyLength is the minimum character length for a JSON-LD /
// __NEXT_DATA__ articleBody to be considered usable.
const minStructuredBodyLength = 200

// maxURLsInArticle is the maximum number of hyperlinks allowed before a page
// is considered navigation/boilerplate rather than article content.
const maxURLsInArticle = 8

// minArticleTextLength is the minimum length of stripped article text (in
// characters) required before we accept it as a valid article snippet.
const minArticleTextLength = 300

// defaultTextSnippetLength is the default maximum snippet length in characters.
const defaultTextSnippetLength = 900

// stripHTMLText strips HTML tags and decodes entities, inserting newlines at block boundaries.
func stripHTMLText(src string) string {
	s := reScriptStyle.ReplaceAllString(src, " ")
	s = reBR.ReplaceAllString(s, "\n")
	s = reBlockClose.ReplaceAllString(s, "\n")
	s = reBlockOpen.ReplaceAllString(s, "\n")
	s = reAllTags.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			out = append(out, line)
		}
	}
	s = strings.Join(out, "\n")
	s = reMultiNewline.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// trimBoilerplate cuts article text at known boilerplate markers.
func trimBoilerplate(text string) string {
	for _, marker := range boilerplateCutMarkers {
		idx := strings.Index(text, marker)
		if idx > minBoilerplateMarkerPosition {
			text = strings.TrimSpace(text[:idx])
		}
	}
	return text
}

// deepFindArticleBody traverses arbitrary JSON to find the first non-empty
// "articleBody" string field. Mirrors deepFindFirstStringByKey() in .mjs.
func deepFindArticleBody(v interface{}) string {
	switch val := v.(type) {
	case map[string]interface{}:
		if s, ok := val["articleBody"].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
		for _, child := range val {
			if found := deepFindArticleBody(child); found != "" {
				return found
			}
		}
	case []interface{}:
		for _, item := range val {
			if found := deepFindArticleBody(item); found != "" {
				return found
			}
		}
	}
	return ""
}

// extractStructuredBody tries to extract article body text from JSON-LD or
// __NEXT_DATA__ embedded in an HTML page.
func extractStructuredBody(pageHTML string) string {
	for _, m := range reJsonLDScript.FindAllStringSubmatch(pageHTML, -1) {
		if len(m) < 2 {
			continue
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(m[1]), &parsed); err != nil {
			continue
		}
		if body := deepFindArticleBody(parsed); len(body) > minStructuredBodyLength {
				return body
			}
	}
	if m := reNextData.FindStringSubmatch(pageHTML); len(m) >= 2 {
		var parsed interface{}
		if err := json.Unmarshal([]byte(m[1]), &parsed); err == nil {
			if body := deepFindArticleBody(parsed); len(body) > minStructuredBodyLength {
				return body
			}
		}
	}
	return ""
}

// fetchArticleText fetches and cleans the text of a single article URL.
// Returns nil if the page appears to be boilerplate-only or too short.
func fetchArticleText(fullURL string, maxChars int) *string {
	body, err := toolutil.HTTPGetJSON(fullURL, 25000, map[string]string{
		"User-Agent": "Mozilla/5.0 (OpenClaw; fxempire-articles)",
		"Accept":     "*/*",
	})
	if err != nil {
		return nil
	}
	pageHTML := string(body)
	structured := extractStructuredBody(pageHTML)
	var raw string
	if structured != "" {
		raw = stripHTMLText(structured)
	} else {
		raw = stripHTMLText(pageHTML)
	}
	cleaned := trimBoilerplate(raw)
	if reNavBoiler.MatchString(cleaned) {
		return nil
	}
	if len(reTooManyURLs.FindAllString(cleaned, -1)) > maxURLsInArticle {
		return nil
	}
	if len(cleaned) < minArticleTextLength {
		return nil
	}
	if maxChars > 0 && len(cleaned) > maxChars {
		cleaned = cleaned[:maxChars]
	}
	return &cleaned
}

// ---------------------------------------------------------------------------
// Hub article fetch (paginated)
// ---------------------------------------------------------------------------

// fetchHubPage fetches one page of hub articles for a tag and type.
// Returns (items, totalPages, error). totalPages is only non-zero for forecasts.
func fetchHubPage(base, articleType, tag string, pageSize, page int) ([]rawHubArticle, int, error) {
	var u string
	if articleType == "news" {
		u = hubNewsURL(base, tag, pageSize, page)
	} else {
		u = hubForecastsURL(base, tag, pageSize, page)
	}

	body, err := toolutil.HTTPGetJSON(u, 20000, nil)
	if err != nil {
		return nil, 0, err
	}

	if articleType == "news" {
		var items []rawHubArticle
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, 0, fmt.Errorf("parsing news response: %w", err)
		}
		return items, 0, nil
	}

	var resp rawHubForecasts
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("parsing forecasts response: %w", err)
	}
	return resp.Articles, resp.Paging.TotalPages, nil
}

// fetchHubArticlesForTag fetches all hub articles for a given tag and type,
// stopping when the oldest page item is before cutoff. Mirrors fetchHub() in .mjs.
func fetchHubArticlesForTag(base, articleType, tag string, pageSize, maxPages int, cutoff time.Time) []rawHubArticle {
	var out []rawHubArticle
	for page := 1; page <= maxPages; page++ {
		items, totalPages, err := fetchHubPage(base, articleType, tag, pageSize, page)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to fetch FXEmpire hub page (type=%s, tag=%s, page=%d): %v\n", articleType, tag, page, err)
			break
		}
		if len(items) == 0 {
			break
		}
		out = append(out, items...)

		// Stop if oldest item timestamp is before cutoff.
		var minTS int64
		for _, a := range items {
			ts := rawArticleTimestamp(a)
			if minTS == 0 || (ts > 0 && ts < minTS) {
				minTS = ts
			}
		}
		if minTS > 0 && minTS < cutoff.UnixMilli() {
			break
		}
		if totalPages > 0 && page >= totalPages {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	return out
}

// ---------------------------------------------------------------------------
// Article detail batch fetch
// ---------------------------------------------------------------------------

// fetchArticleDetails fetches article detail records for a batch of IDs.
func fetchArticleDetails(base string, ids []int) ([]rawDetailArticle, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	u := articleDetailsURL(base, ids)
	body, err := toolutil.HTTPGetJSON(u, 20000, nil)
	if err != nil {
		return nil, err
	}
	var details []rawDetailArticle
	if err := json.Unmarshal(body, &details); err != nil {
		return nil, fmt.Errorf("parsing article details: %w", err)
	}
	return details, nil
}

// ---------------------------------------------------------------------------
// Core fetch + combine logic (shared with fxempire-enrich)
// ---------------------------------------------------------------------------

// buildTagMap constructs the slug→tag map by merging defaults with caller overrides.
func buildTagMap(overrides []string) map[string]string {
	m := make(map[string]string, len(defaultTagMap))
	for k, v := range defaultTagMap {
		m[k] = v
	}
	for _, pair := range overrides {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			m[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return m
}

// fetchArticlesPayload fetches and processes articles for the given commodities.
// hoursOverride == nil means auto-detect from weekday in tzName.
// This is the shared core called by both fxempire-articles and fxempire-enrich.
func fetchArticlesPayload(locale, tzName string, hoursOverride *float64, commodities []string, tagOverrides []string, maxItems, pageSize, maxPages int, fullText bool, maxTextChars int) articlesPayload {
	now := time.Now().UTC()
	hours := windowHoursForDate(now, tzName)
	if hoursOverride != nil && *hoursOverride > 0 {
		hours = *hoursOverride
	}
	cutoff := now.Add(-time.Duration(hours * float64(time.Hour)))
	base := fmt.Sprintf("https://www.fxempire.com/api/v1/%s", locale)
	tags := buildTagMap(tagOverrides)

	// Fetch hub articles for each slug that has a tag mapping.
	type taggedRaw struct {
		raw         rawHubArticle
		articleType string
		tag         string
		commodity   string
	}
	var tagged []taggedRaw

	for _, slug := range commodities {
		tag := tags[slug]
		if tag == "" {
			continue
		}
		news := fetchHubArticlesForTag(base, "news", tag, pageSize, maxPages, cutoff)
		for _, a := range news {
			tagged = append(tagged, taggedRaw{a, "news", tag, slug})
		}
		forecasts := fetchHubArticlesForTag(base, "forecasts", tag, pageSize, maxPages, cutoff)
		for _, a := range forecasts {
			tagged = append(tagged, taggedRaw{a, "forecasts", tag, slug})
		}
	}

	// Normalize and filter by time window.
	norm := make([]Article, 0, len(tagged))
	for _, t := range tagged {
		a := normalizeHubArticle(t.raw, t.articleType, t.tag, t.commodity)
		if a.Timestamp == 0 || a.Timestamp < cutoff.UnixMilli() || a.Timestamp > now.UnixMilli() {
			continue
		}
		norm = append(norm, a)
	}

	// Sort descending, dedup, and cap.
	sortArticlesByTimestamp(norm)
	deduped := deduplicateArticles(norm)
	capped := capArticlesByTypeAndCommodity(deduped, maxItems)

	// Collect IDs that need detail enrichment.
	var idsNeedingDetails []int
	for _, a := range capped {
		if a.ArticleURL == nil || (a.Description == nil && a.Excerpt == nil) {
			if a.ID != 0 {
				idsNeedingDetails = append(idsNeedingDetails, a.ID)
			}
		}
	}

	// Batch-fetch article details.
	detailsMap := map[int]rawDetailArticle{}
	const batchSize = 20
	for i := 0; i < len(idsNeedingDetails); i += batchSize {
		end := i + batchSize
		if end > len(idsNeedingDetails) {
			end = len(idsNeedingDetails)
		}
		if details, err := fetchArticleDetails(base, idsNeedingDetails[i:end]); err == nil {
			for _, d := range details {
				detailsMap[d.ID] = d
			}
		} else {
			fmt.Fprintf(os.Stderr, "warning: failed to fetch FXEmpire article details for ids [%d:%d]: %v\n", i, end, err)
		}
		time.Sleep(120 * time.Millisecond)
	}

	// Merge detail data into capped articles.
	for i := range capped {
		d, ok := detailsMap[capped[i].ID]
		if !ok {
			continue
		}
		if capped[i].ArticleURL == nil && d.ArticleURL != "" {
			s := d.ArticleURL
			capped[i].ArticleURL = &s
			capped[i].FullURL = resolveArticleURL(d.ArticleURL, "", capped[i].Slug, capped[i].ID, capped[i].Type)
		}
		if capped[i].Description == nil && d.Description != "" {
			s := d.Description
			capped[i].Description = &s
		}
		if capped[i].Excerpt == nil && d.Excerpt != "" {
			s := d.Excerpt
			capped[i].Excerpt = &s
		}
		if capped[i].Author == nil && d.Author.Name != "" {
			s := d.Author.Name
			capped[i].Author = &s
		}
	}

	// Fetch text snippets / full article text.
	for i := range capped {
		if capped[i].FullURL != nil {
			if fullText {
				capped[i].TextFull = fetchArticleText(*capped[i].FullURL, maxTextChars)
				if capped[i].TextFull != nil {
					n := len(*capped[i].TextFull)
					if n > defaultTextSnippetLength {
						n = defaultTextSnippetLength
					}
					s := (*capped[i].TextFull)[:n]
					capped[i].TextSnippet = &s
				}
			} else {
				capped[i].TextSnippet = fetchArticleText(*capped[i].FullURL, defaultTextSnippetLength)
			}
			time.Sleep(100 * time.Millisecond)
		}

		// Fallback: use description or excerpt if no text was fetched.
		if capped[i].TextSnippet == nil {
			capped[i].TextSnippet = firstNonNil(capped[i].Description, capped[i].Excerpt)
		}
		if fullText && capped[i].TextFull == nil {
			capped[i].TextFull = capped[i].TextSnippet
		}
	}

	return articlesPayload{
		Meta: ArticlesMeta{
			Now:         now.Format("2006-01-02T15:04:05.000Z"),
			Cutoff:      cutoff.UTC().Format(time.RFC3339),
			Hours:       hours,
			TZ:          tzName,
			Locale:      locale,
			Commodities: commodities,
		},
		Articles: capped,
	}
}

// firstNonNil returns the first non-nil string pointer, or nil if all are nil.
func firstNonNil(ptrs ...*string) *string {
	for _, p := range ptrs {
		if p != nil {
			return p
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Output formatting
// ---------------------------------------------------------------------------

func articlesMarkdown(payload articlesPayload) string {
	lines := []string{fmt.Sprintf("## FXEmpire articles — last %.0fh (%s)", payload.Meta.Hours, payload.Meta.TZ)}

	for _, slug := range payload.Meta.Commodities {
		var bySlug []Article
		for _, a := range payload.Articles {
			if a.Commodity == slug {
				bySlug = append(bySlug, a)
			}
		}
		if len(bySlug) == 0 {
			continue
		}

		lines = append(lines, "\n### "+mdArticleEscape(slug))

		for _, articleType := range []string{"news", "forecasts"} {
			var ofType []Article
			for _, a := range bySlug {
				if a.Type == articleType {
					ofType = append(ofType, a)
				}
			}
			if len(ofType) == 0 {
				continue
			}
			lines = append(lines, "\n**"+articleType+"**")
			for _, a := range ofType {
				when := strings.ReplaceAll(strings.ReplaceAll(a.ISO, "T", " "), "+00:00", "Z")
				linkLabel := articleMarkdownLink(a)
				var meta string
				if when != "" {
					meta = " (" + when
					if a.Author != nil {
						meta += ", " + mdArticleEscape(*a.Author)
					}
					meta += ")"
				} else if a.Author != nil {
					meta = " (" + mdArticleEscape(*a.Author) + ")"
				}
				lines = append(lines, "- "+linkLabel+meta)
			}
		}
	}

	return strings.Join(lines, "\n") + "\n"
}

func mdArticleEscape(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

func articleMarkdownLink(a Article) string {
	if a.FullURL == nil {
		return mdArticleEscape(a.Title)
	}
	title := strings.ReplaceAll(strings.ReplaceAll(a.Title, "[", "\\["), "]", "\\]")
	u := *a.FullURL
	u = strings.ReplaceAll(u, "(", "%28")
	u = strings.ReplaceAll(u, ")", "%29")
	return fmt.Sprintf("[%s](%s)", title, u)
}

// ---------------------------------------------------------------------------
// Cobra command
// ---------------------------------------------------------------------------

var fxempireArticlesCmd = &cobra.Command{
	Use:   "fxempire-articles",
	Short: "Fetch FXEmpire news and forecast articles",
	Long: `Fetch news and forecast articles from the FXEmpire hub API.

Articles are fetched for each commodity slug's corresponding hub tag.
Results are filtered to the configured time window, deduplicated, and
capped per commodity/type.

Examples:
  netcup-claw tool fxempire-articles --json
  netcup-claw tool fxempire-articles --commodities brent-crude-oil,gold --json
  netcup-claw tool fxempire-articles --hours 48 --max-items 10 --json
  netcup-claw tool fxempire-articles --commodities gold --full-text --json`,
	RunE: runFXEmpireArticles,
}

func runFXEmpireArticles(cmd *cobra.Command, _ []string) error {
	commodities := faCommodities
	if len(commodities) == 0 {
		commodities = defaultArticlesCommodities
	}

	var hoursOverride *float64
	if cmd.Flags().Changed("hours") && faHours > 0 {
		h := faHours
		hoursOverride = &h
	}

	payload := fetchArticlesPayload(faLocale, faTZ, hoursOverride, commodities, faTags,
		faMaxItems, faPageSize, faMaxPages, faFullText, faMaxTextChars)

	if faJSON {
		b, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding JSON output: %w", err)
		}
		_, err = fmt.Fprintln(os.Stdout, string(b))
		return err
	}

	fmt.Print(articlesMarkdown(payload))
	return nil
}

func init() {
	fxempireArticlesCmd.Flags().StringVar(&faLocale, "locale", "en", "API locale (e.g. en, de)")
	fxempireArticlesCmd.Flags().StringVar(&faTZ, "tz", "Europe/Berlin", "Timezone for time-window calculation (IANA, e.g. Europe/Berlin)")
	fxempireArticlesCmd.Flags().Float64Var(&faHours, "hours", 0, "Override look-back window in hours (auto-detects from weekday if omitted)")
	fxempireArticlesCmd.Flags().StringSliceVar(&faCommodities, "commodities", nil,
		"Comma-separated commodity slugs (default: brent-crude-oil,gold,…)")
	fxempireArticlesCmd.Flags().IntVar(&faMaxItems, "max-items", 6, "Max articles per commodity per type")
	fxempireArticlesCmd.Flags().IntVar(&faPageSize, "page-size", 50, "Articles per page when fetching")
	fxempireArticlesCmd.Flags().IntVar(&faMaxPages, "max-pages", 10, "Max pages to fetch per tag/type")
	fxempireArticlesCmd.Flags().BoolVar(&faJSON, "json", false, "Output as JSON instead of Markdown")
	fxempireArticlesCmd.Flags().BoolVar(&faFullText, "full-text", false, "Fetch full article text (fetches individual article pages)")
	fxempireArticlesCmd.Flags().IntVar(&faMaxTextChars, "max-text-chars", 12000, "Max characters of full text to include")
	fxempireArticlesCmd.Flags().StringSliceVar(&faTags, "tags", nil,
		"Override slug→tag mapping as slug=tag pairs (e.g. gold=co-gold,silver=co-silver)")
}
