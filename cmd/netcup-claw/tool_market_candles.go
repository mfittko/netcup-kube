package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"

	"github.com/mfittko/netcup-kube/internal/toolutil"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Flags
// ---------------------------------------------------------------------------

var (
	mcProvider          string
	mcLocale            string
	mcMarket            string
	mcInstrument        string
	mcGranularity       string
	mcCount             int
	mcFrom              int64
	mcTo                int64
	mcAlignmentTimezone string
	mcJSON              bool
	mcPretty            bool
	mcVendor            string
	mcPrice             string
	mcWeeklyAlignment   string
	mcDailyAlignment    int
)

// ---------------------------------------------------------------------------
// Output types
// ---------------------------------------------------------------------------

// Candle is the normalized candle shape output by market-candles.
// Matches the .mjs normalized output: time, open, high, low, close, volume, complete.
type Candle struct {
	Time     string  `json:"time"`
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	Volume   float64 `json:"volume"`
	Complete bool    `json:"complete"`
}

// CandlesResult is the JSON output envelope for market-candles.
type CandlesResult struct {
	OK          bool     `json:"ok"`
	Mode        string   `json:"mode"`
	Provider    string   `json:"provider"`
	Market      string   `json:"market"`
	Instrument  string   `json:"instrument"`
	Granularity string   `json:"granularity"`
	RequestURL  string   `json:"requestUrl"`
	Count       int      `json:"count"`
	Candles     []Candle `json:"candles"`
}

// ---------------------------------------------------------------------------
// URL builders
// ---------------------------------------------------------------------------

