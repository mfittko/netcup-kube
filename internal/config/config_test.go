package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	cfg := New()
	if cfg == nil {
		t.Fatal("New() returned nil")
	}
	if cfg.Env == nil {
		t.Fatal("New() did not initialize Env map")
	}
	if len(cfg.Env) != 0 {
		t.Errorf("New() Env map should be empty, got %d entries", len(cfg.Env))
	}
}

func TestLoadEnvFile(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		want        map[string]string
		wantErr     bool
	}{
		{
			name: "simple key-value pairs",
			fileContent: `KEY1=value1
KEY2=value2
KEY3=value3`,
			want: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
				"KEY3": "value3",
			},
			wantErr: false,
		},
		{
			name: "with comments and empty lines",
			fileContent: `# This is a comment
KEY1=value1

# Another comment
KEY2=value2
`,
			want: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
			},
			wantErr: false,
		},
		{
			name: "with whitespace",
			fileContent: `  KEY1  =  value1  
KEY2=value2`,
			want: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
			},
			wantErr: false,
		},
		{
			name: "with variable expansion",
			fileContent: `BASE=/tmp
SUBDIR=${BASE}/data`,
			want: map[string]string{
				"BASE":   "/tmp",
				"SUBDIR": "/tmp/data",
			},
			wantErr: false,
		},
		{
			name: "malformed lines are skipped",
			fileContent: `KEY1=value1
INVALID_LINE_NO_EQUALS
KEY2=value2`,
			want: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "test.env")
			if err := os.WriteFile(tmpFile, []byte(tt.fileContent), 0644); err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}

			cfg := New()
			err := cfg.LoadEnvFile(tmpFile)

			if (err != nil) != tt.wantErr {
				t.Errorf("LoadEnvFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			for key, expectedValue := range tt.want {
				if gotValue, exists := cfg.Env[key]; !exists {
					t.Errorf("Expected key %s not found in Env", key)
				} else if gotValue != expectedValue {
					t.Errorf("Key %s: got value %q, want %q", key, gotValue, expectedValue)
				}
			}

			// Check no unexpected keys
			for key := range cfg.Env {
				if _, expected := tt.want[key]; !expected {
					t.Errorf("Unexpected key %s in Env", key)
				}
			}
		})
	}
}

func TestLoadEnvFile_NonExistent(t *testing.T) {
	cfg := New()
	err := cfg.LoadEnvFile("/nonexistent/file.env")
	if err != nil {
		t.Errorf("LoadEnvFile() with non-existent file should return nil, got error: %v", err)
	}
}

