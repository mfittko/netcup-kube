package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// buildFXEmpireCandlesURL
// ---------------------------------------------------------------------------

func TestBuildFXEmpireCandlesURL_Basic(t *testing.T) {
	u := buildFXEmpireCandlesURL("en", "indices", "NAS100/USD", "M5", "oanda", "M", "Monday", "UTC", 0, 500, 0)

	if !strings.HasPrefix(u, "https://www.fxempire.com/api/v1/en/indices/chart/candles?") {
		t.Errorf("unexpected base URL: %s", u)
	}
	for _, want := range []string{
		"instrument=NAS100%2FUSD",
		"granularity=M5",
		"count=500",
		"price=M",
		"vendor=oanda",
		"alignmentTimezone=UTC",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("URL missing %q:\n%s", want, u)
		}
	}
}

func TestBuildFXEmpireCandlesURL_WithFrom(t *testing.T) {
	u := buildFXEmpireCandlesURL("en", "indices", "NAS100/USD", "M5", "oanda", "M", "Monday", "UTC", 0, 500, 1772582400)
	if !strings.Contains(u, "from=1772582400") {
		t.Errorf("URL should contain from param: %s", u)
	}
}

func TestBuildFXEmpireCandlesURL_NoFromWhenZero(t *testing.T) {
	u := buildFXEmpireCandlesURL("en", "indices", "NAS100/USD", "M5", "oanda", "M", "Monday", "UTC", 0, 500, 0)
	if strings.Contains(u, "from=") {
		t.Errorf("URL should not contain from param when zero: %s", u)
	}
}

func TestBuildFXEmpireCandlesURL_AlignmentTimezone(t *testing.T) {
	u := buildFXEmpireCandlesURL("en", "commodities", "brent-crude-oil", "H1", "oanda", "M", "Monday", "Europe/Berlin", 0, 100, 0)
	if !strings.Contains(u, "alignmentTimezone=Europe%2FBerlin") {
		t.Errorf("URL should contain encoded timezone: %s", u)
	}
}

// ---------------------------------------------------------------------------
// buildOandaCandlesURL
// ---------------------------------------------------------------------------

func TestBuildOandaCandlesURL_Basic(t *testing.T) {
	u := buildOandaCandlesURL("NAS100/USD", "M1", "UTC", 200, 0)

	if !strings.HasPrefix(u, "https://p.fxempire.com/oanda/candles/latest?") {
		t.Errorf("unexpected base URL: %s", u)
	}
	for _, want := range []string{
		"instrument=NAS100%2FUSD",
		"granularity=M1",
		"count=200",
		"alignmentTimezone=UTC",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("URL missing %q:\n%s", want, u)
		}
	}
}

func TestBuildOandaCandlesURL_WithTo(t *testing.T) {
	u := buildOandaCandlesURL("NAS100/USD", "M1", "UTC", 200, 1772654999)
	if !strings.Contains(u, "to=1772654999") {
		t.Errorf("URL should contain to param: %s", u)
	}
}

func TestBuildOandaCandlesURL_NoToWhenZero(t *testing.T) {
	u := buildOandaCandlesURL("NAS100/USD", "M1", "UTC", 200, 0)
	if strings.Contains(u, "to=") {
		t.Errorf("URL should not contain to param when zero: %s", u)
	}
}

func TestBuildOandaCandlesURL_TimezoneEncoded(t *testing.T) {
	u := buildOandaCandlesURL("EUR_USD", "M5", "Europe/Berlin", 100, 0)
	if !strings.Contains(u, "alignmentTimezone=Europe%2FBerlin") {
		t.Errorf("URL should encode timezone: %s", u)
	}
}

// ---------------------------------------------------------------------------
// normalizeFXEmpireCandles
// ---------------------------------------------------------------------------

