package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mfittko/netcup-kube/internal/toolutil"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Flags
// ---------------------------------------------------------------------------

var (
	fxLocale      string
	fxInstruments []string
	fxJSON        bool
)

// ---------------------------------------------------------------------------
// Instrument classification
// Mirrors classifyInstrument() in fxempire_rates.mjs exactly.
// ---------------------------------------------------------------------------

var fxCommoditySet = map[string]bool{
	"brent-crude-oil": true,
	"wti-crude-oil":   true,
	"natural-gas":     true,
	"gold":            true,
	"silver":          true,
	"platinum":        true,
}

var fxIndexSet = map[string]bool{
	"spx":         true,
	"tech100-usd": true,
	"us30-usd":    true,
	"de30-eur":    true,
	"uk100-gbp":   true,
	"jp225-usd":   true,
	"fr40-eur":    true,
	"vix":         true,
}

var fxCurrencySet = map[string]bool{
	"eur-usd": true,
	"usd-jpy": true,
	"gbp-usd": true,
	"usd-chf": true,
	"usd-cad": true,
	"aud-usd": true,
	"nzd-usd": true,
}

// classifyInstrument maps a slug to its FXEmpire API category.
// Priority: explicit commodity/index/currency sets → single-hyphen heuristic → "crypto-coin".
func classifyInstrument(slug string) string {
	s := strings.ToLower(strings.TrimSpace(slug))
	if fxCommoditySet[s] {
		return "commodities"
	}
	if fxIndexSet[s] {
		return "indices"
	}
	if fxCurrencySet[s] {
		return "currencies"
	}
	// Heuristic: exactly one hyphen → treat as a currency pair.
	if strings.Contains(s, "-") && len(strings.Split(s, "-")) == 2 {
		return "currencies"
	}
	return "crypto-coin"
}

// ---------------------------------------------------------------------------
// API response types
// ---------------------------------------------------------------------------

// ratesAPIResponse is the JSON envelope returned by the standard rates endpoints
// (commodities, indices, currencies, crypto-coin rates).
type ratesAPIResponse struct {
	Entities map[string]ratesEntity `json:"entities"`
	Prices   map[string]ratesPrice  `json:"prices"`
}

type ratesEntity struct {
	Name          string   `json:"name"`
	Last          *float64 `json:"last"`
	Change        *float64 `json:"change"`
	PercentChange *float64 `json:"percentChange"`
	LastUpdate    string   `json:"lastUpdate"`
}

type ratesPrice struct {
	Last          *float64 `json:"last"`
	Change        *float64 `json:"change"`
	PercentChange *float64 `json:"percentChange"`
	LastUpdate    string   `json:"lastUpdate"`
}

// cryptoChartResp is the envelope returned by the crypto-coin chart endpoint.
// prices is an array of [ms_timestamp, price] pairs.
type cryptoChartResp struct {
	Prices [][]json.Number `json:"prices"`
}

// ---------------------------------------------------------------------------
// Output types (JSON payload)
// ---------------------------------------------------------------------------

type fxPayload struct {
	Meta        fxMeta    `json:"meta"`
	RatesURL    string    `json:"ratesUrl"`
	Prices      []fxPrice `json:"prices"`
	PricesError *string   `json:"pricesError"`
}

type fxMeta struct {
	Now         string   `json:"now"`
	Locale      string   `json:"locale"`
	Commodities []string `json:"commodities"`
}

type fxPrice struct {
	Slug       string   `json:"slug"`
	Name       string   `json:"name"`
	Last       *float64 `json:"last"`
	Change     *float64 `json:"change"`
	Pct        *float64 `json:"pct"`
	LastUpdate *string  `json:"lastUpdate"`
}

// ---------------------------------------------------------------------------
// Fetch helpers
// ---------------------------------------------------------------------------

// fetchRatesURL calls a single rates endpoint URL and returns the decoded
// entities and prices maps.
func fetchRatesURL(u string) (map[string]ratesEntity, map[string]ratesPrice, error) {
	body, err := toolutil.HTTPGetJSON(u, 20000, nil)
	if err != nil {
		return nil, nil, err
	}
	var resp ratesAPIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("parsing rates response from %s: %w", u, err)
	}
	entities := resp.Entities
	if entities == nil {
		entities = map[string]ratesEntity{}
	}
	prices := resp.Prices
	if prices == nil {
		prices = map[string]ratesPrice{}
	}
	return entities, prices, nil
}

// cryptoSnapshot holds the price info derived from the 1d chart endpoint.
type cryptoSnapshot struct {
	slug       string
	chartURL   string
	price      *float64
	change     *float64
	pct        *float64
	lastUpdate string
}

