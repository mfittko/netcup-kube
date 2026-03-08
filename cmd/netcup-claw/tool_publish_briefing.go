package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	publishBriefingInputFile   string
	publishBriefingSeries      string
	publishBriefingRepo        string
	publishBriefingBranch      string
	publishBriefingSiteBaseURL string
	publishBriefingTokenEnv    string
	publishBriefingTimestamp   string
	publishBriefingDryRun      bool
)

var publishBriefingGitHubAPIBaseURL = "https://api.github.com"

const publishBriefingUserAgent = "Mozilla/5.0 (OpenClaw; briefing-publisher)"

type publishBriefingOptions struct {
	InputFile   string
	Series      string
	Repo        string
	Branch      string
	SiteBaseURL string
	TokenEnv    string
	Timestamp   string
	DryRun      bool
}

type publishBriefingPaths struct {
	ArchivePath       string `json:"archivePath"`
	LatestPath        string `json:"latestPath"`
	IndexPath         string `json:"indexPath"`
	IndexHTMLPath     string `json:"indexHtmlPath"`
	SeriesFeedPath    string `json:"seriesFeedPath"`
	RootFeedPath      string `json:"rootFeedPath"`
	RootIndexPath     string `json:"rootIndexPath"`
	RootIndexHTMLPath string `json:"rootIndexHtmlPath"`
	ArchiveRelative   string `json:"-"`
}

type publishBriefingDryRunResult struct {
	DryRun            bool   `json:"dryRun"`
	Repo              string `json:"repo"`
	Branch            string `json:"branch"`
	Series            string `json:"series"`
	Stamp             string `json:"stamp"`
	ArchivePath       string `json:"archivePath"`
	LatestPath        string `json:"latestPath"`
	IndexPath         string `json:"indexPath"`
	SeriesFeedPath    string `json:"seriesFeedPath"`
	RootFeedPath      string `json:"rootFeedPath"`
	RootIndexPath     string `json:"rootIndexPath"`
	RootIndexHTMLPath string `json:"rootIndexHtmlPath"`
	ArchiveURL        string `json:"archiveUrl"`
	LatestURL         string `json:"latestUrl"`
	SeriesFeedURL     string `json:"seriesFeedUrl"`
	RootFeedURL       string `json:"rootFeedUrl"`
}

type publishBriefingResult struct {
	OK                bool   `json:"ok"`
	Repo              string `json:"repo"`
	Branch            string `json:"branch"`
	Series            string `json:"series"`
	Stamp             string `json:"stamp"`
	ArchivePath       string `json:"archivePath"`
	LatestPath        string `json:"latestPath"`
	IndexPath         string `json:"indexPath"`
	SeriesFeedPath    string `json:"seriesFeedPath"`
	RootFeedPath      string `json:"rootFeedPath"`
	RootIndexPath     string `json:"rootIndexPath"`
	RootIndexHTMLPath string `json:"rootIndexHtmlPath"`
	CommitSHA         string `json:"commitSha"`
	ArchiveURL        string `json:"archiveUrl"`
	LatestURL         string `json:"latestUrl"`
	SeriesFeedURL     string `json:"seriesFeedUrl"`
	RootFeedURL       string `json:"rootFeedUrl"`
}

type publishBriefingIndexEntry struct {
	Label string
	Link  string
}

type publishBriefingRootEntry struct {
	Label     string
	ViewerURL string
	MDURL     string
}

type publishBriefingRSSEntry struct {
	Title           string
	Link            string
	GUID            string
	PubDate         string
	Description     string
	DescriptionHTML string
	ContentHTML     string
}

type publishBriefingSeriesIndexHTMLInput struct {
	Title          string
	Entries        []publishBriefingIndexEntry
	HomePath       string
	LatestViewPath string
	FeedPath       string
}

type publishBriefingRootHomeHTMLInput struct {
	Series          string
	LatestViewerURL string
	LatestMDURL     string
	SeriesFeedURL   string
	SeriesIndexURL  string
	Entries         []publishBriefingRootEntry
}

type publishBriefingRootSectionInput struct {
	Series          string
	LatestViewerURL string
	LatestMDURL     string
	SeriesIndexURL  string
	SeriesFeedURL   string
	Entries         []publishBriefingRootEntry
}

type publishBriefingRootSectionUpsert struct {
	Series  string
	Section string
}

type publishBriefingFeedInput struct {
	Title       string
	Description string
	SiteURL     string
	FeedPath    string
	Items       []publishBriefingRSSEntry
}

type publishBriefingGitHubClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type publishBriefingGitCommit struct {
	Tree struct {
		SHA string `json:"sha"`
	} `json:"tree"`
}

type publishBriefingGitRef struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

type publishBriefingBlob struct {
	SHA string `json:"sha"`
}

type publishBriefingTree struct {
	SHA string `json:"sha"`
}

