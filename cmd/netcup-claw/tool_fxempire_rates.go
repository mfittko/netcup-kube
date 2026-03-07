package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

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
// Domain types
// ---------------------------------------------------------------------------

// RateEntry holds the market data for a single instrument.
type RateEntry struct {
	Slug      string  `json:"slug"`
	Name      string  `json:"name"`
	Category  string  `json:"category"`
	Price     float64 `json:"price"`
	Change    float64 `json:"change"`
	ChangePct float64 `json:"changePct"`
}

// apiRatesResponse is the envelope returned by the standard rates endpoints.
type apiRatesResponse struct {
	Data []apiRateItem `json:"data"`
}

type apiRateItem struct {
	Slug      string  `json:"slug"`
	Name      string  `json:"name"`
	Last      float64 `json:"last"`
	Change    float64 `json:"change"`
	ChangePct float64 `json:"changePct"`
}

// apiCryptoResponse is the envelope returned by the crypto chart endpoint.
type apiCryptoResponse struct {
	Data []apiCryptoCandle `json:"data"`
}

type apiCryptoCandle struct {
	Close     float64 `json:"c"`
	Change    float64 `json:"change"`
	ChangePct float64 `json:"changePct"`
}

// ---------------------------------------------------------------------------
// Instrument classification
// ---------------------------------------------------------------------------

// cryptoSlugs is the set of known crypto instrument slugs.
var cryptoSlugs = map[string]bool{
	"bitcoin":      true,
	"ethereum":     true,
	"litecoin":     true,
	"ripple":       true,
	"bitcoin-cash": true,
	"bitcoin-sv":   true,
	"eos":          true,
	"stellar":      true,
	"cardano":      true,
	"polkadot":     true,
	"solana":       true,
	"dogecoin":     true,
	"shiba-inu":    true,
	"binancecoin":  true,
	"avalanche":    true,
	"chainlink":    true,
	"uniswap":      true,
	"tron":         true,
	"toncoin":      true,
}

// indicesSlugs is the set of known index instrument slugs.
var indicesSlugs = map[string]bool{
	"spx":          true,
	"dji":          true,
	"nasdaq":       true,
	"nasdaq100":    true,
	"ftse":         true,
	"dax":          true,
	"cac":          true,
	"nikkei":       true,
	"hang-seng":    true,
	"asx200":       true,
	"euro-stoxx50": true,
	"russell2000":  true,
	"vix":          true,
	"ibex35":       true,
	"stoxx600":     true,
}

// currencySlugs is the set of known forex (currency pair) instrument slugs.
var currencySlugs = map[string]bool{
	"eur-usd": true,
	"gbp-usd": true,
	"usd-jpy": true,
	"usd-chf": true,
	"aud-usd": true,
	"nzd-usd": true,
	"usd-cad": true,
	"eur-gbp": true,
	"eur-jpy": true,
	"gbp-jpy": true,
	"eur-chf": true,
	"eur-aud": true,
	"eur-cad": true,
	"aud-jpy": true,
	"usd-mxn": true,
	"usd-sek": true,
	"usd-nok": true,
	"usd-dkk": true,
	"usd-pln": true,
	"usd-huf": true,
	"usd-czk": true,
	"usd-try": true,
	"usd-zar": true,
	"usd-sgd": true,
	"usd-hkd": true,
	"usd-cnh": true,
	"usd-inr": true,
	"usd-brl": true,
}

// classifyInstrument maps a slug to its FXEmpire API category.
// Categories: "commodities", "indices", "currencies", "crypto"
// Unknown slugs default to "commodities".
func classifyInstrument(slug string) string {
	s := strings.ToLower(strings.TrimSpace(slug))
	switch {
	case cryptoSlugs[s]:
		return "crypto"
	case indicesSlugs[s]:
		return "indices"
	case currencySlugs[s]:
		return "currencies"
	default:
		return "commodities"
	}
}

// ---------------------------------------------------------------------------
// HTTP fetch helpers
// ---------------------------------------------------------------------------

const (
	fxDefaultTimeoutMs = 20_000
	fxAPIBase          = "https://www.fxempire.com/api/v1"
)

// fetchRates fetches market rates for the given category and slug list from
// the standard FXEmpire rates endpoint.
func fetchRates(baseURL, locale, category string, slugs []string) ([]RateEntry, error) {
	url := fmt.Sprintf("%s/%s/%s/rates", baseURL, locale, category)

	body, err := toolutil.HTTPGetJSON(url, fxDefaultTimeoutMs, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching %s rates: %w", category, err)
	}

	var resp apiRatesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing %s rates response: %w", category, err)
	}

	// Build a lookup set for quick filtering.
	wanted := make(map[string]bool, len(slugs))
	for _, s := range slugs {
		wanted[strings.ToLower(s)] = true
	}

	var results []RateEntry
	for _, item := range resp.Data {
		if len(wanted) > 0 && !wanted[strings.ToLower(item.Slug)] {
			continue
		}
		results = append(results, RateEntry{
			Slug:      item.Slug,
			Name:      item.Name,
			Category:  category,
			Price:     item.Last,
			Change:    item.Change,
			ChangePct: item.ChangePct,
		})
	}
	return results, nil
}

