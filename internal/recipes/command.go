package recipes

import (
	"bytes"
	"context"
	"os/exec"
)

// Command keeps sensitive stdin separate from argv and captures both output streams.
type Command func(context.Context, string, []string, []byte) ([]byte, []byte, error)

func Exec(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