type publishBriefingCommit struct {
	SHA string `json:"sha"`
}

type publishBriefingContents struct {
	Content string `json:"content"`
}

type publishBriefingGitFile struct {
	Path    string
	Content string
}

func sanitizePublishBriefingSeries(series string) (string, error) {
	normalized := strings.TrimSpace(series)
	normalized = strings.ReplaceAll(normalized, "\\", "/")
	normalized = strings.TrimLeft(normalized, "/")
	normalized = strings.TrimRight(normalized, "/")
	if normalized == "" {
		return "", fmt.Errorf("series cannot be empty")
	}
	if strings.Contains(normalized, "..") {
		return "", fmt.Errorf("series cannot contain ..")
	}
	return normalized, nil
}

func publishBriefingUTCStamp(date time.Time) string {
	date = date.UTC()
	return fmt.Sprintf("%04d-%02d-%02dT%02d%02d%02dZ",
		date.Year(), date.Month(), date.Day(), date.Hour(), date.Minute(), date.Second())
}

func resolvePublishBriefingToken(preferredEnvName string) string {
	if preferredEnvName != "" {
		if v := os.Getenv(preferredEnvName); v != "" {
			return v
		}
	}
	if v := os.Getenv("BRIEFINGS_GH_TOKEN"); v != "" {
		return v
	}
	return os.Getenv("GITHUB_TOKEN")
}

func publishBriefingHTMLEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(value)
}

func publishBriefingXMLEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(value)
}

func publishBriefingCDATAEscape(value string) string {
	return strings.ReplaceAll(value, "]]>", "]]]]><![CDATA[>")
}

func publishBriefingStampRFC822(stamp string) string {
	parsed, err := time.Parse("2006-01-02T150405Z", stamp)
	if err != nil {
		return publishBriefingUTCString(time.Now())
	}
	return publishBriefingUTCString(parsed)
}

func publishBriefingUTCString(date time.Time) string {
	return date.UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
}

func buildPublishBriefingRSSFeed(input publishBriefingFeedInput) string {
	channelLink := strings.TrimRight(input.SiteURL, "/") + "/"
	atomSelf := strings.TrimRight(input.SiteURL, "/") + "/" + input.FeedPath
	var body strings.Builder
	for index, item := range input.Items {
		if index > 0 {
			body.WriteByte('\n')
		}
		body.WriteString("    <item>\n")
		body.WriteString("      <title>" + publishBriefingXMLEscape(item.Title) + "</title>\n")
		body.WriteString("      <link>" + publishBriefingXMLEscape(item.Link) + "</link>\n")
		guid := item.GUID
		if guid == "" {
			guid = item.Link
		}
		body.WriteString("      <guid isPermaLink=\"true\">" + publishBriefingXMLEscape(guid) + "</guid>\n")
		body.WriteString("      <pubDate>" + publishBriefingXMLEscape(item.PubDate) + "</pubDate>\n")
		if item.DescriptionHTML != "" {
			body.WriteString("      <description><![CDATA[" + publishBriefingCDATAEscape(item.DescriptionHTML) + "]]></description>\n")
		} else if item.Description != "" {
			body.WriteString("      <description>" + publishBriefingXMLEscape(item.Description) + "</description>\n")
		}
		if item.ContentHTML != "" {
			body.WriteString("      <content:encoded><![CDATA[" + publishBriefingCDATAEscape(item.ContentHTML) + "]]></content:encoded>\n")
		}
		body.WriteString("    </item>")
	}

	return strings.Join([]string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:content="http://purl.org/rss/1.0/modules/content/">`,
		`  <channel>`,
		"    <title>" + publishBriefingXMLEscape(input.Title) + "</title>",
		"    <description>" + publishBriefingXMLEscape(input.Description) + "</description>",
		"    <link>" + publishBriefingXMLEscape(channelLink) + "</link>",
		`    <language>en-us</language>`,
		`    <generator>briefing-publisher</generator>`,
		`    <docs>https://www.rssboard.org/rss-specification</docs>`,
		"    <atom:link href=\"" + publishBriefingXMLEscape(atomSelf) + `\" rel="self" type="application/rss+xml" />`,
		"    <lastBuildDate>" + publishBriefingUTCString(time.Now()) + "</lastBuildDate>",
		body.String(),
		`  </channel>`,
		`</rss>`,
		``,
	}, "\n")
}

