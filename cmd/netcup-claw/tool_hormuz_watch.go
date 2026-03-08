package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

const (
	hormuzWatchURL         = "wss://stream.aisstream.io/v0/stream"
	defaultHormuzStateFile = "./state/hormuz-ais-watch/seen_vessels.json"
)

var hormuzRandIntn = rand.Intn

type hormuzWatchConfig struct {
	APIKey             string
	LatMin             float64
	LatMax             float64
	LonMin             float64
	LonMax             float64
	MinSOG             float64
	WindowSeconds      int
	RetryMaxAttempts   int
	RetryBaseMS        int
	RetryMaxMS         int
	WSConnectTimeoutMS int
	StateFile          string
	NoDedupe           bool
}

type aisStreamEnvelope struct {
	MessageType string `json:"MessageType"`
	MetaData    struct {
		MMSI     interface{} `json:"MMSI"`
		ShipName string      `json:"ShipName"`
		Name     string      `json:"NAME"`
	} `json:"MetaData"`
	Message struct {
		PositionReport               *aisPositionPayload `json:"PositionReport"`
		StandardClassBPositionReport *aisPositionPayload `json:"StandardClassBPositionReport"`
		ExtendedClassBPositionReport *aisPositionPayload `json:"ExtendedClassBPositionReport"`
	} `json:"Message"`
}

type aisPositionPayload struct {
	Latitude  float64 `json:"Latitude"`
	Longitude float64 `json:"Longitude"`
	SOG       float64 `json:"Sog"`
}

type hormuzPositionReport struct {
	MMSI string
	Name string
	Lat  float64
	Lon  float64
	SOG  float64
}

type hormuzAlert struct {
	MMSI string
	Text string
}

var hormuzWatchCmd = &cobra.Command{
	Use:   "hormuz-watch",
	Short: "Watch AISStream traffic in the Strait of Hormuz and print vessel alerts",
	Long: `Connect to AISStream over WebSocket, watch AIS position reports inside the
configured Strait of Hormuz bounding box, and print deduplicated vessel alerts.

Configuration is provided entirely via environment variables:
  AISSTREAM_API_KEY        required AISStream API key
  HORMUZ_LAT_MIN/MAX      bounding-box latitude overrides
  HORMUZ_LON_MIN/MAX      bounding-box longitude overrides
  MIN_SOG                 minimum speed-over-ground threshold (default 1.5)
  WINDOW_SECONDS          run window before exiting (default 90)
  RETRY_MAX_ATTEMPTS      max websocket attempts (default 4)
  RETRY_BASE_MS           retry backoff base (default 1000)
  RETRY_MAX_MS            retry backoff cap (default 8000)
  WS_CONNECT_TIMEOUT_MS   websocket connect timeout (default 15000)
  STATE_FILE              dedupe state file (default ./state/hormuz-ais-watch/seen_vessels.json)
  NO_DEDUPE=1             disable dedupe state`,
	RunE: runHormuzWatch,
}

func runHormuzWatch(_ *cobra.Command, _ []string) error {
	cfg, err := parseHormuzWatchConfig()
	if err != nil {
		return err
	}
	return runHormuzWatchWindow(context.Background(), cfg, os.Stdout, os.Stderr)
}

func runHormuzWatchWindow(ctx context.Context, cfg hormuzWatchConfig, stdout, stderr io.Writer) error {
	seen := map[string]struct{}{}
	if !cfg.NoDedupe {
		seen = loadSeenMMSI(cfg.StateFile)
	}

	alerts := make([]hormuzAlert, 0)
	deadline := time.Now().Add(time.Duration(cfg.WindowSeconds) * time.Second)

	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		err := runHormuzWatchAttempt(ctx, cfg, seen, &alerts, deadline)
		if err == nil {
			break
		}

		retryable := isRetryableWebSocketError(err)
		retriesLeft := attempt < cfg.RetryMaxAttempts
		timeLeft := time.Until(deadline)
		if !retryable || !retriesLeft || timeLeft <= 0 {
			if len(alerts) == 0 {
				_, _ = fmt.Fprintf(stderr, "hormuz_watch: websocket ended after %d attempt(s): %s\n", attempt, err.Error())
			}
			break
		}

		wait := time.Duration(backoffDelayMs(cfg, attempt)) * time.Millisecond
		if wait > timeLeft {
			wait = timeLeft
		}
		_, _ = fmt.Fprintf(stderr, "hormuz_watch: websocket attempt %d failed (%s), retrying in %dms\n", attempt, err.Error(), wait/time.Millisecond)
		if err := sleepContext(ctx, wait); err != nil {
			return err
		}
	}

	for _, alert := range alerts {
		if _, err := fmt.Fprintf(stdout, "%s\n---\n", alert.Text); err != nil {
			return err
		}
	}

	if len(alerts) > 0 && !cfg.NoDedupe {
		return saveSeenMMSI(cfg.StateFile, seen)
	}
	return nil
}

