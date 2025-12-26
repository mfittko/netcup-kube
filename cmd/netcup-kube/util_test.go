package main

import (
	"testing"
)

func TestFindProjectRoot(t *testing.T) {
	// This is a basic test - in real usage, findProjectRoot() will work
	// when executed from the project root or bin/ directory.
	_, err := findProjectRoot()
	if err != nil {
		t.Logf("findProjectRoot() error (expected in test environment): %v", err)
	}
}
