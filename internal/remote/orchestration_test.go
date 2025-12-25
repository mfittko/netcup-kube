package remote

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fakeClient struct {
	testConnErr error

	execCalls   []execCall
	scriptCalls []scriptCall
	uploads     []uploadCall
	runCalls    []runCall

	output map[string][]byte

	execErrByKey map[string]error
	uploadErr    error
	runErr       error
}

type execCall struct {
	command  string
	args     []string
	forceTTY bool
}
type scriptCall struct {
	script string
	args   []string
}
type uploadCall struct {
	local  string
	remote string
}
type runCall struct {
	cmdString string
	forceTTY  bool
}

func (f *fakeClient) TestConnection() error { return f.testConnErr }
func (f *fakeClient) Execute(command string, args []string, forceTTY bool) error {
	f.execCalls = append(f.execCalls, execCall{command: command, args: append([]string{}, args...), forceTTY: forceTTY})
	if f.execErrByKey != nil {
		key := command + " " + strings.Join(args, " ")
		if err, ok := f.execErrByKey[key]; ok {
			return err
		}
	}
	return nil
}
func (f *fakeClient) ExecuteScript(script string, args []string) error {
	f.scriptCalls = append(f.scriptCalls, scriptCall{script: script, args: append([]string{}, args...)})
	return nil
}
func (f *fakeClient) Upload(localPath, remotePath string) error {
	f.uploads = append(f.uploads, uploadCall{local: localPath, remote: remotePath})
	return f.uploadErr
}
func (f *fakeClient) RunCommandString(cmdString string, forceTTY bool) error {
	f.runCalls = append(f.runCalls, runCall{cmdString: cmdString, forceTTY: forceTTY})
	return f.runErr
}
func (f *fakeClient) OutputCommand(command string, args []string) ([]byte, error) {
	key := command + " " + strings.Join(args, " ")
	if f.output != nil {
		if b, ok := f.output[key]; ok {
			return b, nil
		}
	}
	return nil, errors.New("no output configured")
}

func repoTmp(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestRemoteGitSync_Placeholders(t *testing.T) {
	fc := &fakeClient{}
	err := RemoteGitSync(fc, "/home/u/netcup-kube", GitOptions{Branch: "", Ref: "", Pull: true})
	if err != nil {
		t.Fatalf("RemoteGitSync error: %v", err)
	}
	if len(fc.scriptCalls) != 1 {
		t.Fatalf("expected 1 script call, got %d", len(fc.scriptCalls))
	}
	args := fc.scriptCalls[0].args
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %#v", len(args), args)
	}
	if args[1] != "__NONE__" || args[2] != "__NONE__" || args[3] != "true" {
		t.Fatalf("unexpected placeholders: %#v", args)
	}
}

func TestRemoteDetectGoarch(t *testing.T) {
	fc := &fakeClient{output: map[string][]byte{
		"uname -m": []byte("x86_64\n"),
	}}
	arch, err := remoteDetectGoarch(fc)
	if err != nil {
		t.Fatalf("remoteDetectGoarch error: %v", err)
	}
	if arch != "amd64" {
		t.Fatalf("want amd64, got %s", arch)
	}

	fc.output["uname -m"] = []byte("aarch64\n")
	arch, err = remoteDetectGoarch(fc)
	if err != nil {
		t.Fatalf("remoteDetectGoarch error: %v", err)
	}
	if arch != "arm64" {
		t.Fatalf("want arm64, got %s", arch)
	}

	fc.output["uname -m"] = []byte("mips\n")
	_, err = remoteDetectGoarch(fc)
	if err == nil {
		t.Fatalf("expected error for unsupported arch")
	}
}

func TestRemoteBuildAndUpload_Success(t *testing.T) {
	tmp := repoTmp(t)
	// stub local toolchain + temp dir + build
	oldLook := lookPath
	oldMk := mkdirTemp
	oldRm := removeAll
	oldBuild := localGoBuild
	t.Cleanup(func() {
		lookPath = oldLook
		mkdirTemp = oldMk
		removeAll = oldRm
		localGoBuild = oldBuild
	})

	lookPath = func(_ string) (string, error) { return "/usr/bin/go", nil }
	mkdirTemp = func(_ string, _ string) (string, error) {
		return os.MkdirTemp(tmp, "build-*")
	}
	removeAll = func(path string) error { return os.RemoveAll(path) }
	localGoBuild = func(_ string, out string, _ string) error {
		return os.WriteFile(out, []byte("bin"), 0755)
	}

	fc := &fakeClient{output: map[string][]byte{"uname -m": []byte("x86_64\n")}}
	cfg := NewConfig()
	cfg.Host = "example.com"
	cfg.User = "ops"

	if err := RemoteBuildAndUpload(fc, cfg, tmp, GitOptions{}); err != nil {
		t.Fatalf("RemoteBuildAndUpload error: %v", err)
	}
	if len(fc.uploads) != 1 {
		t.Fatalf("expected 1 upload, got %d", len(fc.uploads))
	}
	if len(fc.execCalls) == 0 {
		t.Fatalf("expected exec calls for install/chmod")
	}
}

