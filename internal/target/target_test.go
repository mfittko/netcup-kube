package target

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const fake = `#!/bin/sh
{ printf 'KUBERC=%s' "${KUBERC-<unset>}"; for a in "$@"; do printf ' [%s]' "$a"; done; echo; } >> "$FAKE_LOG"
case "$1" in
config)
  if [ -n "$FAKE_FAIL" ]; then echo "$FAKE_FAIL"; echo "$FAKE_FAIL" >&2; exit 3; fi
  if [ "$KUBERC" != off ]; then cp "$KUBERC" "$FAKE_PROJECTION"; fi
  while [ $# -gt 0 ]; do
    if [ "$1" = --kubeconfig ]; then cat "$2"; exit 0; fi
    shift
  done
  exit 1 ;;
kuberc)
  if [ -n "$FAKE_KUBERC_FAIL" ]; then exit 1; fi
  printf '%s' "$FAKE_KUBERC_VIEW" ;;
get)
  if [ -n "$FAKE_HANG" ]; then sleep 30 & echo $! > "$FAKE_HANG"; wait; fi
  if [ -n "$FAKE_FAIL_GET" ]; then echo "$FAKE_FAIL_GET" >&2; exit 1; fi
  if [ -n "$FAKE_SWAP" ]; then cp "$FAKE_SWAP" "$FAKE_SWAP_TO"; fi
  echo '{}' ;;
esac
`

type env struct {
	dir, log, projection string
	r                    Resolver
}

func newEnv(t *testing.T, extra ...string) *env {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir()) // resolved paths are canonical
	if err != nil {
		t.Fatal(err)
	}
	e := &env{dir: dir, log: filepath.Join(dir, "kubectl.log"), projection: filepath.Join(dir, "projection")}
	kubectl := filepath.Join(dir, "kubectl")
	write(t, kubectl, fake)
	if err := os.Chmod(kubectl, 0755); err != nil {
		t.Fatal(err)
	}
	e.r = Resolver{
		Cwd:     dir,
		Kubectl: kubectl,
		Environ: append([]string{"HOME=" + filepath.Join(dir, "home"), "FAKE_LOG=" + e.log, "FAKE_PROJECTION=" + e.projection, "PATH=" + os.Getenv("PATH")}, extra...),
	}
	return e
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func newCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"},
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

type cluster struct {
	Server     string `json:"server"`
	CAData     []byte `json:"certificate-authority-data,omitempty"`
	CAFile     string `json:"certificate-authority,omitempty"`
	Insecure   bool   `json:"insecure-skip-tls-verify,omitempty"`
	ServerName string `json:"tls-server-name,omitempty"`
}

type kc struct {
	current  string
	clusters map[string]cluster
	users    map[string]string // name -> token
	contexts map[string][3]string
}

// kubeconfig writes k as JSON; contexts map name -> [cluster, user, namespace].
func (k kc) write(t *testing.T, path string) string {
	t.Helper()
	var cl, us, cx []map[string]any
	for n, c := range k.clusters {
		cl = append(cl, map[string]any{"name": n, "cluster": c})
	}
	for n, tok := range k.users {
		us = append(us, map[string]any{"name": n, "user": map[string]any{"token": tok}})
	}
	for n, c := range k.contexts {
		cx = append(cx, map[string]any{"name": n, "context": map[string]string{"cluster": c[0], "user": c[1], "namespace": c[2]}})
	}
	data, err := json.Marshal(map[string]any{"clusters": cl, "users": us, "contexts": cx, "current-context": k.current})
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, string(data))
	return path
}

func twoContexts(ca string) kc {
	return kc{
		current:  "a",
		clusters: map[string]cluster{"a": {Server: "https://a.example:6443", CAData: []byte(ca)}, "b": {Server: "https://b.example:6443", CAData: []byte(ca)}},
		users:    map[string]string{"ua": "SECRET-A", "ub": "SECRET-B"},
		contexts: map[string][3]string{"a": {"a", "ua", ""}, "b": {"b", "ub", "team-b"}},
	}
}

