package target

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const preferenceAPIVersion = "kubectl.config.k8s.io/v1beta1"

// interruptSignals are trapped while kubectl runs. kubectl runs in its own process group, so a
// terminal hangup or quit never reaches it directly; the trap kills the group instead.
var interruptSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}

// kubectlEnv returns the environment for core kubectl calls. Kubectl preferences are either off
// or a temporary projection that keeps only the credential-plugin policy of the user's effective
// preference file. cleanup removes the projection.
func (r Resolver) kubectlEnv(ctx context.Context, env map[string]string) (toolEnv []string, cleanup func(), err error) {
	for _, kv := range r.Environ {
		if !strings.HasPrefix(kv, "KUBECONFIG=") && !strings.HasPrefix(kv, "KUBERC=") {
			toolEnv = append(toolEnv, kv)
		}
	}
	noop := func() {}
	pref := env["KUBERC"]
	if pref == "" && env["HOME"] != "" {
		home := filepath.Join(env["HOME"], ".kube", "kuberc")
		// Only a missing file means no preferences; any other stat error could hide a policy.
		switch _, statErr := os.Stat(home); {
		case statErr == nil:
			pref = home
		case !errors.Is(statErr, fs.ErrNotExist):
			return nil, nil, errors.New("$HOME/.kube/kuberc could not be checked; refusing because its credential-plugin policy cannot be preserved")
		}
	}
	if pref == "" || pref == "off" {
		return append(toolEnv, "KUBERC=off"), noop, nil
	}
	if !filepath.IsAbs(pref) {
		pref = filepath.Join(r.Cwd, pref)
	}

	raw, err := r.output(ctx, toolEnv, "kuberc", "view", "--kuberc", pref, "-o", "json")
	if err != nil {
		return nil, nil, fmt.Errorf("kubectl could not read the kuberc preference file %s (%v); refusing because its credential-plugin policy cannot be preserved", pref, err)
	}
	var p struct {
		APIVersion string          `json:"apiVersion"`
		Kind       string          `json:"kind"`
		Policy     json.RawMessage `json:"credentialPluginPolicy,omitempty"`
		Allowlist  json.RawMessage `json:"credentialPluginAllowlist,omitempty"`
	}
	var keys map[string]json.RawMessage
	known := true
	if json.Unmarshal(raw, &keys) == nil {
		for k := range keys {
			switch k {
			case "apiVersion", "kind", "defaults", "aliases", "credentialPluginPolicy", "credentialPluginAllowlist":
			default:
				known = false
			}
		}
	}
	if err := json.Unmarshal(raw, &p); err != nil || !known || p.APIVersion != preferenceAPIVersion || p.Kind != "Preference" {
		return nil, nil, fmt.Errorf("kuberc preference file %s has an unsupported format; refusing", pref)
	}
	projection, err := json.Marshal(p)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.CreateTemp("", "netcup-kube-kuberc-*.json")
	if err != nil {
		return nil, nil, err
	}
	cleanup = func() { _ = os.Remove(f.Name()) }
	if _, err := f.Write(projection); err != nil {
		_ = f.Close()
		cleanup()
		return nil, nil, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return nil, nil, err
	}
	return append(toolEnv, "KUBERC="+f.Name()), cleanup, nil
}

// output runs kubectl and returns stdout. Errors carry only the exit status, never tool output.
// An interrupt kills the process group of the call.
func (r Resolver) output(ctx context.Context, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.Kubectl, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, errors.New("interrupted")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil, errors.New(exitErr.ProcessState.String())
	}
	return out, err
}

// liveCheck reads /version from the selected API server. The kubectl process runs in its own
// process group; the group is killed when the deadline passes or ctx is cancelled by a signal.
func (r Resolver) liveCheck(ctx context.Context, env []string, t *Target) error {
	timeout := r.CheckTimeout
	if timeout == 0 {
		timeout = DefaultCheckTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Pin the endpoint and TLS name to the captured identity instead of re-reading them.
	args := []string{"get", "--raw", "/version", "--request-timeout=10s",
		"--kubeconfig", t.Kubeconfig, "--context", t.Context, "--server", t.Server}
	if t.TLS.ServerName != nil {
		args = append(args, "--tls-server-name", *t.TLS.ServerName)
	}
	cmd := exec.CommandContext(ctx, r.Kubectl, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return fmt.Errorf("live check of context %q was interrupted or did not finish within %s; kubectl was terminated", t.Context, timeout)
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			err = errors.New(exitErr.ProcessState.String())
		}
		return fmt.Errorf("live check of context %q against %s failed (%v)", t.Context, t.Server, err)
	}
	return nil
}