func TestRemoteBuildAndUpload_WithGitSync(t *testing.T) {
	tmp := t.TempDir()

	oldLook := lookPath
	oldMk := mkdirTemp
	oldRm := removeAll
	oldBuild := localGoBuild
	t.Cleanup(func() {
		lookPath = oldLook
		mkdirTemp = oldMk
		removeAll = oldRm
		localGoBuild = oldBuild
	})

	lookPath = func(_ string) (string, error) { return "/usr/bin/go", nil }
	mkdirTemp = func(_ string, _ string) (string, error) { return os.MkdirTemp(tmp, "build-*") }
	removeAll = func(path string) error { return os.RemoveAll(path) }
	localGoBuild = func(_ string, out string, _ string) error { return os.WriteFile(out, []byte("bin"), 0755) }

	fc := &fakeClient{output: map[string][]byte{"uname -m": []byte("x86_64\n")}}
	cfg := NewConfig()
	cfg.Host = "example.com"
	cfg.User = "ops"

	if err := RemoteBuildAndUpload(fc, cfg, tmp, GitOptions{Branch: "main", Pull: true}); err != nil {
		t.Fatalf("RemoteBuildAndUpload error: %v", err)
	}
	if len(fc.scriptCalls) == 0 {
		t.Fatalf("expected git sync script call when Branch/Pull set")
	}
}

func TestRemoteBuildAndUpload_NoGoToolchain(t *testing.T) {
	oldLook := lookPath
	t.Cleanup(func() { lookPath = oldLook })
	lookPath = func(_ string) (string, error) { return "", exec.ErrNotFound }

	fc := &fakeClient{output: map[string][]byte{"uname -m": []byte("x86_64\n")}}
	cfg := NewConfig()
	cfg.Host = "example.com"
	cfg.User = "ops"
	if err := RemoteBuildAndUpload(fc, cfg, t.TempDir(), GitOptions{}); err == nil {
		t.Fatalf("expected error when go toolchain missing")
	}
}

func TestRun_WrapperExecutes(t *testing.T) {
	oldExec := execCommand
	t.Cleanup(func() { execCommand = oldExec })
	execCommand = func(name string, args ...string) *exec.Cmd {
		// Provide uname -m output when OutputCommand probes arch.
		for _, a := range args {
			if a == "uname" {
				return exec.Command("sh", "-c", "echo x86_64")
			}
		}
		return exec.Command("true")
	}

	cfg := NewConfig()
	cfg.Host = "example.com"
	cfg.User = "ops"

	if err := Run(cfg, RunOptions{ForceTTY: false, Args: []string{"dns", "--help"}}); err != nil {
		t.Fatalf("Run error: %v", err)
	}
}

func TestProvision_WrapperExecutes(t *testing.T) {
	oldExec := execCommand
	t.Cleanup(func() { execCommand = oldExec })
	execCommand = func(_ string, _ ...string) *exec.Cmd { return exec.Command("true") }

	tmp := t.TempDir()
	pub := filepath.Join(tmp, "id.pub")
	if err := os.WriteFile(pub, []byte("ssh-ed25519 AAAA test@localhost"), 0644); err != nil {
		t.Fatalf("write pubkey: %v", err)
	}

	cfg := NewConfig()
	cfg.Host = "example.com"
	cfg.User = "ops"
	cfg.PubKeyPath = pub

	if err := Provision(cfg); err != nil {
		t.Fatalf("Provision error: %v", err)
	}
}

func TestGetHostname_ReturnsNonEmpty(t *testing.T) {
	if got := getHostname(); got == "" {
		t.Fatalf("expected non-empty hostname")
	}
}

func TestEnsureUserAccess_Error(t *testing.T) {
	fc := &fakeClient{testConnErr: errors.New("nope")}
	cfg := NewConfig()
	cfg.Host = "example.com"
	cfg.User = "ops"
	if err := ensureUserAccess(fc, cfg); err == nil {
		t.Fatalf("expected error")
	}
}

func TestEnsureRemoteRepo_Error(t *testing.T) {
	cfg := NewConfig()
	cfg.Host = "example.com"
	cfg.User = "ops"
	fc := &fakeClient{
		testConnErr: nil,
		execErrByKey: map[string]error{
			"test -d " + cfg.GetRemoteRepoDir(): errors.New("missing"),
		},
	}
	if err := ensureRemoteRepo(fc, cfg); err == nil {
		t.Fatalf("expected error")
	}
}

