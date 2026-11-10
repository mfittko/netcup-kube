package recipes

import (
	"context"
	"testing"
)

func TestExecStreams(t *testing.T) {
	stdout, stderr, err := Exec(context.Background(), "sh", []string{"-c", "cat; printf diagnostic >&2; exit 7"}, []byte("private stdin"))
	if string(stdout) != "private stdin" || string(stderr) != "diagnostic" || err == nil {
		t.Fatalf("command streams or status lost: %v", err)
	}
}
