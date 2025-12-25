package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Config holds the configuration for netcup-kube commands
type Config struct {
	// Common flags
	DryRun          bool
	DryRunWriteFiles bool
	
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
		
		// Only set if not already set (precedence: flags > env > env-file)
		if _, exists := c.Env[key]; !exists {
			c.Env[key] = value
		}
	}

	return scanner.Err()
}

// LoadFromEnvironment loads environment variables from the current process
func (c *Config) LoadFromEnvironment() {
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		
		key := parts[0]
		value := parts[1]
		
		// Only set if not already set (precedence: flags > env)
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

// expandVars performs simple variable expansion for ${VAR} syntax
func (c *Config) expandVars(value string) string {
	result := value
	
	// Simple expansion: ${VAR}
	for {
		start := strings.Index(result, "${")
		if start == -1 {
			break
		}
		
		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		end += start
		
		varName := result[start+2 : end]
		
		// Look up the variable value
		varValue := ""
		if val, exists := c.Env[varName]; exists {
			varValue = val
		} else if val := os.Getenv(varName); val != "" {
			varValue = val
		}
		
		result = result[:start] + varValue + result[end+1:]
	}
	
	return result
}

// ToEnvSlice converts the config to a slice of "KEY=value" strings
func (c *Config) ToEnvSlice() []string {
	env := make([]string, 0, len(c.Env))
	for k, v := range c.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}
