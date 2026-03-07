---
name: fxempire-analysis
description: Build a daily/weekly FXEmpire analysis report from multi-asset rates (commodities, indices, FX, crypto) plus related news/forecast coverage. Use when you need a reproducible market analysis pipeline, not just raw data pulls.
---

# FXEmpire analysis pipeline

Use the bundled Node scripts with clear separation of concerns:
- `scripts/fxempire_rates.mjs` for multi-asset **rates/price snapshot** (commodities, indices, FX, crypto)
- `scripts/fxempire_articles.mjs` for **news + forecasts** retrieval and snippet extraction only
- `scripts/fxempire_enrich.mjs` as an orchestrator that combines both into an **in-depth markdown market report**

## Quick start

Run the orchestrator (auto window = 24h weekdays / 48h Saturday / 72h Sunday, in Europe/Berlin by default):

```bash
node skills/fxempire-analysis/scripts/fxempire_enrich.mjs \
  --commodities brent-crude-oil,natural-gas,gold,silver \
  --focus brent-crude-oil
```

Output defaults to **markdown** on stdout (safe for cron piping). Use `--json` for structured output.

Write the report directly to a markdown file (while still returning markdown on stdout):

```bash
node skills/fxempire-analysis/scripts/fxempire_enrich.mjs \
  --hours 24 \
  --commodities brent-crude-oil,wti-crude-oil,natural-gas,gold,silver,platinum \
  --output-file /home/node/.openclaw/workspace/state/fxempire/market-analysis-24h.md
```

Run concern-specific scripts directly:

```bash
# Rates only
node skills/fxempire-analysis/scripts/fxempire_rates.mjs \
  --commodities brent-crude-oil,natural-gas,gold,silver

# Articles only
node skills/fxempire-analysis/scripts/fxempire_articles.mjs \
  --commodities brent-crude-oil,natural-gas,gold,silver \
  --tz Europe/Berlin

# Articles with full extracted body text in JSON (`textFull`)
node skills/fxempire-analysis/scripts/fxempire_articles.mjs \
  --commodities brent-crude-oil,natural-gas,gold,silver \
  --full-text --json
```

## Key endpoints (as observed)

- Rates:
  - `https://www.fxempire.com/api/v1/<locale>/commodities/rates?instruments=...&includeFullData=true&includeSparkLines=true`
  - `https://www.fxempire.com/api/v1/<locale>/indices/rates?instruments=...&includeFullData=true&includeSparkLines=true`
  - `https://www.fxempire.com/api/v1/<locale>/currencies/rates?category=&includeSparkLines=true&includeFullData=true&instruments=...`
  - `https://www.fxempire.com/api/v1/<locale>/crypto-coin/rates?instruments=...&includeFullData=true`
- Articles hub:
  - `https://www.fxempire.com/api/v1/<locale>/articles/hub/news?size=..&page=..&tag=<tag>`
  - `https://www.fxempire.com/api/v1/<locale>/articles/hub/forecasts?size=..&page=..&tag=<tag>`
- Full article pages:
  - `https://www.fxempire.com<articleUrl>` (use `articleUrl` from the hub response)

## Tag mapping

The script uses these defaults (override with `--tags` if needed):
- brent-crude-oil → `co-brent-crude-oil`
- wti-crude-oil → `co-wti-crude-oil`
- natural-gas → `co-natural-gas`
- gold → `co-gold`
- silver → `co-silver`
- platinum → `co-platinum`
- spx → `i-spx`
- tech100-usd → `i-tech100-usd`
- us30-usd → `i-us30-usd`
- eur-usd → `c-eur-usd`
- usd-jpy → `c-usd-jpy`
- bitcoin → `cc-bitcoin`
- ethereum → `cc-ethereum`
- solana → `cc-solana`

## Integration into an existing cron job

Typical pattern:

1) Existing cron produces your base market brief.
2) Call this script and append the markdown block.

Example:

```bash
base_brief_command | tee /tmp/brief.md
node skills/fxempire-analysis/scripts/fxempire_enrich.mjs --commodities brent-crude-oil,natural-gas,gold,silver >> /tmp/brief.md
send_to_destination /tmp/brief.md
```

## Options

- `--locale en` (default: `en`)
- `--tz Europe/Berlin` (default: `Europe/Berlin`) controls weekend/weekday window logic
- `--hours N` override rolling window
- `--commodities <csv>` slugs to fetch rates for
- `--focus <slug>` commodity to prioritize in output ordering
- `--max-items N` cap number of articles per tag/type after filtering (default 6)
- `--json` output JSON instead of markdown
- `--output-file /path/report.md` write full markdown report to file
- `--full-text` include extracted full article text in JSON field `textFull`
- `--max-text-chars N` cap extracted `textFull` length (default 12000)
- `--tags brent-crude-oil=co-brent-crude-oil,gold=co-gold` override mapping

## Notes / caveats

- FXEmpire endpoints are **undocumented** and may change or rate-limit.
- Some article pages are heavy; extraction is heuristic. If extraction fails, the script falls back to the API `description`/`excerpt`.
