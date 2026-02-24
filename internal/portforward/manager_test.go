package portforward

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestManager creates a Manager with a temp state dir and injected functions
func newTestManager(t *testing.T, startFn StartFunc, checker ProcessChecker) *Manager {
	t.Helper()
	dir := t.TempDir()
	localPort := freeLocalPort(t)
	opts := []Option{WithStateDir(dir)}
	if startFn != nil {
		opts = append(opts, WithStartFunc(startFn))
	}
	if checker != nil {
		opts = append(opts, WithProcessChecker(checker))
	}
	return New("openclaw", "svc/openclaw", localPort, "18789", opts...)
}

func freeLocalPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate free local port: %v", err)
	}
	defer func() { _ = ln.Close() }()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort error: %v", err)
	}
	return port
}

func TestNew(t *testing.T) {
	m := newTestManager(t, nil, nil)
	if m == nil {
		t.Fatal("New() returned nil")
	}
	if m.Namespace != "openclaw" {
		t.Errorf("Namespace = %q, want %q", m.Namespace, "openclaw")
	}
	if m.LocalPort == "" {
		t.Errorf("LocalPort = %q, want non-empty", m.LocalPort)
	}
}

func TestStatus_Stopped(t *testing.T) {
	m := newTestManager(t, nil, nil)
	st := m.Status()
	if st.State != StateStopped {
		t.Errorf("Status().State = %q, want %q", st.State, StateStopped)
	}
}

func TestStart_Success(t *testing.T) {
	fakePID := 12345
	startFn := func(namespace, target, localPort, remotePort, logFile string) (int, error) {
		return fakePID, nil
	}
	checker := func(pid int) bool { return pid == fakePID }

	m := newTestManager(t, startFn, checker)

	if err := m.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	st := m.Status()
	if st.State != StateRunning {
		t.Errorf("State = %q, want %q", st.State, StateRunning)
	}
	if st.PID != fakePID {
		t.Errorf("PID = %d, want %d", st.PID, fakePID)
	}
}

func TestStart_Idempotent(t *testing.T) {
	fakePID := 12345
	startCount := 0
	startFn := func(namespace, target, localPort, remotePort, logFile string) (int, error) {
		startCount++
		return fakePID, nil
	}
	checker := func(pid int) bool { return pid == fakePID }

	m := newTestManager(t, startFn, checker)

	// Start twice
	if err := m.Start(); err != nil {
		t.Fatalf("First Start() error: %v", err)
	}
	if err := m.Start(); err != nil {
		t.Fatalf("Second Start() error: %v", err)
	}

	// Should only have been called once
	if startCount != 1 {
		t.Errorf("start function called %d times, want 1", startCount)
	}
}

func TestStart_Failure(t *testing.T) {
	startFn := func(namespace, target, localPort, remotePort, logFile string) (int, error) {
		return 0, fmt.Errorf("kubectl not found")
	}

	m := newTestManager(t, startFn, nil)

	err := m.Start()
	if err == nil {
		t.Fatal("Start() expected error, got nil")
	}

	st := m.Status()
	if st.State != StateFailed {
		t.Errorf("State = %q after failure, want %q", st.State, StateFailed)
	}
}

func TestStart_PortAlreadyInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("Cannot bind listener: %v", err)
	}
	defer func() { _ = ln.Close() }()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort error: %v", err)
	}

	startCount := 0
	startFn := func(namespace, target, localPort, remotePort, logFile string) (int, error) {
		startCount++
		return 1234, nil
	}

	m := New("openclaw", "svc/openclaw", port, "18789",
		WithStateDir(t.TempDir()),
		WithStartFunc(startFn),
	)

	err = m.Start()
	if err == nil {
		t.Fatal("Start() expected error when local port is already in use, got nil")
	}
	if !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("Start() error = %v, want message containing 'already in use'", err)
	}
	if startCount != 0 {
		t.Fatalf("start function called %d times, want 0", startCount)
	}
}

func TestStart_ProcessExitsImmediately(t *testing.T) {
	fakePID := 12345
	startFn := func(namespace, target, localPort, remotePort, logFile string) (int, error) {
		if writeErr := os.WriteFile(logFile, []byte("unable to listen on any of the requested ports"), 0600); writeErr != nil {
			t.Fatalf("failed to write synthetic log: %v", writeErr)
		}
		return fakePID, nil
	}
	checker := func(pid int) bool { return false }

	m := newTestManager(t, startFn, checker)
	err := m.Start()
	if err == nil {
		t.Fatal("Start() expected error for immediately exited process, got nil")
	}
	if !strings.Contains(err.Error(), "exited immediately") {
		t.Fatalf("Start() error = %v, want message containing 'exited immediately'", err)
	}

	st := m.Status()
	if st.State != StateFailed {
		t.Errorf("State = %q, want %q", st.State, StateFailed)
	}
	if st.PID != fakePID {
		t.Errorf("PID = %d, want %d", st.PID, fakePID)
	}
	if st.LogFile == "" {
		t.Error("LogFile is empty, want non-empty")
	}
}