func TestFingerprintVector(t *testing.T) {
	src, sn, ca := "/etc/netcup/b.env", "api.b.example", "4bf9786af942540c8b9f0b5275dbf658d9950f523b199b77b07aa8921c6cbdbf"
	full := &Target{ConfigSource: &src, Kubeconfig: "/etc/netcup/b.yaml", Context: "b", Cluster: "cb", User: "ub",
		Namespace: "team-b", Server: "https://b.example:6443", TLS: TLS{ServerName: &sn, CASHA256: &ca}, Transport: "ssh",
		Tunnel:        &Tunnel{"ops", "b-mgmt.example", "127.0.0.1", "6443", "127.0.0.1", "6443"},
		SelectionMode: "explicit", Fingerprint: "ignored", LiveCheck: "verified"}
	minimal := &Target{Kubeconfig: "/k.yaml", Context: "c", Cluster: "a", User: "u", Namespace: "default",
		Server: "https://a.example", TLS: TLS{SystemRoots: true}, Transport: "direct"}
	vectors := []struct {
		target    *Target
		canonical string
		sum       string
	}{
		{full, `{"schema_version":1,"config_source":"/etc/netcup/b.env","kubeconfig":"/etc/netcup/b.yaml","context":"b","cluster":"cb","user":"ub","namespace":"team-b","server":"https://b.example:6443","tls":{"server_name":"api.b.example","ca_sha256":"4bf9786af942540c8b9f0b5275dbf658d9950f523b199b77b07aa8921c6cbdbf","system_roots":false},"transport":"ssh","tunnel":{"ssh_user":"ops","ssh_host":"b-mgmt.example","local_host":"127.0.0.1","local_port":"6443","remote_host":"127.0.0.1","remote_port":"6443"}}`,
			"sha256:984ecf4e08491c2e8e526abd97ed04f927f13792afa9228a42a47bc4bf7e2914"},
		{minimal, `{"schema_version":1,"config_source":null,"kubeconfig":"/k.yaml","context":"c","cluster":"a","user":"u","namespace":"default","server":"https://a.example","tls":{"server_name":null,"ca_sha256":null,"system_roots":true},"transport":"direct","tunnel":null}`,
			"sha256:9eecc6c8d51d1130a5f625b56316cbfc3b7d5e2bbff8b0ef498f66b3c3dd5780"},
	}
	for _, v := range vectors {
		if got := string(Canonical(v.target)); got != v.canonical {
			t.Errorf("canonical:\n got %s\nwant %s", got, v.canonical)
		}
		if got := Fingerprint(v.target); got != v.sum {
			t.Errorf("fingerprint = %s, want %s", got, v.sum)
		}
		if !ValidFingerprint(Fingerprint(v.target)) {
			t.Error("fingerprint does not match its own format")
		}
	}
	for _, bad := range []string{"", "sha256:", "sha256:" + strings.Repeat("A", 64), "sha1:" + strings.Repeat("a", 64), "sha256:" + strings.Repeat("a", 63)} {
		if ValidFingerprint(bad) {
			t.Errorf("ValidFingerprint(%q) = true", bad)
		}
	}
}