// fetchCryptoUSDSnapshot fetches a 1-day USD chart for slug and derives the
// last price, change, and percentage change from the first→last candle.
// Mirrors fetchCryptoUsdSnapshot() in fxempire_rates.mjs.
func fetchCryptoUSDSnapshot(base, locale, slug string) (cryptoSnapshot, error) {
	u := fmt.Sprintf("%s/%s/crypto-coin/chart?slug=%s&from=1d&quote=usd",
		base, locale, url.QueryEscape(slug))

	body, err := toolutil.HTTPGetJSON(u, 20000, nil)
	if err != nil {
		return cryptoSnapshot{slug: slug, chartURL: u}, err
	}

	var resp cryptoChartResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return cryptoSnapshot{slug: slug, chartURL: u},
			fmt.Errorf("parsing crypto chart for %s: %w", slug, err)
	}

	snap := cryptoSnapshot{slug: slug, chartURL: u}
	points := resp.Prices
	if len(points) == 0 {
		return snap, nil
	}

	first := points[0]
	last := points[len(points)-1]

	firstPrice := cryptoPointPrice(first)
	lastPrice := cryptoPointPrice(last)

	if firstPrice != nil && lastPrice != nil {
		c := *lastPrice - *firstPrice
		snap.change = &c
		if *firstPrice != 0 {
			p := (c / *firstPrice) * 100
			snap.pct = &p
		}
	}
	snap.price = lastPrice

	// Derive lastUpdate from the last candle's ms timestamp (index 0).
	if len(last) > 0 {
		if ts, err := last[0].Float64(); err == nil && ts > 0 {
			snap.lastUpdate = time.UnixMilli(int64(ts)).UTC().Format(time.RFC3339)
		}
	}

	return snap, nil
}

