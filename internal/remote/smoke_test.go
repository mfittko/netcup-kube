package remote

import (
	"os"
	"strings"
	"testing"
)

func TestCreateSmokeEnvFile(t *testing.T) {
	tmpFile, err := createSmokeEnvFile()
	if err != nil {
		t.Fatalf("createSmokeEnvFile() error = %v", err)
	}
	defer os.Remove(tmpFile)

	// Check that file exists
	if _, err := os.Stat(tmpFile); err != nil {
		t.Errorf("Temp file should exist: %v", err)
	}

	// Read and verify content
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}

	contentStr := string(content)

	// Check for required environment variables
	requiredVars := []string{
		"DRY_RUN=true",
		"DRY_RUN_WRITE_FILES=false",
		"ENABLE_UFW=false",
		"EDGE_PROXY=none",
		"DASH_ENABLE=false",
		"CONFIRM=true",
	}

	for _, v := range requiredVars {
		if !strings.Contains(contentStr, v) {
			t.Errorf("Smoke env file should contain: %s", v)
		}
	}
}

func TestCreateSmokeJoinEnvFile(t *testing.T) {
	tmpFile, err := createSmokeJoinEnvFile()
	if err != nil {
		t.Fatalf("createSmokeJoinEnvFile() error = %v", err)
	}
	defer os.Remove(tmpFile)

	// Check that file exists
	if _, err := os.Stat(tmpFile); err != nil {
		t.Errorf("Temp file should exist: %v", err)
	}

	// Read and verify content
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}

	contentStr := string(content)

	// Check for required environment variables
	requiredVars := []string{
		"DRY_RUN=true",
		"DRY_RUN_WRITE_FILES=false",
		"ENABLE_UFW=false",
		"EDGE_PROXY=none",
		"DASH_ENABLE=false",
		"CONFIRM=true",
		"SERVER_URL=https://1.2.3.4:6443",
		"TOKEN=dummytoken",
	}

	for _, v := range requiredVars {
		if !strings.Contains(contentStr, v) {
			t.Errorf("Smoke join env file should contain: %s", v)
		}
	}
}

func TestSmoke_InvalidConfig(t *testing.T) {
	cfg := NewConfig()
	// Don't set host - should fail
	opts := GitOptions{}

	err := Smoke(cfg, opts, "/tmp")
	if err == nil {
		t.Error("Smoke should fail with missing host")
	}
}