func TestResolveExplicitPrecedence(t *testing.T) {
	ca := newCA(t)
	e := newEnv(t, "KUBECONFIG=/a/ambient.yaml", "NETCUP_KUBE_CONTEXT=a", "MGMT_HOST=a-mgmt", "KUBE_TRANSPORT=ssh")
	kubeconfig := twoContexts(ca).write(t, filepath.Join(e.dir, "deploy", "kube.json"))
	write(t, filepath.Join(e.dir, "deploy", "b.env"), "KUBECONFIG=kube.json\nNETCUP_KUBE_CONTEXT=b\n")

	t.Run("env file", func(t *testing.T) {
		got, err := e.r.Resolve(Selection{EnvFile: "deploy/b.env"}, false)
		if err != nil {
			t.Fatal(err)
		}
		if got.Kubeconfig != kubeconfig || got.Context != "b" || got.Namespace != "team-b" || got.Transport != "direct" ||
			*got.ConfigSource != filepath.Join(e.dir, "deploy", "b.env") || got.LiveCheck != "not_requested" {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("flag context over env file", func(t *testing.T) {
		got, err := e.r.Resolve(Selection{EnvFile: "deploy/b.env", Context: "a"}, false)
		if err != nil || got.Context != "a" || got.Namespace != "default" {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("ambient context never fills an env file without a context", func(t *testing.T) {
		only := twoContexts(ca)
		only.current = "b"
		only.write(t, filepath.Join(e.dir, "deploy", "current-b.json"))
		write(t, filepath.Join(e.dir, "deploy", "only.env"), "KUBECONFIG=current-b.json\n")
		got, err := e.r.Resolve(Selection{EnvFile: "deploy/only.env"}, false) // ambient NETCUP_KUBE_CONTEXT=a
		if err != nil || got.Context != "b" {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("flag kubeconfig over env file", func(t *testing.T) {
		other := twoContexts(ca).write(t, filepath.Join(e.dir, "other", "kube.json"))
		got, err := e.r.Resolve(Selection{EnvFile: "deploy/b.env", Kubeconfig: other}, false)
		if err != nil || got.Kubeconfig != other || *got.ConfigSource != filepath.Join(e.dir, "deploy", "b.env") {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("process kubeconfig and context", func(t *testing.T) {
		r := e.r
		r.Environ = append(r.Environ, "KUBECONFIG=deploy/kube.json", "NETCUP_KUBE_CONTEXT=b")
		got, err := r.Resolve(Selection{}, false)
		if err != nil || got.Kubeconfig != kubeconfig || got.Context != "b" || got.ConfigSource != nil || got.SelectionMode != "explicit" {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("current-context", func(t *testing.T) {
		r := e.r
		r.Environ = []string{"FAKE_LOG=" + e.log, "PATH=" + os.Getenv("PATH")}
		got, err := r.Resolve(Selection{Kubeconfig: kubeconfig}, false)
		if err != nil || got.Context != "a" {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	before, _ := os.ReadFile(kubeconfig)
	refusals := map[string]Selection{
		"env file without KUBECONFIG": {EnvFile: "deploy/none.env"},
		"missing env file":            {EnvFile: "deploy/missing.env"},
		"path list flag":              {Kubeconfig: "a" + string(os.PathListSeparator) + "b"},
		"path list env file":          {EnvFile: "deploy/list.env"},
		"path list in env-file flag":  {EnvFile: "x" + string(os.PathListSeparator) + "y"},
		"missing context":             {Kubeconfig: kubeconfig, Context: "nope"},
		"unresolved isolated key":     {EnvFile: "deploy/indirect.env"},
		"unknown transport":           {EnvFile: "deploy/transport.env"},
		"ssh without host":            {EnvFile: "deploy/nohost.env"},
	}
	write(t, filepath.Join(e.dir, "deploy", "none.env"), "MGMT_HOST=b\n")
	write(t, filepath.Join(e.dir, "deploy", "list.env"), "KUBECONFIG=a"+string(os.PathListSeparator)+"b\n")
	write(t, filepath.Join(e.dir, "deploy", "indirect.env"), "KUBECONFIG=kube.json\nTUNNEL_HOST=${MGMT_HOST}\n")
	write(t, filepath.Join(e.dir, "deploy", "transport.env"), "KUBECONFIG=kube.json\nKUBE_TRANSPORT=vpn\n")
	write(t, filepath.Join(e.dir, "deploy", "nohost.env"), "KUBECONFIG=kube.json\nKUBE_TRANSPORT=ssh\n")
	for name, sel := range refusals {
		t.Run(name, func(t *testing.T) {
			if _, err := e.r.Resolve(sel, false); err == nil {
				t.Fatal("want refusal")
			} else if strings.Contains(err.Error(), "a-mgmt") || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("error leaks ambient or secret data: %v", err)
			}
		})
	}
	t.Run("unresolved non-isolated reference names the variable", func(t *testing.T) {
		write(t, filepath.Join(e.dir, "deploy", "unset.env"), "KUBECONFIG=kube.json\nNETCUP_KUBE_CONTEXT=${STAGE_CTX}\n")
		got, err := e.r.Resolve(Selection{EnvFile: "deploy/unset.env"}, false)
		if err == nil || !strings.Contains(err.Error(), "${STAGE_CTX}") {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	r := e.r
	r.Environ = []string{"NETCUP_KUBE_CONTEXT=b"}
	if _, err := r.Resolve(Selection{}, false); err == nil {
		t.Fatal("context without kubeconfig must refuse")
	}
	if after, _ := os.ReadFile(kubeconfig); string(after) != string(before) {
		t.Fatal("kubeconfig changed")
	}
}

func TestResolveSSHTransport(t *testing.T) {
	ca := newCA(t)
	e := newEnv(t, "MGMT_HOST=a-mgmt", "TUNNEL_USER=a-user")
	twoContexts(ca).write(t, filepath.Join(e.dir, "kube.json"))
	write(t, filepath.Join(e.dir, "b.env"), "KUBECONFIG=kube.json\nKUBE_TRANSPORT=ssh\nMGMT_IP=10.0.0.2\nDEFAULT_USER=bops\nTUNNEL_LOCAL_PORT=16443\nTUNNEL_REMOTE_PORT=7443\n")
	got, err := e.r.Resolve(Selection{EnvFile: "b.env"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := Tunnel{"bops", "10.0.0.2", "127.0.0.1", "16443", "127.0.0.1", "7443"}
	if got.Transport != "ssh" || got.Tunnel == nil || *got.Tunnel != want {
		t.Fatalf("got %+v", got.Tunnel)
	}
	if _, err := e.r.Resolve(Selection{EnvFile: "b.env"}, true); err == nil || !strings.Contains(err.Error(), "ssh") {
		t.Fatalf("check over ssh must refuse, got %v", err)
	}
	if log, _ := os.ReadFile(e.log); strings.Count(string(log), "\n") != 1 {
		t.Fatalf("ssh check refusal must precede tool calls:\n%s", log)
	}
}

func TestTunnelTupleForms(t *testing.T) {
	ssh := func(kv ...string) map[string]string {
		file := map[string]string{"KUBE_TRANSPORT": "ssh", "TUNNEL_HOST": "10.0.0.2"}
		for i := 0; i < len(kv); i += 2 {
			file[kv[i]] = kv[i+1]
		}
		return file
	}
	accepted := map[string]map[string]string{
		"6443":             ssh("TUNNEL_LOCAL_PORT", "6443"),
		"1":                ssh("TUNNEL_REMOTE_PORT", "1"),
		"65535":            ssh("TUNNEL_LOCAL_PORT", "65535"),
		"ops":              ssh("TUNNEL_USER", "ops"),
		"mgmt.example.com": ssh("TUNNEL_HOST", "mgmt.example.com"),
	}
	for form, file := range accepted {
		t.Run("accepts "+form, func(t *testing.T) {
			got := &Target{}
			if err := transport(file, got); err != nil || got.Tunnel == nil {
				t.Fatalf("got %+v, %v", got.Tunnel, err)
			}
		})
	}
	rejected := map[string][]map[string]string{
		"abc":              {ssh("TUNNEL_LOCAL_PORT", "abc"), ssh("TUNNEL_REMOTE_PORT", "abc")},
		"0":                {ssh("TUNNEL_LOCAL_PORT", "0"), ssh("TUNNEL_REMOTE_PORT", "0")},
		"70000":            {ssh("TUNNEL_LOCAL_PORT", "70000"), ssh("TUNNEL_REMOTE_PORT", "70000")},
		"06443":            {ssh("TUNNEL_LOCAL_PORT", "06443"), ssh("TUNNEL_REMOTE_PORT", "06443")},
		"-oProxyCommand=x": {ssh("TUNNEL_HOST", "-oProxyCommand=x"), ssh("TUNNEL_USER", "-oProxyCommand=x"), ssh("TUNNEL_REMOTE_HOST", "-oProxyCommand=x")},
		"":                 {ssh("TUNNEL_HOST", "")},
		"a b":              {ssh("TUNNEL_HOST", "a b"), ssh("TUNNEL_USER", "a b"), ssh("TUNNEL_REMOTE_HOST", "a b")},
	}
	for form, files := range rejected {
		for _, file := range files {
			t.Run("rejects "+strconv.Quote(form), func(t *testing.T) {
				got := &Target{}
				if err := transport(file, got); err == nil || got.Tunnel != nil {
					t.Fatalf("accepted %+v", file)
				}
			})
		}
	}
}

func TestSymlinkedPathsShareOneFingerprint(t *testing.T) {
	e := newEnv(t)
	realDir := filepath.Join(e.dir, "real")
	twoContexts(newCA(t)).write(t, filepath.Join(realDir, "kube.json"))
	write(t, filepath.Join(realDir, "b.env"), "KUBECONFIG=kube.json\nNETCUP_KUBE_CONTEXT=b\n")
	if err := os.Symlink(realDir, filepath.Join(e.dir, "link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	direct, err := e.r.Resolve(Selection{EnvFile: "real/b.env"}, false)
	if err != nil {
		t.Fatal(err)
	}
	// A linked env file resolves its relative KUBECONFIG next to the target file, not the link;
	// a decoy kubeconfig beside the link would otherwise be selected.
	decoy := twoContexts(newCA(t))
	decoy.write(t, filepath.Join(e.dir, "deploy", "kube.json"))
	if err := os.Symlink(filepath.Join(realDir, "b.env"), filepath.Join(e.dir, "deploy", "b.env")); err != nil {
		t.Fatal(err)
	}
	for _, sel := range []Selection{{EnvFile: "link/b.env"}, {EnvFile: "real/b.env", Kubeconfig: "link/kube.json"}, {EnvFile: "deploy/b.env"}} {
		got, err := e.r.Resolve(sel, false)
		if err != nil || got.Fingerprint != direct.Fingerprint || *got.ConfigSource != filepath.Join(realDir, "b.env") ||
			got.Kubeconfig != filepath.Join(realDir, "kube.json") {
			t.Fatalf("%+v: got %+v, %v", sel, got, err)
		}
	}
}

func TestResolveLegacy(t *testing.T) {
	ca := newCA(t)
	k := twoContexts(ca)
	setup := func(t *testing.T) (*env, string) {
		e := newEnv(t, "MGMT_HOST=a-mgmt")
		e.r.CoreDir = filepath.Join(e.dir, "core")
		e.r.Cwd = filepath.Join(e.dir, "cwd")
		e.r.NodeKubeconfig = filepath.Join(e.dir, "etc", "k3s.yaml")
		return e, e.r.CoreDir
	}
	t.Run("cwd env file wins without merging", func(t *testing.T) {
		e, core := setup(t)
		write(t, filepath.Join(e.r.Cwd, "config", "netcup-kube.env"), "KUBECONFIG=../kube.json\n")
		write(t, filepath.Join(core, "config", "netcup-kube.env"), "NETCUP_KUBE_CONTEXT=b\n")
		kubeconfig := k.write(t, filepath.Join(e.r.Cwd, "kube.json"))
		got, err := e.r.Resolve(Selection{}, false)
		if err != nil || got.SelectionMode != "legacy" || got.Kubeconfig != kubeconfig || got.Context != "a" ||
			*got.ConfigSource != filepath.Join(e.r.Cwd, "config", "netcup-kube.env") {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("core env file then node kubeconfig", func(t *testing.T) {
		e, core := setup(t)
		write(t, filepath.Join(core, "config", "netcup-kube.env"), "NETCUP_KUBE_CONTEXT=b\n")
		k.write(t, e.r.NodeKubeconfig)
		k.write(t, filepath.Join(core, "config", "k3s.yaml"))
		got, err := e.r.Resolve(Selection{}, false)
		if err != nil || got.Kubeconfig != e.r.NodeKubeconfig || got.Context != "b" {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("core kubeconfig without env file", func(t *testing.T) {
		e, core := setup(t)
		kubeconfig := k.write(t, filepath.Join(core, "config", "k3s.yaml"))
		got, err := e.r.Resolve(Selection{}, false)
		if err != nil || got.Kubeconfig != kubeconfig || got.ConfigSource != nil {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("missing kubeconfig instructs and never fetches", func(t *testing.T) {
		e, _ := setup(t)
		write(t, filepath.Join(e.r.Cwd, "config", "netcup-kube.env"), "KUBECONFIG=k3s.yaml\n")
		_, err := e.r.Resolve(Selection{}, false)
		if err == nil || !strings.Contains(err.Error(), "copy the cluster kubeconfig explicitly") || !strings.Contains(err.Error(), "never fetches") {
			t.Fatalf("got %v", err)
		}
		if _, statErr := os.Stat(e.log); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatal("kubectl ran")
		}
	})
	t.Run("no core dir and nothing found", func(t *testing.T) {
		e, _ := setup(t)
		e.r.CoreDir = ""
		if _, err := e.r.Resolve(Selection{}, false); err == nil || !strings.Contains(err.Error(), "config/k3s.yaml") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("unreadable candidate refuses", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads unreadable files")
		}
		e, core := setup(t)
		path := filepath.Join(e.r.Cwd, "config", "netcup-kube.env")
		write(t, path, "KUBECONFIG=x\n")
		k.write(t, filepath.Join(core, "config", "k3s.yaml"))
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := e.r.Resolve(Selection{}, false); err == nil {
			t.Fatal("want refusal")
		}
	})
	t.Run("unreadable node kubeconfig refuses without falling through", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads unreadable files")
		}
		e, core := setup(t)
		k.write(t, e.r.NodeKubeconfig)
		k.write(t, filepath.Join(core, "config", "k3s.yaml"))
		if err := os.Chmod(e.r.NodeKubeconfig, 0); err != nil {
			t.Fatal(err)
		}
		_, err := e.r.Resolve(Selection{}, false)
		if err == nil || !strings.Contains(err.Error(), "not readable") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("linked env file resolves KUBECONFIG next to its target", func(t *testing.T) {
		e, _ := setup(t)
		shared := filepath.Join(e.dir, "shared")
		write(t, filepath.Join(shared, "netcup-kube.env"), "KUBECONFIG=kube.json\n")
		kubeconfig := k.write(t, filepath.Join(shared, "kube.json"))
		k.write(t, filepath.Join(e.r.Cwd, "config", "kube.json")) // decoy beside the link
		if err := os.Symlink(filepath.Join(shared, "netcup-kube.env"), filepath.Join(e.r.Cwd, "config", "netcup-kube.env")); err != nil {
			t.Fatal(err)
		}
		got, err := e.r.Resolve(Selection{}, false)
		if err != nil || got.Kubeconfig != kubeconfig || *got.ConfigSource != filepath.Join(shared, "netcup-kube.env") {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("dangling env file symlink refuses without falling through", func(t *testing.T) {
		e, core := setup(t)
		write(t, filepath.Join(core, "config", "netcup-kube.env"), "NETCUP_KUBE_CONTEXT=b\n")
		k.write(t, filepath.Join(core, "config", "k3s.yaml"))
		link := filepath.Join(e.r.Cwd, "config", "netcup-kube.env")
		if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(e.dir, "unmounted", "b.env"), link); err != nil {
			t.Fatal(err)
		}
		if got, err := e.r.Resolve(Selection{}, false); err == nil {
			t.Fatalf("want refusal, got %+v", got)
		}
		if _, statErr := os.Stat(e.log); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatal("kubectl ran: the core env file was used")
		}
	})
	t.Run("path list in legacy env file", func(t *testing.T) {
		e, _ := setup(t)
		write(t, filepath.Join(e.r.Cwd, "config", "netcup-kube.env"), "KUBECONFIG=a"+string(os.PathListSeparator)+"b\n")
		if _, err := e.r.Resolve(Selection{}, false); err == nil {
			t.Fatal("want refusal")
		}
	})
}

func TestIdentity(t *testing.T) {
	ca, ca2 := newCA(t), newCA(t)
	e := newEnv(t)
	resolve := func(t *testing.T, k kc, ctx string) (*Target, error) {
		t.Helper()
		path := k.write(t, filepath.Join(t.TempDir(), "kube.json"))
		got, err := e.r.Resolve(Selection{Kubeconfig: path, Context: ctx}, false)
		if got != nil {
			got.Kubeconfig = "/fixed" // compare identity independent of the temp path
			got.Fingerprint = Fingerprint(got)
		}
		return got, err
	}
	base, err := resolve(t, twoContexts(ca), "b")
	if err != nil {
		t.Fatal(err)
	}

	caDir := t.TempDir()
	write(t, filepath.Join(caDir, "ca.pem"), ca)
	fileCA := twoContexts(ca)
	fileCA.clusters["b"] = cluster{Server: "https://b.example:6443", CAFile: "ca.pem"}
	path := fileCA.write(t, filepath.Join(caDir, "kube.json"))
	viaFile, err := e.r.Resolve(Selection{Kubeconfig: path, Context: "b"}, false)
	if err != nil || *viaFile.TLS.CASHA256 != *base.TLS.CASHA256 {
		t.Fatalf("equal CA bytes inline and from a file must hash equally: %v", err)
	}

	rotated := twoContexts(ca)
	rotated.users["ub"] = "ROTATED"
	same := map[string]kc{"credential-only rotation": rotated}
	for name, k := range same {
		got, err := resolve(t, k, "b")
		if err != nil || got.Fingerprint != base.Fingerprint {
			t.Errorf("%s changed the fingerprint: %v", name, err)
		}
	}

	changed := map[string]func(k kc) kc{
		"CA change": func(k kc) kc {
			k.clusters["b"] = cluster{Server: "https://b.example:6443", CAData: []byte(ca2)}
			return k
		},
		"server change": func(k kc) kc {
			k.clusters["b"] = cluster{Server: "https://c.example:6443", CAData: []byte(ca)}
			return k
		},
		"server name": func(k kc) kc {
			k.clusters["b"] = cluster{Server: "https://b.example:6443", CAData: []byte(ca), ServerName: "x"}
			return k
		},
		"system roots":      func(k kc) kc { k.clusters["b"] = cluster{Server: "https://b.example:6443"}; return k },
		"namespace change":  func(k kc) kc { k.contexts["b"] = [3]string{"b", "ub", "other"}; return k },
		"context reference": func(k kc) kc { k.contexts["b"] = [3]string{"a", "ub", "team-b"}; return k },
	}
	for name, mutate := range changed {
		got, err := resolve(t, mutate(twoContexts(ca)), "b")
		if err != nil || got.Fingerprint == base.Fingerprint {
			t.Errorf("%s: fingerprint unchanged or error %v", name, err)
		}
	}
	if got, err := resolve(t, twoContexts(ca), "a"); err != nil || got.Fingerprint == base.Fingerprint {
		t.Errorf("context change kept the fingerprint: %v", err)
	}

	refusals := map[string]func(k kc) kc{
		"plain http": func(k kc) kc { k.clusters["b"] = cluster{Server: "http://b.example:6443"}; return k },
		"userinfo":   func(k kc) kc { k.clusters["b"] = cluster{Server: "https://SECRET:pw@b.example"}; return k },
		"query":      func(k kc) kc { k.clusters["b"] = cluster{Server: "https://b.example?token=SECRET"}; return k },
		"fragment":   func(k kc) kc { k.clusters["b"] = cluster{Server: "https://b.example#SECRET"}; return k },
		"insecure":   func(k kc) kc { k.clusters["b"] = cluster{Server: "https://b.example", Insecure: true}; return k },
		"malformed CA": func(k kc) kc {
			k.clusters["b"] = cluster{Server: "https://b.example", CAData: []byte("not a cert")}
			return k
		},
		"unreadable CA": func(k kc) kc {
			k.clusters["b"] = cluster{Server: "https://b.example", CAFile: "/nonexistent/ca.pem"}
			return k
		},
		"both CA forms": func(k kc) kc {
			k.clusters["b"] = cluster{Server: "https://b.example", CAData: []byte(ca), CAFile: "ca.pem"}
			return k
		},
		"missing cluster":    func(k kc) kc { k.contexts["b"] = [3]string{"zz", "ub", ""}; return k },
		"missing user":       func(k kc) kc { k.contexts["b"] = [3]string{"b", "zz", ""}; return k },
		"no current context": func(k kc) kc { k.current = ""; return k },
	}
	for name, mutate := range refusals {
		ctx := "b"
		if name == "no current context" {
			ctx = ""
		}
		_, err := resolve(t, mutate(twoContexts(ca)), ctx)
		if err == nil {
			t.Errorf("%s: want refusal", name)
		} else if strings.Contains(err.Error(), "SECRET") {
			t.Errorf("%s: error leaks secret: %v", name, err)
		}
	}

	dup := filepath.Join(t.TempDir(), "dup.json")
	write(t, dup, `{"clusters":[{"name":"b","cluster":{"server":"https://b.example"}}],"users":[{"name":"u"}],"contexts":[{"name":"x","context":{"cluster":"b","user":"u"}},{"name":"x","context":{"cluster":"b","user":"u"}}]}`)
	if _, err := e.r.Resolve(Selection{Kubeconfig: dup, Context: "x"}, false); err == nil {
		t.Error("duplicate context names must refuse")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	write(t, bad, `SECRET-not-json`)
	if _, err := e.r.Resolve(Selection{Kubeconfig: bad}, false); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("unparseable kubeconfig: %v", err)
	}
}

func TestKubercControl(t *testing.T) {
	ca := newCA(t)
	view := func(policy string) string {
		return `{"kind":"Preference","apiVersion":"kubectl.config.k8s.io/v1beta1","defaults":[{"command":"get","options":[{"name":"server","default":"https://a.example"}]}],"aliases":[{"name":"g","command":"get"}]` + policy + `}`
	}
	cases := []struct {
		name, kuberc, view string
		home, fail         bool
		wantErr            bool
		wantProjection     string // "" means KUBERC=off
	}{
		{name: "no preference file"},
		{name: "KUBERC off", kuberc: "off"},
		{name: "HOME DenyAll", home: true, view: view(`,"credentialPluginPolicy":"DenyAll"`),
			wantProjection: `{"apiVersion":"kubectl.config.k8s.io/v1beta1","kind":"Preference","credentialPluginPolicy":"DenyAll"}`},
		{name: "KUBERC Allowlist", kuberc: "prefs", view: view(`,"credentialPluginPolicy":"Allowlist","credentialPluginAllowlist":[{"command":"/bin/x"}]`),
			wantProjection: `{"apiVersion":"kubectl.config.k8s.io/v1beta1","kind":"Preference","credentialPluginPolicy":"Allowlist","credentialPluginAllowlist":[{"command":"/bin/x"}]}`},
		{name: "no policy", kuberc: "prefs", view: view(""),
			wantProjection: `{"apiVersion":"kubectl.config.k8s.io/v1beta1","kind":"Preference"}`},
		{name: "unknown apiVersion", kuberc: "prefs", view: `{"kind":"Preference","apiVersion":"example.com/v9"}`, wantErr: true},
		{name: "unknown kind", kuberc: "prefs", view: `{"kind":"Other","apiVersion":"kubectl.config.k8s.io/v1beta1"}`, wantErr: true},
		{name: "unknown top-level key", kuberc: "prefs", view: view(`,"overrides":{"server":"SECRET"}`), wantErr: true},
		{name: "not JSON", kuberc: "prefs", view: `SECRET`, wantErr: true},
		{name: "client cannot view", kuberc: "prefs", fail: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, "FAKE_KUBERC_VIEW="+tc.view)
			if tc.fail {
				e.r.Environ = append(e.r.Environ, "FAKE_KUBERC_FAIL=1")
			}
			if tc.kuberc != "" {
				e.r.Environ = append(e.r.Environ, "KUBERC="+tc.kuberc)
			}
			if tc.home {
				write(t, filepath.Join(e.dir, "home", ".kube", "kuberc"), "user file")
			}
			if err := os.Mkdir(filepath.Join(e.dir, "tmp"), 0700); err != nil {
				t.Fatal(err)
			}
			kubeconfig := twoContexts(ca).write(t, filepath.Join(e.dir, "kube.json"))
			t.Setenv("TMPDIR", filepath.Join(e.dir, "tmp"))
			_, err := e.r.Resolve(Selection{Kubeconfig: kubeconfig}, false)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("error leaks preference content: %v", err)
			}
			log, _ := os.ReadFile(e.log)
			if tc.wantErr {
				if strings.Contains(string(log), "[config]") {
					t.Fatal("kubeconfig captured despite preference refusal")
				}
				return
			}
			projection, _ := os.ReadFile(e.projection)
			if string(projection) != tc.wantProjection {
				t.Fatalf("projection = %s, want %s", projection, tc.wantProjection)
			}
			if tc.wantProjection == "" && !strings.Contains(string(log), "KUBERC=off [config]") {
				t.Fatalf("capture must run with KUBERC=off:\n%s", log)
			}
			if tc.view != "" && !strings.Contains(string(log), "KUBERC=<unset> [kuberc] [view] [--kuberc]") {
				t.Fatalf("kuberc view must run without inherited KUBERC:\n%s", log)
			}
			if tc.kuberc == "prefs" && !strings.Contains(string(log), "[--kuberc] ["+filepath.Join(e.dir, "prefs")+"]") {
				t.Fatalf("relative KUBERC must resolve against Cwd:\n%s", log)
			}
			if left, _ := os.ReadDir(filepath.Join(e.dir, "tmp")); len(left) != 0 {
				t.Fatalf("projection not removed: %v", left)
			}
		})
	}
}

func TestHomeKubercStatErrorRefuses(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	e := newEnv(t)
	kubeconfig := twoContexts(newCA(t)).write(t, filepath.Join(e.dir, "kube.json"))
	kubeDir := filepath.Join(e.dir, "home", ".kube")
	write(t, filepath.Join(kubeDir, "kuberc"), "user file")
	if err := os.Chmod(kubeDir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(kubeDir, 0755) })
	_, err := e.r.Resolve(Selection{Kubeconfig: kubeconfig}, false)
	if err == nil || strings.Contains(err.Error(), e.dir) {
		t.Fatalf("want redacted refusal, got %v", err)
	}
	if log, _ := os.ReadFile(e.log); len(log) != 0 {
		t.Fatalf("kubectl ran despite refusal:\n%s", log)
	}
}

func TestReadableRefusesFIFO(t *testing.T) {
	e := newEnv(t)
	fifo := filepath.Join(e.dir, "kube.fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	caKubeconfig := twoContexts(newCA(t))
	caKubeconfig.clusters["b"] = cluster{Server: "https://b.example:6443", CAFile: "kube.fifo"}
	caKubeconfig.write(t, filepath.Join(e.dir, "ca-fifo.json"))
	for name, sel := range map[string]Selection{
		"FIFO kubeconfig": {Kubeconfig: fifo},
		"FIFO CA":         {Kubeconfig: filepath.Join(e.dir, "ca-fifo.json"), Context: "b"},
	} {
		t.Run(name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() { _, err := e.r.Resolve(sel, false); done <- err }()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("FIFO must refuse")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("readable blocked on a FIFO")
			}
		})
	}
}

func TestLiveCheck(t *testing.T) {
	ca := newCA(t)
	if DefaultCheckTimeout != 10*time.Second {
		t.Fatalf("default live-check deadline = %s", DefaultCheckTimeout)
	}
	t.Run("verified", func(t *testing.T) {
		e := newEnv(t)
		k := twoContexts(ca)
		b := k.clusters["b"]
		b.ServerName = "api.b.example"
		k.clusters["b"] = b
		kubeconfig := k.write(t, filepath.Join(e.dir, "kube.json"))
		got, err := e.r.Resolve(Selection{Kubeconfig: kubeconfig, Context: "b"}, true)
		if err != nil || got.LiveCheck != "verified" {
			t.Fatalf("got %+v, %v", got, err)
		}
		log, _ := os.ReadFile(e.log)
		if !strings.Contains(string(log), "KUBERC=off [get] [--raw] [/version] [--request-timeout=10s] [--kubeconfig] ["+kubeconfig+"] [--context] [b]"+
			" [--server] [https://b.example:6443] [--tls-server-name] [api.b.example]") {
			t.Fatalf("live check argv:\n%s", log)
		}
	})
	t.Run("kubeconfig changed during the check refuses", func(t *testing.T) {
		e := newEnv(t)
		kubeconfig := twoContexts(ca).write(t, filepath.Join(e.dir, "kube.json"))
		moved := twoContexts(ca)
		moved.clusters["b"] = cluster{Server: "https://moved.example:6443", CAData: []byte(ca)}
		swap := moved.write(t, filepath.Join(e.dir, "moved.json"))
		e.r.Environ = append(e.r.Environ, "FAKE_SWAP="+swap, "FAKE_SWAP_TO="+kubeconfig)
		got, err := e.r.Resolve(Selection{Kubeconfig: kubeconfig, Context: "b"}, true)
		if err == nil || got != nil || !strings.Contains(err.Error(), "changed during the live check") {
			t.Fatalf("got %+v, %v", got, err)
		}
		log, _ := os.ReadFile(e.log)
		if !strings.Contains(string(log), "[--server] [https://b.example:6443]") {
			t.Fatalf("live check was not pinned to the captured server:\n%s", log)
		}
	})
	t.Run("failure is redacted", func(t *testing.T) {
		e := newEnv(t, "FAKE_FAIL_GET=SECRET-TOKEN")
		kubeconfig := twoContexts(ca).write(t, filepath.Join(e.dir, "kube.json"))
		got, err := e.r.Resolve(Selection{Kubeconfig: kubeconfig, Context: "b"}, true)
		if err == nil || got != nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("capture failure is redacted", func(t *testing.T) {
		e := newEnv(t, "FAKE_FAIL=SECRET-TOKEN")
		kubeconfig := twoContexts(ca).write(t, filepath.Join(e.dir, "kube.json"))
		if _, err := e.r.Resolve(Selection{Kubeconfig: kubeconfig}, false); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("missing kubectl", func(t *testing.T) {
		e := newEnv(t)
		e.r.Kubectl = filepath.Join(e.dir, "absent")
		kubeconfig := twoContexts(ca).write(t, filepath.Join(e.dir, "kube.json"))
		if _, err := e.r.Resolve(Selection{Kubeconfig: kubeconfig}, false); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("hanging client is killed and reaped", func(t *testing.T) {
		e := newEnv(t)
		pidFile := filepath.Join(e.dir, "child.pid")
		e.r.Environ = append(e.r.Environ, "FAKE_HANG="+pidFile)
		// The deadline leaves a slow runner time to start the fixture and record its child.
		e.r.CheckTimeout = 2 * time.Second
		kubeconfig := twoContexts(ca).write(t, filepath.Join(e.dir, "kube.json"))
		start := time.Now()
		got, err := e.r.Resolve(Selection{Kubeconfig: kubeconfig, Context: "b"}, true)
		if err == nil || got != nil || !strings.Contains(err.Error(), "did not finish within 2s") {
			t.Fatalf("got %+v, %v", got, err)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("live check returned after %s", elapsed)
		}
		assertReaped(t, pidFile)
	})
	t.Run("hangup kills and reaps the client", func(t *testing.T) {
		e := newEnv(t)
		pidFile := filepath.Join(e.dir, "child.pid")
		e.r.Environ = append(e.r.Environ, "FAKE_HANG="+pidFile)
		e.r.CheckTimeout = 30 * time.Second
		kubeconfig := twoContexts(ca).write(t, filepath.Join(e.dir, "kube.json"))
		go func() {
			// The pid file exists only while liveCheck runs kubectl, so the trap is installed.
			for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				if data, _ := os.ReadFile(pidFile); strings.TrimSpace(string(data)) != "" {
					_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
					return
				}
			}
		}()
		start := time.Now()
		got, err := e.r.Resolve(Selection{Kubeconfig: kubeconfig, Context: "b"}, true)
		if err == nil || got != nil || !strings.Contains(err.Error(), "interrupted") {
			t.Fatalf("got %+v, %v", got, err)
		}
		if elapsed := time.Since(start); elapsed > 15*time.Second {
			t.Fatalf("live check returned after %s", elapsed)
		}
		assertReaped(t, pidFile)
	})
}

// assertReaped waits until the hanging fixture's recorded child no longer exists.
func assertReaped(t *testing.T, pidFile string) {
	t.Helper()
	data, _ := os.ReadFile(pidFile)
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil {
		t.Fatalf("hanging fixture did not record its child: %q", data)
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("process group member %d survived the deadline", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