func TestStart_WriteStateError(t *testing.T) {
	startFn := func(namespace, target, localPort, remotePort, logFile string) (int, error) {
		return 1234, nil
	}

	// Use a non-existent state directory to force writeState failure
	m := New("openclaw", "svc/openclaw", "18789", "18789",
		WithStateDir("/nonexistent/path/that/cannot/be/created"),
		WithStartFunc(startFn),
	)

	err := m.Start()
	if err == nil {
		t.Fatal("Start() expected error on writeState failure, got nil")
	}
}

func TestStop_NotRunning(t *testing.T) {
	m := newTestManager(t, nil, nil)

	// Stop when not running should be idempotent
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop() on non-running port-forward error: %v", err)
	}
}

func TestStop_Running(t *testing.T) {
	// Use a fake PID that won't be alive (high number)
	fakePID := 999990
	startFn := func(namespace, target, localPort, remotePort, logFile string) (int, error) {
		return fakePID, nil
	}
	checker := func(pid int) bool { return true }

	m := newTestManager(t, startFn, checker)

	if err := m.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Stop should handle "no such process" gracefully and update state
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop() unexpected error: %v", err)
	}

	st := m.Status()
	if st.State != StateStopped {
		t.Errorf("State = %q after Stop(), want %q", st.State, StateStopped)
	}
}

func TestStop_ZeroPID(t *testing.T) {
	// Test Stop when state has PID 0 (skips kill, updates state directly)
	dir := t.TempDir()
	m := New("openclaw", "svc/openclaw", "18789", "18789", WithStateDir(dir))

	// Write a state with state=running but PID=0
	if err := m.writeState(&stateFile{
		State:     StateRunning,
		PID:       0,
		LocalPort: "18789",
	}); err != nil {
		t.Fatalf("writeState error: %v", err)
	}

	if err := m.Stop(); err != nil {
		t.Fatalf("Stop() unexpected error: %v", err)
	}

	st := m.Status()
	if st.State != StateStopped {
		t.Errorf("State = %q after Stop(), want %q", st.State, StateStopped)
	}
}

func TestStatus_StalePID(t *testing.T) {
	fakePID := 12345
	startFn := func(namespace, target, localPort, remotePort, logFile string) (int, error) {
		return fakePID, nil
	}
	alive := true
	checker := func(pid int) bool { return alive }

	m := newTestManager(t, startFn, checker)

	if err := m.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Simulate process death
	alive = false
	st := m.Status()
	if st.State != StateFailed {
		t.Errorf("State = %q after stale PID, want %q", st.State, StateFailed)
	}
}

func TestStateFilePath(t *testing.T) {
	dir := t.TempDir()
	m := New("openclaw", "svc/openclaw", "18789", "18789", WithStateDir(dir))

	path := m.stateFilePath()
	if !filepath.IsAbs(path) {
		t.Errorf("stateFilePath() = %q is not absolute", path)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("stateFilePath() dir = %q, want %q", filepath.Dir(path), dir)
	}
}

func TestLogFilePath(t *testing.T) {
	dir := t.TempDir()
	m := New("openclaw", "svc/openclaw", "18789", "18789", WithStateDir(dir))

	path := m.logFilePath()
	if !filepath.IsAbs(path) {
		t.Errorf("logFilePath() = %q is not absolute", path)
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"openclaw", "openclaw"},
		{"my/namespace", "my_namespace"},
		{"port:18789", "port_18789"},
		{"a b", "a_b"},
	}

	for _, tt := range tests {
		got := sanitize(tt.input)
		if got != tt.want {
			t.Errorf("sanitize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestReadinessCheck_Timeout(t *testing.T) {
	// No server listening on this port; should time out quickly
	err := ReadinessCheck("59876", 200*time.Millisecond)
	if err == nil {
		t.Fatal("ReadinessCheck() expected timeout error, got nil")
	}
}

func TestReadinessCheck_Success(t *testing.T) {
	// Start a real TCP listener to simulate a ready port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("Cannot bind listener: %v", err)
	}
	defer func() { _ = ln.Close() }()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort error: %v", err)
	}

	if err := ReadinessCheck(port, 2*time.Second); err != nil {
		t.Fatalf("ReadinessCheck() unexpected error: %v", err)
	}
}

