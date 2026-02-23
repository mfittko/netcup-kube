package portforward

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultStartFunc_OpenLogFileError(t *testing.T) {
	logPath := t.TempDir()
	_, err := defaultStartFunc("openclaw", "svc/openclaw", "18789", "18789", logPath)
	if err == nil {
		t.Fatal("expected error when log path is a directory")
	}
}

func TestDefaultStartFunc_CommandStartError(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no kubectl in PATH
	logPath := filepath.Join(t.TempDir(), "pf.log")

	_, err := defaultStartFunc("openclaw", "svc/openclaw", "18789", "18789", logPath)
	if err == nil {
		t.Fatal("expected error when kubectl is not available")
	}
}

func TestDefaultStartFunc_SuccessWithFakeKubectl(t *testing.T) {
	binDir := t.TempDir()
	kubectlPath := filepath.Join(binDir, "kubectl")
	script := "#!/bin/sh\nsleep 5\n"
	if err := os.WriteFile(kubectlPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed writing fake kubectl: %v", err)
	}
	t.Setenv("PATH", binDir)

	logPath := filepath.Join(t.TempDir(), "pf.log")
	pid, err := defaultStartFunc("openclaw", "svc/openclaw", "18789", "18789", logPath)
	if err != nil {
		t.Fatalf("defaultStartFunc error: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d, want > 0", pid)
	}

	if !defaultProcessChecker(pid) {
		t.Fatalf("expected process %d to be alive", pid)
	}

	if err := killProcess(pid); err != nil {
		t.Fatalf("killProcess(%d) error: %v", pid, err)
	}
}

func TestDefaultProcessChecker_InvalidPID(t *testing.T) {
	if defaultProcessChecker(-1) {
		t.Fatal("expected false for invalid pid")
	}
}

func TestKillProcess_InvalidPID(t *testing.T) {
	if err := killProcess(-1); err == nil {
		t.Fatal("expected error for invalid pid")
	}
}

func TestTCPProbe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	defer func() { _ = ln.Close() }()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host/port error: %v", err)
	}

	if !tcpProbe("127.0.0.1", port) {
		t.Fatalf("tcpProbe expected true for listening port %s", port)
	}

	if err := ln.Close(); err != nil {
		t.Fatalf("close listener error: %v", err)
	}
	time.Sleep(25 * time.Millisecond)
	if tcpProbe("127.0.0.1", port) {
		t.Fatalf("tcpProbe expected false for closed port %s", port)
	}
}

func TestDefaultProcessChecker_LiveProcess(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start sleep: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	if !defaultProcessChecker(cmd.Process.Pid) {
		t.Fatalf("expected process %d to be alive", cmd.Process.Pid)
	}
}
