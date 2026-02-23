package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildShellRunKubectlArgs(t *testing.T) {
	got := buildShellRunKubectlArgs("openclaw", "openclaw-abc", []string{"echo", "hello", "&&", "id"})
	want := []string{
		"-n", "openclaw",
		"exec",
		"-c", "main",
		"openclaw-abc",
		"--",
		"sh",
		"-lc",
		"echo hello && id",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildShellRunKubectlArgs() = %v, want %v", got, want)
	}
}

func TestBuildOpenClawCLIKubectlArgs(t *testing.T) {
	got := buildOpenClawCLIKubectlArgs("openclaw", "openclaw-abc", []string{"status"})
	want := []string{
		"-n", "openclaw",
		"exec",
		"-c", "main",
		"openclaw-abc",
		"--",
		"node",
		"--no-warnings",
		"/app/openclaw.mjs",
		"status",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildOpenClawCLIKubectlArgs() = %v, want %v", got, want)
	}
}

func TestChartVersionFromChart(t *testing.T) {
	tests := []struct {
		chart string
		want  string
	}{
		{"openclaw-1.3.18", "1.3.18"},
		{"openclaw-1.3.21", "1.3.21"},
		{"myrelease-0.1.0", "0.1.0"},
		{"noversion", "noversion"},
	}
	for _, tc := range tests {
		got := chartVersionFromChart(tc.chart)
		if got != tc.want {
			t.Errorf("chartVersionFromChart(%q) = %q, want %q", tc.chart, got, tc.want)
		}
	}
}

func TestUpdateRecipesConfPinAt(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "recipes.conf")

	original := "# Helm Chart Versions\nCHART_VERSION_OPENCLAW=1.3.18\nCHART_VERSION_METORO_EXPORTER=0.469.0\n"
	if err := os.WriteFile(confPath, []byte(original), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := updateRecipesConfPinAt(confPath, "1.3.21"); err != nil {
		t.Fatalf("updateRecipesConfPinAt: %v", err)
	}

	got, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	want := "# Helm Chart Versions\nCHART_VERSION_OPENCLAW=1.3.21\nCHART_VERSION_METORO_EXPORTER=0.469.0\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpdateRecipesConfPinAt_MissingKey(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "recipes.conf")

	if err := os.WriteFile(confPath, []byte("# empty\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := updateRecipesConfPinAt(confPath, "1.3.21")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
