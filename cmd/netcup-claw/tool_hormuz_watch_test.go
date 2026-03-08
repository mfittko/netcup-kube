package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParsePositionReport_PositionReport(t *testing.T) {
	var msg aisStreamEnvelope
	raw := []byte(`{
		"MessageType":"PositionReport",
		"MetaData":{"MMSI":123456789,"ShipName":"PACIFIC TRADER"},
		"Message":{"PositionReport":{"Latitude":26.5,"Longitude":56.9,"Sog":12.4}}
	}`)
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	report := parsePositionReport(msg)
	if report == nil {
		t.Fatal("expected report")
	}
	if report.MMSI != "123456789" {
		t.Fatalf("MMSI = %q, want 123456789", report.MMSI)
	}
	if report.Name != "PACIFIC TRADER" {
		t.Fatalf("Name = %q, want PACIFIC TRADER", report.Name)
	}
	if report.Lat != 26.5 || report.Lon != 56.9 || report.SOG != 12.4 {
		t.Fatalf("unexpected report: %+v", *report)
	}
}

func TestParsePositionReport_ClassBVariants(t *testing.T) {
	tests := []struct {
		name        string
		messageType string
		payloadKey  string
		wantName    string
	}{
		{
			name:        "standard class b",
			messageType: "StandardClassBPositionReport",
			payloadKey:  "StandardClassBPositionReport",
			wantName:    "CLASS B ONE",
		},
		{
			name:        "extended class b falls back to NAME",
			messageType: "ExtendedClassBPositionReport",
			payloadKey:  "ExtendedClassBPositionReport",
			wantName:    "CLASS B TWO",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var msg aisStreamEnvelope
			raw := []byte(`{
				"MessageType":"` + tc.messageType + `",
				"MetaData":{"MMSI":"987654321","NAME":"` + tc.wantName + `"},
				"Message":{"` + tc.payloadKey + `":{"Latitude":27.0,"Longitude":57.2,"Sog":7.1}}
			}`)
			if err := json.Unmarshal(raw, &msg); err != nil {
				t.Fatalf("unexpected unmarshal error: %v", err)
			}

			report := parsePositionReport(msg)
			if report == nil {
				t.Fatal("expected report")
			}
			if report.Name != tc.wantName {
				t.Fatalf("Name = %q, want %q", report.Name, tc.wantName)
			}
		})
	}
}

func TestParsePositionReport_InvalidCases(t *testing.T) {
	tests := []aisStreamEnvelope{
		{MessageType: "StaticDataReport"},
		{
			MessageType: "PositionReport",
			MetaData: struct {
				MMSI     interface{} `json:"MMSI"`
				ShipName string      `json:"ShipName"`
				Name     string      `json:"NAME"`
			}{},
		},
		{
			MessageType: "PositionReport",
			MetaData: struct {
				MMSI     interface{} `json:"MMSI"`
				ShipName string      `json:"ShipName"`
				Name     string      `json:"NAME"`
			}{MMSI: "123"},
		},
	}

	for i, msg := range tests {
		if report := parsePositionReport(msg); report != nil {
			t.Fatalf("case %d: expected nil report, got %+v", i, *report)
		}
	}
}

func TestInBox(t *testing.T) {
	cfg := hormuzWatchConfig{LatMin: 25.5, LatMax: 27.5, LonMin: 56.0, LonMax: 57.5}

	if !inBox(cfg, 25.5, 56.0) {
		t.Fatal("expected lower bound to be included")
	}
	if !inBox(cfg, 27.5, 57.5) {
		t.Fatal("expected upper bound to be included")
	}
	if inBox(cfg, 25.49, 56.0) {
		t.Fatal("expected latitude below bound to be excluded")
	}
	if inBox(cfg, 26.0, 57.5001) {
		t.Fatal("expected longitude above bound to be excluded")
	}
}

func TestBackoffDelayMs(t *testing.T) {
	oldRand := hormuzRandIntn
	hormuzRandIntn = func(max int) int {
		if max != 500 {
			t.Fatalf("max jitter = %d, want 500", max)
		}
		return 123
	}
	defer func() {
		hormuzRandIntn = oldRand
	}()

	cfg := hormuzWatchConfig{RetryBaseMS: 1000, RetryMaxMS: 8000}
	if got := backoffDelayMs(cfg, 1); got != 1123 {
		t.Fatalf("attempt 1 delay = %d, want 1123", got)
	}
	if got := backoffDelayMs(cfg, 3); got != 4123 {
		t.Fatalf("attempt 3 delay = %d, want 4123", got)
	}
	if got := backoffDelayMs(cfg, 5); got != 8000 {
		t.Fatalf("attempt 5 delay = %d, want capped 8000", got)
	}
}

func TestLoadAndSaveSeenMMSI(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state", "seen_vessels.json")
	seen := map[string]struct{}{
		"333333333": {},
		"111111111": {},
		"222222222": {},
	}

	if err := saveSeenMMSI(stateFile, seen); err != nil {
		t.Fatalf("saveSeenMMSI: %v", err)
	}

	raw, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}

	var gotItems []string
	if err := json.Unmarshal(raw, &gotItems); err != nil {
		t.Fatalf("unmarshal state file: %v", err)
	}
	wantItems := []string{"111111111", "222222222", "333333333"}
	if !reflect.DeepEqual(gotItems, wantItems) {
		t.Fatalf("saved items = %v, want %v", gotItems, wantItems)
	}

	gotSeen := loadSeenMMSI(stateFile)
	if !reflect.DeepEqual(gotSeen, seen) {
		t.Fatalf("loaded seen map = %v, want %v", gotSeen, seen)
	}
}

func TestParseHormuzWatchConfig_StateFileOverride(t *testing.T) {
	t.Setenv("AISSTREAM_API_KEY", "test-key")
	t.Setenv("STATE_FILE", "/tmp/custom-seen.json")

	cfg, err := parseHormuzWatchConfig()
	if err != nil {
		t.Fatalf("parseHormuzWatchConfig: %v", err)
	}
	if cfg.StateFile != "/tmp/custom-seen.json" {
		t.Fatalf("StateFile = %q, want /tmp/custom-seen.json", cfg.StateFile)
	}
}

func TestParseHormuzWatchConfig_Defaults(t *testing.T) {
	t.Setenv("AISSTREAM_API_KEY", "test-key")
	t.Setenv("STATE_FILE", "")
	t.Setenv("NO_DEDUPE", "")

	cfg, err := parseHormuzWatchConfig()
	if err != nil {
		t.Fatalf("parseHormuzWatchConfig: %v", err)
	}
	if cfg.StateFile != defaultHormuzStateFile {
		t.Fatalf("StateFile = %q, want %q", cfg.StateFile, defaultHormuzStateFile)
	}
	if cfg.MinSOG != 1.5 || cfg.WindowSeconds != 90 || cfg.RetryMaxAttempts != 4 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}
