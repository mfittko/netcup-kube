package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/mfittko/netcup-kube/internal/target"
	"github.com/spf13/cobra"
)

// onceFlag refuses empty values, flag-like values and conflicting repeated values.
type onceFlag struct{ name, value string }

func (f *onceFlag) String() string { return f.value }
func (f *onceFlag) Type() string   { return "string" }
func (f *onceFlag) Set(v string) error {
	if v == "" || strings.HasPrefix(v, "-") {
		return fmt.Errorf("--%s requires a value", f.name)
	}
	if f.value != "" && f.value != v {
		return fmt.Errorf("conflicting --%s values", f.name)
	}
	f.value = v
	return nil
}

var (
	targetEnvFile     = onceFlag{name: "env-file"}
	targetKubeconfig  = onceFlag{name: "kubeconfig"}
	targetContext     = onceFlag{name: "context"}
	targetOutput      = onceFlag{name: "output"}
	targetFingerprint = onceFlag{name: "fingerprint"}
	targetCheck       bool
)

var targetCmd = &cobra.Command{
	Use:   "target",
	Short: "Inspect and verify the selected cluster target",
	Long: `Inspect and verify the cluster target that netcup-kube resolves.

Selection: --env-file selects one deployment env file (its KUBECONFIG and NETCUP_KUBE_CONTEXT);
--kubeconfig and --context override it. Without an env file, --kubeconfig or KUBECONFIG and
--context or NETCUP_KUBE_CONTEXT select the target. NETCUP_KUBE_CONTEXT applies only with ambient
KUBECONFIG, never with --kubeconfig. With no selector, the legacy defaults apply.
These commands never fetch a kubeconfig, start a tunnel or change files.

Examples:
  netcup-kube target inspect --env-file deploy/b.env
  netcup-kube target inspect --kubeconfig ~/b.yaml --context b --output json --check
  netcup-kube target verify --env-file deploy/b.env --fingerprint sha256:<hex>`,
	// Replaces the root hook: target commands read only their own selection inputs.
	PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
}

var targetInspectCmd = &cobra.Command{
	Use:   "inspect",
	Short: "Print the resolved target and its fingerprint",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		format := targetOutput.value
		if format == "" {
			format = "text"
		}
		if format != "text" && format != "json" {
			return fmt.Errorf("unsupported --output %q: use text or json", format)
		}
		t, err := resolveTarget(targetCheck)
		if err != nil {
			return err
		}
		if format == "json" {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(t)
		}
		printTarget(cmd.OutOrStdout(), t)
		return nil
	},
}

var targetVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify the resolved target against an expected fingerprint",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		want := targetFingerprint.value
		if want == "" {
			return fmt.Errorf("--fingerprint is required")
		}
		if !target.ValidFingerprint(want) {
			return fmt.Errorf("malformed --fingerprint: expected sha256: followed by 64 lowercase hex digits")
		}
		t, err := resolveTarget(false)
		if err != nil {
			return err
		}
		if t.Fingerprint != want {
			return fmt.Errorf("target fingerprint mismatch: context %q resolves to %s", t.Context, t.Fingerprint)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "target fingerprint matches: context %q, server %s\n", t.Context, t.Server)
		return err
	},
}

func init() {
	pf := targetCmd.PersistentFlags()
	pf.Var(&targetEnvFile, "env-file", "Deployment env file that selects the target")
	pf.Var(&targetKubeconfig, "kubeconfig", "Kubeconfig file (one file, no path list)")
	pf.Var(&targetContext, "context", "Kubeconfig context")
	targetInspectCmd.Flags().VarP(&targetOutput, "output", "o", "Output format: text or json (default text)")
	targetInspectCmd.Flags().BoolVar(&targetCheck, "check", false, "Also read /version from the API server (10s deadline)")
	targetVerifyCmd.Flags().Var(&targetFingerprint, "fingerprint", "Expected fingerprint (sha256:<64 lowercase hex>)")
	targetCmd.AddCommand(targetInspectCmd, targetVerifyCmd)
	rootCmd.AddCommand(targetCmd)
}

func resolveTarget(check bool) (*target.Target, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	core, _ := findProjectRoot() // only the legacy path uses it; empty disables core candidates
	return target.Resolver{
		Cwd:            cwd,
		CoreDir:        core,
		NodeKubeconfig: serverKubeconfigPath,
		Environ:        os.Environ(),
		Kubectl:        "kubectl",
	}.Resolve(target.Selection{
		EnvFile:    targetEnvFile.value,
		Kubeconfig: targetKubeconfig.value,
		Context:    targetContext.value,
	}, check)
}

// printable quotes s when it holds a non-printable character, so a kubeconfig-derived name
// cannot inject an output line or a terminal escape.
func printable(s string) string {
	if strings.IndexFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
		return strconv.Quote(s)
	}
	return s
}

func printTarget(w io.Writer, t *target.Target) {
	orNone := func(s *string) string {
		if s == nil {
			return "(none)"
		}
		return printable(*s)
	}
	tunnel := "(none)"
	if t.Tunnel != nil {
		tunnel = printable(fmt.Sprintf("%s@%s %s:%s -> %s:%s", t.Tunnel.SSHUser, t.Tunnel.SSHHost,
			t.Tunnel.LocalHost, t.Tunnel.LocalPort, t.Tunnel.RemoteHost, t.Tunnel.RemotePort))
	}
	_, _ = fmt.Fprintf(w, "schema_version: %d\nselection_mode: %s\nconfig_source: %s\nkubeconfig: %s\n"+
		"context: %s\ncluster: %s\nuser: %s\nnamespace: %s\nserver: %s\n"+
		"tls.server_name: %s\ntls.ca_sha256: %s\ntls.system_roots: %t\n"+
		"transport: %s\ntunnel: %s\nfingerprint: %s\nlive_check: %s\n",
		t.SchemaVersion, t.SelectionMode, orNone(t.ConfigSource), printable(t.Kubeconfig),
		printable(t.Context), printable(t.Cluster), printable(t.User), printable(t.Namespace), printable(t.Server),
		orNone(t.TLS.ServerName), orNone(t.TLS.CASHA256), t.TLS.SystemRoots,
		t.Transport, tunnel, t.Fingerprint, t.LiveCheck)
}
