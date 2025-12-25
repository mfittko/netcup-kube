package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Config holds the configuration for netcup-kube commands
type Config struct {
	// Environment variables to pass to scripts
	Env map[string]string
}

// New creates a new Config instance
func New() *Config {
	return &Config{
		Env: make(map[string]string),
	}
}

// LoadEnvFile loads environment variables from a file
// Returns nil if the file doesn't exist (not an error)
// NOTE: Values from env files are considered trusted. Ensure env files come from trusted sources only.
func (c *Config) LoadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, not an error
		}
		return fmt.Errorf("failed to open env file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		
		// Parse KEY=value format
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		
		// Simple variable expansion: ${VAR} -> value of VAR
		value = c.expandVars(value)
		
		// Set value, overriding any existing values (env-file has higher priority than process env)
		c.Env[key] = value
	}

	return scanner.Err()
}

// LoadFromEnvironment loads environment variables from the current process
// This includes all system environment variables. They are passed through to scripts,
// which may rely on variables like PATH, HOME, USER, etc.
func (c *Config) LoadFromEnvironment() {
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		
		key := parts[0]
		value := parts[1]
		
		// Only set if not already set; allows later config sources to override
		if _, exists := c.Env[key]; !exists {
			c.Env[key] = value
		}
	}
}

// SetFromFlags sets configuration values from command-line flags
func (c *Config) SetFromFlags(key, value string) {
	if value != "" {
		c.Env[key] = value
	}
}

// SetFlag sets a configuration flag (overrides anything else)
func (c *Config) SetFlag(key, value string) {
	c.Env[key] = value
}

// expandVars performs simple variable expansion for ${VAR} syntax.
// NOTE: This performs single-pass expansion only. Variables are not recursively expanded.
// For example, if VAR1="${VAR2}" and VAR2="value", VAR1 will expand to "${VAR2}", not "value".
func (c *Config) expandVars(value string) string {
	var result strings.Builder
	result.Grow(len(value)) // Pre-allocate capacity
	
	pos := 0
	iterations := 0
	maxIterations := 100 // Safety limit to prevent bugs in loop logic from causing hangs
	
	// Simple expansion: ${VAR}
	for pos < len(value) && iterations < maxIterations {
		iterations++
		
		start := strings.Index(value[pos:], "${")
		if start == -1 {
			// No more variables, append the rest
			result.WriteString(value[pos:])
			break
		}
		start += pos
		
		// Append text before the variable
		result.WriteString(value[pos:start])
		
		end := strings.Index(value[start+2:], "}")
		if end == -1 {
			// Malformed variable reference, append as-is and continue
			result.WriteString(value[start:])
			break
		}
		end += start + 2
		
		varName := value[start+2 : end]
		
		// Look up the variable value
		varValue := ""
		if val, exists := c.Env[varName]; exists {
			varValue = val
		} else if val, ok := os.LookupEnv(varName); ok {
			varValue = val
		}
		
		result.WriteString(varValue)
		pos = end + 1
	}
	
	// If we hit the maximum iteration limit before processing the entire value,
	// log a warning and append the remaining text without further expansion.
	if iterations >= maxIterations && pos < len(value) {
		fmt.Fprintf(os.Stderr, "config: variable expansion exceeded max iterations; returning partially expanded result\n")
		result.WriteString(value[pos:])
	}
	
	return result.String()
}

// ToEnvSlice converts the config to a slice of "KEY=value" strings
func (c *Config) ToEnvSlice() []string {
	env := make([]string, 0, len(c.Env))
	for k, v := range c.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}
