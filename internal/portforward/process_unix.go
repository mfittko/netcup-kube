package portforward

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const dialTimeout = 500 * time.Millisecond

// defaultStartFunc starts kubectl port-forward as a detached background process
// and returns its PID.
func defaultStartFunc(namespace, target, localPort, remotePort, logFile string) (int, error) {
	// Open (or create) the log file for stdout/stderr of the child process
	lf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return 0, fmt.Errorf("failed to open log file: %w", err)
	}
	defer func() { _ = lf.Close() }()

	portMapping := fmt.Sprintf("%s:%s", localPort, remotePort)
	cmd := exec.Command("kubectl",
		"-n", namespace,
		"port-forward",
		target,
		portMapping,
	)
	cmd.Stdout = lf
	cmd.Stderr = lf

	// Detach the process from the parent so it survives when the parent exits
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to launch kubectl port-forward: %w", err)
	}

	return cmd.Process.Pid, nil
}

// defaultProcessChecker checks if a process with the given PID is alive
func defaultProcessChecker(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; use Signal(0) to check liveness
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// killProcess sends SIGTERM to the process with the given PID
func killProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}

// tcpProbe checks if a TCP port is accepting connections on localhost
func tcpProbe(host, port string) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), dialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
