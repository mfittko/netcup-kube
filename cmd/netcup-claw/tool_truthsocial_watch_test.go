package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUserAgentPoolFromEnv(t *testing.T) {
	got := userAgentPoolFromEnv(" UA-1 || UA-2 ||  || UA-3 ")
	want := []string{"UA-1", "UA-2", "UA-3"}
	if len(got) != len(want) {
		t.Fatalf("userAgentPoolFromEnv() len=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("userAgentPoolFromEnv()[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildDefaultUserAgentPool(t *testing.T) {
	got := buildDefaultUserAgentPool()
	if len(got) != 50 {
		t.Fatalf("buildDefaultUserAgentPool() len=%d, want 50", len(got))
	}
	if !containsString(got, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36") {
		t.Fatal("expected Chrome 126 Windows UA in default pool")
	}
	if !containsString(got, "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36") {
		t.Fatal("expected Chrome 135 Linux UA in default pool")
	}
}

func TestResolveUserAgentSettings_FixedWins(t *testing.T) {
	t.Setenv("TRUTHSOCIAL_USER_AGENT_MODE", "off")
	t.Setenv("TRUTHSOCIAL_USER_AGENT", "Fixed UA")

	got := resolveUserAgentSettings()
	if got.Mode != "fixed" {
		t.Fatalf("Mode=%q, want fixed", got.Mode)
	}
	if got.UserAgent == nil || *got.UserAgent != "Fixed UA" {
		t.Fatalf("UserAgent=%v, want Fixed UA", got.UserAgent)
	}
}

func TestResolveUserAgentSettings_Off(t *testing.T) {
	t.Setenv("TRUTHSOCIAL_USER_AGENT_MODE", "disabled")
	t.Setenv("TRUTHSOCIAL_USER_AGENT", "")

	got := resolveUserAgentSettings()
	if got.Mode != "off" {
		t.Fatalf("Mode=%q, want off", got.Mode)
	}
	if got.UserAgent != nil {
		t.Fatalf("UserAgent=%v, want nil", got.UserAgent)
	}
}

func TestResolveUserAgentSettings_RandomFromPool(t *testing.T) {
	t.Setenv("TRUTHSOCIAL_USER_AGENT_MODE", "random")
	t.Setenv("TRUTHSOCIAL_USER_AGENT", "")
	t.Setenv("TRUTHSOCIAL_USER_AGENT_POOL", "Only UA")

	got := resolveUserAgentSettings()
	if got.Mode != "random" {
		t.Fatalf("Mode=%q, want random", got.Mode)
	}
	if got.UserAgent == nil || *got.UserAgent != "Only UA" {
		t.Fatalf("UserAgent=%v, want Only UA", got.UserAgent)
	}
}

func TestParseTimestampBestEffort(t *testing.T) {
	got := parseTimestampBestEffort("March 7, 2026, 2:30 PM", "America/New_York")
	if got == nil || *got != "2026-03-07T19:30:00Z" {
		t.Fatalf("parseTimestampBestEffort()=%v, want 2026-03-07T19:30:00Z", got)
	}

	got = parseTimestampBestEffort("June 7, 2026, 2:30 PM", "America/New_York")
	if got == nil || *got != "2026-06-07T18:30:00Z" {
		t.Fatalf("parseTimestampBestEffort() summer=%v, want 2026-06-07T18:30:00Z", got)
	}
}

func TestParseTruthSocialArchiveHTML(t *testing.T) {
	html := sampleTruthSocialArchiveHTML([]truthSocialHTMLPost{
		{
			ID:        "123456789012345678",
			Text:      "<p>Hello &amp; welcome</p>",
			Timestamp: "March 7, 2026, 2:30 PM",
		},
		{
			ID:        "123456789012345679",
			Text:      "<p>Second post</p>",
			Timestamp: "March 7, 2026, 1:15 PM",
		},
		{
			ID:        "123456789012345679",
			Text:      "<p>Duplicate should be ignored</p>",
			Timestamp: "March 7, 2026, 1:15 PM",
		},
	})

	now := time.Date(2026, 3, 7, 20, 0, 0, 0, time.UTC)
	got := parseTruthSocialArchiveHTML(html, "https://www.trumpstruth.org/", "realDonaldTrump", 8, "America/New_York", now)

	if got.Title != "Donald J. Trump - Truth Social Archive" {
		t.Fatalf("Title=%q", got.Title)
	}
	if got.PolledAt != "2026-03-07T20:00:00Z" {
		t.Fatalf("PolledAt=%q", got.PolledAt)
	}
	if len(got.Posts) != 2 {
		t.Fatalf("len(Posts)=%d, want 2", len(got.Posts))
	}
	if got.Posts[0].ID != "123456789012345678" {
		t.Fatalf("first post ID=%q", got.Posts[0].ID)
	}
	if got.Posts[0].URL != "https://truthsocial.com/@realDonaldTrump/123456789012345678" {
		t.Fatalf("first post URL=%q", got.Posts[0].URL)
	}
	if got.Posts[0].Text != "Hello & welcome" {
		t.Fatalf("first post Text=%q", got.Posts[0].Text)
	}
	if got.Posts[0].TimestampText == nil || *got.Posts[0].TimestampText != "March 7, 2026, 2:30 PM" {
		t.Fatalf("TimestampText=%v", got.Posts[0].TimestampText)
	}
	if got.Posts[0].TimestampISO == nil || *got.Posts[0].TimestampISO != "2026-03-07T19:30:00Z" {
		t.Fatalf("TimestampISO=%v", got.Posts[0].TimestampISO)
	}
}

func TestNewPostsSinceLatest(t *testing.T) {
	previous := "2"
	posts := []truthSocialPost{
		{ID: "4", URL: "u4"},
		{ID: "3", URL: "u3"},
		{ID: "2", URL: "u2"},
	}
	got := newPostsSinceLatest(&previous, posts)
	if len(got) != 2 || got[0].ID != "4" || got[1].ID != "3" {
		t.Fatalf("newPostsSinceLatest()=%v, want IDs 4,3", got)
	}

	previous = "9"
	got = newPostsSinceLatest(&previous, posts)
	if len(got) != 1 || got[0].ID != "4" {
		t.Fatalf("fallback newPostsSinceLatest()=%v, want only latest post", got)
	}
}

func TestIsLikelyChallengePage(t *testing.T) {
	if !isLikelyChallengePage("Just a moment...", "Checking your browser before accessing the site") {
		t.Fatal("expected Cloudflare challenge markers to be detected")
	}
	if isLikelyChallengePage("Normal page", "Latest posts archive") {
		t.Fatal("did not expect normal page to be detected as challenge")
	}
}

func TestReadWriteTruthSocialState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := truthSocialState{
		SeenPostIDs:   []string{"1", "2"},
		LatestPostID:  stringPtr("2"),
		LatestPostURL: stringPtr("https://example.com/2"),
		LastPollAt:    stringPtr("2026-03-07T20:00:00Z"),
		LastSeenAt:    stringPtr("2026-03-07T20:01:00Z"),
		LastTitle:     stringPtr("Archive"),
	}

	if err := writeTruthSocialState(path, state); err != nil {
		t.Fatalf("writeTruthSocialState() error: %v", err)
	}

	got := readTruthSocialState(path)
	if len(got.SeenPostIDs) != 2 || got.SeenPostIDs[0] != "1" || got.SeenPostIDs[1] != "2" {
		t.Fatalf("SeenPostIDs=%v", got.SeenPostIDs)
	}
	if got.LatestPostID == nil || *got.LatestPostID != "2" {
		t.Fatalf("LatestPostID=%v", got.LatestPostID)
	}
	if got.LastTitle == nil || *got.LastTitle != "Archive" {
		t.Fatalf("LastTitle=%v", got.LastTitle)
	}
}

func TestRunTruthSocialWatch_HeartbeatThenAlertAndStateFiles(t *testing.T) {
	oldNow := truthSocialNowFunc
	truthSocialNowFunc = func() time.Time {
		return time.Date(2026, 3, 7, 20, 45, 0, 0, time.UTC)
	}
	defer func() { truthSocialNowFunc = oldNow }()

	var requestedUserAgent string
	htmlBody := sampleTruthSocialArchiveHTML([]truthSocialHTMLPost{
		{
			ID:        "123456789012345678",
			Text:      "<p>First archived post</p>",
			Timestamp: "March 7, 2026, 2:30 PM",
		},
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(htmlBody))
	}))
	defer server.Close()

	stateFile := filepath.Join(t.TempDir(), "custom-state.json")
	postsFile := filepath.Join(t.TempDir(), "custom-posts.json")

	t.Setenv("TRUTHSOCIAL_PROFILE_URL", server.URL)
	t.Setenv("TRUTHSOCIAL_SOURCE_MODE", "node")
	t.Setenv("TRUTHSOCIAL_USERNAME", "realDonaldTrump")
	t.Setenv("STATE_FILE", stateFile)
	t.Setenv("TRUTHSOCIAL_POSTS_FILE", postsFile)
	t.Setenv("TRUTHSOCIAL_USER_AGENT_MODE", "random")
	t.Setenv("TRUTHSOCIAL_USER_AGENT_POOL", "Only UA")
	t.Setenv("NAV_TIMEOUT_MS", "5000")
	t.Setenv("MAX_POSTS", "8")

	var stdout bytes.Buffer
	if err := runTruthSocialWatch(&stdout); err != nil {
		t.Fatalf("first run error: %v", err)
	}
	if stdout.String() != "HEARTBEAT_OK\n" {
		t.Fatalf("first run stdout=%q, want HEARTBEAT_OK", stdout.String())
	}
	if requestedUserAgent != "Only UA" {
		t.Fatalf("request User-Agent=%q, want Only UA", requestedUserAgent)
	}

	state := readTruthSocialState(stateFile)
	if state.LatestPostID == nil || *state.LatestPostID != "123456789012345678" {
		t.Fatalf("state LatestPostID=%v", state.LatestPostID)
	}

	htmlBody = sampleTruthSocialArchiveHTML([]truthSocialHTMLPost{
		{
			ID:        "123456789012345679",
			Text:      "<p>Newest archived post</p>",
			Timestamp: "March 7, 2026, 3:00 PM",
		},
		{
			ID:        "123456789012345678",
			Text:      "<p>First archived post</p>",
			Timestamp: "March 7, 2026, 2:30 PM",
		},
	})

	stdout.Reset()
	if err := runTruthSocialWatch(&stdout); err != nil {
		t.Fatalf("second run error: %v", err)
	}
	if !strings.Contains(stdout.String(), "🟠 New Truth Social post(s): @realDonaldTrump") {
		t.Fatalf("second run stdout missing alert heading:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "https://truthsocial.com/@realDonaldTrump/123456789012345679") {
		t.Fatalf("second run stdout missing new post URL:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Newest archived post") {
		t.Fatalf("second run stdout missing post text:\n%s", stdout.String())
	}

	state = readTruthSocialState(stateFile)
	if state.LatestPostID == nil || *state.LatestPostID != "123456789012345679" {
		t.Fatalf("updated LatestPostID=%v", state.LatestPostID)
	}
	if state.LastSeenAt == nil {
		t.Fatal("expected LastSeenAt to be set after detecting new post")
	}

	var snapshot truthSocialSnapshot
	rawSnapshot, err := os.ReadFile(postsFile)
	if err != nil {
		t.Fatalf("reading snapshot: %v", err)
	}
	if err := json.Unmarshal(rawSnapshot, &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if snapshot.Count != 2 {
		t.Fatalf("snapshot.Count=%d, want 2", snapshot.Count)
	}
	if len(snapshot.NewSinceLastSeen) != 1 || snapshot.NewSinceLastSeen[0].ID != "123456789012345679" {
		t.Fatalf("snapshot.NewSinceLastSeen=%v", snapshot.NewSinceLastSeen)
	}
}

func TestRunTruthSocialWatch_ChallengePage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>Just a moment...</title></head><body>Checking your browser before accessing. Cloudflare</body></html>`))
	}))
	defer server.Close()

	stateFile := filepath.Join(t.TempDir(), "state.json")
	postsFile := filepath.Join(t.TempDir(), "posts.json")
	t.Setenv("TRUTHSOCIAL_PROFILE_URL", server.URL)
	t.Setenv("STATE_FILE", stateFile)
	t.Setenv("TRUTHSOCIAL_POSTS_FILE", postsFile)

	err := runTruthSocialWatch(&bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "TRUTHSOCIAL_CHALLENGE_PAGE") {
		t.Fatalf("expected challenge error, got %v", err)
	}

	rawSnapshot, readErr := os.ReadFile(postsFile)
	if readErr != nil {
		t.Fatalf("reading snapshot: %v", readErr)
	}
	var snapshot truthSocialSnapshot
	if err := json.Unmarshal(rawSnapshot, &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if !snapshot.ChallengeDetected {
		t.Fatal("expected snapshot.ChallengeDetected=true")
	}
	if snapshot.Count != 0 {
		t.Fatalf("snapshot.Count=%d, want 0", snapshot.Count)
	}
}

type truthSocialHTMLPost struct {
	ID        string
	Text      string
	Timestamp string
}

func sampleTruthSocialArchiveHTML(posts []truthSocialHTMLPost) string {
	var b strings.Builder
	b.WriteString(`<html><head><title>Donald J. Trump - Truth Social Archive</title></head><body>`)
	for _, post := range posts {
		b.WriteString(`<div class="status" data-status-url="https://www.trumpstruth.org/statuses/`)
		b.WriteString(post.ID)
		b.WriteString(`">`)
		b.WriteString(`<a href="https://truthsocial.com/@realDonaldTrump/`)
		b.WriteString(post.ID)
		b.WriteString(`">View</a>`)
		b.WriteString(`<div class="status__content">`)
		b.WriteString(post.Text)
		b.WriteString(`</div>`)
		b.WriteString(`<a href="https://www.trumpstruth.org/statuses/`)
		b.WriteString(post.ID)
		b.WriteString(`" class="status-info__meta-item">`)
		b.WriteString(post.Timestamp)
		b.WriteString(`</a>`)
		b.WriteString(`<div class="status__footer"></div></div>`)
	}
	b.WriteString(`</body></html>`)
	return b.String()
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
