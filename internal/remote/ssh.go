package remote

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SSHClient handles SSH operations to remote hosts
type SSHClient struct {
	Host         string
	User         string
	IdentityFile string
}

// NewSSHClient creates a new SSH client
func NewSSHClient(host, user string) *SSHClient {
	// Pick a public key (prefer ed25519)
	identityFile := ""
	for _, cand := range []string{
		filepath.Join(os.Getenv("HOME"), ".ssh", "id_ed25519"),
		filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa"),
	} {
		if _, err := os.Stat(cand); err == nil {
			identityFile = cand
			break
		}
	}

	return &SSHClient{
		Host:         host,
		User:         user,
		IdentityFile: identityFile,
	}
}

// Execute runs a command on the remote host
func (c *SSHClient) Execute(command string, args []string, forceTTY bool) error {
	return c.ExecuteWithEnv(command, args, nil, forceTTY)
}

// ExecuteWithEnv runs a command with environment variables on the remote host
func (c *SSHClient) ExecuteWithEnv(command string, args []string, env map[string]string, forceTTY bool) error {
	sshArgs := []string{
		"-o", "StrictHostKeyChecking=no",
	}

	if c.IdentityFile != "" {
		sshArgs = append(sshArgs, "-i", c.IdentityFile)
	}

	if forceTTY {
		sshArgs = append(sshArgs, "-tt")
	}

	target := fmt.Sprintf("%s@%s", c.User, c.Host)
	sshArgs = append(sshArgs, target)

	// Build the remote command
	remoteCmd := c.buildRemoteCommand(command, args, env)
	sshArgs = append(sshArgs, remoteCmd)

	cmd := execCommand("ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// ExecuteScript runs a bash script on the remote host via stdin
func (c *SSHClient) ExecuteScript(script string, args []string) error {
	sshArgs := []string{
		"-o", "StrictHostKeyChecking=no",
	}

	if c.IdentityFile != "" {
		sshArgs = append(sshArgs, "-i", c.IdentityFile)
	}

	target := fmt.Sprintf("%s@%s", c.User, c.Host)
	sshArgs = append(sshArgs, target, "bash", "-s", "--")

	sshArgs = append(sshArgs, args...)

	cmd := execCommand("ssh", sshArgs...)
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// Upload copies a local file to the remote host using scp
func (c *SSHClient) Upload(localPath, remotePath string) error {
	scpArgs := []string{
		"-o", "StrictHostKeyChecking=no",
	}

	if c.IdentityFile != "" {
		scpArgs = append(scpArgs, "-i", c.IdentityFile)
	}

	target := fmt.Sprintf("%s@%s:%s", c.User, c.Host, remotePath)
	scpArgs = append(scpArgs, localPath, target)

	cmd := execCommand("scp", scpArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// TestConnection tests if SSH connection works (batch mode)
func (c *SSHClient) TestConnection() error {
	sshArgs := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=no",
	}

	if c.IdentityFile != "" {
		sshArgs = append(sshArgs, "-i", c.IdentityFile)
	}

	target := fmt.Sprintf("%s@%s", c.User, c.Host)
	sshArgs = append(sshArgs, target, "true")

	cmd := execCommand("ssh", sshArgs...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	return cmd.Run()
}

// RunCommandString executes a raw remote shell command string via ssh.
func (c *SSHClient) RunCommandString(cmdString string, forceTTY bool) error {
	sshArgs := []string{
		"-o", "StrictHostKeyChecking=no",
	}

	if c.IdentityFile != "" {
		sshArgs = append(sshArgs, "-i", c.IdentityFile)
	}

	if forceTTY {
		sshArgs = append(sshArgs, "-tt")
	}

	target := fmt.Sprintf("%s@%s", c.User, c.Host)
	sshArgs = append(sshArgs, target, cmdString)

	cmd := execCommand("ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// OutputCommand runs a remote command via ssh and returns stdout.
func (c *SSHClient) OutputCommand(command string, args []string) ([]byte, error) {
	sshArgs := []string{
		"-o", "StrictHostKeyChecking=no",
	}

	if c.IdentityFile != "" {
		sshArgs = append(sshArgs, "-i", c.IdentityFile)
	}

	target := fmt.Sprintf("%s@%s", c.User, c.Host)
	sshArgs = append(sshArgs, target, command)
	sshArgs = append(sshArgs, args...)

	cmd := execCommand("ssh", sshArgs...)
	return cmd.Output()
}

// buildRemoteCommand constructs a properly escaped remote command
func (c *SSHClient) buildRemoteCommand(command string, args []string, env map[string]string) string {
	var parts []string

	// Add environment variables
	if len(env) > 0 {
		for k, v := range env {
			parts = append(parts, fmt.Sprintf("%s=%s", shellEscape(k), shellEscape(v)))
		}
	}

	// Add the command
	parts = append(parts, shellEscape(command))

	// Add arguments
	for _, arg := range args {
		parts = append(parts, shellEscape(arg))
	}

	return strings.Join(parts, " ")
}

// shellEscape escapes a string for safe use in a shell command
func shellEscape(s string) string {
	// Use single quotes for simplicity and safety
	// Replace any single quotes in the string with '\''
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return fmt.Sprintf("'%s'", escaped)
}