func TestCleanupRemoteEnv_NoOp(t *testing.T) {
	fc := &fakeClient{}
	cleanupRemoteEnv(fc, "__NONE__", false)
	if len(fc.execCalls) != 0 {
		t.Fatalf("expected no exec calls")
	}
}

func TestRunWithClient_UploadsEnvAndCleansUp(t *testing.T) {
	tmp := repoTmp(t)
	envFile := filepath.Join(tmp, "env.test")
	if err := os.WriteFile(envFile, []byte("CONFIRM=true\n"), 0644); err != nil {
		t.Fatalf("write env: %v", err)
	}

	cfg := NewConfig()
	cfg.Host = "example.com"
	cfg.User = "ops"

	fc := &fakeClient{
		execErrByKey: map[string]error{},
	}
	// satisfy repo + bin checks
	repoDir := cfg.GetRemoteRepoDir()
	binPath := cfg.GetRemoteBinPath()
	fc.execErrByKey["test -d "+repoDir] = nil
	fc.execErrByKey["test -x "+binPath] = nil

	opts := RunOptions{
		ForceTTY: true,
		EnvFile:  envFile,
		Args:     []string{"dns", "--type", "edge-http", "--domains", "kube.example.com"},
	}
	if err := runWithClient(fc, cfg, opts); err != nil {
		t.Fatalf("runWithClient error: %v", err)
	}
	if len(fc.uploads) != 1 {
		t.Fatalf("expected 1 upload, got %d", len(fc.uploads))
	}
	if len(fc.runCalls) != 1 {
		t.Fatalf("expected 1 run call, got %d", len(fc.runCalls))
	}
	// cleanup should attempt sudo rm -f <remoteEnv>
	foundCleanup := false
	for _, c := range fc.execCalls {
		if c.command == "sudo" && len(c.args) >= 3 && c.args[0] == "rm" && c.args[1] == "-f" {
			foundCleanup = true
		}
	}
	if !foundCleanup {
		t.Fatalf("expected cleanup sudo rm -f call, got exec calls: %#v", fc.execCalls)
	}
}

func TestSmoke_WrapperExecutes(t *testing.T) {
	tmp := t.TempDir()

	// stub local toolchain + temp dir + build (avoid real go build)
	oldLook := lookPath
	oldMk := mkdirTemp
	oldRm := removeAll
	oldBuild := localGoBuild
	oldExec := execCommand
	t.Cleanup(func() {
		lookPath = oldLook
		mkdirTemp = oldMk
		removeAll = oldRm
		localGoBuild = oldBuild
		execCommand = oldExec
	})

	lookPath = func(_ string) (string, error) { return "/usr/bin/go", nil }
	mkdirTemp = func(_ string, _ string) (string, error) { return os.MkdirTemp(tmp, "build-*") }
	removeAll = func(path string) error { return os.RemoveAll(path) }
	localGoBuild = func(_ string, out string, _ string) error { return os.WriteFile(out, []byte("bin"), 0755) }

	// Make SSHClient.TestConnection succeed and OutputCommand(uname -m) return x86_64.
	execCommand = func(name string, args ...string) *exec.Cmd {
		// for OutputCommand("uname","-m") we return stdout with arch
		for _, a := range args {
			if a == "uname" {
				return exec.Command("sh", "-c", "echo x86_64")
			}
		}
		return exec.Command("true")
	}

	cfg := NewConfig()
	cfg.Host = "example.com"
	cfg.User = "ops"
	if err := Smoke(cfg, GitOptions{}, tmp); err != nil {
		t.Fatalf("Smoke error: %v", err)
	}
}

func TestRunWithClient_Errors(t *testing.T) {
	cfg := NewConfig()
	cfg.Host = "example.com"
	cfg.User = "ops"

	// missing args
	if err := runWithClient(&fakeClient{}, cfg, RunOptions{Args: []string{}}); err == nil {
		t.Fatalf("expected error for missing args")
	}
	// unsupported command
	if err := runWithClient(&fakeClient{}, cfg, RunOptions{Args: []string{"rm"}}); err == nil {
		t.Fatalf("expected error for unsupported cmd")
	}

	// env file missing
	fc := &fakeClient{}
	if err := runWithClient(fc, cfg, RunOptions{Args: []string{"dns"}, EnvFile: "/does-not-exist"}); err == nil {
		t.Fatalf("expected error for missing env file")
	}
}