// cryptoPointPrice extracts the price value (index 1) from a chart point.
func cryptoPointPrice(point []json.Number) *float64 {
	if len(point) < 2 {
		return nil
	}
	v, err := point[1].Float64()
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

// fmtNumMD returns the US-locale-formatted number string, or "null" for nil
// (matching the JS template-literal behaviour when fmtNum returns null).
func fmtNumMD(v *float64) string {
	if v == nil {
		return "null"
	}
	return toolutil.FmtNumUS(*v)
}

// mdEscape escapes pipe characters in a Markdown cell value.
func mdEscape(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// formatMarkdown formats the payload as a Markdown bullet list, matching the
// output of the JS version exactly.
func formatMarkdown(payload fxPayload) string {
	lines := []string{"## FXEmpire rates"}

	if payload.PricesError != nil {
		lines = append(lines, "- ERROR: "+mdEscape(*payload.PricesError))
	} else {
		for _, row := range payload.Prices {
			var chStr, pctStr string
			if row.Change != nil {
				chStr = toolutil.FmtNumUS(*row.Change)
			}
			if row.Pct != nil {
				pctStr = toolutil.FmtPct(*row.Pct)
			}

			line := fmt.Sprintf("- **%s** (%s): %s",
				mdEscape(row.Name), row.Slug, fmtNumMD(row.Last))

			if chStr != "" && pctStr != "" {
				line += fmt.Sprintf(" (%s, %s)", chStr, pctStr)
			}
			if row.LastUpdate != nil && *row.LastUpdate != "" {
				line += fmt.Sprintf(" — lastUpdate %s", *row.LastUpdate)
			}

			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "\n") + "\n"
}

// ---------------------------------------------------------------------------
// Cobra command
// ---------------------------------------------------------------------------

var fxempireRatesCmd = &cobra.Command{
	Use:   "fxempire-rates",
	Short: "Fetch and format FXEmpire market rates",
	Long: `Fetch market rates from the FXEmpire public API and output them as
Markdown (default) or JSON.

Instruments are specified as comma-separated FXEmpire slugs.  The tool
automatically classifies each slug into the correct API category
(commodities, indices, currencies, crypto-coin) and batches requests.

Examples:
  netcup-claw tool fxempire-rates --commodities brent-crude-oil,gold
  netcup-claw tool fxempire-rates --commodities brent-crude-oil,gold --json
  netcup-claw tool fxempire-rates --commodities brent-crude-oil,spx,eur-usd,bitcoin`,
	RunE: runFXEmpireRates,
}

func runFXEmpireRates(_ *cobra.Command, _ []string) error {
	// Resolve instrument list (defaults match JS defaults).
	instruments := fxInstruments
	if len(instruments) == 0 {
		instruments = []string{"brent-crude-oil", "natural-gas", "gold", "silver"}
	}

	base := "https://www.fxempire.com/api/v1"

	// Group slugs by API category.
	groups := map[string][]string{
		"commodities": nil,
		"indices":     nil,
		"currencies":  nil,
		"crypto-coin": nil,
	}
	for _, raw := range instruments {
		for _, slug := range strings.Split(raw, ",") {
			slug = strings.TrimSpace(slug)
			if slug == "" {
				continue
			}
			cat := classifyInstrument(slug)
			groups[cat] = append(groups[cat], slug)
		}
	}

	// Build rates URLs (standard endpoints).
	var ratesURLs []string
	if len(groups["commodities"]) > 0 {
		ratesURLs = append(ratesURLs, fmt.Sprintf(
			"%s/%s/commodities/rates?instruments=%s&includeFullData=true&includeSparkLines=true",
			base, fxLocale, url.QueryEscape(strings.Join(groups["commodities"], ","))))
	}
	if len(groups["indices"]) > 0 {
		ratesURLs = append(ratesURLs, fmt.Sprintf(
			"%s/%s/indices/rates?instruments=%s&includeFullData=true&includeSparkLines=true",
			base, fxLocale, url.QueryEscape(strings.Join(groups["indices"], ","))))
	}
	if len(groups["currencies"]) > 0 {
		ratesURLs = append(ratesURLs, fmt.Sprintf(
			"%s/%s/currencies/rates?category=&includeSparkLines=true&includeFullData=true&instruments=%s",
			base, fxLocale, url.QueryEscape(strings.Join(groups["currencies"], ","))))
	}
	if len(groups["crypto-coin"]) > 0 {
		ratesURLs = append(ratesURLs, fmt.Sprintf(
			"%s/%s/crypto-coin/rates?instruments=%s&includeFullData=true",
			base, fxLocale, url.QueryEscape(strings.Join(groups["crypto-coin"], ","))))
	}

	// Fetch and merge all standard rates endpoints.
	allEntities := map[string]ratesEntity{}
	allPrices := map[string]ratesPrice{}
	var ratesErr string

	for _, u := range ratesURLs {
		ents, prs, err := fetchRatesURL(u)
		if err != nil {
			if ratesErr != "" {
				ratesErr += "; " + err.Error()
			} else {
				ratesErr = err.Error()
			}
			continue
		}
		for k, v := range ents {
			allEntities[k] = v
		}
		for k, v := range prs {
			allPrices[k] = v
		}
	}

	// Fetch crypto chart snapshots and merge into prices (overrides rates data).
	var cryptoChartURLs []string
	for _, slug := range groups["crypto-coin"] {
		snap, err := fetchCryptoUSDSnapshot(base, fxLocale, slug)
		cryptoChartURLs = append(cryptoChartURLs, snap.chartURL)
		if err != nil {
			if ratesErr != "" {
				ratesErr += "; " + err.Error()
			} else {
				ratesErr = err.Error()
			}
			continue
		}
		existing := allPrices[slug]
		if snap.price != nil {
			existing.Last = snap.price
		}
		if snap.change != nil {
			existing.Change = snap.change
		}
		if snap.pct != nil {
			existing.PercentChange = snap.pct
		}
		if snap.lastUpdate != "" {
			existing.LastUpdate = snap.lastUpdate
		}
		allPrices[slug] = existing
	}

	// Build the ordered flat price list (same order as input instruments).
	var prices []fxPrice
	seen := map[string]bool{}
	for _, raw := range instruments {
		for _, slug := range strings.Split(raw, ",") {
			slug = strings.TrimSpace(slug)
			if slug == "" || seen[slug] {
				continue
			}
			seen[slug] = true

			e := allEntities[slug]
			p := allPrices[slug]

			name := e.Name
			if name == "" {
				name = slug
			}

			fp := fxPrice{Slug: slug, Name: name}
			fp.Last = coalesce(p.Last, e.Last)
			fp.Change = coalesce(p.Change, e.Change)
			fp.Pct = coalesce(p.PercentChange, e.PercentChange)

			lu := coalesceStr(p.LastUpdate, e.LastUpdate)
			if lu != "" {
				fp.LastUpdate = &lu
			}

			prices = append(prices, fp)
		}
	}

	// Combine all URLs into the ratesUrl field.
	allURLs := append(ratesURLs, cryptoChartURLs...)
	ratesURLStr := strings.Join(allURLs, " | ")

	var pricesErrPtr *string
	if ratesErr != "" {
		pricesErrPtr = &ratesErr
	}

	payload := fxPayload{
		Meta: fxMeta{
			Now:         time.Now().UTC().Format(time.RFC3339),
			Locale:      fxLocale,
			Commodities: instruments,
		},
		RatesURL:    ratesURLStr,
		Prices:      prices,
		PricesError: pricesErrPtr,
	}

	if fxJSON {
		b, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding JSON output: %w", err)
		}
		_, err = fmt.Fprintln(os.Stdout, string(b))
		return err
	}

	fmt.Print(formatMarkdown(payload))
	return nil
}

// coalesce returns the first non-nil float64 pointer.
func coalesce(a, b *float64) *float64 {
	if a != nil {
		return a
	}
	return b
}

// coalesceStr returns the first non-empty string.
func coalesceStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func init() {
	fxempireRatesCmd.Flags().StringVar(&fxLocale, "locale", "en", "API locale (e.g. en, de)")
	fxempireRatesCmd.Flags().StringSliceVar(&fxInstruments, "commodities", nil,
		"Comma-separated instrument slugs (e.g. brent-crude-oil,gold,eur-usd,bitcoin)")
	fxempireRatesCmd.Flags().BoolVar(&fxJSON, "json", false, "Output as JSON instead of Markdown")
}
