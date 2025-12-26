package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	formatter := New(FormatText)
	if formatter == nil {
		t.Fatal("New() returned nil")
	}
	if formatter.format != FormatText {
		t.Errorf("New() format = %v, want %v", formatter.format, FormatText)
	}
}

func TestFormatter_PrintText(t *testing.T) {
	tests := []struct {
		name    string
		result  *Result
		want    string
		wantErr bool
	}{
		{
			name: "success with message",
			result: &Result{
				Success: true,
				Message: "Operation completed successfully",
			},
			want:    "Operation completed successfully\n",
			wantErr: false,
		},
		{
			name: "success with data",
			result: &Result{
				Success: true,
				Data: map[string]interface{}{
					"mode":    "bootstrap",
					"version": "1.0.0",
				},
			},
			want:    "", // Output contains key-value pairs, order may vary
			wantErr: false,
		},
		{
			name: "failure with error",
			result: &Result{
				Success: false,
				Error:   "Something went wrong",
			},
			want:    "Error: Something went wrong\n",
			wantErr: false,
		},
		{
			name: "failure without error message",
			result: &Result{
				Success: false,
			},
			want:    "Command failed\n",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			formatter := New(FormatText)
			formatter.SetWriter(&buf)

			err := formatter.Print(tt.result)
			if (err != nil) != tt.wantErr {
				t.Errorf("Print() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			got := buf.String()
			
			// For data results, just verify output is not empty and contains expected keys
			if len(tt.result.Data) > 0 {
				if got == "" {
					t.Errorf("Print() output is empty for data result")
				}
				for k := range tt.result.Data {
					if !strings.Contains(got, k) {
						t.Errorf("Print() output missing key %q, got: %s", k, got)
					}
				}
				return
			}
			
			// For message/error results, check exact match
			if tt.want != "" && got != tt.want {
				t.Errorf("Print() output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatter_PrintJSON(t *testing.T) {
	tests := []struct {
		name    string
		result  *Result
		wantErr bool
	}{
		{
			name: "success with message",
			result: &Result{
				Success: true,
				Message: "Operation completed successfully",
			},
			wantErr: false,
		},
		{
			name: "success with data",
			result: &Result{
				Success: true,
				Data: map[string]interface{}{
					"mode":    "bootstrap",
					"version": "1.0.0",
				},
			},
			wantErr: false,
		},
		{
			name: "failure with error",
			result: &Result{
				Success: false,
				Error:   "Something went wrong",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			formatter := New(FormatJSON)
			formatter.SetWriter(&buf)

			err := formatter.Print(tt.result)
			if (err != nil) != tt.wantErr {
				t.Errorf("Print() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify it's valid JSON
			var decoded Result
			if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
				t.Errorf("Print() produced invalid JSON: %v", err)
				return
			}

			// Verify key fields match
			if decoded.Success != tt.result.Success {
				t.Errorf("JSON success = %v, want %v", decoded.Success, tt.result.Success)
			}
			if decoded.Message != tt.result.Message {
				t.Errorf("JSON message = %q, want %q", decoded.Message, tt.result.Message)
			}
			if decoded.Error != tt.result.Error {
				t.Errorf("JSON error = %q, want %q", decoded.Error, tt.result.Error)
			}
		})
	}
}

func TestFormatter_PrintValidationText(t *testing.T) {
	tests := []struct {
		name    string
		result  *ValidationResult
		want    string
		wantErr bool
	}{
		{
			name: "valid",
			result: &ValidationResult{
				Valid: true,
			},
			want:    "Validation passed\n",
			wantErr: false,
		},
		{
			name: "invalid with errors",
			result: &ValidationResult{
				Valid: false,
				Errors: []ValidationError{
					{
						Field:       "NODE_IP",
						Message:     "invalid IP address",
						Remediation: "Provide a valid IPv4 or IPv6 address",
					},
				},
			},
			want:    "", // Output is multi-line, we'll check it contains expected text
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			formatter := New(FormatText)
			formatter.SetWriter(&buf)

			err := formatter.PrintValidation(tt.result)
			if (err != nil) != tt.wantErr {
				t.Errorf("PrintValidation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			got := buf.String()
			
			if tt.result.Valid {
				if got != tt.want {
					t.Errorf("PrintValidation() output = %q, want %q", got, tt.want)
				}
			} else {
				// For invalid results, check it contains the field name and error message
				if !strings.Contains(got, "Validation failed") {
					t.Errorf("PrintValidation() output should contain 'Validation failed', got: %s", got)
				}
				for _, e := range tt.result.Errors {
					if !strings.Contains(got, e.Field) {
						t.Errorf("PrintValidation() output missing field %q, got: %s", e.Field, got)
					}
					if !strings.Contains(got, e.Message) {
						t.Errorf("PrintValidation() output missing message %q, got: %s", e.Message, got)
					}
					if e.Remediation != "" && !strings.Contains(got, e.Remediation) {
						t.Errorf("PrintValidation() output missing remediation, got: %s", got)
					}
				}
			}
		})
	}
}

func TestFormatter_PrintValidationJSON(t *testing.T) {
	result := &ValidationResult{
		Valid: false,
		Errors: []ValidationError{
			{
				Field:       "NODE_IP",
				Message:     "invalid IP address",
				Remediation: "Provide a valid IPv4 or IPv6 address",
			},
		},
	}

	var buf bytes.Buffer
	formatter := New(FormatJSON)
	formatter.SetWriter(&buf)

	err := formatter.PrintValidation(result)
	if err != nil {
		t.Errorf("PrintValidation() error = %v", err)
		return
	}

	// Verify it's valid JSON
	var decoded ValidationResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Errorf("PrintValidation() produced invalid JSON: %v", err)
		return
	}

	// Verify fields match
	if decoded.Valid != result.Valid {
		t.Errorf("JSON valid = %v, want %v", decoded.Valid, result.Valid)
	}
	if len(decoded.Errors) != len(result.Errors) {
		t.Errorf("JSON errors count = %d, want %d", len(decoded.Errors), len(result.Errors))
	}
}

func TestFormatter_UnsupportedFormat(t *testing.T) {
	formatter := &Formatter{
		format: Format("unsupported"),
		writer: &bytes.Buffer{},
	}

	result := &Result{Success: true}
	err := formatter.Print(result)
	if err == nil {
		t.Error("Print() should return error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("Print() error should mention unsupported format, got: %v", err)
	}

	validationResult := &ValidationResult{Valid: true}
	err = formatter.PrintValidation(validationResult)
	if err == nil {
		t.Error("PrintValidation() should return error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("PrintValidation() error should mention unsupported format, got: %v", err)
	}
}

func TestParseFormat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Format
		wantErr bool
	}{
		{
			name:    "valid text format",
			input:   "text",
			want:    FormatText,
			wantErr: false,
		},
		{
			name:    "valid json format",
			input:   "json",
			want:    FormatJSON,
			wantErr: false,
		},
		{
			name:    "invalid format",
			input:   "xml",
			want:    FormatText,
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			want:    FormatText,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFormat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFormat() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseFormat() = %v, want %v", got, tt.want)
			}
			if err != nil && !strings.Contains(err.Error(), "invalid output format") {
				t.Errorf("ParseFormat() error should mention invalid format, got: %v", err)
			}
		})
	}
}

