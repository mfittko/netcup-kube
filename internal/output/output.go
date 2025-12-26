package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Format represents the output format
type Format string

const (
	// FormatText is the default human-readable text format
	FormatText Format = "text"
	// FormatJSON is machine-readable JSON format
	FormatJSON Format = "json"
)

// ParseFormat parses a format string and validates it
func ParseFormat(s string) (Format, error) {
	format := Format(s)
	switch format {
	case FormatText, FormatJSON:
		return format, nil
	default:
		return FormatText, fmt.Errorf("invalid output format: %q (must be 'text' or 'json')", s)
	}
}

// Formatter handles outputting data in different formats
type Formatter struct {
	format Format
	writer io.Writer
}

// New creates a new Formatter with the specified format
func New(format Format) *Formatter {
	return &Formatter{
		format: format,
		writer: os.Stdout,
	}
}

// SetWriter sets the output writer (useful for testing)
func (f *Formatter) SetWriter(w io.Writer) {
	f.writer = w
}

// Result represents a command result that can be output in different formats
type Result struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message,omitempty"`
	Error   string                 `json:"error,omitempty"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

// Print outputs a result in the configured format
func (f *Formatter) Print(result *Result) error {
	switch f.format {
	case FormatJSON:
		return f.printJSON(result)
	case FormatText:
		return f.printText(result)
	default:
		return fmt.Errorf("unsupported output format: %s", f.format)
	}
}

// printJSON outputs the result as JSON
func (f *Formatter) printJSON(result *Result) error {
	encoder := json.NewEncoder(f.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// printText outputs the result as human-readable text
func (f *Formatter) printText(result *Result) error {
	if !result.Success {
		if result.Error != "" {
			_, err := fmt.Fprintf(f.writer, "Error: %s\n", result.Error)
			return err
		}
		_, err := fmt.Fprintln(f.writer, "Command failed")
		return err
	}

	if result.Message != "" {
		_, err := fmt.Fprintln(f.writer, result.Message)
		return err
	}

	// Print data as key-value pairs
	if len(result.Data) > 0 {
		for k, v := range result.Data {
			_, err := fmt.Fprintf(f.writer, "%s: %v\n", k, v)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// ValidationError represents a validation error in structured format
type ValidationError struct {
	Field       string `json:"field"`
	Value       string `json:"value,omitempty"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

// ValidationResult represents validation results
type ValidationResult struct {
	Valid  bool               `json:"valid"`
	Errors []ValidationError  `json:"errors,omitempty"`
}

// PrintValidation outputs validation results
func (f *Formatter) PrintValidation(result *ValidationResult) error {
	switch f.format {
	case FormatJSON:
		encoder := json.NewEncoder(f.writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	case FormatText:
		if result.Valid {
			_, err := fmt.Fprintln(f.writer, "Validation passed")
			return err
		}
		_, err := fmt.Fprintln(f.writer, "Validation failed:")
		if err != nil {
			return err
		}
		for _, e := range result.Errors {
			_, err = fmt.Fprintf(f.writer, "  - %s: %s\n", e.Field, e.Message)
			if err != nil {
				return err
			}
			if e.Remediation != "" {
				_, err = fmt.Fprintf(f.writer, "    Remediation: %s\n", e.Remediation)
				if err != nil {
					return err
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported output format: %s", f.format)
	}
}
