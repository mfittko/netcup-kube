package target

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const preferenceAPIVersion = "kubectl.config.k8s.io/v1beta1"

// kubectlEnv returns the environment for core kubectl calls. Kubectl preferences are either off
// or a temporary projection that keeps only the credential-plugin policy of the user's effective
// preference file. cleanup removes the projection.
func (r Resolver) kubectlEnv(env map[string]string) (toolEnv []string, cleanup func(), err error) {
	for _, kv := range r.Environ {
		if !strings.HasPrefix(kv, "KUBECONFIG=") && !strings.HasPrefix(kv, "KUBERC=") {
			toolEnv = append(toolEnv, kv)
		}
	}
	noop := func() {}
	pref := env["KUBERC"]
	if pref == "" && env["HOME"] != "" {
		home := filepath.Join(env["HOME"], ".kube", "kuberc")
		if _, statErr := os.Stat(home); statErr == nil {
			pref = home
		}
	}
	if pref == "" || pref == "off" {
		return append(toolEnv, "KUBERC=off"), noop, nil
	}
	if !filepath.IsAbs(pref) {
		pref = filepath.Join(r.Cwd, pref)
	}

	raw, err := r.output(toolEnv, "kuberc", "view", "--kuberc", pref, "-o", "json")
	if err != nil {
		return nil, nil, fmt.Errorf("kubectl could not read the kuberc preference file %s (%v); refusing because its credential-plugin policy cannot be preserved", pref, err)
	}
	var p struct {
		APIVersion string          `json:"apiVersion"`
		Kind       string          `json:"kind"`
		Policy     json.RawMessage `json:"credentialPluginPolicy,omitempty"`
		Allowlist  json.RawMessage `json:"credentialPluginAllowlist,omitempty"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.APIVersion != preferenceAPIVersion || p.Kind != "Preference" {
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
func (r Resolver) output(env []string, args ...string) ([]byte, error) {
	cmd := exec.Command(r.Kubectl, args...)
	cmd.Env = env
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil, errors.New(exitErr.ProcessState.String())
	}
	return out, err
}

// liveCheck reads /version from the selected API server. The kubectl process runs in its own
// process group; the group is killed when the deadline passes or the command is interrupted.
func (r Resolver) liveCheck(env []string, t *Target) error {
	timeout := r.CheckTimeout
	if timeout == 0 {
		timeout = DefaultCheckTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := exec.CommandContext(ctx, r.Kubectl, "get", "--raw", "/version", "--request-timeout=10s",
		"--kubeconfig", t.Kubeconfig, "--context", t.Context)
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
