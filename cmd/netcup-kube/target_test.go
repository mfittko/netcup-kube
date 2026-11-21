package main

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain lets public invocation tests run the CLI entry point in a child process.
func TestMain(m *testing.M) {
	if os.Getenv("NETCUP_KUBE_TEST_RUN_MAIN") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

const fakeKubectl = `#!/bin/sh
{ printf 'KUBERC=%s' "${KUBERC-<unset>}"; for a in "$@"; do printf ' [%s]' "$a"; done; echo; } >> "$FAKE_LOG"
case "$1" in
config)
  while [ $# -gt 0 ]; do
    if [ "$1" = --kubeconfig ]; then cat "$2"; exit 0; fi
    shift
  done
  exit 1 ;;
get) echo '{"gitVersion":"v1.30.0"}' ;;
*) exit 1 ;;
esac
`

const fakeRemote = `#!/bin/sh
echo "$0 $*" >> "$FAKE_REMOTE_LOG"
exit 1
`

type cliResult struct {
	stdout, stderr string
	code           int
}

// runCLI runs the CLI in dir with exactly env plus PATH entries for the fake tools.
func runCLI(t *testing.T, dir string, env []string, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Dir = dir
	cmd.Env = append([]string{"NETCUP_KUBE_TEST_RUN_MAIN=1"}, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return cliResult{stdout.String(), stderr.String(), code}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

func testCAPEM(t *testing.T) string {
	t.Helper()
	srv := httptest.NewTLSServer(nil)
	defer srv.Close()
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
}

// kubeconfigJSON renders a kubeconfig whose contexts map name -> [server, namespace].
func kubeconfigJSON(t *testing.T, current string, caPEM string, contexts map[string][2]string) string {
	t.Helper()
	var clusters, users, ctxs []map[string]any
	for name, c := range contexts {
		clusters = append(clusters, map[string]any{"name": name, "cluster": map[string]any{
			"server": c[0], "certificate-authority-data": []byte(caPEM)}})
		users = append(users, map[string]any{"name": name, "user": map[string]any{"token": "SECRET-TOKEN-" + name}})
		ctx := map[string]any{"cluster": name, "user": name}
		if c[1] != "" {
			ctx["namespace"] = c[1]
		}
		ctxs = append(ctxs, map[string]any{"name": name, "context": ctx})
	}
	data, err := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "Config", "clusters": clusters,
		"users": users, "contexts": ctxs, "current-context": current})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

type abFixture struct {
	cwd, bDir, bKubeconfig, aKubeconfig, kubectlLog, remoteLog string
	env                                                        []string
}

// newABFixture places deployment A in the default files and the ambient environment, and
// deployment B in a directory whose path contains spaces.
func newABFixture(t *testing.T) abFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := abFixture{
		cwd:        filepath.Join(root, "work"),
		bDir:       filepath.Join(root, "deploy B dir"),
		kubectlLog: filepath.Join(root, "kubectl.log"),
		remoteLog:  filepath.Join(root, "remote.log"),
	}
	ca := testCAPEM(t)
	f.aKubeconfig = filepath.Join(f.cwd, "config", "k3s.yaml")
	f.bKubeconfig = filepath.Join(f.bDir, "b kubeconfig.json")
	writeFile(t, f.aKubeconfig, kubeconfigJSON(t, "ctx-a", ca, map[string][2]string{"ctx-a": {"https://a.example:6443", "team-a"}}), 0600)
	writeFile(t, f.bKubeconfig, kubeconfigJSON(t, "ctx-a", ca, map[string][2]string{
		"ctx-a": {"https://a.example:6443", "team-a"},
		"ctx-b": {"https://b.example:6443", "team-b"},
	}), 0600)
	writeFile(t, filepath.Join(f.cwd, "scripts", "main.sh"), "#!/bin/sh\n", 0755)
	// The default env file is unreadable: any read of it would fail the command.
	writeFile(t, filepath.Join(f.cwd, "config", "netcup-kube.env"), "KUBECONFIG="+f.aKubeconfig+"\nMGMT_HOST=a-mgmt.example\nKUBE_TRANSPORT=ssh\n", 0000)
	bin := filepath.Join(root, "bin")
	writeFile(t, filepath.Join(bin, "kubectl"), fakeKubectl, 0755)
	for _, name := range []string{"ssh", "scp", "caddy", "apt-get", "helm"} {
		writeFile(t, filepath.Join(bin, name), fakeRemote, 0755)
	}
	f.env = []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + filepath.Join(root, "home"),
		"FAKE_LOG=" + f.kubectlLog,
		"FAKE_REMOTE_LOG=" + f.remoteLog,
		"KUBECONFIG=" + f.aKubeconfig,
		"NETCUP_KUBE_CONTEXT=ctx-a",
		"KUBE_TRANSPORT=ssh",
		"DEFAULT_USER=a-user",
		"MGMT_HOST=a-mgmt.example",
		"MGMT_USER=a-user",
		"TUNNEL_HOST=a-mgmt.example",
		"EDGE_PROXY=caddy",
		"HELM_KUBECONTEXT=ctx-a",
		"HELM_KUBEAPISERVER=https://a.example:6443",
		"HELM_NAMESPACE=team-a",
	}
	return f
}

