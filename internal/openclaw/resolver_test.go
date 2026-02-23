package openclaw

import (
	"fmt"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Namespace != DefaultNamespace {
		t.Errorf("Namespace = %q, want %q", cfg.Namespace, DefaultNamespace)
	}
	if cfg.LabelSelector != DefaultLabelSelector {
		t.Errorf("LabelSelector = %q, want %q", cfg.LabelSelector, DefaultLabelSelector)
	}
	if cfg.FallbackSvc != DefaultFallbackService {
		t.Errorf("FallbackSvc = %q, want %q", cfg.FallbackSvc, DefaultFallbackService)
	}
	if cfg.LocalPort != DefaultLocalPort {
		t.Errorf("LocalPort = %q, want %q", cfg.LocalPort, DefaultLocalPort)
	}
	if cfg.RemotePort != DefaultRemotePort {
		t.Errorf("RemotePort = %q, want %q", cfg.RemotePort, DefaultRemotePort)
	}
}

func TestNew(t *testing.T) {
	cfg := DefaultConfig()
	called := false
	execFn := func(name string, args ...string) ([]byte, error) {
		called = true
		return nil, nil
	}

	r := New(cfg, execFn)

	if r == nil {
		t.Fatal("New() returned nil")
	}

	// Verify the config is stored
	got := r.Config()
	if got.Namespace != cfg.Namespace {
		t.Errorf("Config().Namespace = %q, want %q", got.Namespace, cfg.Namespace)
	}

	_ = called
}

func TestNew_NilExecFunc(t *testing.T) {
	cfg := DefaultConfig()
	r := New(cfg, nil)
	if r == nil {
		t.Fatal("New() with nil execFunc returned nil")
	}
}

func TestResolveService_LabelFound(t *testing.T) {
	cfg := DefaultConfig()
	execFn := func(name string, args ...string) ([]byte, error) {
		return []byte("openclaw-svc"), nil
	}

	r := New(cfg, execFn)
	svc, err := r.ResolveService()

	if err != nil {
		t.Fatalf("ResolveService() unexpected error: %v", err)
	}
	if svc != "svc/openclaw-svc" {
		t.Errorf("ResolveService() = %q, want %q", svc, "svc/openclaw-svc")
	}
}

func TestResolveService_LabelEmpty(t *testing.T) {
	cfg := DefaultConfig()
	execFn := func(name string, args ...string) ([]byte, error) {
		return []byte("  "), nil
	}

	r := New(cfg, execFn)
	svc, err := r.ResolveService()

	if err != nil {
		t.Fatalf("ResolveService() unexpected error: %v", err)
	}
	if svc != DefaultFallbackService {
		t.Errorf("ResolveService() = %q, want %q", svc, DefaultFallbackService)
	}
}

func TestResolveService_ExecError_Fallback(t *testing.T) {
	cfg := DefaultConfig()
	execFn := func(name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("kubectl not found")
	}

	r := New(cfg, execFn)
	svc, err := r.ResolveService()

	if err != nil {
		t.Fatalf("ResolveService() should not error on exec failure, got: %v", err)
	}
	if svc != DefaultFallbackService {
		t.Errorf("ResolveService() = %q, want %q", svc, DefaultFallbackService)
	}
}

func TestResolvePod_Found(t *testing.T) {
	cfg := DefaultConfig()
	execFn := func(name string, args ...string) ([]byte, error) {
		return []byte("openclaw-pod-xyz"), nil
	}

	r := New(cfg, execFn)
	pod, err := r.ResolvePod()

	if err != nil {
		t.Fatalf("ResolvePod() unexpected error: %v", err)
	}
	if pod != "openclaw-pod-xyz" {
		t.Errorf("ResolvePod() = %q, want %q", pod, "openclaw-pod-xyz")
	}
}

func TestResolvePod_ExecError(t *testing.T) {
	cfg := DefaultConfig()
	execFn := func(name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("kubectl error")
	}

	r := New(cfg, execFn)
	_, err := r.ResolvePod()

	if err == nil {
		t.Fatal("ResolvePod() expected error on exec failure, got nil")
	}
}

func TestResolvePod_Empty(t *testing.T) {
	cfg := DefaultConfig()
	execFn := func(name string, args ...string) ([]byte, error) {
		return []byte(""), nil
	}

	r := New(cfg, execFn)
	_, err := r.ResolvePod()

	if err == nil {
		t.Fatal("ResolvePod() expected error for empty pod name, got nil")
	}
}

func TestPortForwardTarget(t *testing.T) {
	cfg := DefaultConfig()
	r := New(cfg, nil)

	target := r.PortForwardTarget("svc/openclaw")
	expected := "svc/openclaw:18789"
	if target != expected {
		t.Errorf("PortForwardTarget() = %q, want %q", target, expected)
	}
}

func TestPortForwardTarget_CustomPort(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RemotePort = "9090"
	r := New(cfg, nil)

	target := r.PortForwardTarget("svc/my-svc")
	expected := "svc/my-svc:9090"
	if target != expected {
		t.Errorf("PortForwardTarget() = %q, want %q", target, expected)
	}
}

func TestDefaultExec_Success(t *testing.T) {
	// Use a built-in command to test the default exec function
	out, err := defaultExec("echo", "hello")
	if err != nil {
		t.Fatalf("defaultExec(echo, hello) error: %v", err)
	}
	if len(out) == 0 {
		t.Error("defaultExec(echo, hello) returned empty output")
	}
}

func TestDefaultExec_Error(t *testing.T) {
	// Use a command that's guaranteed to fail
	_, err := defaultExec("false")
	if err == nil {
		t.Fatal("defaultExec(false) expected error, got nil")
	}
}
