package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPublishBriefingUTCStamp(t *testing.T) {
	got := publishBriefingUTCStamp(time.Date(2026, 3, 8, 0, 53, 51, 687000000, time.UTC))
	if got != "2026-03-08T005351Z" {
		t.Fatalf("publishBriefingUTCStamp() = %q, want %q", got, "2026-03-08T005351Z")
	}
}

func TestDerivePublishBriefingPaths(t *testing.T) {
	got := derivePublishBriefingPaths("market", "2026-03-08T005351Z")
	if got.ArchivePath != "docs/reports/market/2026/03/2026-03-08T005351Z.md" {
		t.Fatalf("ArchivePath = %q", got.ArchivePath)
	}
	if got.LatestPath != "docs/reports/market/latest.md" {
		t.Fatalf("LatestPath = %q", got.LatestPath)
	}
	if got.IndexPath != "docs/reports/market/index.md" {
		t.Fatalf("IndexPath = %q", got.IndexPath)
	}
	if got.SeriesFeedPath != "docs/reports/market/feed.xml" {
		t.Fatalf("SeriesFeedPath = %q", got.SeriesFeedPath)
	}
	if got.RootFeedPath != "docs/feed.xml" {
		t.Fatalf("RootFeedPath = %q", got.RootFeedPath)
	}
	if got.RootIndexPath != "docs/index.md" {
		t.Fatalf("RootIndexPath = %q", got.RootIndexPath)
	}
	if got.RootIndexHTMLPath != "docs/index.html" {
		t.Fatalf("RootIndexHTMLPath = %q", got.RootIndexHTMLPath)
	}
	if got.ArchiveRelative != "2026/03/2026-03-08T005351Z.md" {
		t.Fatalf("ArchiveRelative = %q", got.ArchiveRelative)
	}
}

func TestUpdatePublishBriefingIndexContent(t *testing.T) {
	oldContent := strings.Join([]string{
		"# market",
		"",
		"Automated briefing archive.",
		"",
		"## Entries",
		"",
		"- [2026-03-07T005351Z](2026/03/2026-03-07T005351Z.md)",
		"",
	}, "\n")
	got := updatePublishBriefingIndexContent(oldContent, "2026/03/2026-03-08T005351Z.md", "2026-03-08T005351Z")
	entries := parsePublishBriefingIndexEntries(got)
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(entries))
	}
	if entries[0].Label != "2026-03-08T005351Z" {
		t.Fatalf("newest label = %q", entries[0].Label)
	}
	if strings.Count(got, "- [2026-03-08T005351Z](2026/03/2026-03-08T005351Z.md)") != 1 {
		t.Fatalf("new entry duplicated in index:\n%s", got)
	}

	gotAgain := updatePublishBriefingIndexContent(got, "2026/03/2026-03-08T005351Z.md", "2026-03-08T005351Z")
	if gotAgain != got {
		t.Fatalf("duplicate update changed content")
	}
}

func TestUpsertPublishBriefingRootIndexSeriesSection(t *testing.T) {
	section := buildPublishBriefingRootSeriesSection(publishBriefingRootSectionInput{
		Series:          "market/daily-pulse",
		LatestViewerURL: "https://example.test/viewer.html?src=%2Freports%2Fmarket%2Flatest.md",
		LatestMDURL:     "https://example.test/reports/market/latest.md",
		SeriesIndexURL:  "https://example.test/reports/market/index.html",
		SeriesFeedURL:   "https://example.test/reports/market/feed.xml",
		Entries: []publishBriefingRootEntry{{
			Label:     "2026-03-08T005351Z",
			ViewerURL: "https://example.test/viewer.html?src=%2Freports%2Fmarket%2F2026%2F03%2F2026-03-08T005351Z.md",
			MDURL:     "https://example.test/reports/market/2026/03/2026-03-08T005351Z.md",
		}},
	})

	got := upsertPublishBriefingRootIndexSeriesSection("", publishBriefingRootSectionUpsert{Series: "market/daily-pulse", Section: section})
	if !strings.Contains(got, "## Reports") || !strings.Contains(got, "### Market / Daily pulse") {
		t.Fatalf("unexpected root index content:\n%s", got)
	}

	replaced := upsertPublishBriefingRootIndexSeriesSection(got, publishBriefingRootSectionUpsert{Series: "market/daily-pulse", Section: section + "\nExtra line"})
	if strings.Count(replaced, "### Market / Daily pulse") != 1 {
		t.Fatalf("expected section replacement, got:\n%s", replaced)
	}
	if !strings.Contains(replaced, "Extra line") {
		t.Fatalf("replacement section missing updated content")
	}
}