func TestRemoteBuildAndUpload_Errors(t *testing.T) {
	tmp := t.TempDir()
	cfg := NewConfig()
	cfg.Host = "example.com"
	cfg.User = "ops"

	fc := &fakeClient{output: map[string][]byte{"uname -m": []byte("x86_64\n")}}

	// go toolchain missing
	oldLook := lookPath
	lookPath = func(_ string) (string, error) { return "", exec.ErrNotFound }
	if err := RemoteBuildAndUpload(fc, cfg, tmp, GitOptions{}); err == nil {
		t.Fatalf("expected error when go missing")
	}
	lookPath = oldLook

	// local build fails
	oldBuild := localGoBuild
	localGoBuild = func(_ string, _ string, _ string) error { return errors.New("buildfail") }
	oldMk := mkdirTemp
	mkdirTemp = func(_ string, _ string) (string, error) { return os.MkdirTemp(tmp, "build-*") }
	t.Cleanup(func() { localGoBuild = oldBuild; mkdirTemp = oldMk })

	if err := RemoteBuildAndUpload(fc, cfg, tmp, GitOptions{}); err == nil {
		t.Fatalf("expected error when build fails")
	}
}

func TestRemoteDetectGoarch_Error(t *testing.T) {
	fc := &fakeClient{}
	if _, err := remoteDetectGoarch(fc); err == nil {
		t.Fatalf("expected error when OutputCommand not configured")
	}
}

func TestSmokeWithClient_Errors(t *testing.T) {
	cfg := NewConfig()
	cfg.Host = "example.com"
	cfg.User = "ops"

	// 1) SSH connection failure
	fc := &fakeClient{testConnErr: errors.New("no ssh")}
	if err := smokeWithClient(fc, cfg, GitOptions{}, t.TempDir()); err == nil {
		t.Fatalf("expected error on SSH connection failure")
	}

	// 2) Build/upload failure (missing local go)
	oldLook := lookPath
	t.Cleanup(func() { lookPath = oldLook })
	lookPath = func(_ string) (string, error) { return "", exec.ErrNotFound }
	fc2 := &fakeClient{
		testConnErr: nil,
		output:      map[string][]byte{"uname -m": []byte("x86_64\n")},
	}
	if err := smokeWithClient(fc2, cfg, GitOptions{}, t.TempDir()); err == nil {
		t.Fatalf("expected error when build/upload fails")
	}

	// 3) Run failure mid-smoke (missing remote binary)
	lookPath = func(_ string) (string, error) { return "/usr/bin/go", nil }
	oldBuild := localGoBuild
	oldMk := mkdirTemp
	oldRm := removeAll
	t.Cleanup(func() { localGoBuild = oldBuild; mkdirTemp = oldMk; removeAll = oldRm })
	mkdirTemp = func(_ string, _ string) (string, error) { return os.MkdirTemp(t.TempDir(), "build-*") }
	removeAll = func(path string) error { return os.RemoveAll(path) }
	localGoBuild = func(_ string, out string, _ string) error { return os.WriteFile(out, []byte("bin"), 0755) }

	fc3 := &fakeClient{
		testConnErr: nil,
		output:      map[string][]byte{"uname -m": []byte("x86_64\n")},
		execErrByKey: map[string]error{
			"test -d " + cfg.GetRemoteRepoDir(): nil,
			// fail the binary check in runWithClient
			"test -x " + cfg.GetRemoteBinPath(): errors.New("no bin"),
		},
	}
	if err := smokeWithClient(fc3, cfg, GitOptions{}, t.TempDir()); err == nil {
		t.Fatalf("expected error when runWithClient fails")
	}
}

func TestSmokeWithClient_CoversSmokePaths(t *testing.T) {
	tmp := t.TempDir()

	// stub local toolchain + temp dir + build (avoid real go build)
	oldLook := lookPath
	oldMk := mkdirTemp
	oldRm := removeAll
	oldBuild := localGoBuild
	t.Cleanup(func() {
		lookPath = oldLook
		mkdirTemp = oldMk
		removeAll = oldRm
		localGoBuild = oldBuild
	})

	lookPath = func(_ string) (string, error) { return "/usr/bin/go", nil }
	mkdirTemp = func(_ string, _ string) (string, error) {
		return os.MkdirTemp(tmp, "build-*")
	}
	removeAll = func(path string) error { return os.RemoveAll(path) }
	localGoBuild = func(_ string, out string, _ string) error {
		return os.WriteFile(out, []byte("bin"), 0755)
	}

	cfg := NewConfig()
	cfg.Host = "example.com"
	cfg.User = "ops"

	fc := &fakeClient{
		output: map[string][]byte{"uname -m": []byte("x86_64\n")},
	}

	// satisfy runWithClient repo/bin checks for all smoke sub-tests
	repoDir := cfg.GetRemoteRepoDir()
	binPath := cfg.GetRemoteBinPath()
	fc.execErrByKey = map[string]error{
		"test -d " + repoDir: nil,
		"test -x " + binPath: nil,
	}

	if err := smokeWithClient(fc, cfg, GitOptions{}, tmp); err != nil {
		t.Fatalf("smokeWithClient error: %v", err)
	}
	// smoke should have executed multiple remote runs
	if len(fc.runCalls) == 0 {
		t.Fatalf("expected remote run calls")
	}
}