func buildPublishBriefingSeriesIndexHTML(input publishBriefingSeriesIndexHTMLInput) string {
	items := make([]string, 0, len(input.Entries))
	for _, entry := range input.Entries {
		items = append(items, fmt.Sprintf(`      <li><a href="%s">%s</a></li>`, publishBriefingHTMLEscape(entry.Link), publishBriefingHTMLEscape(entry.Label)))
	}
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>%s</title>
    <style>
      :root { color-scheme: dark; }
      body {
        font-family: -apple-system,BlinkMacSystemFont,Segoe UI,Helvetica,Arial,sans-serif;
        max-width: 860px;
        margin: 2rem auto;
        padding: 0 1rem;
        line-height: 1.5;
        background: #0d1117;
        color: #c9d1d9;
      }
      h1, h2 { color: #f0f6fc; }
      a { color: #58a6ff; }
      ul { padding-left: 1.2rem; }
    </style>
  </head>
  <body>
    <h1>%s</h1>
    <p>Automated briefing archive.</p>
    <p>
      <a href="%s">Home</a>
      ·
      <a href="%s">Latest report</a>
      ·
      <a href="%s">RSS feed</a>
    </p>
    <h2>Entries</h2>
    <ul>
%s
    </ul>
  </body>
</html>
`,
		publishBriefingHTMLEscape(input.Title),
		publishBriefingHTMLEscape(input.Title),
		publishBriefingHTMLEscape(input.HomePath),
		publishBriefingHTMLEscape(input.LatestViewPath),
		publishBriefingHTMLEscape(input.FeedPath),
		strings.Join(items, "\n"),
	)
}

func titleCasePublishBriefingSeries(series string) string {
	parts := strings.Split(series, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		part = strings.ReplaceAll(part, "-", " ")
		part = strings.ReplaceAll(part, "_", " ")
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, strings.ToUpper(part[:1])+part[1:])
	}
	return strings.Join(out, " / ")
}

func buildPublishBriefingRootHomeHTML(input publishBriefingRootHomeHTMLInput) string {
	seriesTitle := titleCasePublishBriefingSeries(input.Series)
	items := make([]string, 0, 5)
	for _, entry := range input.Entries {
		if len(items) >= 5 {
			break
		}
		items = append(items, fmt.Sprintf(`        <li class="inline-links">%s (<a href="%s">html</a>|<a href="%s">md</a>)</li>`,
			publishBriefingHTMLEscape(entry.Label),
			publishBriefingHTMLEscape(entry.ViewerURL),
			publishBriefingHTMLEscape(entry.MDURL),
		))
	}
	if len(items) == 0 {
		items = append(items, `        <li class="muted">No archive entries yet.</li>`)
	}
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>AI Briefings</title>
    <style>
      :root { color-scheme: dark; }
      body {
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
        margin: 0;
        background: #0d1117;
        color: #c9d1d9;
      }
      .shell {
        margin: 2rem auto;
        max-width: 860px;
        line-height: 1.5;
        padding: 0 1rem;
      }
      h1, h2 { margin-bottom: 0.4rem; color: #f0f6fc; }
      h3, h4 { margin-bottom: 0.3rem; color: #f0f6fc; }
      ul { margin-top: 0.4rem; }
      a { color: #58a6ff; }
      code { background: #161b22; padding: 0.1rem 0.35rem; border-radius: 4px; color: #c9d1d9; }
      .inline-links a { margin: 0 0.2rem; }
      .muted { color: #8b949e; font-size: 0.95rem; }
    </style>
  </head>
  <body>
    <div class="shell">
      <h1>AI Briefings</h1>
      <p>Published briefings for market and geopolitics workflows.</p>

      <h2>Reports</h2>
      <h3>%s</h3>
      <ul>
        <li class="inline-links">latest (
          <a href="%s">html</a>|
          <a href="%s">md</a>
        )</li>
        <li>RSS feed: <a href="%s">feed.xml</a></li>
      </ul>

      <h4>archive (limit to 5 max)</h4>
      <ul>
%s
      </ul>
      <p class="muted">Full archive: <a href="%s">reports/%s/index.html</a></p>
    </div>
  </body>
</html>
`,
		publishBriefingHTMLEscape(seriesTitle),
		publishBriefingHTMLEscape(input.LatestViewerURL),
		publishBriefingHTMLEscape(input.LatestMDURL),
		publishBriefingHTMLEscape(input.SeriesFeedURL),
		strings.Join(items, "\n"),
		publishBriefingHTMLEscape(input.SeriesIndexURL),
		publishBriefingHTMLEscape(input.Series),
	)
}

func buildPublishBriefingRootSeriesSection(input publishBriefingRootSectionInput) string {
	lines := []string{
		"### " + titleCasePublishBriefingSeries(input.Series),
		"",
		fmt.Sprintf("- latest ( [html](%s)| [md](%s) )", input.LatestViewerURL, input.LatestMDURL),
		fmt.Sprintf("- RSS feed: [feed.xml](%s)", input.SeriesFeedURL),
		"",
		"#### archive (limit to 5 max)",
		"",
	}
	for i, entry := range input.Entries {
		if i >= 5 {
			break
		}
		lines = append(lines, fmt.Sprintf("- %s ([html](%s)|[md](%s))", entry.Label, entry.ViewerURL, entry.MDURL))
	}
	lines = append(lines,
		"",
		fmt.Sprintf("Full archive: [reports/%s/index.html](%s)", input.Series, input.SeriesIndexURL),
		"",
	)
	return strings.Join(lines, "\n")
}

func upsertPublishBriefingRootIndexSeriesSection(oldContent string, input publishBriefingRootSectionUpsert) string {
	sectionTitle := regexp.QuoteMeta(titleCasePublishBriefingSeries(input.Series))
	normalized := strings.TrimSpace(oldContent)
	if normalized == "" {
		return strings.Join([]string{
			"# AI Briefings",
			"",
			"Published briefings for market and geopolitics workflows.",
			"",
			"## Reports",
			"",
			input.Section,
		}, "\n")
	}
	sectionStart := regexp.MustCompile(`(?m)^### ` + sectionTitle + `\n`)
	if loc := sectionStart.FindStringIndex(oldContent); loc != nil {
		start := loc[0]
		rest := oldContent[loc[1]:]
		end := len(rest)
		for _, marker := range []string{"\n### ", "\n## Naming convention"} {
			if idx := strings.Index(rest, marker); idx >= 0 && idx < end {
				end = idx
			}
		}
		return oldContent[:start] + input.Section + "\n" + rest[end:]
	}
	reportsHeader := regexp.MustCompile(`(?m)^## Reports\s*$`)
	if reportsHeader.MatchString(oldContent) {
		return reportsHeader.ReplaceAllString(oldContent, "## Reports\n\n"+input.Section)
	}
	return strings.TrimRight(oldContent, " \t\r\n") + "\n\n## Reports\n\n" + input.Section
}

func publishBriefingBranchRefPath(branch string) string {
	parts := strings.Split(branch, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func buildPublishBriefingIndex(series string, entries []publishBriefingIndexEntry, feedPath string) string {
	lines := []string{"# " + series, "", "Automated briefing archive."}
	if feedPath != "" {
		lines = append(lines, "", fmt.Sprintf("RSS feed: [feed.xml](%s)", feedPath))
	}
	lines = append(lines, "", "## Entries", "")
	for _, entry := range entries {
		lines = append(lines, fmt.Sprintf("- [%s](%s)", entry.Label, entry.Link))
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

func updatePublishBriefingIndexContent(oldContent, archiveRelativePath, stamp string) string {
	entry := fmt.Sprintf("- [%s](%s)", stamp, archiveRelativePath)
	if strings.TrimSpace(oldContent) == "" {
		return buildPublishBriefingIndex("Briefings", []publishBriefingIndexEntry{{Label: stamp, Link: archiveRelativePath}}, "feed.xml")
	}
	lines := strings.Split(oldContent, "\n")
	existing := map[string]bool{}
	for _, line := range lines {
		if strings.HasPrefix(line, "- [") {
			existing[line] = true
		}
	}
	if existing[entry] {
		return oldContent
	}
	out := make([]string, 0, len(lines)+2)
	inserted := false
	for _, line := range lines {
		out = append(out, line)
		if !inserted && strings.TrimSpace(line) == "## Entries" {
			out = append(out, "", entry)
			inserted = true
		}
	}
	if !inserted {
		out = append(out, "", "## Entries", "", entry)
	}
	joined := strings.Join(out, "\n")
	repeatedBlankLines := regexp.MustCompile(`\n{3,}`)
	return repeatedBlankLines.ReplaceAllString(joined, "\n\n")
}

func parsePublishBriefingIndexEntries(content string) []publishBriefingIndexEntry {
	lines := strings.Split(content, "\n")
	entries := make([]publishBriefingIndexEntry, 0)
	pattern := regexp.MustCompile(`^- \[(.+?)\]\((.+?)\)$`)
	for _, line := range lines {
		match := pattern.FindStringSubmatch(line)
		if len(match) == 3 {
			entries = append(entries, publishBriefingIndexEntry{Label: match[1], Link: match[2]})
		}
	}
	return entries
}

func buildPublishBriefingRSSItemHTML(series, label, viewerURL, mdURL string) string {
	return strings.Join([]string{
		"<p><strong>" + publishBriefingHTMLEscape(series) + " briefing</strong> — " + publishBriefingHTMLEscape(label) + "</p>",
		"<p><a href=\"" + publishBriefingHTMLEscape(viewerURL) + "\">Read in HTML viewer</a> · <a href=\"" + publishBriefingHTMLEscape(mdURL) + "\">Raw Markdown</a></p>",
	}, "")
}

func derivePublishBriefingPaths(series, stamp string) publishBriefingPaths {
	year := stamp[0:4]
	month := stamp[5:7]
	archiveFile := stamp + ".md"
	baseDir := "docs/reports/" + series
	return publishBriefingPaths{
		ArchivePath:       baseDir + "/" + year + "/" + month + "/" + archiveFile,
		LatestPath:        baseDir + "/latest.md",
		IndexPath:         baseDir + "/index.md",
		IndexHTMLPath:     baseDir + "/index.html",
		SeriesFeedPath:    baseDir + "/feed.xml",
		RootFeedPath:      "docs/feed.xml",
		RootIndexPath:     "docs/index.md",
		RootIndexHTMLPath: "docs/index.html",
		ArchiveRelative:   year + "/" + month + "/" + stamp + ".md",
	}
}

func newPublishBriefingGitHubClient(token string) *publishBriefingGitHubClient {
	return &publishBriefingGitHubClient{
		baseURL: strings.TrimRight(publishBriefingGitHubAPIBaseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *publishBriefingGitHubClient) request(method, urlPath string, body any, out any) error {
	fullURL := c.baseURL + urlPath
	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding GitHub request body: %w", err)
		}
		bodyReader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("creating request for %s: %w", fullURL, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", publishBriefingUserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, fullURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response from %s: %w", fullURL, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload map[string]any
		if json.Unmarshal(responseBody, &payload) == nil {
			if message, ok := payload["message"].(string); ok && message != "" {
				return &publishBriefingHTTPError{status: resp.StatusCode, message: message}
			}
		}
		return &publishBriefingHTTPError{status: resp.StatusCode, message: strings.TrimSpace(string(responseBody))}
	}
	if out == nil || len(responseBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, out); err != nil {
		return fmt.Errorf("decoding response from %s: %w", fullURL, err)
	}
	return nil
}

type publishBriefingHTTPError struct {
	status  int
	message string
}

func (e *publishBriefingHTTPError) Error() string {
	if e.message == "" {
		return fmt.Sprintf("GitHub API error %d", e.status)
	}
	return e.message
}

func (e *publishBriefingHTTPError) Status() int {
	return e.status
}

func getPublishBriefingBranchHeadSHA(client *publishBriefingGitHubClient, owner, repo, branch string) (string, error) {
	var data publishBriefingGitRef
	if err := client.request(http.MethodGet, "/repos/"+owner+"/"+repo+"/git/ref/heads/"+publishBriefingBranchRefPath(branch), nil, &data); err != nil {
		return "", err
	}
	if data.Object.SHA == "" {
		return "", fmt.Errorf("unable to resolve branch head SHA for %s", branch)
	}
	return data.Object.SHA, nil
}

func getPublishBriefingCommit(client *publishBriefingGitHubClient, owner, repo, sha string) (publishBriefingGitCommit, error) {
	var data publishBriefingGitCommit
	err := client.request(http.MethodGet, "/repos/"+owner+"/"+repo+"/git/commits/"+url.PathEscape(sha), nil, &data)
	return data, err
}

func createPublishBriefingBlob(client *publishBriefingGitHubClient, owner, repo, content string) (publishBriefingBlob, error) {
	var data publishBriefingBlob
	err := client.request(http.MethodPost, "/repos/"+owner+"/"+repo+"/git/blobs", map[string]string{
		"content":  content,
		"encoding": "utf-8",
	}, &data)
	return data, err
}

func createPublishBriefingTree(client *publishBriefingGitHubClient, owner, repo, baseTreeSHA string, tree []map[string]string) (publishBriefingTree, error) {
	var data publishBriefingTree
	err := client.request(http.MethodPost, "/repos/"+owner+"/"+repo+"/git/trees", map[string]any{
		"base_tree": baseTreeSHA,
		"tree":      tree,
	}, &data)
	return data, err
}

func createPublishBriefingCommitObject(client *publishBriefingGitHubClient, owner, repo, message, treeSHA, parentSHA string) (publishBriefingCommit, error) {
	var data publishBriefingCommit
	err := client.request(http.MethodPost, "/repos/"+owner+"/"+repo+"/git/commits", map[string]any{
		"message": message,
		"tree":    treeSHA,
		"parents": []string{parentSHA},
	}, &data)
	return data, err
}

func updatePublishBriefingBranchHead(client *publishBriefingGitHubClient, owner, repo, branch, sha string) error {
	return client.request(http.MethodPatch, "/repos/"+owner+"/"+repo+"/git/refs/heads/"+publishBriefingBranchRefPath(branch), map[string]any{
		"sha":   sha,
		"force": false,
	}, nil)
}

func commitPublishBriefingFilesBatch(client *publishBriefingGitHubClient, owner, repo, branch, message string, files []publishBriefingGitFile) (string, error) {
	headSHA, err := getPublishBriefingBranchHeadSHA(client, owner, repo, branch)
	if err != nil {
		return "", err
	}
	headCommit, err := getPublishBriefingCommit(client, owner, repo, headSHA)
	if err != nil {
		return "", err
	}
	if headCommit.Tree.SHA == "" {
		return "", fmt.Errorf("unable to resolve base tree SHA")
	}
	treeEntries := make([]map[string]string, 0, len(files))
	for _, file := range files {
		blob, err := createPublishBriefingBlob(client, owner, repo, file.Content)
		if err != nil {
			return "", err
		}
		treeEntries = append(treeEntries, map[string]string{
			"path": file.Path,
			"mode": "100644",
			"type": "blob",
			"sha":  blob.SHA,
		})
	}
	tree, err := createPublishBriefingTree(client, owner, repo, headCommit.Tree.SHA, treeEntries)
	if err != nil {
		return "", err
	}
	commit, err := createPublishBriefingCommitObject(client, owner, repo, message, tree.SHA, headSHA)
	if err != nil {
		return "", err
	}
	if err := updatePublishBriefingBranchHead(client, owner, repo, branch, commit.SHA); err != nil {
		return "", err
	}
	return commit.SHA, nil
}

func getPublishBriefingTextFile(client *publishBriefingGitHubClient, owner, repo, branch, filePath string) (string, error) {
	encodedPath := strings.ReplaceAll(url.PathEscape(filePath), "%2F", "/")
	var data publishBriefingContents
	err := client.request(http.MethodGet, "/repos/"+owner+"/"+repo+"/contents/"+encodedPath+"?ref="+url.QueryEscape(branch), nil, &data)
	if err != nil {
		var httpErr *publishBriefingHTTPError
		if ok := errorAs(err, &httpErr); ok && httpErr.Status() == http.StatusNotFound {
			return "", nil
		}
		return "", err
	}
	if data.Content == "" {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(data.Content, "\n", ""))
	if err != nil {
		return "", fmt.Errorf("decoding base64 content for %s: %w", filePath, err)
	}
	return string(decoded), nil
}

func errorAs(err error, target **publishBriefingHTTPError) bool {
	httpErr, ok := err.(*publishBriefingHTTPError)
	if !ok {
		return false
	}
	*target = httpErr
	return true
}

func publishBriefingSitePaths(baseURL, series string, paths publishBriefingPaths) (archiveURL, latestURL, seriesFeedURL, rootFeedURL, latestMDURL, seriesIndexURL string, err error) {
	base := strings.TrimRight(baseURL, "/")
	parsed, err := url.Parse(base)
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("invalid --site-base-url: %w", err)
	}
	sitePath := strings.TrimRight(parsed.Path, "/")
	archiveMDPath := sitePath + "/reports/" + series + "/" + paths.ArchiveRelative
	latestMDPath := sitePath + "/reports/" + series + "/latest.md"
	archiveURL = base + "/viewer.html?src=" + url.QueryEscape(archiveMDPath)
	latestURL = base + "/viewer.html?src=" + url.QueryEscape(latestMDPath)
	seriesFeedURL = base + "/reports/" + series + "/feed.xml"
	rootFeedURL = base + "/feed.xml"
	latestMDURL = base + "/reports/" + series + "/latest.md"
	seriesIndexURL = base + "/reports/" + series + "/index.html"
	return archiveURL, latestURL, seriesFeedURL, rootFeedURL, latestMDURL, seriesIndexURL, nil
}

func runPublishBriefingWithOptions(opts publishBriefingOptions, stdout io.Writer) error {
	series, err := sanitizePublishBriefingSeries(opts.Series)
	if err != nil {
		return err
	}
	inputFile, err := filepath.Abs(opts.InputFile)
	if err != nil {
		return fmt.Errorf("resolving input file path: %w", err)
	}
	inputContentBytes, err := os.ReadFile(inputFile)
	if err != nil {
		return fmt.Errorf("reading input file: %w", err)
	}
	inputContent := string(inputContentBytes)
	if strings.TrimSpace(inputContent) == "" {
		return fmt.Errorf("input file is empty")
	}
	var date time.Time
	if opts.Timestamp != "" {
		date, err = time.Parse(time.RFC3339, opts.Timestamp)
		if err != nil {
			return fmt.Errorf("invalid --timestamp value")
		}
	} else {
		date = time.Now().UTC()
	}
	stamp := publishBriefingUTCStamp(date)
	parts := strings.Split(opts.Repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid --repo, expected owner/name")
	}
	owner, repo := parts[0], parts[1]
	paths := derivePublishBriefingPaths(series, stamp)
	archiveURL, latestURL, seriesFeedURL, rootFeedURL, latestMDURL, seriesIndexURL, err := publishBriefingSitePaths(opts.SiteBaseURL, series, paths)
	if err != nil {
		return err
	}
	if opts.DryRun {
		result := publishBriefingDryRunResult{
			DryRun:            true,
			Repo:              opts.Repo,
			Branch:            opts.Branch,
			Series:            series,
			Stamp:             stamp,
			ArchivePath:       paths.ArchivePath,
			LatestPath:        paths.LatestPath,
			IndexPath:         paths.IndexPath,
			SeriesFeedPath:    paths.SeriesFeedPath,
			RootFeedPath:      paths.RootFeedPath,
			RootIndexPath:     paths.RootIndexPath,
			RootIndexHTMLPath: paths.RootIndexHTMLPath,
			ArchiveURL:        archiveURL,
			LatestURL:         latestURL,
			SeriesFeedURL:     seriesFeedURL,
			RootFeedURL:       rootFeedURL,
		}
		payload, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding dry-run output: %w", err)
		}
		_, err = fmt.Fprintln(stdout, string(payload))
		return err
	}
	token := resolvePublishBriefingToken(opts.TokenEnv)
	if token == "" {
		return fmt.Errorf("missing token env. Set %s (or BRIEFINGS_GH_TOKEN / GITHUB_TOKEN)", opts.TokenEnv)
	}
	client := newPublishBriefingGitHubClient(token)
	oldIndex, err := getPublishBriefingTextFile(client, owner, repo, opts.Branch, paths.IndexPath)
	if err != nil {
		return err
	}
	oldRootIndex, err := getPublishBriefingTextFile(client, owner, repo, opts.Branch, paths.RootIndexPath)
	if err != nil {
		return err
	}
	newIndex := updatePublishBriefingIndexContent(oldIndex, paths.ArchiveRelative, stamp)
	parsedBase, _ := url.Parse(strings.TrimRight(opts.SiteBaseURL, "/"))
	sitePath := strings.TrimRight(parsedBase.Path, "/")
	entries := make([]publishBriefingIndexEntry, 0)
	entryDetails := make([]publishBriefingRootEntry, 0)
	for _, entry := range parsePublishBriefingIndexEntries(newIndex) {
		mdPath := sitePath + "/reports/" + series + "/" + entry.Link
		viewerLink := strings.TrimRight(opts.SiteBaseURL, "/") + "/viewer.html?src=" + url.QueryEscape(mdPath)
		mdURL := strings.TrimRight(opts.SiteBaseURL, "/") + "/reports/" + series + "/" + entry.Link
		entries = append(entries, publishBriefingIndexEntry{Label: entry.Label, Link: viewerLink})
		entryDetails = append(entryDetails, publishBriefingRootEntry{Label: entry.Label, ViewerURL: viewerLink, MDURL: mdURL})
	}
	indexHTML := buildPublishBriefingSeriesIndexHTML(publishBriefingSeriesIndexHTMLInput{
		Title:          series + " archive",
		Entries:        entries,
		HomePath:       strings.TrimRight(opts.SiteBaseURL, "/") + "/index.html",
		LatestViewPath: latestURL,
		FeedPath:       seriesFeedURL,
	})
	rssItems := make([]publishBriefingRSSEntry, 0)
	for i, entry := range parsePublishBriefingIndexEntries(newIndex) {
		if i >= 100 {
			break
		}
		mdPath := sitePath + "/reports/" + series + "/" + entry.Link
		mdLink := strings.TrimRight(opts.SiteBaseURL, "/") + "/reports/" + series + "/" + entry.Link
		viewerLink := strings.TrimRight(opts.SiteBaseURL, "/") + "/viewer.html?src=" + url.QueryEscape(mdPath)
		itemHTML := buildPublishBriefingRSSItemHTML(series, entry.Label, viewerLink, mdLink)
		rssItems = append(rssItems, publishBriefingRSSEntry{
			Title:           series + " " + entry.Label,
			Link:            viewerLink,
			GUID:            viewerLink,
			PubDate:         publishBriefingStampRFC822(entry.Label),
			DescriptionHTML: itemHTML,
			ContentHTML:     itemHTML,
		})
	}
	seriesFeed := buildPublishBriefingRSSFeed(publishBriefingFeedInput{
		Title:       series + " briefings",
		Description: "Automated " + series + " briefing archive feed",
		SiteURL:     strings.TrimRight(opts.SiteBaseURL, "/"),
		FeedPath:    "reports/" + series + "/feed.xml",
		Items:       rssItems,
	})
	rootFeed := buildPublishBriefingRSSFeed(publishBriefingFeedInput{
		Title:       series + " briefings",
		Description: "Automated " + series + " briefing archive feed",
		SiteURL:     strings.TrimRight(opts.SiteBaseURL, "/"),
		FeedPath:    "feed.xml",
		Items:       rssItems,
	})
	rootSection := buildPublishBriefingRootSeriesSection(publishBriefingRootSectionInput{
		Series:          series,
		LatestViewerURL: latestURL,
		LatestMDURL:     latestMDURL,
		SeriesIndexURL:  seriesIndexURL,
		SeriesFeedURL:   seriesFeedURL,
		Entries:         entryDetails,
	})
	newRootIndex := upsertPublishBriefingRootIndexSeriesSection(oldRootIndex, publishBriefingRootSectionUpsert{Series: series, Section: rootSection})
	newRootIndexHTML := buildPublishBriefingRootHomeHTML(publishBriefingRootHomeHTMLInput{
		Series:          series,
		LatestViewerURL: latestURL,
		LatestMDURL:     latestMDURL,
		SeriesFeedURL:   seriesFeedURL,
		SeriesIndexURL:  seriesIndexURL,
		Entries:         entryDetails,
	})
	commitSHA, err := commitPublishBriefingFilesBatch(client, owner, repo, opts.Branch, "publish: "+series+" "+stamp, []publishBriefingGitFile{
		{Path: paths.ArchivePath, Content: inputContent},
		{Path: paths.LatestPath, Content: inputContent},
		{Path: paths.IndexPath, Content: newIndex},
		{Path: paths.IndexHTMLPath, Content: indexHTML},
		{Path: paths.SeriesFeedPath, Content: seriesFeed},
		{Path: paths.RootFeedPath, Content: rootFeed},
		{Path: paths.RootIndexPath, Content: newRootIndex},
		{Path: paths.RootIndexHTMLPath, Content: newRootIndexHTML},
	})
	if err != nil {
		return err
	}
	result := publishBriefingResult{
		OK:                true,
		Repo:              opts.Repo,
		Branch:            opts.Branch,
		Series:            series,
		Stamp:             stamp,
		ArchivePath:       paths.ArchivePath,
		LatestPath:        paths.LatestPath,
		IndexPath:         paths.IndexPath,
		SeriesFeedPath:    paths.SeriesFeedPath,
		RootFeedPath:      paths.RootFeedPath,
		RootIndexPath:     paths.RootIndexPath,
		RootIndexHTMLPath: paths.RootIndexHTMLPath,
		CommitSHA:         commitSHA,
		ArchiveURL:        archiveURL,
		LatestURL:         latestURL,
		SeriesFeedURL:     seriesFeedURL,
		RootFeedURL:       rootFeedURL,
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding publish output: %w", err)
	}
	_, err = fmt.Fprintln(stdout, string(payload))
	return err
}

var publishBriefingCmd = &cobra.Command{
	Use:   "publish-briefing",
	Short: "Publish markdown briefings to GitHub Pages via GitHub Git API",
	Long: `Publish a local markdown briefing into a GitHub repository using the GitHub Git API.

The command creates an atomic multi-file commit with archive/latest markdown,
series index files, series RSS feed, root RSS feed, and root index files.

Examples:
  netcup-claw tool publish-briefing --input-file /tmp/briefing.md --dry-run
  netcup-claw tool publish-briefing --input-file /tmp/briefing.md --series market
  netcup-claw tool publish-briefing --input-file /tmp/briefing.md --repo owner/name --branch main`,
	RunE: runPublishBriefing,
}

func runPublishBriefing(_ *cobra.Command, _ []string) error {
	return runPublishBriefingWithOptions(publishBriefingOptions{
		InputFile:   publishBriefingInputFile,
		Series:      publishBriefingSeries,
		Repo:        publishBriefingRepo,
		Branch:      publishBriefingBranch,
		SiteBaseURL: publishBriefingSiteBaseURL,
		TokenEnv:    publishBriefingTokenEnv,
		Timestamp:   publishBriefingTimestamp,
		DryRun:      publishBriefingDryRun,
	}, os.Stdout)
}

func init() {
	publishBriefingCmd.Flags().StringVar(&publishBriefingInputFile, "input-file", "", "Markdown file to publish")
	publishBriefingCmd.Flags().StringVar(&publishBriefingSeries, "series", "market", "Series path under docs/reports")
	publishBriefingCmd.Flags().StringVar(&publishBriefingRepo, "repo", "mfittko/ai-briefings", "GitHub repo in owner/name form")
	publishBriefingCmd.Flags().StringVar(&publishBriefingBranch, "branch", "main", "Git branch to update")
	publishBriefingCmd.Flags().StringVar(&publishBriefingSiteBaseURL, "site-base-url", "https://mfittko.github.io/ai-briefings", "GitHub Pages base URL")
	publishBriefingCmd.Flags().StringVar(&publishBriefingTokenEnv, "token-env", "GH_TOKEN", "Preferred token environment variable name")
	publishBriefingCmd.Flags().StringVar(&publishBriefingTimestamp, "timestamp", "", "Override timestamp used for archive path (RFC3339)")
	publishBriefingCmd.Flags().BoolVar(&publishBriefingDryRun, "dry-run", false, "Compute and print result without writing to GitHub")
	_ = publishBriefingCmd.MarkFlagRequired("input-file")
}