// buildFXEmpireCandlesURL builds the FXEmpire chart candles endpoint URL.
// Mirrors buildCandlesUrl() in fxempire_live_data.mjs for provider=fxempire.
func buildFXEmpireCandlesURL(locale, market, instrument, granularity, vendor, price, weeklyAlignment, alignmentTimezone string, dailyAlignment, count int, from int64) string {
	base := fmt.Sprintf("https://www.fxempire.com/api/v1/%s/%s/chart/candles", locale, market)
	u, _ := url.Parse(base)
	q := url.Values{}
	q.Set("instrument", instrument)
	q.Set("granularity", granularity)
	q.Set("count", strconv.Itoa(count))
	q.Set("price", price)
	q.Set("weeklyAlignment", weeklyAlignment)
	q.Set("alignmentTimezone", alignmentTimezone)
	q.Set("dailyAlignment", strconv.Itoa(dailyAlignment))
	q.Set("vendor", vendor)
	if from > 0 {
		q.Set("from", strconv.FormatInt(from, 10))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// buildOandaCandlesURL builds the Oanda proxy candles endpoint URL.
// Mirrors buildCandlesUrl() in fxempire_live_data.mjs for provider=oanda.
func buildOandaCandlesURL(instrument, granularity, alignmentTimezone string, count int, to int64) string {
	u, _ := url.Parse("https://p.fxempire.com/oanda/candles/latest")
	q := url.Values{}
	q.Set("instrument", instrument)
	q.Set("granularity", granularity)
	q.Set("count", strconv.Itoa(count))
	q.Set("alignmentTimezone", alignmentTimezone)
	if to > 0 {
		q.Set("to", strconv.FormatInt(to, 10))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// ---------------------------------------------------------------------------
// Candle normalization
// ---------------------------------------------------------------------------

// fxRawCandle is the per-candle shape returned by the FXEmpire chart API.
type fxRawCandle struct {
	Date   string      `json:"Date"`
	DateLC string      `json:"date"`
	Open   json.Number `json:"Open"`
	High   json.Number `json:"High"`
	Low    json.Number `json:"Low"`
	Close  json.Number `json:"Close"`
	Volume json.Number `json:"Volume"`
}

// normalizeFXEmpireCandles converts raw FXEmpire candle JSON (array) into the
// normalized Candle slice. Mirrors normalizeFxEmpireCandles() in .mjs.
func normalizeFXEmpireCandles(raw []byte) ([]Candle, error) {
	var rows []fxRawCandle
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("parsing FXEmpire candles: %w", err)
	}
	out := make([]Candle, 0, len(rows))
	for _, row := range rows {
		t := row.Date
		if t == "" {
			t = row.DateLC
		}
		if t == "" {
			continue
		}
		open, err1 := row.Open.Float64()
		high, err2 := row.High.Float64()
		low, err3 := row.Low.Float64()
		cls, err4 := row.Close.Float64()
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue
		}
		if !candleFinite(open) || !candleFinite(high) || !candleFinite(low) || !candleFinite(cls) {
			continue
		}
		vol, _ := row.Volume.Float64()
		out = append(out, Candle{
			Time:     t,
			Open:     open,
			High:     high,
			Low:      low,
			Close:    cls,
			Volume:   vol,
			Complete: true,
		})
	}
	return out, nil
}

// oandaRawPayload is the top-level shape returned by the Oanda proxy API.
type oandaRawPayload struct {
	Candles []oandaRawCandle `json:"candles"`
}

// oandaRawCandle is the per-candle shape from the Oanda proxy API.
type oandaRawCandle struct {
	Time     string         `json:"time"`
	Mid      oandaMidPrices `json:"mid"`
	Volume   json.Number    `json:"volume"`
	Complete bool           `json:"complete"`
}

// oandaMidPrices holds the mid-price OHLC values from the Oanda proxy.
type oandaMidPrices struct {
	O json.Number `json:"o"`
	H json.Number `json:"h"`
	L json.Number `json:"l"`
	C json.Number `json:"c"`
}

// normalizeOandaCandles converts raw Oanda candle JSON into the normalized
// Candle slice. Mirrors normalizeOandaCandles() in fxempire_live_data.mjs.
func normalizeOandaCandles(raw []byte) ([]Candle, error) {
	var payload oandaRawPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parsing Oanda candles: %w", err)
	}
	out := make([]Candle, 0, len(payload.Candles))
	for _, row := range payload.Candles {
		if row.Time == "" {
			continue
		}
		open, err1 := row.Mid.O.Float64()
		high, err2 := row.Mid.H.Float64()
		low, err3 := row.Mid.L.Float64()
		cls, err4 := row.Mid.C.Float64()
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue
		}
		if !candleFinite(open) || !candleFinite(high) || !candleFinite(low) || !candleFinite(cls) {
			continue
		}
		vol, _ := row.Volume.Float64()
		out = append(out, Candle{
			Time:     row.Time,
			Open:     open,
			High:     high,
			Low:      low,
			Close:    cls,
			Volume:   vol,
			Complete: row.Complete,
		})
	}
	return out, nil
}

// candleFinite returns true if v is a finite float64 (not NaN or ±Inf).
func candleFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// ---------------------------------------------------------------------------
// Cobra command
// ---------------------------------------------------------------------------

var marketCandlesCmd = &cobra.Command{
	Use:   "market-candles",
	Short: "Fetch OHLCV market candle data from FXEmpire or Oanda",
	Long: `Fetch OHLCV candle data and output it as normalized JSON.

Supports two providers:
  fxempire  - FXEmpire chart API (chart/candles endpoint)
  oanda     - FXEmpire-proxied Oanda candles endpoint

The normalized output always uses the unified candle shape:
  time, open, high, low, close, volume, complete

Examples:
  netcup-claw tool market-candles --provider oanda --instrument NAS100/USD --granularity M1 --count 200 --json
  netcup-claw tool market-candles --provider fxempire --market indices --instrument NAS100/USD --granularity M5 --count 500 --json
  netcup-claw tool market-candles --provider oanda --instrument EUR_USD --granularity M5 --count 100 --pretty=false`,
	RunE: runMarketCandles,
}

