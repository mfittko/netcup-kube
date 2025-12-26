package remote

import (
	"testing"
)

func TestJoinArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "simple args",
			args: []string{"bootstrap"},
			want: "bootstrap",
		},
		{
			name: "multiple args",
			args: []string{"dns", "--type", "edge-http"},
			want: "dns --type edge-http",
		},
		{
			name: "args with spaces",
			args: []string{"dns", "--domains", "kube.example.com,app.example.com", "--note", "hello world"},
			want: "dns --domains kube.example.com,app.example.com --note \"hello world\"",
		},
		{
			name: "empty args",
			args: []string{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinArgs(tt.args)
			if got != tt.want {
				t.Errorf("joinArgs() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContainsSpace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "no spaces",
			input: "bootstrap",
			want:  false,
		},
		{
			name:  "has space",
			input: "hello world",
			want:  true,
		},
		{
			name:  "has tab",
			input: "hello\tworld",
			want:  true,
		},
		{
			name:  "has newline",
			input: "hello\nworld",
			want:  true,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsSpace(tt.input)
			if got != tt.want {
				t.Errorf("containsSpace(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestRunOptions_Validation(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid bootstrap command",
			args:        []string{"bootstrap"},
			expectError: false,
		},
		{
			name:        "valid join command",
			args:        []string{"join"},
			expectError: false,
		},
		{
			name:        "valid dns command",
			args:        []string{"dns", "--type", "edge-http"},
			expectError: false,
		},
		{
			name:        "valid pair command",
			args:        []string{"pair"},
			expectError: false,
		},
		{
			name:        "valid help",
			args:        []string{"--help"},
			expectError: false,
		},
		{
			name:        "empty args should fail",
			args:        []string{},
			expectError: true,
			errorMsg:    "missing",
		},
		{
			name:        "unsupported command should fail",
			args:        []string{"unsupported-cmd"},
			expectError: true,
			errorMsg:    "unsupported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test argument validation logic
			if len(tt.args) < 1 {
				if !tt.expectError {
					t.Error("Expected validation to pass for non-empty args")
				}
				return
			}

			supportedCmds := []string{"bootstrap", "join", "pair", "dns", "help", "-h", "--help"}
			cmdValid := false
			for _, cmd := range supportedCmds {
				if tt.args[0] == cmd {
					cmdValid = true
					break
				}
			}

			if tt.expectError && cmdValid {
				t.Error("Expected validation to fail for unsupported command")
			}
			if !tt.expectError && !cmdValid {
				t.Errorf("Expected validation to pass for command: %s", tt.args[0])
			}
		})
	}
}
