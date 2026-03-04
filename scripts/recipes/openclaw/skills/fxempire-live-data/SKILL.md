````skill
---
name: fxempire-live-data
description: Fetch live FXEmpire market data (candles and rates) for indices, commodities, FX, and crypto via documented website APIs and Oanda proxy candles endpoint.
---

# FXEmpire live data

Use `scripts/fxempire_live_data.mjs` when you need machine-readable near-live candles or rates snapshots in cron/agent workflows.

## Quick examples

```bash
# 10-minute candles via FXEmpire chart API (indices)
node skills/fxempire-live-data/scripts/fxempire_live_data.mjs \
  --mode candles \
  --provider fxempire \
  --market indices \
  --instrument NAS100/USD \
  --granularity M5 \
  --from 1772582400 \
  --count 500

# 1-minute candles via Oanda proxy endpoint
node skills/fxempire-live-data/scripts/fxempire_live_data.mjs \
  --mode candles \
  --provider oanda \
  --instrument NAS100/USD \
  --granularity M1 \
  --to 1772654999 \
  --count 1000 \
  --alignmentTimezone Europe/Berlin

# Live rates snapshot for mixed watchlist (run per market)
node skills/fxempire-live-data/scripts/fxempire_live_data.mjs \
  --mode rates \
  --market indices \
  --slugs spx,tech100-usd,us30-usd

node skills/fxempire-live-data/scripts/fxempire_live_data.mjs \
  --mode rates \
  --market currencies \
  --slugs eur-usd,usd-jpy,gbp-usd

node skills/fxempire-live-data/scripts/fxempire_live_data.mjs \
  --mode rates \
  --market crypto-coin \
  --slugs bitcoin,ethereum,solana
```

## Output

- Default output is JSON (`--json true`) with normalized fields.
- Candles output includes:
  - `time`, `open`, `high`, `low`, `close`, `volume`, `complete`
- Rates output includes:
  - `slug`, `name`, `symbol`, `last`, `change`, `percentChange`, `lastUpdate`, `currency`, `vendor`
- Add `--raw true` to include the raw upstream payload in the response.

## Supported options

- `--mode candles|rates` (default: `candles`)
- `--provider fxempire|oanda` (candles mode, default: `fxempire`)
- `--market commodities|indices|currencies|crypto-coin` (default: `indices`)
- `--instrument <value>` (candles mode, default: `NAS100/USD`)
- `--slugs <csv>` (rates mode, required)
- `--granularity <M1|M5|H1|...>` (default: `M5`)
- `--from <unix-seconds>` (FXEmpire candles)
- `--to <unix-seconds>` (Oanda proxy candles)
- `--count <n>` (default: `500`)
- `--alignmentTimezone <tz>` (default: `UTC`)
- `--vendor <oanda|...>` (FXEmpire candles default: `oanda`)
- `--price <M|B|A>` (FXEmpire candles default: `M`)
- `--includeFullData true|false` (rates mode)
- `--includeSparkLines true|false` (rates mode)
- `--timeoutMs <ms>` (default: `25000`)
- `--raw true|false` (default: `false`)
- `--pretty true|false` (default: `true`)

## Notes

- Endpoints are not formally public APIs and may change.
- Prefer using this skill as the single integration point so endpoint changes are isolated.
````