func runMarketCandles(_ *cobra.Command, _ []string) error {
	var requestURL string
	if mcProvider == "oanda" {
		requestURL = buildOandaCandlesURL(mcInstrument, mcGranularity, mcAlignmentTimezone, mcCount, mcTo)
	} else {
		requestURL = buildFXEmpireCandlesURL(mcLocale, mcMarket, mcInstrument, mcGranularity,
			mcVendor, mcPrice, mcWeeklyAlignment, mcAlignmentTimezone, mcDailyAlignment, mcCount, mcFrom)
	}

	raw, err := toolutil.HTTPGetJSON(requestURL, 25000, nil)
	if err != nil {
		return err
	}

	var candles []Candle
	if mcProvider == "oanda" {
		candles, err = normalizeOandaCandles(raw)
	} else {
		candles, err = normalizeFXEmpireCandles(raw)
	}
	if err != nil {
		return err
	}
	if candles == nil {
		candles = []Candle{}
	}

	result := CandlesResult{
		OK:          true,
		Mode:        "candles",
		Provider:    mcProvider,
		Market:      mcMarket,
		Instrument:  mcInstrument,
		Granularity: mcGranularity,
		RequestURL:  requestURL,
		Count:       len(candles),
		Candles:     candles,
	}

	var b []byte
	if mcPretty {
		b, err = json.MarshalIndent(result, "", "  ")
	} else {
		b, err = json.Marshal(result)
	}
	if err != nil {
		return fmt.Errorf("encoding JSON output: %w", err)
	}
	_, err = fmt.Fprintln(os.Stdout, string(b))
	return err
}

func init() {
	marketCandlesCmd.Flags().StringVar(&mcProvider, "provider", "fxempire", "Candle data provider: fxempire|oanda")
	marketCandlesCmd.Flags().StringVar(&mcLocale, "locale", "en", "API locale (FXEmpire only)")
	marketCandlesCmd.Flags().StringVar(&mcMarket, "market", "indices", "Market category: commodities|indices|currencies|crypto-coin (FXEmpire only)")
	marketCandlesCmd.Flags().StringVar(&mcInstrument, "instrument", "NAS100/USD", "Instrument identifier (e.g. NAS100/USD, EUR_USD)")
	marketCandlesCmd.Flags().StringVar(&mcGranularity, "granularity", "M5", "Candle granularity (e.g. M1, M5, H1, D)")
	marketCandlesCmd.Flags().IntVar(&mcCount, "count", 500, "Number of candles to fetch")
	marketCandlesCmd.Flags().Int64Var(&mcFrom, "from", 0, "Start timestamp in Unix seconds (FXEmpire only)")
	marketCandlesCmd.Flags().Int64Var(&mcTo, "to", 0, "End timestamp in Unix seconds (Oanda only)")
	marketCandlesCmd.Flags().StringVar(&mcAlignmentTimezone, "alignment-timezone", "UTC", "Alignment timezone (e.g. UTC, Europe/Berlin)")
	marketCandlesCmd.Flags().BoolVar(&mcJSON, "json", true, "Output as JSON (market-candles always outputs JSON)")
	marketCandlesCmd.Flags().BoolVar(&mcPretty, "pretty", true, "Pretty-print JSON output (use --pretty=false for compact)")
	marketCandlesCmd.Flags().StringVar(&mcVendor, "vendor", "oanda", "Data vendor hint (FXEmpire only)")
	marketCandlesCmd.Flags().StringVar(&mcPrice, "price", "M", "Price type: M (mid)|B (bid)|A (ask) (FXEmpire only)")
	marketCandlesCmd.Flags().StringVar(&mcWeeklyAlignment, "weekly-alignment", "Monday", "Weekly alignment day (FXEmpire only)")
	marketCandlesCmd.Flags().IntVar(&mcDailyAlignment, "daily-alignment", 0, "Daily alignment hour 0-23 (FXEmpire only)")
}
