package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecipeInternalRunnerCommand(t *testing.T) {
	for _, useK3s := range []bool{false, true} {
		t.Run(map[bool]string{false: "kubectl", true: "k3s"}[useK3s], func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "calls")
			t.Setenv("PATH", dir)
			t.Setenv("CALLS", logPath)
			t.Setenv("KUBECONFIG", "fixture-kubeconfig")
			t.Setenv("GITHUB_PAT", "fixture-canary")
			t.Setenv("HELM_DRIVER", "")
			helm := `#!/bin/sh
[ "$KUBECONFIG" = fixture-kubeconfig ] || exit 1
printf 'helm fixture-kubeconfig %s\n' "$*" >> "$CALLS"
if [ "$1" = get ]; then
 /bin/cat << 'STUB_JSON'
{"githubConfigUrl":"https://github.com/o/app","runnerScaleSetName":"app","githubConfigSecret":"gha-app-github"}
STUB_JSON
fi
`
			kube := `#!/bin/sh
[ "$KUBECONFIG" = fixture-kubeconfig ] || exit 1
printf 'kube fixture-kubeconfig %s\n' "$*" >> "$CALLS"
if [ "$1" = kubectl ]; then shift; fi
if [ "$1 $2" = 'get secrets' ]; then
 if [ "$INSTALLED" = true ]; then
  /bin/cat << 'STUB_JSON'
{"items":[{"metadata":{"name":"sh.helm.release.v1.app.v1","namespace":"arc-runners","labels":{"name":"app","owner":"helm","status":"unknown"}}}]}
STUB_JSON
 else printf '{"items":[]}'; fi
fi
if [ "$1" = apply ]; then
 /bin/cat >> "$CALLS.payload"
 if [ "$FAIL_APPLY" = true ]; then echo fixture-canary >&2; exit 1; fi
fi
`
			name := "kubectl"
			if useK3s {
				name = "k3s"
			}
			for file, script := range map[string]string{"helm": helm, name: kube} {
				if err := os.WriteFile(filepath.Join(dir, file), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			for _, tc := range []struct {
				name         string
				args         []string
				fail, cancel bool
			}{
				{"credentials", []string{"gha-runners-credentials"}, false, false},
				{"redacted-failure", []string{"gha-runners-credentials"}, true, false},
				{"dry", []string{"gha-runners-credentials", "--dry-run"}, false, false},
				{"confirmed", []string{"gha-runners-uninstall", "--confirmed"}, false, false},
				{"uninstall-live", []string{"gha-runners-uninstall", "--confirmed"}, false, false},
				{"unconfirmed", []string{"gha-runners-uninstall"}, true, false},
				{"cancelled", []string{"gha-runners-credentials"}, true, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Setenv("INSTALLED", map[bool]string{false: "false", true: "true"}[tc.name == "uninstall-live"])
					t.Setenv("FAIL_APPLY", map[bool]string{false: "false", true: "true"}[tc.name == "redacted-failure"])
					cmd := newRecipeInternalCommand()
					var out bytes.Buffer
					cmd.SetOut(&out)
					cmd.SetErr(&out)
					if tc.cancel {
						ctx, cancel := context.WithCancel(context.Background())
						cancel()
						cmd.SetContext(ctx)
					}
					cmd.SetArgs(append(tc.args, "--repo", "o/app", "--name", "app", "--namespace", "arc-runners", "--controller-namespace", "arc-systems"))
					err := cmd.Execute()
					if (err != nil) != tc.fail {
						t.Fatalf("unexpected error %v", err)
					}
					calls, _ := os.ReadFile(logPath)
					if strings.Contains(string(calls)+out.String(), "fixture-canary") || err != nil && strings.Contains(err.Error(), "fixture-canary") {
						t.Fatal("credential leaked")
					}
					if !strings.Contains(string(calls), "kube fixture-kubeconfig "+map[bool]string{false: "get secrets", true: "kubectl get secrets"}[useK3s]) || !strings.Contains(string(calls), "kube fixture-kubeconfig "+map[bool]string{false: "apply", true: "kubectl apply"}[useK3s]) {
						t.Fatalf("routing lost: %s", calls)
					}
				})
			}
			payload, err := os.ReadFile(logPath + ".payload")
			if err != nil || !bytes.Contains(payload, []byte("fixture-canary")) {
				t.Fatal("missing stdin credential")
			}
			calls, _ := os.ReadFile(logPath)
			if !strings.Contains(string(calls), "helm fixture-kubeconfig uninstall app --namespace arc-runners --wait --timeout 5m") {
				t.Fatal("Helm uninstall routing lost")
			}
		})
	}
}

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
if [ "$1" = create ] || [ "$1" = replace ]; then
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
