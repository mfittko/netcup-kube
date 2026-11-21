// Package target resolves the one cluster target a command operates on and computes its identity.
//
// # Schema v1 canonical serialization
//
// The fingerprint is "sha256:" followed by the lowercase hex SHA-256 of the canonical bytes. The
// canonical bytes are one JSON object produced by Go encoding/json Marshal: UTF-8, no
// insignificant whitespace, no trailing newline, HTML-sensitive characters escaped as <,
// > and &, explicit null for absent optional fields, and exactly this field order:
//
//	schema_version  number, always 1
//	config_source   absolute env-file path with symlinks resolved, or null
//	kubeconfig      absolute kubeconfig path with symlinks resolved
//	context         selected context name
//	cluster         cluster name referenced by the context
//	user            user name referenced by the context
//	namespace       context namespace, or "default"
//	server          https API server URL
//	tls             object: server_name (string or null), ca_sha256 (lowercase hex of the
//	                CA bytes, or null), system_roots (true only when no CA is set)
//	transport       "direct" or "ssh"
//	tunnel          null, or object: ssh_user, ssh_host, local_host, local_port,
//	                remote_host, remote_port (all strings)
//
// Authentication material (tokens, client keys, passwords, exec plugin settings) never enters
// the canonical bytes, so credential rotation alone leaves the fingerprint unchanged.
package target

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mfittko/netcup-kube/internal/config"
)

// SchemaVersion is the version of the canonical serialization and the inspection object.
const SchemaVersion = 1

// DefaultCheckTimeout bounds the whole live-check subprocess.
const DefaultCheckTimeout = 10 * time.Second

// Selection holds the command-line selectors; empty means not given.
type Selection struct {
	EnvFile, Kubeconfig, Context string
}

// Resolver resolves a Selection against the filesystem, the process environment and kubectl.
type Resolver struct {
	Cwd            string   // base for relative command-line and process-environment paths
	CoreDir        string   // core checkout for legacy candidates; empty disables them
	NodeKubeconfig string   // legacy node-local kubeconfig path
	Environ        []string // process environment
	Kubectl        string   // kubectl binary name or path
	CheckTimeout   time.Duration
}

// TLS is the trust part of the identity.
type TLS struct {
	ServerName  *string `json:"server_name"`
	CASHA256    *string `json:"ca_sha256"`
	SystemRoots bool    `json:"system_roots"`
}

// Tunnel is the declared SSH transport tuple.
type Tunnel struct {
	SSHUser    string `json:"ssh_user"`
	SSHHost    string `json:"ssh_host"`
	LocalHost  string `json:"local_host"`
	LocalPort  string `json:"local_port"`
	RemoteHost string `json:"remote_host"`
	RemotePort string `json:"remote_port"`
}

// Target is the schema-v1 inspection object. It holds no authentication material.
type Target struct {
	SchemaVersion int     `json:"schema_version"`
	ConfigSource  *string `json:"config_source"`
	SelectionMode string  `json:"selection_mode"`
	Kubeconfig    string  `json:"kubeconfig"`
	Context       string  `json:"context"`
	Cluster       string  `json:"cluster"`
	User          string  `json:"user"`
	Namespace     string  `json:"namespace"`
	Server        string  `json:"server"`
	TLS           TLS     `json:"tls"`
	Transport     string  `json:"transport"`
	Tunnel        *Tunnel `json:"tunnel"`
	Fingerprint   string  `json:"fingerprint"`
	LiveCheck     string  `json:"live_check"`
}

type canonical struct {
	SchemaVersion int     `json:"schema_version"`
	ConfigSource  *string `json:"config_source"`
	Kubeconfig    string  `json:"kubeconfig"`
	Context       string  `json:"context"`
	Cluster       string  `json:"cluster"`
	User          string  `json:"user"`
	Namespace     string  `json:"namespace"`
	Server        string  `json:"server"`
	TLS           TLS     `json:"tls"`
	Transport     string  `json:"transport"`
	Tunnel        *Tunnel `json:"tunnel"`
}

// Canonical returns the schema-v1 canonical bytes of t.
func Canonical(t *Target) []byte {
	data, err := json.Marshal(canonical{SchemaVersion, t.ConfigSource, t.Kubeconfig, t.Context, t.Cluster,
		t.User, t.Namespace, t.Server, t.TLS, t.Transport, t.Tunnel})
	if err != nil {
		panic(err) // only strings, bools and ints: Marshal cannot fail
	}
	return data
}

// Fingerprint returns "sha256:" plus the lowercase hex digest of Canonical(t).
func Fingerprint(t *Target) string {
	sum := sha256.Sum256(Canonical(t))
	return "sha256:" + hex.EncodeToString(sum[:])
}

var fingerprintPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ValidFingerprint reports whether s is "sha256:" followed by 64 lowercase hex digits.
func ValidFingerprint(s string) bool { return fingerprintPattern.MatchString(s) }

// isolated reports whether an ambient variable may select or redirect a target.
func isolated(key string) bool {
	switch key {
	case "KUBECONFIG", "NETCUP_KUBE_CONTEXT", "KUBE_TRANSPORT", "DEFAULT_USER", "HELM_NAMESPACE":
		return true
	}
	for _, prefix := range []string{"MGMT_", "TUNNEL_", "EDGE_", "CADDY_", "HELM_KUBE"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func envMap(environ []string) map[string]string {
	env := make(map[string]string, len(environ))
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

// path resolves raw against base and refuses path lists. An empty raw stays empty.
func path(base, raw, what string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if strings.ContainsRune(raw, os.PathListSeparator) {
		return "", fmt.Errorf("%s %q is a path list; select exactly one file", what, raw)
	}
	if !filepath.IsAbs(raw) {
		raw = filepath.Join(base, raw)
	}
	return filepath.Clean(raw), nil
}

// realpath returns the symlink-free path of a readable file, so one file has one recorded path.
func realpath(p string) (string, error) {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("%s could not be resolved: %w", p, err)
	}
	return resolved, nil
}

// readable requires a regular file before opening it, so a FIFO or device never blocks Open.
func readable(p string) error {
	info, err := os.Stat(p)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", p)
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	return f.Close()
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return !errors.Is(err, fs.ErrNotExist)
}

// loadEnvFile parses a selected env file and returns its canonical path. Isolated keys never
// resolve from the environment.
func loadEnvFile(p string, env map[string]string) (map[string]string, string, error) {
	if err := readable(p); err != nil {
		return nil, "", fmt.Errorf("env file is not readable: %w", err)
	}
	source, err := realpath(p)
	if err != nil {
		return nil, "", err
	}
	file, err := config.ParseEnvFile(source, func(name string) (string, bool) {
		if isolated(name) {
			return "", false
		}
		return env[name], true
	})
	return file, source, err
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// Resolve resolves sel into a Target. With check it also runs the bounded live API read.
func (r Resolver) Resolve(sel Selection, check bool) (*Target, error) {
	env := envMap(r.Environ)
	t := &Target{SchemaVersion: SchemaVersion, SelectionMode: "explicit", Transport: "direct", LiveCheck: "not_requested"}

	kubeconfig, err := path(r.Cwd, sel.Kubeconfig, "--kubeconfig")
	if err != nil {
		return nil, err
	}
	contextName := sel.Context
	var file map[string]string

	switch {
	case sel.EnvFile != "":
		given, err := path(r.Cwd, sel.EnvFile, "--env-file")
		if err != nil {
			return nil, err
		}
		var source string
		if file, source, err = loadEnvFile(given, env); err != nil {
			return nil, err
		}
		t.ConfigSource = &source
		if kubeconfig == "" {
			if kubeconfig, err = path(filepath.Dir(given), file["KUBECONFIG"], "KUBECONFIG in "+source); err != nil {
				return nil, err
			}
		}
		if kubeconfig == "" {
			return nil, fmt.Errorf("env file %s sets no KUBECONFIG; set it there or pass --kubeconfig", source)
		}
		contextName = firstNonEmpty(contextName, file["NETCUP_KUBE_CONTEXT"])
	case sel.Kubeconfig != "" || sel.Context != "" || env["KUBECONFIG"] != "" || env["NETCUP_KUBE_CONTEXT"] != "":
		if kubeconfig == "" {
			if kubeconfig, err = path(r.Cwd, env["KUBECONFIG"], "KUBECONFIG"); err != nil {
				return nil, err
			}
		}
		if kubeconfig == "" {
			return nil, errors.New("a context was selected without a kubeconfig; pass --kubeconfig or set KUBECONFIG")
		}
		// Ambient NETCUP_KUBE_CONTEXT selects only within the ambient KUBECONFIG.
		if sel.Kubeconfig == "" {
			contextName = firstNonEmpty(contextName, env["NETCUP_KUBE_CONTEXT"])
		}
	default:
		t.SelectionMode = "legacy"
		if file, kubeconfig, err = r.legacy(env, t); err != nil {
			return nil, err
		}
		contextName = file["NETCUP_KUBE_CONTEXT"]
	}

	if err := readable(kubeconfig); err != nil {
		return nil, fmt.Errorf("kubeconfig is not readable: %w", err)
	}
	if kubeconfig, err = realpath(kubeconfig); err != nil {
		return nil, err
	}
	t.Kubeconfig = kubeconfig
	if err := transport(file, t); err != nil {
		return nil, err
	}
	if check && t.Transport == "ssh" {
		return nil, errors.New("live check over ssh transport is not supported until exact tunnel verification exists; refusing")
	}

	// One signal context covers every kubectl call, so an interrupt kills the running call and
	// the deferred projection cleanup still runs.
	ctx, stop := signal.NotifyContext(context.Background(), interruptSignals...)
	defer stop()
	toolEnv, cleanup, err := r.kubectlEnv(ctx, env)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	raw, err := r.output(ctx, toolEnv, "config", "view", "--raw", "-o", "json", "--kubeconfig", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("kubectl could not read kubeconfig %s (%v)", kubeconfig, err)
	}
	if err := identity(raw, contextName, t); err != nil {
		return nil, err
	}
	t.Fingerprint = Fingerprint(t)

	if check {
		if err := r.liveCheck(ctx, toolEnv, t); err != nil {
			return nil, err
		}
		t.LiveCheck = "verified"
	}
	return t, nil
}

// legacy selects at most one default env file and the kubeconfig fallback chain. It never fetches.
func (r Resolver) legacy(env map[string]string, t *Target) (map[string]string, string, error) {
	candidates := []string{filepath.Join(r.Cwd, "config", "netcup-kube.env")}
	if r.CoreDir != "" {
		candidates = append(candidates, filepath.Join(r.CoreDir, "config", "netcup-kube.env"))
	}
	var file map[string]string
	kubeconfig := ""
	for _, c := range candidates {
		if _, err := os.Stat(c); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		var source string
		var err error
		if file, source, err = loadEnvFile(c, env); err != nil {
			return nil, "", err
		}
		t.ConfigSource = &source
		if kubeconfig, err = path(filepath.Dir(c), file["KUBECONFIG"], "KUBECONFIG in "+c); err != nil {
			return nil, "", err
		}
		break
	}
	switch {
	case kubeconfig != "":
	case r.NodeKubeconfig != "" && exists(r.NodeKubeconfig):
		// An existing node kubeconfig is the selected candidate; Resolve refuses it when unreadable.
		kubeconfig = r.NodeKubeconfig
	case r.CoreDir != "":
		kubeconfig = filepath.Join(r.CoreDir, "config", "k3s.yaml")
	}
	if _, err := os.Stat(kubeconfig); kubeconfig == "" || errors.Is(err, fs.ErrNotExist) {
		return nil, "", fmt.Errorf("no kubeconfig found: copy the cluster kubeconfig explicitly "+
			"(for example: scp <user>@<host>:/etc/rancher/k3s/k3s.yaml %s), set KUBECONFIG in the env file, "+
			"or pass --kubeconfig; netcup-kube target never fetches a kubeconfig automatically",
			firstNonEmpty(kubeconfig, "config/k3s.yaml"))
	}
	return file, kubeconfig, nil
}

// transport reads KUBE_TRANSPORT and the tunnel tuple from the selected env file only.
func transport(file map[string]string, t *Target) error {
	switch file["KUBE_TRANSPORT"] {
	case "", "direct":
		return nil
	case "ssh":
	default:
		return fmt.Errorf("unsupported KUBE_TRANSPORT %q: use direct or ssh", file["KUBE_TRANSPORT"])
	}
	host := firstNonEmpty(file["TUNNEL_HOST"], file["MGMT_HOST"], file["MGMT_IP"])
	if host == "" {
		return errors.New("KUBE_TRANSPORT=ssh needs TUNNEL_HOST, MGMT_HOST or MGMT_IP in the selected env file")
	}
	tunnel := &Tunnel{
		SSHUser:    firstNonEmpty(file["TUNNEL_USER"], file["MGMT_USER"], file["DEFAULT_USER"], "ops"),
		SSHHost:    host,
		LocalHost:  "127.0.0.1",
		LocalPort:  firstNonEmpty(file["TUNNEL_LOCAL_PORT"], "6443"),
		RemoteHost: firstNonEmpty(file["TUNNEL_REMOTE_HOST"], "127.0.0.1"),
		RemotePort: firstNonEmpty(file["TUNNEL_REMOTE_PORT"], "6443"),
	}
	for _, f := range [][2]string{{"ssh user", tunnel.SSHUser}, {"ssh host", tunnel.SSHHost}, {"remote host", tunnel.RemoteHost}} {
		if !validSSHName(f[1]) {
			return fmt.Errorf("tunnel %s %q must be non-empty, contain no whitespace and not start with '-'", f[0], f[1])
		}
	}
	for _, f := range [][2]string{{"local port", tunnel.LocalPort}, {"remote port", tunnel.RemotePort}} {
		if !validPort(f[1]) {
			return fmt.Errorf("tunnel %s %q must be a decimal integer from 1 to 65535 without leading zeros", f[0], f[1])
		}
	}
	t.Transport = "ssh"
	t.Tunnel = tunnel
	return nil
}

// validSSHName refuses values that ssh could read as an option or split into two arguments.
func validSSHName(s string) bool {
	return s != "" && !strings.HasPrefix(s, "-") && !strings.ContainsFunc(s, unicode.IsSpace)
}

var portPattern = regexp.MustCompile(`^[1-9][0-9]{0,4}$`)

// validPort accepts only the canonical decimal form of 1 to 65535.
func validPort(s string) bool {
	if !portPattern.MatchString(s) {
		return false
	}
	n, err := strconv.Atoi(s)
	return err == nil && n <= 65535
}

type kubeconfigView struct {
	Clusters []struct {
		Name    string `json:"name"`
		Cluster struct {
			Server     string `json:"server"`
			CAFile     string `json:"certificate-authority"`
			CAData     string `json:"certificate-authority-data"`
			Insecure   bool   `json:"insecure-skip-tls-verify"`
			ServerName string `json:"tls-server-name"`
		} `json:"cluster"`
	} `json:"clusters"`
	Users []struct {
		Name string `json:"name"`
	} `json:"users"`
	Contexts []struct {
		Name    string `json:"name"`
		Context struct {
			Cluster   string `json:"cluster"`
			User      string `json:"user"`
			Namespace string `json:"namespace"`
		} `json:"context"`
	} `json:"contexts"`
	CurrentContext string `json:"current-context"`
}

// identity fills the non-secret identity from captured kubeconfig JSON. Errors never quote it.
func identity(raw []byte, contextName string, t *Target) error {
	var kc kubeconfigView
	if err := json.Unmarshal(raw, &kc); err != nil {
		return fmt.Errorf("kubeconfig %s could not be parsed", t.Kubeconfig)
	}
	contextName = firstNonEmpty(contextName, kc.CurrentContext)
	if contextName == "" {
		return fmt.Errorf("no context selected and kubeconfig %s has no current-context; pass --context", t.Kubeconfig)
	}
	n, ci := 0, 0
	for i, c := range kc.Contexts {
		if c.Name == contextName {
			n, ci = n+1, i
		}
	}
	if n != 1 {
		return fmt.Errorf("kubeconfig %s must contain exactly one context %q (found %d)", t.Kubeconfig, contextName, n)
	}
	ctx := kc.Contexts[ci].Context
	n, cl := 0, 0
	for i, c := range kc.Clusters {
		if c.Name == ctx.Cluster {
			n, cl = n+1, i
		}
	}
	if n != 1 {
		return fmt.Errorf("context %q must reference exactly one cluster %q (found %d)", contextName, ctx.Cluster, n)
	}
	n = 0
	for _, u := range kc.Users {
		if u.Name == ctx.User {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("context %q must reference exactly one user %q (found %d)", contextName, ctx.User, n)
	}
	c := kc.Clusters[cl].Cluster
	u, err := url.Parse(c.Server)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("cluster %q: server must be an https:// URL", ctx.Cluster)
	}
	if u.User != nil || strings.ContainsAny(c.Server, "?#") {
		return fmt.Errorf("cluster %q: server URL must not contain userinfo, a query or a fragment", ctx.Cluster)
	}
	if c.Insecure {
		return fmt.Errorf("cluster %q: insecure-skip-tls-verify is not allowed", ctx.Cluster)
	}

	var ca []byte
	switch {
	case c.CAData != "" && c.CAFile != "":
		return fmt.Errorf("cluster %q sets both certificate-authority and certificate-authority-data", ctx.Cluster)
	case c.CAData != "":
		if ca, err = base64.StdEncoding.DecodeString(c.CAData); err != nil {
			return fmt.Errorf("cluster %q: certificate-authority-data is malformed", ctx.Cluster)
		}
	case c.CAFile != "":
		caPath := c.CAFile
		if !filepath.IsAbs(caPath) {
			caPath = filepath.Join(filepath.Dir(t.Kubeconfig), caPath)
		}
		if err = readable(caPath); err == nil {
			ca, err = os.ReadFile(caPath)
		}
		if err != nil {
			return fmt.Errorf("cluster %q: certificate-authority is not readable: %w", ctx.Cluster, err)
		}
	}
	if ca != nil {
		if !x509.NewCertPool().AppendCertsFromPEM(ca) {
			return fmt.Errorf("cluster %q: certificate authority holds no valid PEM certificate", ctx.Cluster)
		}
		sum := sha256.Sum256(ca)
		digest := hex.EncodeToString(sum[:])
		t.TLS.CASHA256 = &digest
	}
	t.TLS.SystemRoots = ca == nil
	if c.ServerName != "" {
		name := c.ServerName
		t.TLS.ServerName = &name
	}
	t.Context, t.Cluster, t.User, t.Server = contextName, ctx.Cluster, ctx.User, c.Server
	t.Namespace = firstNonEmpty(ctx.Namespace, "default")
	return nil
}
