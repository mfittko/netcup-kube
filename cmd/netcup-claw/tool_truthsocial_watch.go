package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	truthSocialTagRegex          = regexp.MustCompile(`<[^>]*>`)
	truthSocialStatusRegex       = regexp.MustCompile(`<div class="status"[^>]*data-status-url="([^"]+)"[\s\S]*?<div class="status__content">([\s\S]*?)</div>[\s\S]*?<div class="status__footer"></div>`)
	truthSocialExternalURLRegex  = regexp.MustCompile(`href="(https://truthsocial\.com/@[^"\s]+/\d+)"`)
	truthSocialTimestampRegex    = regexp.MustCompile(`href="https://www\.trumpstruth\.org/statuses/\d+"\s+class="status-info__meta-item">([\s\S]*?)</a>`)
	truthSocialTitleRegex        = regexp.MustCompile(`(?is)<title[^>]*>([\s\S]*?)</title>`)
	truthSocialNumericIDRegex    = regexp.MustCompile(`/(\d+)(?:\D*)$`)
	truthSocialNowFunc           = time.Now
	truthSocialHTTPClientFactory = func(timeout time.Duration) *http.Client {
		return &http.Client{Timeout: timeout}
	}
)

type truthSocialUserAgentSettings struct {
	UserAgent *string
	Mode      string
}

type truthSocialPost struct {
	ID                string  `json:"id"`
	URL               string  `json:"url"`
	Text              string  `json:"text"`
	TimestampText     *string `json:"timestampText"`
	TimestampISO      *string `json:"timestampISO"`
	TimestampTimeZone *string `json:"timestampTimeZone,omitempty"`
}

type truthSocialState struct {
	SeenPostIDs   []string `json:"seenPostIds"`
	LatestPostID  *string  `json:"latestPostId"`
	LatestPostURL *string  `json:"latestPostUrl"`
	LastPollAt    *string  `json:"lastPollAt"`
	LastSeenAt    *string  `json:"lastSeenAt"`
	LastTitle     *string  `json:"lastTitle"`
}

type truthSocialSnapshot struct {
	ProfileURL        string            `json:"profileUrl"`
	ProfileUser       string            `json:"profileUser"`
	PolledAt          string            `json:"polledAt"`
	LatestPostID      *string           `json:"latestPostId"`
	LatestPostURL     *string           `json:"latestPostUrl"`
	PageTitle         string            `json:"pageTitle"`
	BodyPreview       string            `json:"bodyPreview"`
	ChallengeDetected bool              `json:"challengeDetected"`
	Count             int               `json:"count"`
	Posts             []truthSocialPost `json:"posts"`
	NewSinceLastSeen  []truthSocialPost `json:"newSinceLastSeen"`
}

type truthSocialScrapeResult struct {
	Title       string
	Href        string
	BodyPreview string
	PolledAt    string
	Posts       []truthSocialPost
}