func runHormuzWatchAttempt(ctx context.Context, cfg hormuzWatchConfig, seen map[string]struct{}, alerts *[]hormuzAlert, deadline time.Time) error {
	connectTimeout := time.Duration(cfg.WSConnectTimeoutMS) * time.Millisecond
	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	conn, _, err := websocket.Dial(connectCtx, hormuzWatchURL, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	subscribe := map[string]interface{}{
		"APIKey": cfg.APIKey,
		"BoundingBoxes": [][][]float64{
			{
				{cfg.LatMin, cfg.LonMin},
				{cfg.LatMax, cfg.LonMax},
			},
		},
		"FilterMessageTypes": []string{
			"PositionReport",
			"StandardClassBPositionReport",
			"ExtendedClassBPositionReport",
		},
	}

	writeCtx, writeCancel := context.WithTimeout(ctx, connectTimeout)
	defer writeCancel()
	if err := wsjson.Write(writeCtx, conn, subscribe); err != nil {
		return err
	}

	for {
		if time.Now().After(deadline) {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return nil
		}

		readCtx, readCancel := context.WithDeadline(ctx, deadline)
		var msg aisStreamEnvelope
		err := wsjson.Read(readCtx, conn, &msg)
		readCancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return nil
			}
			if websocket.CloseStatus(err) != -1 {
				return nil
			}
			return err
		}

		report := parsePositionReport(msg)
		if report == nil || report.SOG < cfg.MinSOG || !inBox(cfg, report.Lat, report.Lon) {
			continue
		}
		if !cfg.NoDedupe {
			if _, ok := seen[report.MMSI]; ok {
				continue
			}
			seen[report.MMSI] = struct{}{}
		}

		*alerts = append(*alerts, hormuzAlert{
			MMSI: report.MMSI,
			Text: strings.Join([]string{
				"AIS Hormuz match",
				"Name: " + report.Name,
				"MMSI: " + report.MMSI,
				fmt.Sprintf("SOG: %.1f kn", report.SOG),
				fmt.Sprintf("Pos: %v, %v", report.Lat, report.Lon),
				"Time: " + nowUTCMinute(),
				"Track: https://www.marinetraffic.com/en/ais/details/ships/mmsi:" + report.MMSI,
			}, "\n"),
		})
	}
}

func parseHormuzWatchConfig() (hormuzWatchConfig, error) {
	apiKey := strings.TrimSpace(os.Getenv("AISSTREAM_API_KEY"))
	if apiKey == "" {
		return hormuzWatchConfig{}, errors.New("AISSTREAM_API_KEY is required (set env var)")
	}

	cfg := hormuzWatchConfig{
		APIKey:             apiKey,
		LatMin:             25.5,
		LatMax:             27.5,
		LonMin:             56.0,
		LonMax:             57.5,
		MinSOG:             1.5,
		WindowSeconds:      90,
		RetryMaxAttempts:   4,
		RetryBaseMS:        1000,
		RetryMaxMS:         8000,
		WSConnectTimeoutMS: 15000,
		StateFile:          defaultHormuzStateFile,
		NoDedupe:           strings.TrimSpace(os.Getenv("NO_DEDUPE")) == "1",
	}

	var err error
	if cfg.LatMin, err = getenvFloat64("HORMUZ_LAT_MIN", cfg.LatMin); err != nil {
		return hormuzWatchConfig{}, err
	}
	if cfg.LatMax, err = getenvFloat64("HORMUZ_LAT_MAX", cfg.LatMax); err != nil {
		return hormuzWatchConfig{}, err
	}
	if cfg.LonMin, err = getenvFloat64("HORMUZ_LON_MIN", cfg.LonMin); err != nil {
		return hormuzWatchConfig{}, err
	}
	if cfg.LonMax, err = getenvFloat64("HORMUZ_LON_MAX", cfg.LonMax); err != nil {
		return hormuzWatchConfig{}, err
	}
	if cfg.MinSOG, err = getenvFloat64("MIN_SOG", cfg.MinSOG); err != nil {
		return hormuzWatchConfig{}, err
	}
	if cfg.WindowSeconds, err = getenvInt("WINDOW_SECONDS", cfg.WindowSeconds); err != nil {
		return hormuzWatchConfig{}, err
	}
	if cfg.RetryMaxAttempts, err = getenvInt("RETRY_MAX_ATTEMPTS", cfg.RetryMaxAttempts); err != nil {
		return hormuzWatchConfig{}, err
	}
	if cfg.RetryBaseMS, err = getenvInt("RETRY_BASE_MS", cfg.RetryBaseMS); err != nil {
		return hormuzWatchConfig{}, err
	}
	if cfg.RetryMaxMS, err = getenvInt("RETRY_MAX_MS", cfg.RetryMaxMS); err != nil {
		return hormuzWatchConfig{}, err
	}
	if cfg.WSConnectTimeoutMS, err = getenvInt("WS_CONNECT_TIMEOUT_MS", cfg.WSConnectTimeoutMS); err != nil {
		return hormuzWatchConfig{}, err
	}
	if stateFile := strings.TrimSpace(os.Getenv("STATE_FILE")); stateFile != "" {
		cfg.StateFile = stateFile
	}

	return cfg, nil
}