func TestBuildPublishBriefingRSSFeedIsValidXML(t *testing.T) {
	feed := buildPublishBriefingRSSFeed(publishBriefingFeedInput{
		Title:       "market briefings",
		Description: "Automated market briefing archive feed",
		SiteURL:     "https://example.test/base",
		FeedPath:    "reports/market/feed.xml",
		Items: []publishBriefingRSSEntry{{
			Title:           "market 2026-03-08T005351Z",
			Link:            "https://example.test/viewer.html?src=%2Freports%2Fmarket%2F2026%2F03%2F2026-03-08T005351Z.md",
			GUID:            "https://example.test/viewer.html?src=%2Freports%2Fmarket%2F2026%2F03%2F2026-03-08T005351Z.md",
			PubDate:         publishBriefingStampRFC822("2026-03-08T005351Z"),
			DescriptionHTML: "<p>description</p>",
			ContentHTML:     "<p>content</p>",
		}},
	})
	var rss struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Title string `xml:"title"`
			Items []struct {
				Title string `xml:"title"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal([]byte(feed), &rss); err != nil {
		t.Fatalf("xml.Unmarshal(feed): %v\n%s", err, feed)
	}
	if rss.XMLName.Local != "rss" || rss.Channel.Title != "market briefings" || len(rss.Channel.Items) != 1 {
		t.Fatalf("unexpected RSS content: %+v", rss)
	}
}

func TestBuildPublishBriefingHTML(t *testing.T) {
	seriesHTML := buildPublishBriefingSeriesIndexHTML(publishBriefingSeriesIndexHTMLInput{
		Title:          "market archive",
		Entries:        []publishBriefingIndexEntry{{Label: "2026-03-08T005351Z", Link: "https://example.test/report"}},
		HomePath:       "https://example.test/index.html",
		LatestViewPath: "https://example.test/viewer.html?src=%2Freports%2Fmarket%2Flatest.md",
		FeedPath:       "https://example.test/reports/market/feed.xml",
	})
	if !strings.Contains(seriesHTML, "color-scheme: dark") || !strings.Contains(seriesHTML, "Latest report") {
		t.Fatalf("unexpected series HTML:\n%s", seriesHTML)
	}

	rootHTML := buildPublishBriefingRootHomeHTML(publishBriefingRootHomeHTMLInput{
		Series:          "market/daily-pulse",
		LatestViewerURL: "https://example.test/viewer.html?src=%2Freports%2Fmarket%2Flatest.md",
		LatestMDURL:     "https://example.test/reports/market/latest.md",
		SeriesFeedURL:   "https://example.test/reports/market/feed.xml",
		SeriesIndexURL:  "https://example.test/reports/market/index.html",
		Entries: []publishBriefingRootEntry{{
			Label:     "2026-03-08T005351Z",
			ViewerURL: "https://example.test/viewer.html?src=%2Freports%2Fmarket%2F2026%2F03%2F2026-03-08T005351Z.md",
			MDURL:     "https://example.test/reports/market/2026/03/2026-03-08T005351Z.md",
		}},
	})
	if !strings.Contains(rootHTML, "AI Briefings") || !strings.Contains(rootHTML, "Market / Daily pulse") {
		t.Fatalf("unexpected root HTML:\n%s", rootHTML)
	}
}

func TestRunPublishBriefingDryRunMakesNoAPICalls(t *testing.T) {
	oldBaseURL := publishBriefingGitHubAPIBaseURL
	defer func() { publishBriefingGitHubAPIBaseURL = oldBaseURL }()

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	publishBriefingGitHubAPIBaseURL = server.URL

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "briefing.md")
	if err := os.WriteFile(inputPath, []byte("# Briefing\n\nhello\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	var stdout bytes.Buffer
	err := runPublishBriefingWithOptions(publishBriefingOptions{
		InputFile:   inputPath,
		Series:      "market",
		Repo:        "owner/repo",
		Branch:      "main",
		SiteBaseURL: "https://example.test/base",
		TokenEnv:    "TEST_PUBLISH_TOKEN",
		Timestamp:   "2026-03-08T00:53:51Z",
		DryRun:      true,
	}, &stdout)
	if err != nil {
		t.Fatalf("runPublishBriefingWithOptions(dry-run): %v", err)
	}
	if requests != 0 {
		t.Fatalf("dry-run made %d API requests", requests)
	}
	var payload publishBriefingDryRunResult
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(stdout): %v\n%s", err, stdout.String())
	}
	if payload.ArchivePath != "docs/reports/market/2026/03/2026-03-08T005351Z.md" {
		t.Fatalf("ArchivePath = %q", payload.ArchivePath)
	}
	if payload.ArchiveURL != "https://example.test/base/viewer.html?src=%2Fbase%2Freports%2Fmarket%2F2026%2F03%2F2026-03-08T005351Z.md" {
		t.Fatalf("ArchiveURL = %q", payload.ArchiveURL)
	}
}

func TestRunPublishBriefingLivePublishCreatesAtomicTree(t *testing.T) {
	oldBaseURL := publishBriefingGitHubAPIBaseURL
	defer func() { publishBriefingGitHubAPIBaseURL = oldBaseURL }()

	const tokenEnv = "TEST_PUBLISH_TOKEN"
	if err := os.Setenv(tokenEnv, "secret-token"); err != nil {
		t.Fatalf("os.Setenv: %v", err)
	}
	defer os.Unsetenv(tokenEnv)

	type blobRequest struct {
		SHA     string
		Content string
	}
	var (
		mu           sync.Mutex
		blobCounter  int
		blobs        []blobRequest
		treeEntries  []map[string]string
		commitBody   map[string]any
		patchBody    map[string]any
		requestOrder []string
	)

	oldIndex := strings.Join([]string{
		"# market",
		"",
		"Automated briefing archive.",
		"",
		"RSS feed: [feed.xml](feed.xml)",
		"",
		"## Entries",
		"",
		"- [2026-03-07T005351Z](2026/03/2026-03-07T005351Z.md)",
		"",
	}, "\n")
	oldRoot := strings.Join([]string{
		"# AI Briefings",
		"",
		"Published briefings for market and geopolitics workflows.",
		"",
		"## Reports",
		"",
		"### Market",
		"",
		"- latest ( [html](https://example.test/viewer.html?src=%2Freports%2Fmarket%2Flatest.md)| [md](https://example.test/reports/market/latest.md) )",
		"- RSS feed: [feed.xml](https://example.test/reports/market/feed.xml)",
		"",
		"#### archive (limit to 5 max)",
		"",
		"- 2026-03-07T005351Z ([html](https://example.test/viewer.html?src=%2Freports%2Fmarket%2F2026%2F03%2F2026-03-07T005351Z.md)|[md](https://example.test/reports/market/2026/03/2026-03-07T005351Z.md))",
		"",
		"Full archive: [reports/market/index.html](https://example.test/reports/market/index.html)",
		"",
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requestOrder = append(requestOrder, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/git/ref/heads/main":
			io.WriteString(w, `{"object":{"sha":"headsha"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/git/commits/headsha":
			io.WriteString(w, `{"tree":{"sha":"basetree"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/contents/docs/reports/market/index.md":
			io.WriteString(w, `{"content":"`+base64.StdEncoding.EncodeToString([]byte(oldIndex))+`"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/contents/docs/index.md":
			io.WriteString(w, `{"content":"`+base64.StdEncoding.EncodeToString([]byte(oldRoot))+`"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/git/blobs":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode blob request: %v", err)
			}
			blobCounter++
			sha := "blobsha-" + string(rune('0'+blobCounter))
			blobs = append(blobs, blobRequest{SHA: sha, Content: body["content"]})
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"sha":"`+sha+`"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/git/trees":
			var body struct {
				BaseTree string              `json:"base_tree"`
				Tree     []map[string]string `json:"tree"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode tree request: %v", err)
			}
			if body.BaseTree != "basetree" {
				t.Fatalf("base_tree = %q, want basetree", body.BaseTree)
			}
			treeEntries = append([]map[string]string(nil), body.Tree...)
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"sha":"treesha"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/git/commits":
			if err := json.NewDecoder(r.Body).Decode(&commitBody); err != nil {
				t.Fatalf("decode commit request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"sha":"commitsha"}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/owner/repo/git/refs/heads/main":
			if err := json.NewDecoder(r.Body).Decode(&patchBody); err != nil {
				t.Fatalf("decode patch request: %v", err)
			}
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, `{}`)
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()
	publishBriefingGitHubAPIBaseURL = server.URL

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "briefing.md")
	inputContent := "# Briefing\n\nhello\n"
	if err := os.WriteFile(inputPath, []byte(inputContent), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	var stdout bytes.Buffer
	err := runPublishBriefingWithOptions(publishBriefingOptions{
		InputFile:   inputPath,
		Series:      "market",
		Repo:        "owner/repo",
		Branch:      "main",
		SiteBaseURL: "https://example.test",
		TokenEnv:    tokenEnv,
		Timestamp:   "2026-03-08T00:53:51Z",
		DryRun:      false,
	}, &stdout)
	if err != nil {
		t.Fatalf("runPublishBriefingWithOptions(live): %v", err)
	}

	if len(blobs) != 8 {
		t.Fatalf("len(blobs) = %d, want 8", len(blobs))
	}
	if len(treeEntries) != 8 {
		t.Fatalf("len(treeEntries) = %d, want 8", len(treeEntries))
	}

	shaToContent := map[string]string{}
	for _, blob := range blobs {
		shaToContent[blob.SHA] = blob.Content
	}
	pathToContent := map[string]string{}
	var gotPaths []string
	for _, entry := range treeEntries {
		gotPaths = append(gotPaths, entry["path"])
		pathToContent[entry["path"]] = shaToContent[entry["sha"]]
	}
	sort.Strings(gotPaths)
	wantPaths := []string{
		"docs/feed.xml",
		"docs/index.html",
		"docs/index.md",
		"docs/reports/market/2026/03/2026-03-08T005351Z.md",
		"docs/reports/market/feed.xml",
		"docs/reports/market/index.html",
		"docs/reports/market/index.md",
		"docs/reports/market/latest.md",
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("tree paths = %#v, want %#v", gotPaths, wantPaths)
	}
	if pathToContent["docs/reports/market/2026/03/2026-03-08T005351Z.md"] != inputContent {
		t.Fatalf("archive content mismatch")
	}
	if pathToContent["docs/reports/market/latest.md"] != inputContent {
		t.Fatalf("latest content mismatch")
	}
	if !strings.Contains(pathToContent["docs/reports/market/index.md"], "- [2026-03-08T005351Z](2026/03/2026-03-08T005351Z.md)") {
		t.Fatalf("series index missing new entry:\n%s", pathToContent["docs/reports/market/index.md"])
	}
	if !strings.Contains(pathToContent["docs/index.md"], "### Market") || !strings.Contains(pathToContent["docs/index.md"], "2026-03-08T005351Z") {
		t.Fatalf("root index missing updated market section:\n%s", pathToContent["docs/index.md"])
	}
	if !strings.Contains(pathToContent["docs/reports/market/feed.xml"], "<rss version=\"2.0\"") {
		t.Fatalf("series feed invalid:\n%s", pathToContent["docs/reports/market/feed.xml"])
	}
	if !strings.Contains(pathToContent["docs/index.html"], "AI Briefings") {
		t.Fatalf("root HTML missing title:\n%s", pathToContent["docs/index.html"])
	}
	if commitBody["message"] != "publish: market 2026-03-08T005351Z" {
		t.Fatalf("commit message = %#v", commitBody["message"])
	}
	parents, ok := commitBody["parents"].([]any)
	if !ok || len(parents) != 1 || parents[0] != "headsha" {
		t.Fatalf("commit parents = %#v", commitBody["parents"])
	}
	if patchBody["sha"] != "commitsha" || patchBody["force"] != false {
		t.Fatalf("patch body = %#v", patchBody)
	}

	var result publishBriefingResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(stdout): %v\n%s", err, stdout.String())
	}
	if !result.OK || result.CommitSHA != "commitsha" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
