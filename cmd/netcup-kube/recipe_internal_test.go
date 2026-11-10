package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecipeInternalSecretCommand(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KUBECONFIG", "fixture-kubeconfig")
	t.Setenv("CALLS", logPath)
	t.Setenv("LLM_PROXY_MANAGEMENT_TOKEN", "SECRET-SENTINEL")
	t.Setenv("LLM_PROXY_ENCRYPTION_KEY", "KEY-SENTINEL")
	script := `#!/bin/sh
printf '%s %s\n' "$KUBECONFIG" "$*" >> "$CALLS"
if [ "$1" = create ] || [ "$1" = apply ]; then
 cat > "$CALLS.payload"
 echo SECRET-SENTINEL >&2
 exit 1
fi
`
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	for _, dry := range []bool{true, false} {
		cmd := newRecipeInternalCommand()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		args := []string{"llm-proxy-secret", "--namespace", "test", "--secret-name", "app", "--create-secret"}
		if dry {
			args = append(args, "--dry-run")
		}
		cmd.SetArgs(args)
		err := cmd.Execute()
		if (err == nil) != dry {
			t.Fatalf("unexpected command result: %v", err)
		}
		calls, readErr := os.ReadFile(logPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(calls)+output.String(), "SENTINEL") || (err != nil && strings.Contains(err.Error(), "SENTINEL")) {
			t.Fatal("credential escaped")
		}
		if !strings.Contains(string(calls), "fixture-kubeconfig get secret app") {
			t.Fatal("kubeconfig/read target lost")
		}
		if dry {
			if _, err := os.Stat(logPath + ".payload"); !os.IsNotExist(err) {
				t.Fatal("dry run wrote payload")
			}
		} else {
			payload, err := os.ReadFile(logPath + ".payload")
			if err != nil || len(payload) == 0 {
				t.Fatal("missing stdin payload")
			}
		}
	}
}
