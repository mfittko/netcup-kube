package main

import "github.com/spf13/cobra"

// toolCmd is the parent command for backend-agnostic data tools.
// All tool subcommands are registered under this group.
var toolCmd = &cobra.Command{
	Use:   "tool",
	Short: "Backend-agnostic data tools for skills",
	Long: `Backend-agnostic data tools for OpenClaw skills.

Sub-commands:
  fxempire-rates    - Fetch and format FXEmpire market rates
  market-candles    - Fetch OHLCV market candle data (FXEmpire or Oanda)
  fxempire-articles - Fetch FXEmpire news and forecast articles
  fxempire-enrich   - Fetch and enrich FXEmpire data with article analysis`,
}

func init() {
	toolCmd.AddCommand(fxempireRatesCmd)
	toolCmd.AddCommand(marketCandlesCmd)
	toolCmd.AddCommand(fxempireArticlesCmd)
	toolCmd.AddCommand(fxempireEnrichCmd)
	rootCmd.AddCommand(toolCmd)
}