// fetchCryptoUSDSnapshot fetches the latest USD price snapshot for a single
// crypto instrument using the FXEmpire chart endpoint.
func fetchCryptoUSDSnapshot(baseURL, locale, slug string) (RateEntry, error) {
	url := fmt.Sprintf("%s/%s/crypto/chart/usd/%s", baseURL, locale, slug)

	body, err := toolutil.HTTPGetJSON(url, fxDefaultTimeoutMs, nil)
	if err != nil {
		return RateEntry{}, fmt.Errorf("fetching crypto snapshot for %s: %w", slug, err)
	}

	var resp apiCryptoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return RateEntry{}, fmt.Errorf("parsing crypto response for %s: %w", slug, err)
	}
	if len(resp.Data) == 0 {
		return RateEntry{}, fmt.Errorf("no crypto data returned for %s", slug)
	}

	last := resp.Data[len(resp.Data)-1]
	return RateEntry{
		Slug:      slug,
		Name:      slug, // name not provided by chart endpoint; slug is used as fallback
		Category:  "crypto",
		Price:     last.Close,
		Change:    last.Change,
		ChangePct: last.ChangePct,
	}, nil
}

// ---------------------------------------------------------------------------
// Output formatting
// ---------------------------------------------------------------------------

// formatMarkdown formats a slice of RateEntry values as a Markdown table
// grouped by category.
func formatMarkdown(rates []RateEntry) string {
	if len(rates) == 0 {
		return "_No rates available._\n"
	}

	// Group by category, preserving insertion order within each group.
	groups := map[string][]RateEntry{}
	var order []string
	for _, r := range rates {
		if _, ok := groups[r.Category]; !ok {
			order = append(order, r.Category)
		}
		groups[r.Category] = append(groups[r.Category], r)
	}
	sort.Strings(order)

	var sb strings.Builder
	for _, cat := range order {
		entries := groups[cat]
		sb.WriteString(fmt.Sprintf("## %s\n\n", strings.Title(cat))) //nolint:staticcheck
		sb.WriteString("| Instrument | Price | Change | Change % |\n")
		sb.WriteString("|---|---|---|---|\n")
		for _, e := range entries {
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
				e.Name,
				toolutil.FmtNum(e.Price, 2),
				toolutil.FmtNum(e.Change, 2),
				toolutil.FmtPct(e.ChangePct),
			))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Cobra command
// ---------------------------------------------------------------------------

var fxempireRatesCmd = &cobra.Command{
	Use:   "fxempire-rates",
	Short: "Fetch and format FXEmpire market rates",
	Long: `Fetch market rates from the FXEmpire public API and output them as
Markdown (default) or JSON.

Instruments are specified by their FXEmpire slug.  The tool automatically
classifies each slug into the correct API category (commodities, indices,
currencies, crypto) and batches the requests accordingly.

Examples:
  netcup-claw tool fxempire-rates --commodities brent-crude-oil,gold
  netcup-claw tool fxempire-rates --commodities eur-usd,bitcoin --json
  netcup-claw tool fxempire-rates --commodities brent-crude-oil,spx,eur-usd,bitcoin`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(fxInstruments) == 0 {
			return fmt.Errorf("at least one instrument slug is required (use --commodities)")
		}

		// Group slugs by category.
		byCategory := map[string][]string{}
		for _, raw := range fxInstruments {
			// Each element of fxInstruments may itself be a comma-separated list.
			for _, slug := range strings.Split(raw, ",") {
				slug = strings.TrimSpace(slug)
				if slug == "" {
					continue
				}
				cat := classifyInstrument(slug)
				byCategory[cat] = append(byCategory[cat], slug)
			}
		}

		var allRates []RateEntry

		// Fetch standard categories (commodities, indices, currencies).
		for _, cat := range []string{"commodities", "indices", "currencies"} {
			slugs, ok := byCategory[cat]
			if !ok {
				continue
			}
			rates, err := fetchRates(fxAPIBase, fxLocale, cat, slugs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: %v\n", err)
				continue
			}
			allRates = append(allRates, rates...)
		}

		// Fetch crypto individually.
		if cryptoSlugs, ok := byCategory["crypto"]; ok {
			for _, slug := range cryptoSlugs {
				entry, err := fetchCryptoUSDSnapshot(fxAPIBase, fxLocale, slug)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: %v\n", err)
					continue
				}
				allRates = append(allRates, entry)
			}
		}

		if fxJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(allRates)
		}

		fmt.Print(formatMarkdown(allRates))
		return nil
	},
}

func init() {
	fxempireRatesCmd.Flags().StringVar(&fxLocale, "locale", "en", "API locale (e.g. en, de)")
	fxempireRatesCmd.Flags().StringSliceVar(&fxInstruments, "commodities", nil,
		"Comma-separated list of instrument slugs (e.g. brent-crude-oil,gold,eur-usd,bitcoin)")
	fxempireRatesCmd.Flags().BoolVar(&fxJSON, "json", false, "Output as JSON instead of Markdown")
}