func (f abFixture) writeBEnv(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(f.bDir, "b deploy.env")
	writeFile(t, path, content, 0600)
	return path
}

// decodeOne asserts stdout holds exactly one JSON object.
func decodeOne(t *testing.T, stdout string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout)
	}
	if dec.More() || strings.TrimSpace(stdout[dec.InputOffset():]) != "" {
		t.Fatalf("stdout holds more than one JSON object:\n%s", stdout)
	}
	return obj
}

func TestTargetInspectSelectsOnlyB(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the unreadable default env file guard")
	}
	cases := []struct {
		name       string
		args       func(f abFixture, t *testing.T) []string
		wantSource func(f abFixture) any
		wantTunnel bool
	}{
		{
			name: "cli flags",
			args: func(f abFixture, t *testing.T) []string {
				return []string{"target", "inspect", "--output", "json", "--kubeconfig", f.bKubeconfig, "--context=ctx-b"}
			},
			wantSource: func(abFixture) any { return nil },
		},
		{
			name: "env file with relative kubeconfig",
			args: func(f abFixture, t *testing.T) []string {
				env := f.writeBEnv(t, "KUBECONFIG=b kubeconfig.json\nNETCUP_KUBE_CONTEXT=ctx-b\n")
				return []string{"--env-file", env, "target", "inspect", "-o", "json"}
			},
			wantSource: func(f abFixture) any { return filepath.Join(f.bDir, "b deploy.env") },
		},
		{
			name: "env file with indirect management host",
			args: func(f abFixture, t *testing.T) []string {
				env := f.writeBEnv(t, "KUBECONFIG=b kubeconfig.json\nNETCUP_KUBE_CONTEXT=ctx-b\nMGMT_HOST=b-mgmt.example\nKUBE_TRANSPORT=ssh\nTUNNEL_HOST=${MGMT_HOST}\n")
				return []string{"target", "inspect", "--env-file=" + env, "--output=json"}
			},
			wantSource: func(f abFixture) any { return filepath.Join(f.bDir, "b deploy.env") },
			wantTunnel: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newABFixture(t)
			before := readFile(t, f.bKubeconfig)
			res := runCLI(t, f.cwd, f.env, tc.args(f, t)...)
			if res.code != 0 {
				t.Fatalf("exit %d, stderr: %s", res.code, res.stderr)
			}
			obj := decodeOne(t, res.stdout)
			if obj["kubeconfig"] != f.bKubeconfig || obj["context"] != "ctx-b" || obj["server"] != "https://b.example:6443" || obj["namespace"] != "team-b" {
				t.Fatalf("resolved wrong target: %v", obj)
			}
			if obj["config_source"] != tc.wantSource(f) || obj["selection_mode"] != "explicit" {
				t.Fatalf("config_source/selection_mode = %v/%v", obj["config_source"], obj["selection_mode"])
			}
			if tunnel, _ := obj["tunnel"].(map[string]any); tc.wantTunnel != (tunnel != nil) || (tunnel != nil && tunnel["ssh_host"] != "b-mgmt.example") {
				t.Fatalf("tunnel = %v", obj["tunnel"])
			}
			log := readFile(t, f.kubectlLog)
			if !strings.Contains(log, "["+f.bKubeconfig+"]") || strings.Contains(log, f.aKubeconfig) {
				t.Fatalf("kubectl saw a non-B kubeconfig:\n%s", log)
			}
			if readFile(t, f.remoteLog) != "" {
				t.Fatalf("remote or package tools were invoked: %s", readFile(t, f.remoteLog))
			}
			for _, leak := range []string{"a-mgmt", "a.example", "a-user", "SECRET-TOKEN"} {
				if strings.Contains(res.stdout+res.stderr, leak) {
					t.Fatalf("output contains %q:\n%s%s", leak, res.stdout, res.stderr)
				}
			}
			if readFile(t, f.bKubeconfig) != before {
				t.Fatal("kubeconfig was modified")
			}
		})
	}
}

