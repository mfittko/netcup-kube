package toolutil_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mfittko/netcup-kube/internal/toolutil"
)

// ---------------------------------------------------------------------------
// HTTPGetJSON
// ---------------------------------------------------------------------------

func TestHTTPGetJSON_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	body, err := toolutil.HTTPGetJSON(srv.URL, 5000, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestHTTPGetJSON_UserAgentHeader(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	_, err := toolutil.HTTPGetJSON(srv.URL, 5000, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Mozilla/5.0 (OpenClaw; fxempire-rates)"
	if gotUA != want {
		t.Fatalf("User-Agent = %q, want %q", gotUA, want)
	}
}

func TestHTTPGetJSON_CustomHeaders(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	headers := map[string]string{"Accept": "application/json"}
	_, err := toolutil.HTTPGetJSON(srv.URL, 5000, headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAccept != "application/json" {
		t.Fatalf("Accept header = %q, want %q", gotAccept, "application/json")
	}
}

func TestHTTPGetJSON_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := toolutil.HTTPGetJSON(srv.URL, 5000, nil)
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestHTTPGetJSON_BadURL(t *testing.T) {
	_, err := toolutil.HTTPGetJSON("://bad-url", 5000, nil)
	if err == nil {
		t.Fatal("expected error for bad URL, got nil")
	}
}

func TestHTTPGetJSON_Timeout(t *testing.T) {
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the test ends so the server can close cleanly.
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	_, err := toolutil.HTTPGetJSON(srv.URL, 1 /* 1ms timeout */, nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// ---------------------------------------------------------------------------
// FmtNum
// ---------------------------------------------------------------------------

func TestFmtNum(t *testing.T) {
	tests := []struct {
		v        float64
		decimals int
		want     string
	}{
		{75.5, 2, "75.50"},
		{-0.42, 2, "-0.42"},
		{0, 2, "0.00"},
		{1234.5678, 2, "1234.57"},
		{1.0, 0, "1"},
		{-1.006, 2, "-1.01"},
	}
	for _, tt := range tests {
		got := toolutil.FmtNum(tt.v, tt.decimals)
		if got != tt.want {
			t.Errorf("FmtNum(%v, %d) = %q, want %q", tt.v, tt.decimals, got, tt.want)
		}
	}
}

func TestFmtNum_NegativeDecimals(t *testing.T) {
	// Negative decimals should be treated as 0.
	got := toolutil.FmtNum(3.7, -1)
	if got != "4" {
		t.Errorf("FmtNum(3.7, -1) = %q, want %q", got, "4")
	}
}

// ---------------------------------------------------------------------------
// FmtNumUS
// ---------------------------------------------------------------------------

func TestFmtNumUS(t *testing.T) {
	tests := []struct {
		v    float64
		want string
	}{
		{75.5, "75.5"},
		{42000, "42,000"},
		{1985.3, "1,985.3"},
		{1234567.89, "1,234,567.89"},
		{-0.42, "-0.42"},
		{0, "0"},
		{1.0, "1"},
		{1.123, "1.123"},
		{1.1234, "1.123"}, // rounds to 3 dp
		{-1234.5, "-1,234.5"},
		{1000000, "1,000,000"},
	}
	for _, tt := range tests {
		got := toolutil.FmtNumUS(tt.v)
		if got != tt.want {
			t.Errorf("FmtNumUS(%v) = %q, want %q", tt.v, got, tt.want)
		}
	}
}

func TestFmtPct(t *testing.T) {
	tests := []struct {
		v    float64
		want string
	}{
		{-0.42, "-0.42%"},
		{1.5, "1.50%"},
		{0, "0.00%"},
		{100, "100.00%"},
	}
	for _, tt := range tests {
		got := toolutil.FmtPct(tt.v)
		if got != tt.want {
			t.Errorf("FmtPct(%v) = %q, want %q", tt.v, got, tt.want)
		}
	}
}
