package remote

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSSHClient_Execute_BuildsSSHArgs(t *testing.T) {
	old := execCommand
	defer func() { execCommand = old }()

	var gotName string
	var gotArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string{}, args...)
		return exec.Command("true")
	}

	c := &SSHClient{Host: "example.com", User: "ops", IdentityFile: "/id_ed25519"}
	if err := c.Execute("echo", []string{"hi"}, false); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gotName != "ssh" {
		t.Fatalf("expected ssh command, got %q", gotName)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "StrictHostKeyChecking=no") {
		t.Fatalf("expected StrictHostKeyChecking=no in args: %v", gotArgs)
	}
	if !strings.Contains(joined, "-i /id_ed25519") {
		t.Fatalf("expected identity file in args: %v", gotArgs)
	}
	if !strings.Contains(joined, "ops@example.com") {
		t.Fatalf("expected target in args: %v", gotArgs)
	}
	// The last arg is the escaped remote command string.
	if len(gotArgs) == 0 || !strings.Contains(gotArgs[len(gotArgs)-1], "'echo'") {
		t.Fatalf("expected escaped command in last arg, got: %v", gotArgs)
	}
}

func TestSSHClient_RunCommandString_ForceTTY(t *testing.T) {
	old := execCommand
	defer func() { execCommand = old }()

	var gotArgs []string
	execCommand = func(_ string, args ...string) *exec.Cmd {
		gotArgs = append([]string{}, args...)
		return exec.Command("true")
	}

	c := &SSHClient{Host: "example.com", User: "ops", IdentityFile: ""}
	if err := c.RunCommandString("echo hi", true); err != nil {
		t.Fatalf("RunCommandString error: %v", err)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "-tt") {
		t.Fatalf("expected -tt in args: %v", gotArgs)
	}
	if !strings.Contains(joined, "ops@example.com") {
		t.Fatalf("expected target in args: %v", gotArgs)
	}
	if !strings.Contains(joined, "echo hi") {
		t.Fatalf("expected cmd string in args: %v", gotArgs)
	}
}

func TestSSHClient_OutputCommand(t *testing.T) {
	old := execCommand
	defer func() { execCommand = old }()

	execCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "echo x86_64")
	}

	c := &SSHClient{Host: "example.com", User: "ops"}
	out, err := c.OutputCommand("uname", []string{"-m"})
	if err != nil {
		t.Fatalf("OutputCommand error: %v", err)
	}
	if strings.TrimSpace(string(out)) != "x86_64" {
		t.Fatalf("unexpected output: %q", string(out))
	}
}

func TestSSHClient_ExecuteScript_Upload_TestConnection(t *testing.T) {
	old := execCommand
	defer func() { execCommand = old }()

	var gotName string
	var gotArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string{}, args...)
		return exec.Command("true")
	}

	c := &SSHClient{Host: "example.com", User: "ops", IdentityFile: "/id"}

	_ = c.ExecuteScript("echo hi", []string{"a", "b"})
	if gotName != "ssh" {
		t.Fatalf("expected ssh for ExecuteScript, got %q", gotName)
	}

	_ = c.Upload("/local", "/remote")
	if gotName != "scp" {
		t.Fatalf("expected scp for Upload, got %q", gotName)
	}

	_ = c.TestConnection()
	if gotName != "ssh" {
		t.Fatalf("expected ssh for TestConnection, got %q", gotName)
	}
	if len(gotArgs) == 0 {
		t.Fatalf("expected args captured")
	}
}