func TestTargetRefusesBeforeTools(t *testing.T) {
	f := newABFixture(t)
	missing := filepath.Join(f.bDir, "missing deploy.env")
	unreadable := f.writeBEnv(t, "KUBECONFIG=b kubeconfig.json\n")
	if err := os.Chmod(unreadable, 0000); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]string{
		"missing env file":         {"target", "inspect", "--env-file", missing},
		"empty context":            {"target", "inspect", "--kubeconfig", f.bKubeconfig, "--context="},
		"missing context value":    {"target", "inspect", "--kubeconfig", f.bKubeconfig, "--context"},
		"flag as value":            {"target", "inspect", "--kubeconfig", "--context", "ctx-b"},
		"conflicting context":      {"target", "inspect", "--kubeconfig", f.bKubeconfig, "--context", "ctx-b", "--context=ctx-a"},
		"conflicting kubeconfig":   {"--kubeconfig", f.bKubeconfig, "target", "inspect", "--kubeconfig", f.aKubeconfig},
		"path list":                {"target", "inspect", "--kubeconfig", f.bKubeconfig + string(os.PathListSeparator) + f.aKubeconfig},
		"caller kuberc":            {"target", "inspect", "--kubeconfig", f.bKubeconfig, "--kuberc", "/tmp/x"},
		"caller kuberc equals":     {"target", "inspect", "--kubeconfig", f.bKubeconfig, "--kuberc=/tmp/x"},
		"unsupported output":       {"target", "inspect", "--kubeconfig", f.bKubeconfig, "--output", "yaml"},
		"conflicting output":       {"target", "inspect", "--kubeconfig", f.bKubeconfig, "-o", "json", "--output=text"},
		"missing fingerprint":      {"target", "verify", "--kubeconfig", f.bKubeconfig},
		"malformed fingerprint":    {"target", "verify", "--kubeconfig", f.bKubeconfig, "--fingerprint", "sha256:ABC"},
		"uppercase fingerprint":    {"target", "verify", "--kubeconfig", f.bKubeconfig, "--fingerprint", "sha256:" + strings.Repeat("A", 64)},
		"missing kubeconfig file":  {"target", "inspect", "--kubeconfig", filepath.Join(f.bDir, "none.json")},
		"context without kubeconf": {"target", "inspect", "--context", "ctx-b"},
		"extra argument":           {"target", "inspect", "ctx-b"},
	}
	if os.Geteuid() != 0 {
		cases["unreadable env file"] = []string{"target", "inspect", "--env-file", unreadable}
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			env := f.env
			if name == "context without kubeconf" {
				env = withoutEnv(f.env, "KUBECONFIG")
			}
			res := runCLI(t, f.cwd, env, args...)
			if res.code == 0 || res.stdout != "" || strings.TrimSpace(res.stderr) == "" {
				t.Fatalf("want stderr-only refusal, got exit %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
			}
			if readFile(t, f.kubectlLog) != "" || readFile(t, f.remoteLog) != "" {
				t.Fatalf("tools ran before refusal:\n%s%s", readFile(t, f.kubectlLog), readFile(t, f.remoteLog))
			}
		})
	}
}

func withoutEnv(env []string, keys ...string) []string {
	out := []string{}
	for _, kv := range env {
		keep := true
		for _, k := range keys {
			if strings.HasPrefix(kv, k+"=") {
				keep = false
			}
		}
		if keep {
			out = append(out, kv)
		}
	}
	return out
}

func TestTargetKubeconfigFlagIgnoresAmbientContext(t *testing.T) {
	f := newABFixture(t)
	writeFile(t, f.bKubeconfig, kubeconfigJSON(t, "ctx-b", testCAPEM(t), map[string][2]string{
		"ctx-a": {"https://a.example:6443", "team-a"},
		"ctx-b": {"https://b.example:6443", "team-b"},
	}), 0600)
	res := runCLI(t, f.cwd, f.env, "target", "inspect", "-o", "json", "--kubeconfig", f.bKubeconfig)
	if res.code != 0 {
		t.Fatalf("exit %d, stderr: %s", res.code, res.stderr)
	}
	if obj := decodeOne(t, res.stdout); obj["context"] != "ctx-b" || obj["server"] != "https://b.example:6443" {
		t.Fatalf("ambient NETCUP_KUBE_CONTEXT selected a context in --kubeconfig: %v", obj)
	}
}

