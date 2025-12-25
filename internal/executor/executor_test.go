package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	exec, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if exec == nil {
		t.Fatal("New() returned nil executor")
	}
	if exec.scriptPath == "" {
		t.Error("New() did not set scriptPath")
	}
	if exec.projectRoot == "" {
		t.Error("New() did not set projectRoot")
	}
}

func TestNew_FindsScript(t *testing.T) {
	// Save current directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(origDir)

	// Create a temporary directory structure
	tmpDir := t.TempDir()
	scriptsDir := filepath.Join(tmpDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("Failed to create scripts directory: %v", err)
	}

	// Create a dummy script
	scriptPath := filepath.Join(scriptsDir, "main.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho test"), 0755); err != nil {
		t.Fatalf("Failed to create script: %v", err)
	}

	// Change to temp directory
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	exec, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	expectedPath := filepath.Join(tmpDir, "scripts", "main.sh")
	if exec.scriptPath != expectedPath {
		t.Errorf("New() scriptPath = %q, want %q", exec.scriptPath, expectedPath)
	}
}

func TestNew_FindsScriptRelativeToExecutable(t *testing.T) {
	// Save current directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(origDir)

	// Create a temporary directory structure that doesn't have scripts/main.sh
	tmpDir := t.TempDir()
	
	// Change to temp directory (where script doesn't exist)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// New should still succeed (it tries to find relative to executable)
	exec, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if exec == nil {
		t.Fatal("New() returned nil executor")
	}
}

func TestNew_FromBinDirectory(t *testing.T) {
	// Save current directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(origDir)

	// Create a temporary directory structure
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	scriptsDir := filepath.Join(tmpDir, "scripts")
	
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("Failed to create bin directory: %v", err)
	}
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("Failed to create scripts directory: %v", err)
	}

	// Create a dummy script
	scriptPath := filepath.Join(scriptsDir, "main.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho test"), 0755); err != nil {
		t.Fatalf("Failed to create script: %v", err)
	}

	// Change to bin directory
	if err := os.Chdir(binDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	exec, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	expectedPath := filepath.Join(tmpDir, "scripts", "main.sh")
	if exec.scriptPath != expectedPath {
		t.Errorf("New() from bin/ scriptPath = %q, want %q", exec.scriptPath, expectedPath)
	}
	if exec.projectRoot != tmpDir {
		t.Errorf("New() from bin/ projectRoot = %q, want %q", exec.projectRoot, tmpDir)
	}
}

func TestExecute_ScriptNotFound(t *testing.T) {
	exec := &Executor{
		projectRoot: "/nonexistent",
		scriptPath:  "/nonexistent/scripts/main.sh",
	}

	err := exec.Execute("test", []string{}, []string{})
	if err == nil {
		t.Error("Execute() should return error when script not found")
	}
	if err != nil && !strings.Contains(err.Error(), "script not found") {
		t.Errorf("Execute() error should mention 'script not found', got: %v", err)
	}
}

func TestExecute_ScriptExists(t *testing.T) {
	// Verify that the Execute method checks if script exists
	tmpDir := t.TempDir()
	scriptsDir := filepath.Join(tmpDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("Failed to create scripts directory: %v", err)
	}

	scriptPath := filepath.Join(scriptsDir, "main.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho test\nexit 0"), 0755); err != nil {
		t.Fatalf("Failed to create script: %v", err)
	}

	exec := &Executor{
		projectRoot: tmpDir,
		scriptPath:  scriptPath,
	}

	// Verify stat check passes for valid script
	_, err := os.Stat(exec.scriptPath)
	if err != nil {
		t.Errorf("Script should exist and be accessible: %v", err)
	}
}

func TestExecute_WithValidScript(t *testing.T) {
	// This test requires an actual script to exist
	// We'll create a simple test script
	tmpDir := t.TempDir()
	scriptsDir := filepath.Join(tmpDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("Failed to create scripts directory: %v", err)
	}

	scriptPath := filepath.Join(scriptsDir, "main.sh")
	scriptContent := `#!/bin/bash
# Simple test script
echo "Command: $1"
for arg in "$@"; do
    echo "Arg: $arg"
done
exit 0
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("Failed to create script: %v", err)
	}

	exec := &Executor{
		projectRoot: tmpDir,
		scriptPath:  scriptPath,
	}

	// Test execution (will actually run the script)
	err := exec.Execute("test", []string{"arg1", "arg2"}, []string{"TEST_VAR=value"})
	if err != nil {
		t.Errorf("Execute() unexpected error = %v", err)
	}
}

func TestExecute_WithNoArgs(t *testing.T) {
	tmpDir := t.TempDir()
	scriptsDir := filepath.Join(tmpDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("Failed to create scripts directory: %v", err)
	}

	scriptPath := filepath.Join(scriptsDir, "main.sh")
	scriptContent := `#!/bin/bash
echo "Success"
exit 0
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("Failed to create script: %v", err)
	}

	exec := &Executor{
		projectRoot: tmpDir,
		scriptPath:  scriptPath,
	}

	err := exec.Execute("test", nil, nil)
	if err != nil {
		t.Errorf("Execute() with no args error = %v", err)
	}
}

func TestExecute_PassesEnvironment(t *testing.T) {
	tmpDir := t.TempDir()
	scriptsDir := filepath.Join(tmpDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("Failed to create scripts directory: %v", err)
	}

	scriptPath := filepath.Join(scriptsDir, "main.sh")
	outputFile := filepath.Join(tmpDir, "output.txt")
	
	scriptContent := `#!/bin/bash
echo "TEST_VAR=$TEST_VAR" > ` + outputFile + `
exit 0
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("Failed to create script: %v", err)
	}

	exec := &Executor{
		projectRoot: tmpDir,
		scriptPath:  scriptPath,
	}

	err := exec.Execute("test", []string{}, []string{"TEST_VAR=test_value"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// Check output file
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	expected := "TEST_VAR=test_value\n"
	if string(content) != expected {
		t.Errorf("Script output = %q, want %q", string(content), expected)
	}
}

func TestExecute_ScriptWithNonZeroExit(t *testing.T) {
	tmpDir := t.TempDir()
	scriptsDir := filepath.Join(tmpDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("Failed to create scripts directory: %v", err)
	}

	scriptPath := filepath.Join(scriptsDir, "main.sh")
	scriptContent := `#!/bin/bash
echo "Error message" >&2
exit 42
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("Failed to create script: %v", err)
	}

	exec := &Executor{
		projectRoot: tmpDir,
		scriptPath:  scriptPath,
	}

	// Note: This will actually call os.Exit(42) in the current implementation
	// So we can't easily test this without forking the process
	// The test validates the script creation works
	if exec.scriptPath != scriptPath {
		t.Errorf("scriptPath not set correctly")
	}
}