func TestNormalizeFXEmpireCandles_Basic(t *testing.T) {
	raw := `[
		{"Date":"2024-01-15T10:00:00Z","Open":"17000.5","High":"17050.25","Low":"16990.0","Close":"17020.75","Volume":"1234"},
		{"Date":"2024-01-15T10:05:00Z","Open":"17020.75","High":"17060.0","Low":"17010.0","Close":"17045.0","Volume":"987"}
	]`
	candles, err := normalizeFXEmpireCandles([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candles) != 2 {
		t.Fatalf("expected 2 candles, got %d", len(candles))
	}

	c := candles[0]
	if c.Time != "2024-01-15T10:00:00Z" {
		t.Errorf("Time = %q, want 2024-01-15T10:00:00Z", c.Time)
	}
	if c.Open != 17000.5 {
		t.Errorf("Open = %v, want 17000.5", c.Open)
	}
	if c.High != 17050.25 {
		t.Errorf("High = %v, want 17050.25", c.High)
	}
	if c.Low != 16990.0 {
		t.Errorf("Low = %v, want 16990", c.Low)
	}
	if c.Close != 17020.75 {
		t.Errorf("Close = %v, want 17020.75", c.Close)
	}
	if c.Volume != 1234 {
		t.Errorf("Volume = %v, want 1234", c.Volume)
	}
	if !c.Complete {
		t.Errorf("Complete should be true for FXEmpire candles")
	}
}

func TestNormalizeFXEmpireCandles_LowercaseDateField(t *testing.T) {
	// Some FXEmpire responses use lowercase "date" instead of "Date".
	raw := `[{"date":"2024-01-15T10:00:00Z","Open":"17000","High":"17010","Low":"16990","Close":"17005","Volume":"100"}]`
	candles, err := normalizeFXEmpireCandles([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candles) != 1 {
		t.Fatalf("expected 1 candle, got %d", len(candles))
	}
	if candles[0].Time != "2024-01-15T10:00:00Z" {
		t.Errorf("expected lowercase date field to be read, got %q", candles[0].Time)
	}
}

func TestNormalizeFXEmpireCandles_SkipsMissingDate(t *testing.T) {
	raw := `[
		{"Open":"100","High":"110","Low":"90","Close":"105","Volume":"50"},
		{"Date":"2024-01-15T10:00:00Z","Open":"100","High":"110","Low":"90","Close":"105","Volume":"50"}
	]`
	candles, err := normalizeFXEmpireCandles([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candles) != 1 {
		t.Errorf("expected 1 candle (missing date skipped), got %d", len(candles))
	}
}

func TestNormalizeFXEmpireCandles_EmptyArray(t *testing.T) {
	candles, err := normalizeFXEmpireCandles([]byte(`[]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candles) != 0 {
		t.Errorf("expected 0 candles, got %d", len(candles))
	}
}

func TestNormalizeFXEmpireCandles_InvalidJSON(t *testing.T) {
	_, err := normalizeFXEmpireCandles([]byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// normalizeOandaCandles
// ---------------------------------------------------------------------------

func TestNormalizeOandaCandles_Basic(t *testing.T) {
	raw := `{
		"candles": [
			{"time":"2024-01-15T10:00:00.000000000Z","mid":{"o":"17000.5","h":"17050.25","l":"16990.0","c":"17020.75"},"volume":1234,"complete":true},
			{"time":"2024-01-15T10:01:00.000000000Z","mid":{"o":"17020.75","h":"17060.0","l":"17010.0","c":"17045.0"},"volume":987,"complete":false}
		]
	}`
	candles, err := normalizeOandaCandles([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candles) != 2 {
		t.Fatalf("expected 2 candles, got %d", len(candles))
	}

	c0 := candles[0]
	if c0.Time != "2024-01-15T10:00:00.000000000Z" {
		t.Errorf("Time = %q", c0.Time)
	}
	if c0.Open != 17000.5 {
		t.Errorf("Open = %v, want 17000.5", c0.Open)
	}
	if !c0.Complete {
		t.Errorf("candles[0].Complete should be true")
	}

	c1 := candles[1]
	if c1.Complete {
		t.Errorf("candles[1].Complete should be false")
	}
}

func TestNormalizeOandaCandles_SkipsMissingTime(t *testing.T) {
	raw := `{
		"candles": [
			{"mid":{"o":"100","h":"110","l":"90","c":"105"},"volume":50,"complete":true},
			{"time":"2024-01-15T10:00:00Z","mid":{"o":"100","h":"110","l":"90","c":"105"},"volume":50,"complete":true}
		]
	}`
	candles, err := normalizeOandaCandles([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candles) != 1 {
		t.Errorf("expected 1 candle (missing time skipped), got %d", len(candles))
	}
}

func TestNormalizeOandaCandles_EmptyCandles(t *testing.T) {
	candles, err := normalizeOandaCandles([]byte(`{"candles":[]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candles) != 0 {
		t.Errorf("expected 0 candles, got %d", len(candles))
	}
}

func TestNormalizeOandaCandles_InvalidJSON(t *testing.T) {
	_, err := normalizeOandaCandles([]byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// CandlesResult JSON shape
// ---------------------------------------------------------------------------

func TestCandlesResult_JSONFields(t *testing.T) {
	result := CandlesResult{
		OK:          true,
		Mode:        "candles",
		Provider:    "oanda",
		Market:      "indices",
		Instrument:  "NAS100/USD",
		Granularity: "M1",
		RequestURL:  "https://example.com",
		Count:       2,
		Candles: []Candle{
			{Time: "2024-01-15T10:00:00Z", Open: 100, High: 110, Low: 90, Close: 105, Volume: 50, Complete: true},
		},
	}

	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	for _, key := range []string{"ok", "mode", "provider", "market", "instrument", "granularity", "requestUrl", "count", "candles"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q to be present", key)
		}
	}
}

func TestCandle_JSONFields(t *testing.T) {
	c := Candle{Time: "2024-01-15T10:00:00Z", Open: 100, High: 110, Low: 90, Close: 105, Volume: 50, Complete: true}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	for _, key := range []string{"time", "open", "high", "low", "close", "volume", "complete"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q to be present", key)
		}
	}
}

// ---------------------------------------------------------------------------
// candleFinite helper
// ---------------------------------------------------------------------------

func TestCandleFinite(t *testing.T) {
	if !candleFinite(1.0) {
		t.Error("1.0 should be finite")
	}
	if !candleFinite(-1.0) {
		t.Error("-1.0 should be finite")
	}
	if candleFinite(math.Inf(1)) {
		t.Error("+Inf should not be finite")
	}
	if candleFinite(math.Inf(-1)) {
		t.Error("-Inf should not be finite")
	}
	if candleFinite(math.NaN()) {
		t.Error("NaN should not be finite")
	}
}