func TestIsPortListening_InvalidPort(t *testing.T) {
	// Invalid port numbers should return false
	if isPortListening("notaport") {
		t.Error("isPortListening(notaport) = true, want false")
	}
	if isPortListening("0") {
		t.Error("isPortListening(0) = true, want false")
	}
	if isPortListening("99999") {
		t.Error("isPortListening(99999) = true, want false")
	}
}

func TestDefaultStateDir(t *testing.T) {
	dir := defaultStateDir()
	if dir == "" {
		t.Error("defaultStateDir() returned empty string")
	}
}

func TestDefaultStateDir_WithXDG(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	dir := defaultStateDir()
	if dir != tmpDir {
		t.Errorf("defaultStateDir() = %q with XDG_RUNTIME_DIR set, want %q", dir, tmpDir)
	}
}

func TestDefaultStateDir_WithoutXDG(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")

	dir := defaultStateDir()
	if dir != "/tmp" {
		t.Errorf("defaultStateDir() = %q without XDG_RUNTIME_DIR, want /tmp", dir)
	}
}

func TestDefaultStartFunc_BadLogFile(t *testing.T) {
	// Providing an invalid log file path should cause an early return error
	_, err := defaultStartFunc("openclaw", "svc/openclaw", "18789", "18789", "/nonexistent/dir/test.log")
	if err == nil {
		t.Fatal("defaultStartFunc() expected error for invalid log file, got nil")
	}
}

func TestReadState_CorruptFile(t *testing.T) {
	tmpDir := t.TempDir()
	m := New("openclaw", "svc/openclaw", "18789", "18789", WithStateDir(tmpDir))

	// Write invalid JSON
	if err := os.WriteFile(m.stateFilePath(), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := m.readState()
	if err == nil {
		t.Error("readState() expected error for corrupt JSON, got nil")
	}
}

func TestReadState_UnreadableFile(t *testing.T) {
	tmpDir := t.TempDir()
	m := New("openclaw", "svc/openclaw", "18789", "18789", WithStateDir(tmpDir))

	// Create a valid file, then remove read permission
	stateData := []byte(`{"state":"stopped","local_port":"18789"}`)
	if err := os.WriteFile(m.stateFilePath(), stateData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(m.stateFilePath(), 0000); err != nil {
		t.Skip("Cannot chmod state file; skipping")
	}

	_, err := m.readState()
	// On non-root systems this should fail; on root systems it may succeed
	if err != nil {
		t.Logf("readState() correctly returned error for unreadable file: %v", err)
	} else {
		t.Log("readState() succeeded (likely running as root); skipping unreadable test")
	}
}

func TestDefaultProcessChecker(t *testing.T) {
	// Current process should be alive
	if !defaultProcessChecker(os.Getpid()) {
		t.Error("defaultProcessChecker(current PID) = false, want true")
	}

	// PID 0 is not a valid user-space process
	result := defaultProcessChecker(0)
	t.Logf("defaultProcessChecker(0) = %v (system-dependent)", result)
}

func TestTcpProbe_Success(t *testing.T) {
	// Start a real TCP listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("Cannot bind listener: %v", err)
	}
	defer func() { _ = ln.Close() }()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort error: %v", err)
	}

	if !tcpProbe("127.0.0.1", port) {
		t.Errorf("tcpProbe() = false for listening port, want true")
	}
}

func TestTcpProbe_Failure(t *testing.T) {
	// Port that's not listening
	if tcpProbe("127.0.0.1", "59877") {
		t.Error("tcpProbe() = true for non-listening port, want false")
	}
}

func TestReadLogTail_NonExistent(t *testing.T) {
	result := readLogTail("/nonexistent/file.log", 1024)
	if result != "" {
		t.Errorf("readLogTail(nonexistent) = %q, want empty string", result)
	}
}

func TestReadLogTail_ZeroMaxBytes(t *testing.T) {
	result := readLogTail(filepath.Join(t.TempDir(), "anything.log"), 0)
	if result != "" {
		t.Errorf("readLogTail(maxBytes=0) = %q, want empty string", result)
	}
}

func TestReadLogTail_Empty(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "test-*.log")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	result := readLogTail(f.Name(), 1024)
	if result != "" {
		t.Errorf("readLogTail(empty file) = %q, want empty string", result)
	}
}

func TestReadLogTail_ShortFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	content := "hello world"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	result := readLogTail(path, 1024)
	if result != content {
		t.Errorf("readLogTail(short) = %q, want %q", result, content)
	}
}

func TestReadLogTail_Truncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	content := "line1\nline2\nline3\nline4"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	// Request only last 5 bytes
	result := readLogTail(path, 5)
	if len(result) > 5 {
		t.Errorf("readLogTail(maxBytes=5) returned %d bytes, want <= 5", len(result))
	}
}
