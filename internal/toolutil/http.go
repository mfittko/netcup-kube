// Package toolutil provides shared helpers for backend-agnostic data tools.
package toolutil

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

const defaultUserAgent = "Mozilla/5.0 (OpenClaw; fxempire-rates)"

// HTTPGetJSON performs an HTTP GET request and returns the raw response body.
// It applies a per-request timeout (timeoutMs milliseconds), sets a default
// User-Agent header, and merges any extra headers supplied by the caller.
func HTTPGetJSON(url string, timeoutMs int, headers map[string]string) ([]byte, error) {
	client := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", url, err)
	}

	req.Header.Set("User-Agent", defaultUserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", url, err)
	}
	return body, nil
}

// FmtNum formats a floating-point number with the given number of decimal places.
// Negative zero is normalised to zero.
func FmtNum(v float64, decimals int) string {
	if decimals < 0 {
		decimals = 0
	}
	// Normalise -0 → 0
	if v == 0 {
		v = 0
	}
	format := fmt.Sprintf("%%.%df", decimals)
	return fmt.Sprintf(format, math.Round(v*math.Pow10(decimals))/math.Pow10(decimals))
}

// FmtPct formats a floating-point percentage value with two decimal places
// and appends a "%" suffix.
func FmtPct(v float64) string {
	return FmtNum(v, 2) + "%"
}