func TestTargetEnvFileRefusesAmbientManagementHost(t *testing.T) {
	f := newABFixture(t)
	env := f.writeBEnv(t, "KUBECONFIG=b kubeconfig.json\nNETCUP_KUBE_CONTEXT=ctx-b\nKUBE_TRANSPORT=ssh\nTUNNEL_HOST=${MGMT_HOST}\n")
	res := runCLI(t, f.cwd, f.env, "target", "inspect", "--env-file", env)
	if res.code == 0 || !strings.Contains(res.stderr, "MGMT_HOST") || strings.Contains(res.stderr, "a-mgmt") || res.stdout != "" {
		t.Fatalf("want refusal naming MGMT_HOST without A's value, got exit %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	if readFile(t, f.kubectlLog) != "" || readFile(t, f.remoteLog) != "" {
		t.Fatal("tools ran before refusal")
	}
}

func TestTargetVerifyAndText(t *testing.T) {
	f := newABFixture(t)
	sel := []string{"--kubeconfig", f.bKubeconfig, "--context", "ctx-b"}
	res := runCLI(t, f.cwd, f.env, append([]string{"target", "inspect", "--output", "json"}, sel...)...)
	if res.code != 0 {
		t.Fatal(res.stderr)
	}
	fp, _ := decodeOne(t, res.stdout)["fingerprint"].(string)

	res = runCLI(t, f.cwd, f.env, append([]string{"target", "verify", "--fingerprint", fp}, sel...)...)
	if res.code != 0 || !strings.Contains(res.stdout, "matches") || res.stderr != "" {
		t.Fatalf("verify match: exit %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}

	other := "sha256:" + strings.Repeat("0", 64)
	res = runCLI(t, f.cwd, f.env, append([]string{"target", "verify", "--fingerprint", other}, sel...)...)
	if res.code == 0 || res.stdout != "" || !strings.Contains(res.stderr, "mismatch") {
		t.Fatalf("verify mismatch: exit %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}

	res = runCLI(t, f.cwd, f.env, append([]string{"target", "inspect"}, sel...)...)
	if res.code != 0 || !strings.Contains(res.stdout, "fingerprint: "+fp) || !strings.Contains(res.stdout, "live_check: not_requested") {
		t.Fatalf("text output: exit %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}

	res = runCLI(t, f.cwd, f.env, append([]string{"target", "inspect", "--check", "-o", "json"}, sel...)...)
	if res.code != 0 || decodeOne(t, res.stdout)["live_check"] != "verified" {
		t.Fatalf("check: exit %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(readFile(t, f.kubectlLog), "[get] [--raw] [/version] [--request-timeout=10s] [--kubeconfig] ["+f.bKubeconfig+"] [--context] [ctx-b]") {
		t.Fatalf("live check argv:\n%s", readFile(t, f.kubectlLog))
	}
}

func TestTargetLegacyPaths(t *testing.T) {
	if _, err := os.Stat(serverKubeconfigPath); err == nil {
		t.Skip("node-local kubeconfig present; legacy fallback order differs on this host")
	}
	newLegacy := func(t *testing.T) (abFixture, []string) {
		f := newABFixture(t)
		if err := os.Chmod(filepath.Join(f.cwd, "config", "netcup-kube.env"), 0600); err != nil {
			t.Fatal(err)
		}
		return f, withoutEnv(f.env, "KUBECONFIG", "NETCUP_KUBE_CONTEXT")
	}

	t.Run("env file kubeconfig", func(t *testing.T) {
		f, env := newLegacy(t)
		writeFile(t, filepath.Join(f.cwd, "config", "netcup-kube.env"), "KUBECONFIG=k3s.yaml\n", 0600)
		res := runCLI(t, f.cwd, env, "target", "inspect", "-o", "json")
		obj := decodeOne(t, res.stdout)
		if res.code != 0 || obj["selection_mode"] != "legacy" || obj["kubeconfig"] != f.aKubeconfig || obj["config_source"] != filepath.Join(f.cwd, "config", "netcup-kube.env") {
			t.Fatalf("exit %d obj %v stderr %s", res.code, obj, res.stderr)
		}
	})

	t.Run("core k3s.yaml fallback", func(t *testing.T) {
		f, env := newLegacy(t)
		writeFile(t, filepath.Join(f.cwd, "config", "netcup-kube.env"), "MGMT_HOST=a-mgmt.example\n", 0600)
		res := runCLI(t, f.cwd, env, "target", "inspect", "-o", "json")
		if res.code != 0 || decodeOne(t, res.stdout)["kubeconfig"] != f.aKubeconfig {
			t.Fatalf("exit %d stdout %s stderr %s", res.code, res.stdout, res.stderr)
		}
	})

	t.Run("missing kubeconfig gives instruction and never fetches", func(t *testing.T) {
		f, env := newLegacy(t)
		writeFile(t, filepath.Join(f.cwd, "config", "netcup-kube.env"), "MGMT_HOST=a-mgmt.example\n", 0600)
		if err := os.Remove(f.aKubeconfig); err != nil {
			t.Fatal(err)
		}
		res := runCLI(t, f.cwd, env, "target", "inspect")
		if res.code == 0 || res.stdout != "" || !strings.Contains(res.stderr, "copy") {
			t.Fatalf("exit %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
		}
		if readFile(t, f.remoteLog) != "" || readFile(t, f.kubectlLog) != "" {
			t.Fatal("legacy path invoked tools")
		}
		if _, err := os.Stat(f.aKubeconfig); !os.IsNotExist(err) {
			t.Fatal("kubeconfig was created")
		}
	})
}