func getenvFloat64(name string, def float64) (float64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("%s must be a finite number", name)
	}
	return v, nil
}

func getenvInt(name string, def int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return v, nil
}

func parsePositionReport(msg aisStreamEnvelope) *hormuzPositionReport {
	switch msg.MessageType {
	case "PositionReport", "StandardClassBPositionReport", "ExtendedClassBPositionReport":
	default:
		return nil
	}

	mmsi := normalizeMMSI(msg.MetaData.MMSI)
	if mmsi == "" {
		return nil
	}

	var payload *aisPositionPayload
	switch msg.MessageType {
	case "PositionReport":
		payload = msg.Message.PositionReport
	case "StandardClassBPositionReport":
		payload = msg.Message.StandardClassBPositionReport
	case "ExtendedClassBPositionReport":
		payload = msg.Message.ExtendedClassBPositionReport
	}
	if payload == nil || !isFinite(payload.Latitude) || !isFinite(payload.Longitude) {
		return nil
	}

	name := strings.TrimSpace(msg.MetaData.ShipName)
	if name == "" {
		name = strings.TrimSpace(msg.MetaData.Name)
	}
	if name == "" {
		name = "UNKNOWN"
	}

	return &hormuzPositionReport{
		MMSI: mmsi,
		Name: name,
		Lat:  payload.Latitude,
		Lon:  payload.Longitude,
		SOG:  payload.SOG,
	}
}

func normalizeMMSI(v interface{}) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return strings.TrimSpace(value.String())
	case float64:
		if !isFinite(value) {
			return ""
		}
		if value == math.Trunc(value) {
			return strconv.FormatInt(int64(value), 10)
		}
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func inBox(cfg hormuzWatchConfig, lat, lon float64) bool {
	return cfg.LatMin <= lat && lat <= cfg.LatMax && cfg.LonMin <= lon && lon <= cfg.LonMax
}

func backoffDelayMs(cfg hormuzWatchConfig, attempt int) int {
	if attempt < 1 {
		attempt = 1
	}
	expo := cfg.RetryBaseMS << (attempt - 1)
	jitterMax := maxInt(250, cfg.RetryBaseMS/2)
	jitter := 0
	if jitterMax > 0 {
		jitter = hormuzRandIntn(jitterMax)
	}
	return minInt(cfg.RetryMaxMS, expo+jitter)
}

func isRetryableWebSocketError(err error) bool {
	text := strings.ToLower(strings.TrimSpace(errString(err)))
	if text == "" {
		return true
	}
	for _, token := range []string{
		"abort",
		"aborted",
		"closed before it was established",
		"closed before stream opened",
		"closed before",
		"network",
		"socket",
		"timed out",
		"timeout",
		"econn",
	} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func loadSeenMMSI(stateFile string) map[string]struct{} {
	seen := map[string]struct{}{}

	raw, err := os.ReadFile(stateFile)
	if err != nil {
		return seen
	}

	var items []string
	if err := json.Unmarshal(raw, &items); err != nil {
		return seen
	}
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	return seen
}

func saveSeenMMSI(stateFile string, seen map[string]struct{}) error {
	if err := os.MkdirAll(filepath.Dir(stateFile), 0o755); err != nil {
		return err
	}

	items := make([]string, 0, len(seen))
	for mmsi := range seen {
		items = append(items, mmsi)
	}
	sort.Strings(items)

	data, err := json.Marshal(items)
	if err != nil {
		return err
	}

	tmp := stateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, stateFile)
}

func nowUTCMinute() string {
	return time.Now().UTC().Format("2006-01-02 15:04 UTC")
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
