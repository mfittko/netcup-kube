package main

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// TestTargetKubercRealClient uses the installed kubectl against two local TLS endpoints.
// Preference files point every core kubectl call at endpoint A; the selected target is B.
func TestTargetKubercRealClient(t *testing.T) {
	kubectl, err := exec.LookPath("kubectl")
	if err != nil {
		t.Skip("kubectl not installed; real-client kuberc evidence not collected")
	}
	if err := exec.Command(kubectl, "kuberc", "view", "--help").Run(); err != nil {
		t.Skip("installed kubectl lacks 'kuberc view'; real-client kuberc evidence not collected")
	}
	version, _ := exec.Command(kubectl, "version", "--client").CombinedOutput()
	t.Logf("kubectl %s: %s", kubectl, strings.TrimSpace(string(version)))

	var hitsA, hitsB atomic.Int64
	serve := func(hits *atomic.Int64) *httptest.Server {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			_, _ = w.Write([]byte(`{"major":"1","minor":"30","gitVersion":"v1.30.0"}`))
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	srvA, srvB := serve(&hitsA), serve(&hitsB)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srvB.Certificate().Raw})

	root := t.TempDir()
	sentinel := filepath.Join(root, "exec-plugin-ran")
	plugin := filepath.Join(root, "plugin.sh")
	writeFile(t, plugin, "#!/bin/sh\ntouch '"+sentinel+"'\necho '{\"apiVersion\":\"client.authentication.k8s.io/v1\",\"kind\":\"ExecCredential\",\"status\":{\"token\":\"t\"}}'\n", 0755)

	cluster := func(name, server string) map[string]any {
		return map[string]any{"name": name, "cluster": map[string]any{"server": server, "certificate-authority-data": ca}}
	}
	kc, _ := json.Marshal(map[string]any{
		"apiVersion": "v1", "kind": "Config", "current-context": "ctx-a",
		"clusters": []any{cluster("a", srvA.URL), cluster("b", srvB.URL)},
		"users": []any{
			map[string]any{"name": "ua", "user": map[string]any{"token": "token-a"}},
			map[string]any{"name": "ub", "user": map[string]any{"token": "token-b"}},
			map[string]any{"name": "uexec", "user": map[string]any{"exec": map[string]any{
				"apiVersion": "client.authentication.k8s.io/v1", "command": plugin, "interactiveMode": "Never"}}},
		},
		"contexts": []any{
			map[string]any{"name": "ctx-a", "context": map[string]any{"cluster": "a", "user": "ua"}},
			map[string]any{"name": "ctx-b", "context": map[string]any{"cluster": "b", "user": "ub"}},
			map[string]any{"name": "ctx-b-exec", "context": map[string]any{"cluster": "b", "user": "uexec"}},
		},
	})
	kubeconfig := filepath.Join(root, "kube config.json")
	writeFile(t, kubeconfig, string(kc), 0600)

	attack := func(policy, extraDefaults string) string {
		return "apiVersion: kubectl.config.k8s.io/v1beta1\nkind: Preference\n" + policy +
			"defaults:\n- command: get\n  options:\n  - name: server\n    default: " + srvA.URL +
			"\n  - name: cluster\n    default: a\n  - name: user\n    default: ua\n" + extraDefaults +
			"aliases:\n- name: inspect\n  command: get\n"
	}

	bin := filepath.Join(root, "bin")
	for _, name := range []string{"ssh", "scp"} {
		writeFile(t, filepath.Join(bin, name), fakeRemote, 0755)
	}
	remoteLog := filepath.Join(root, "remote.log")
	baseEnv := func(home, tmp string) []string {
		return []string{
			"PATH=" + bin + string(os.PathListSeparator) + filepath.Dir(kubectl) + string(os.PathListSeparator) + "/usr/bin:/bin",
			"HOME=" + home, "TMPDIR=" + tmp, "FAKE_REMOTE_LOG=" + remoteLog,
		}
	}

	// The fixture must be able to redirect an uncontrolled client to A.
	t.Run("control: uncontrolled kuberc reaches A", func(t *testing.T) {
		rc := filepath.Join(t.TempDir(), "kuberc")
		writeFile(t, rc, attack("", ""), 0600)
		hitsA.Store(0)
		cmd := exec.Command(kubectl, "get", "--raw", "/version", "--kubeconfig", kubeconfig, "--context", "ctx-b")
		cmd.Env = append(baseEnv(t.TempDir(), t.TempDir()), "KUBERC="+rc)
		_ = cmd.Run()
		if hitsA.Load() == 0 {
			t.Fatal("attack preferences did not redirect the plain client; fixture is not meaningful")
		}
	})

	cases := []struct {
		name, policy, extraDefaults, context string
		viaHome, check, wantOK, wantPlugin   bool
		either                               bool // refusal or a B-only success are both acceptable
	}{
		{name: "HOME-discovered preferences", context: "ctx-b", viaHome: true, check: true, wantOK: true},
		{name: "KUBERC-selected preferences", context: "ctx-b", check: true, wantOK: true},
		{name: "local inspect runs no credential plugin", context: "ctx-b-exec", viaHome: true, wantOK: true},
		{name: "DenyAll preserved", policy: "credentialPluginPolicy: DenyAll\n", context: "ctx-b-exec", check: true},
		{name: "Allowlist preserved", policy: "credentialPluginPolicy: Allowlist\ncredentialPluginAllowlist:\n- command: /nonexistent/other\n", context: "ctx-b-exec", check: true},
		{name: "Allowlist permits listed plugin", policy: "credentialPluginPolicy: Allowlist\ncredentialPluginAllowlist:\n- command: " + plugin + "\n", context: "ctx-b-exec", check: true, wantOK: true, wantPlugin: true},
		{name: "unknown preference format refused", policy: "UNKNOWN", context: "ctx-b", check: true},
		{name: "config view defaults", extraDefaults: "- command: config view\n  options:\n  - name: minify\n    default: \"true\"\n", context: "ctx-b", check: true, either: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, tmp := t.TempDir(), t.TempDir()
			content := attack(tc.policy, tc.extraDefaults)
			if tc.policy == "UNKNOWN" {
				content = strings.Replace(attack("", ""), "kubectl.config.k8s.io/v1beta1", "example.com/v9", 1)
			}
			rc := filepath.Join(home, ".kube", "kuberc")
			env := baseEnv(home, tmp)
			if !tc.viaHome {
				rc = filepath.Join(t.TempDir(), "prefs")
				env = append(env, "KUBERC="+rc)
			}
			writeFile(t, rc, content, 0600)
			_ = os.Remove(sentinel)
			hitsA.Store(0)
			hitsB.Store(0)

			args := []string{"target", "inspect", "-o", "json", "--kubeconfig", kubeconfig, "--context", tc.context}
			if tc.check {
				args = append(args, "--check")
			}
			res := runCLI(t, root, env, args...)
			if (res.code == 0) != tc.wantOK && !tc.either {
				t.Fatalf("exit %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
			}
			if res.code == 0 {
				obj := decodeOne(t, res.stdout)
				if obj["server"] != srvB.URL || (tc.check && obj["live_check"] != "verified") {
					t.Fatalf("resolved %v", obj)
				}
			} else if res.stdout != "" || strings.Contains(res.stderr, "token-") {
				t.Fatalf("refusal leaked output: stdout %q stderr %q", res.stdout, res.stderr)
			}
			if hitsA.Load() != 0 {
				t.Fatalf("endpoint A received %d requests", hitsA.Load())
			}
			if tc.check && res.code == 0 && hitsB.Load() == 0 {
				t.Fatal("endpoint B received no request")
			}
			if _, err := os.Stat(sentinel); (err == nil) != tc.wantPlugin {
				t.Fatalf("credential plugin ran = %v, want %v", err == nil, tc.wantPlugin)
			}
			if readFile(t, rc) != content {
				t.Fatal("user preference file changed")
			}
			if left, _ := os.ReadDir(tmp); len(left) != 0 {
				t.Fatalf("temporary preference projection left behind: %v", left)
			}
			if readFile(t, remoteLog) != "" {
				t.Fatal("ssh or scp invoked")
			}
		})
	}
}