func TestLoadEnvFile_Precedence(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.env")
	if err := os.WriteFile(tmpFile, []byte("KEY1=from_file\nKEY2=also_from_file"), 0644); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	cfg := New()
	cfg.Env["KEY1"] = "pre_existing"

	err := cfg.LoadEnvFile(tmpFile)
	if err != nil {
		t.Fatalf("LoadEnvFile() error = %v", err)
	}

	// Env file should override pre-existing values
	if cfg.Env["KEY1"] != "from_file" {
		t.Errorf("LoadEnvFile should override existing values, got %q, want 'from_file'", cfg.Env["KEY1"])
	}
	if cfg.Env["KEY2"] != "also_from_file" {
		t.Errorf("LoadEnvFile should add new values, got %q, want 'also_from_file'", cfg.Env["KEY2"])
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	// Set test environment variables
	os.Setenv("TEST_VAR_1", "value1")
	os.Setenv("TEST_VAR_2", "value2")
	defer os.Unsetenv("TEST_VAR_1")
	defer os.Unsetenv("TEST_VAR_2")

	cfg := New()
	cfg.LoadFromEnvironment()

	if val, exists := cfg.Env["TEST_VAR_1"]; !exists || val != "value1" {
		t.Errorf("LoadFromEnvironment() did not load TEST_VAR_1 correctly")
	}
	if val, exists := cfg.Env["TEST_VAR_2"]; !exists || val != "value2" {
		t.Errorf("LoadFromEnvironment() did not load TEST_VAR_2 correctly")
	}

	// Should also load system variables
	if _, exists := cfg.Env["PATH"]; !exists {
		t.Errorf("LoadFromEnvironment() did not load PATH")
	}
}

func TestLoadFromEnvironment_Precedence(t *testing.T) {
	os.Setenv("TEST_VAR", "from_env")
	defer os.Unsetenv("TEST_VAR")

	cfg := New()
	cfg.Env["TEST_VAR"] = "pre_existing"

	cfg.LoadFromEnvironment()

	// Pre-existing values should not be overwritten
	if cfg.Env["TEST_VAR"] != "pre_existing" {
		t.Errorf("LoadFromEnvironment should not override existing values, got %q", cfg.Env["TEST_VAR"])
	}
}

func TestSetFromFlags(t *testing.T) {
	cfg := New()
	cfg.SetFromFlags("KEY1", "value1")
	cfg.SetFromFlags("KEY2", "")

	if val := cfg.Env["KEY1"]; val != "value1" {
		t.Errorf("SetFromFlags() KEY1 = %q, want 'value1'", val)
	}
	if _, exists := cfg.Env["KEY2"]; exists {
		t.Errorf("SetFromFlags() should not set empty values")
	}
}

func TestSetFlag(t *testing.T) {
	cfg := New()
	cfg.SetFlag("KEY1", "value1")
	cfg.SetFlag("KEY2", "")

	if val := cfg.Env["KEY1"]; val != "value1" {
		t.Errorf("SetFlag() KEY1 = %q, want 'value1'", val)
	}
	if val := cfg.Env["KEY2"]; val != "" {
		t.Errorf("SetFlag() KEY2 = %q, want empty string", val)
	}
}

func TestExpandVars(t *testing.T) {
	tests := []struct {
		name     string
		envSetup map[string]string
		input    string
		want     string
	}{
		{
			name:     "simple variable",
			envSetup: map[string]string{"VAR": "value"},
			input:    "${VAR}",
			want:     "value",
		},
		{
			name:     "variable in text",
			envSetup: map[string]string{"VAR": "value"},
			input:    "prefix_${VAR}_suffix",
			want:     "prefix_value_suffix",
		},
		{
			name:     "multiple variables",
			envSetup: map[string]string{"VAR1": "value1", "VAR2": "value2"},
			input:    "${VAR1} and ${VAR2}",
			want:     "value1 and value2",
		},
		{
			name:     "undefined variable",
			envSetup: map[string]string{},
			input:    "${UNDEFINED}",
			want:     "",
		},
		{
			name:     "malformed variable (no closing brace)",
			envSetup: map[string]string{"VAR": "value"},
			input:    "${VAR",
			want:     "${VAR",
		},
		{
			name:     "no variables",
			envSetup: map[string]string{},
			input:    "plain text",
			want:     "plain text",
		},
		{
			name:     "empty input",
			envSetup: map[string]string{},
			input:    "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := New()
			for k, v := range tt.envSetup {
				cfg.Env[k] = v
			}

			got := cfg.expandVars(tt.input)
			if got != tt.want {
				t.Errorf("expandVars() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExpandVars_FromOsEnv(t *testing.T) {
	os.Setenv("TEST_EXPAND_VAR", "from_os")
	defer os.Unsetenv("TEST_EXPAND_VAR")

	cfg := New()
	got := cfg.expandVars("${TEST_EXPAND_VAR}")
	if got != "from_os" {
		t.Errorf("expandVars() should use os.Getenv, got %q, want 'from_os'", got)
	}
}

func TestExpandVars_MaxIterations(t *testing.T) {
	cfg := New()
	// Create a very long string with many variables
	var input strings.Builder
	for i := 0; i < 150; i++ {
		input.WriteString("${VAR}")
	}

	// Capture stderr to check for warning
	// Note: This is a simple test that just ensures it doesn't panic
	result := cfg.expandVars(input.String())
	
	// Should return something (not panic)
	if len(result) == 0 && input.Len() > 0 {
		t.Error("expandVars() returned empty string for non-empty input")
	}
}

func TestToEnvSlice(t *testing.T) {
	cfg := New()
	cfg.Env["KEY1"] = "value1"
	cfg.Env["KEY2"] = "value2"

	slice := cfg.ToEnvSlice()

	if len(slice) != 2 {
		t.Errorf("ToEnvSlice() returned %d entries, want 2", len(slice))
	}

	// Check that both entries are present (order may vary)
	found := make(map[string]bool)
	for _, entry := range slice {
		if entry == "KEY1=value1" {
			found["KEY1"] = true
		} else if entry == "KEY2=value2" {
			found["KEY2"] = true
		}
	}

	if !found["KEY1"] || !found["KEY2"] {
		t.Errorf("ToEnvSlice() missing expected entries, got %v", slice)
	}
}
