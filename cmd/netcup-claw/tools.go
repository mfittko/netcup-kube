package main

import "github.com/spf13/cobra"

// toolCmd is the parent command for backend-agnostic data tools.
// All tool subcommands are registered under this group.
var toolCmd = &cobra.Command{
	Use:   "tool",
	Short: "Backend-agnostic data tools for skills",
	Long: `Backend-agnostic data tools for OpenClaw skills.

Sub-commands:
  fxempire-rates  - Fetch and format FXEmpire market rates`,
}

func init() {
	toolCmd.AddCommand(fxempireRatesCmd)
	rootCmd.AddCommand(toolCmd)
}