var truthSocialWatchCmd = &cobra.Command{
	Use:   "truthsocial-watch",
	Short: "Fetch and monitor recent Truth Social posts",
	Long: `Fetch the Truth Social archive profile page over HTTP, detect newly
published posts using a persisted state file, and emit either HEARTBEAT_OK or
an alert message for OpenClaw skills.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		return runTruthSocialWatch(os.Stdout)
	},
}

func envOrDefault(name, fallback string) string {
	value := os.Getenv(name)
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func intEnvOrDefault(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(raw, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func userAgentPoolFromEnv(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, "||")
	pool := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			pool = append(pool, part)
		}
	}
	return pool
}

func buildDefaultUserAgentPool() []string {
	osTokens := []string{
		"Windows NT 10.0; Win64; x64",
		"Windows NT 10.0; WOW64",
		"Macintosh; Intel Mac OS X 10_15_7",
		"Macintosh; Intel Mac OS X 14_3_1",
		"X11; Linux x86_64",
	}
	chromeMajors := []int{126, 127, 128, 129, 130, 131, 132, 133, 134, 135}

	pool := make([]string, 0, len(osTokens)*len(chromeMajors))
	for _, osToken := range osTokens {
		for _, major := range chromeMajors {
			pool = append(pool, fmt.Sprintf(
				"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.0.0 Safari/537.36",
				osToken, major,
			))
		}
	}
	return pool
}

func resolveUserAgentSettings() truthSocialUserAgentSettings {
	mode := strings.ToLower(strings.TrimSpace(envOrDefault("TRUTHSOCIAL_USER_AGENT_MODE", "random")))
	fixedUserAgent := strings.TrimSpace(os.Getenv("TRUTHSOCIAL_USER_AGENT"))
	pool := userAgentPoolFromEnv(envOrDefault("TRUTHSOCIAL_USER_AGENT_POOL", ""))
	if len(pool) == 0 {
		pool = buildDefaultUserAgentPool()
	}

	if fixedUserAgent != "" {
		return truthSocialUserAgentSettings{
			UserAgent: stringPtr(fixedUserAgent),
			Mode:      "fixed",
		}
	}

	if mode == "off" || mode == "none" || mode == "disabled" {
		return truthSocialUserAgentSettings{Mode: "off"}
	}

	if len(pool) == 0 {
		return truthSocialUserAgentSettings{Mode: "random"}
	}

	selected := pool[rand.IntN(len(pool))]
	return truthSocialUserAgentSettings{
		UserAgent: stringPtr(selected),
		Mode:      "random",
	}
}

func nowUTCMinute(now time.Time) string {
	iso := now.UTC().Format(time.RFC3339)
	return fmt.Sprintf("%s %s UTC", iso[:10], iso[11:16])
}

func decodeHTMLEntities(text string) string {
	return strings.ReplaceAll(html.UnescapeString(text), "\u00a0", " ")
}

func stripHTMLTags(text string) string {
	withoutTags := truthSocialTagRegex.ReplaceAllString(text, " ")
	return strings.Join(strings.Fields(decodeHTMLEntities(withoutTags)), " ")
}

func extractNumericID(raw string) string {
	match := truthSocialNumericIDRegex.FindStringSubmatch(strings.TrimSpace(raw))
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func parseTimestampBestEffort(timestampText, sourceTimeZone string) *string {
	raw := strings.TrimSpace(timestampText)
	if raw == "" {
		return nil
	}

	loc, err := time.LoadLocation(sourceTimeZone)
	if err != nil {
		return nil
	}

	parsed, err := time.ParseInLocation("January 2, 2006, 3:04 PM", raw, loc)
	if err != nil {
		return nil
	}

	iso := parsed.UTC().Format(time.RFC3339)
	return &iso
}

func parseTruthSocialArchiveHTML(htmlBody, finalURL, username string, maxPosts int, sourceTimeZone string, now time.Time) truthSocialScrapeResult {
	matches := truthSocialStatusRegex.FindAllStringSubmatch(htmlBody, -1)
	posts := make([]truthSocialPost, 0, len(matches))
	seen := map[string]bool{}

	for _, match := range matches {
		if len(match) < 3 {
			continue
		}

		block := match[0]
		statusURL := strings.TrimSpace(decodeHTMLEntities(match[1]))
		externalURLMatch := truthSocialExternalURLRegex.FindStringSubmatch(block)
		externalURL := ""
		if len(externalURLMatch) > 1 {
			externalURL = strings.TrimSpace(decodeHTMLEntities(externalURLMatch[1]))
		}
		timestampMatch := truthSocialTimestampRegex.FindStringSubmatch(block)
		timestampText := ""
		if len(timestampMatch) > 1 {
			timestampText = stripHTMLTags(timestampMatch[1])
		}
		timestampISO := parseTimestampBestEffort(timestampText, sourceTimeZone)
		resolvedURL := externalURL
		if resolvedURL == "" {
			resolvedURL = statusURL
		}
		id := extractNumericID(externalURL)
		if id == "" {
			id = extractNumericID(statusURL)
		}
		if id == "" || resolvedURL == "" || seen[id] {
			continue
		}

		text := truncateRunes(stripHTMLTags(match[2]), 500)
		if text == "" {
			continue
		}

		post := truthSocialPost{
			ID:                id,
			URL:               resolvedURL,
			Text:              text,
			TimestampISO:      timestampISO,
			TimestampTimeZone: stringPtr(sourceTimeZone),
		}
		if timestampText != "" {
			post.TimestampText = stringPtr(timestampText)
		}

		seen[id] = true
		posts = append(posts, post)
		if maxPosts >= 0 && len(posts) >= maxPosts {
			break
		}
	}

	title := ""
	titleMatch := truthSocialTitleRegex.FindStringSubmatch(htmlBody)
	if len(titleMatch) > 1 {
		title = stripHTMLTags(titleMatch[1])
	}

	return truthSocialScrapeResult{
		Title:       title,
		Href:        finalURL,
		BodyPreview: truncateRunes(stripHTMLTags(htmlBody), 240),
		PolledAt:    now.UTC().Format(time.RFC3339),
		Posts:       posts,
	}
}

func fetchRecentPostsNode(profileURL, username string, maxPosts int, userAgent *string, acceptLanguage string, navTimeoutMs int, sourceTimeZone string) (truthSocialScrapeResult, error) {
	timeout := time.Duration(max(1000, navTimeoutMs)) * time.Millisecond
	client := truthSocialHTTPClientFactory(timeout)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, nil)
	if err != nil {
		return truthSocialScrapeResult{}, fmt.Errorf("creating request for %s: %w", profileURL, err)
	}

	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", acceptLanguage)
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	if userAgent != nil && strings.TrimSpace(*userAgent) != "" {
		req.Header.Set("User-Agent", *userAgent)
	}

	resp, err := client.Do(req)
	if err != nil {
		return truthSocialScrapeResult{}, fmt.Errorf("GET %s: %w", profileURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return truthSocialScrapeResult{}, fmt.Errorf("reading response from %s: %w", profileURL, err)
	}

	finalURL := profileURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return parseTruthSocialArchiveHTML(string(body), finalURL, username, maxPosts, sourceTimeZone, truthSocialNowFunc()), nil
}

func isLikelyChallengePage(title, bodyPreview string) bool {
	text := strings.ToLower(strings.TrimSpace(title + " " + bodyPreview))
	markers := []string{
		"just a moment",
		"checking your browser",
		"verify you are human",
		"attention required",
		"cloudflare",
		"cf-challenge",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func formatTruthSocialAlert(posts []truthSocialPost, username, profileURL string) string {
	lines := []string{
		fmt.Sprintf("🟠 New Truth Social post(s): @%s", strings.TrimPrefix(username, "@")),
		fmt.Sprintf("Profile: %s", profileURL),
		fmt.Sprintf("Detected: %s", nowUTCMinute(truthSocialNowFunc())),
		"",
	}

	for _, post := range posts {
		lines = append(lines, fmt.Sprintf("• %s", post.URL))
		if post.Text != "" {
			lines = append(lines, fmt.Sprintf("  %s", post.Text))
		}
	}

	return strings.Join(lines, "\n")
}

func readTruthSocialState(filePath string) truthSocialState {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return truthSocialState{SeenPostIDs: []string{}}
	}

	var parsed truthSocialState
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return truthSocialState{SeenPostIDs: []string{}}
	}
	if parsed.SeenPostIDs == nil {
		parsed.SeenPostIDs = []string{}
	}
	return parsed
}

func writeTruthSocialState(filePath string, state truthSocialState) error {
	return writeJSONAtomic(filePath, state)
}

func writeTruthSocialSnapshot(filePath string, snapshot truthSocialSnapshot) error {
	return writeJSONAtomic(filePath, snapshot)
}

func writeJSONAtomic(filePath string, value any) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return fmt.Errorf("creating directory for %s: %w", filePath, err)
	}

	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding JSON for %s: %w", filePath, err)
	}

	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, body, 0o644); err != nil {
		return fmt.Errorf("writing temp file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmpPath, filePath, err)
	}
	return nil
}

func newPostsSinceLatest(previousLatestID *string, posts []truthSocialPost) []truthSocialPost {
	if previousLatestID == nil || strings.TrimSpace(*previousLatestID) == "" || len(posts) == 0 {
		return nil
	}

	currentLatestID := posts[0].ID
	if currentLatestID == "" || currentLatestID == *previousLatestID {
		return nil
	}

	newest := make([]truthSocialPost, 0, len(posts))
	foundPrevious := false
	for _, post := range posts {
		if post.ID == *previousLatestID {
			foundPrevious = true
			break
		}
		newest = append(newest, post)
	}
	if !foundPrevious {
		return []truthSocialPost{posts[0]}
	}
	return newest
}

func runTruthSocialWatch(stdout io.Writer) error {
	sourceMode := strings.ToLower(strings.TrimSpace(envOrDefault("TRUTHSOCIAL_SOURCE_MODE", "node")))
	if sourceMode != "node" {
		return fmt.Errorf("TRUTHSOCIAL_WATCH_ERROR: unsupported TRUTHSOCIAL_SOURCE_MODE %q: only node mode is supported", sourceMode)
	}

	profileURL := envOrDefault("TRUTHSOCIAL_PROFILE_URL", "https://www.trumpstruth.org/")
	username := envOrDefault("TRUTHSOCIAL_USERNAME", "realDonaldTrump")
	maxPosts := intEnvOrDefault("MAX_POSTS", 8)
	navTimeoutMs := intEnvOrDefault("NAV_TIMEOUT_MS", 30000)
	acceptLanguage := envOrDefault("TRUTHSOCIAL_ACCEPT_LANGUAGE", "en-US,en;q=0.9")
	sourceTimeZone := envOrDefault("TRUTHSOCIAL_SOURCE_TIMEZONE", "America/New_York")
	stateFile := envOrDefault("STATE_FILE", "./state/truthsocial-trump-watch/state.json")
	postsFile := envOrDefault("TRUTHSOCIAL_POSTS_FILE", "./state/truthsocial-trump-watch/latest-posts.json")
	uaSettings := resolveUserAgentSettings()

	state := readTruthSocialState(stateFile)
	scraped, err := fetchRecentPostsNode(profileURL, username, maxPosts, uaSettings.UserAgent, acceptLanguage, navTimeoutMs, sourceTimeZone)
	if err != nil {
		return fmt.Errorf("TRUTHSOCIAL_WATCH_ERROR: %w", err)
	}

	var currentLatestPost *truthSocialPost
	if len(scraped.Posts) > 0 {
		currentLatestPost = &scraped.Posts[0]
	}
	challengeDetected := len(scraped.Posts) == 0 && isLikelyChallengePage(scraped.Title, scraped.BodyPreview)
	newest := newPostsSinceLatest(state.LatestPostID, scraped.Posts)

	nextState := truthSocialState{
		SeenPostIDs:   make([]string, 0, len(scraped.Posts)),
		LatestPostID:  nil,
		LatestPostURL: nil,
		LastPollAt:    stringPtr(scraped.PolledAt),
		LastSeenAt:    state.LastSeenAt,
		LastTitle:     stringPtr(scraped.Title),
	}
	for _, post := range scraped.Posts {
		nextState.SeenPostIDs = append(nextState.SeenPostIDs, post.ID)
	}
	if currentLatestPost != nil {
		nextState.LatestPostID = stringPtr(currentLatestPost.ID)
		nextState.LatestPostURL = stringPtr(currentLatestPost.URL)
	}
	if len(newest) > 0 {
		now := truthSocialNowFunc().UTC().Format(time.RFC3339)
		nextState.LastSeenAt = &now
	}
	if err := writeTruthSocialState(stateFile, nextState); err != nil {
		return fmt.Errorf("TRUTHSOCIAL_WATCH_ERROR: %w", err)
	}

	snapshot := truthSocialSnapshot{
		ProfileURL:        firstNonEmpty(scraped.Href, profileURL),
		ProfileUser:       username,
		PolledAt:          scraped.PolledAt,
		LatestPostID:      nextState.LatestPostID,
		LatestPostURL:     nextState.LatestPostURL,
		PageTitle:         scraped.Title,
		BodyPreview:       scraped.BodyPreview,
		ChallengeDetected: challengeDetected,
		Count:             len(scraped.Posts),
		Posts:             scraped.Posts,
		NewSinceLastSeen:  newest,
	}
	if err := writeTruthSocialSnapshot(postsFile, snapshot); err != nil {
		return fmt.Errorf("TRUTHSOCIAL_WATCH_ERROR: %w", err)
	}

	if challengeDetected {
		return fmt.Errorf("TRUTHSOCIAL_WATCH_ERROR: TRUTHSOCIAL_CHALLENGE_PAGE: blocked by anti-bot challenge page (title/body indicates Cloudflare interstitial)")
	}

	if len(newest) == 0 {
		_, err = io.WriteString(stdout, "HEARTBEAT_OK\n")
		return err
	}

	_, err = io.WriteString(stdout, formatTruthSocialAlert(newest, username, firstNonEmpty(scraped.Href, profileURL))+"\n")
	return err
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

func stringPtr(v string) *string {
	return &v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func init() {
	toolCmd.AddCommand(truthSocialWatchCmd)
}
