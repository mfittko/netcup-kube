package executor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Executor handles execution of the shell scripts
type Executor struct {
	projectRoot string
	scriptPath  string
}

// New creates a new Executor instance
func New() (*Executor, error) {
	// Find the project root (where scripts/ directory is located)
	// Try current directory first, then walk up
	currentDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}
	
	// If we're running from bin/, go up one level
	if filepath.Base(currentDir) == "bin" {
		currentDir = filepath.Dir(currentDir)
	}
	
	// Check if scripts/main.sh exists
	scriptPath := filepath.Join(currentDir, "scripts", "main.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		// Try to find it relative to the executable
		exe, err := os.Executable()
		if err == nil {
			exeDir := filepath.Dir(exe)
			projectRoot := filepath.Dir(exeDir)
			scriptPath = filepath.Join(projectRoot, "scripts", "main.sh")
			if _, err := os.Stat(scriptPath); err == nil {
				currentDir = projectRoot
			}
		}
	}
	
	return &Executor{
		projectRoot: currentDir,
		scriptPath:  scriptPath,
	}, nil
}

// Execute runs a command by delegating to scripts/main.sh
func (e *Executor) Execute(command string, args []string, env []string) error {
	// Validate that the script exists and is accessible
	if _, err := os.Stat(e.scriptPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("script not found: %s (ensure you're running from the project directory or the binary is in the correct location)", e.scriptPath)
		}
		return fmt.Errorf("cannot access script %s: %w", e.scriptPath, err)
	}
	
	// Build the command
	cmd := exec.Command("bash", e.scriptPath, command)
	
	// Add any additional arguments
	if len(args) > 0 {
		cmd.Args = append(cmd.Args, args...)
	}
	
	// Set environment
	cmd.Env = append(os.Environ(), env...)
	
	// Connect stdio
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	// Run the command
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Preserve the exit code from the script
			os.Exit(exitErr.ExitCode())
		}
		// For other types of errors, return them
		return fmt.Errorf("failed to execute command: %w", err)
	}
	
	return nil
}
