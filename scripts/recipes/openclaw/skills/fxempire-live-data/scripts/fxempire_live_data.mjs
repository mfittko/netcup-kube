#!/usr/bin/env node
/**
 * FXEmpire live data fetcher.
 *
 * Modes:
 * - candles: FXEmpire chart candles API or Oanda proxy endpoint
 * - rates: FXEmpire rates APIs for commodities/indices/currencies/crypto-coin
 */

function parseBool(value, defaultValue = false) {
  if (value === undefined || value === null || value === '') return defaultValue;
  const normalized = String(value).trim().toLowerCase();
  if (['1', 'true', 'yes', 'y', 'on'].includes(normalized)) return true;
  if (['0', 'false', 'no', 'n', 'off'].includes(normalized)) return false;
  return defaultValue;
}

function parseArgv(argv) {
  const out = {
    mode: 'candles',
    provider: 'fxempire',
    locale: 'en',
    market: 'indices',
    instrument: 'NAS100/USD',
    slugs: '',
    granularity: 'M5',
    from: '',
    to: '',
    count: '500',
    vendor: 'oanda',
    alignmentTimezone: 'UTC',
    weeklyAlignment: 'Monday',
    dailyAlignment: '0',
    price: 'M',
    includeFullData: true,
    includeSparkLines: false,
    timeoutMs: '25000',
    raw: false,
    json: true,
    pretty: true,
  };

  for (let i = 0; i < argv.length; i++) {
    const token = argv[i];
    if (!token.startsWith('--')) continue;

    let key = token.slice(2);
    let value;
    const eqIdx = key.indexOf('=');
    if (eqIdx >= 0) {
      value = key.slice(eqIdx + 1);
      key = key.slice(0, eqIdx);
    } else {
      const next = argv[i + 1];
      if (next && !next.startsWith('--')) {
        value = next;
        i++;
      } else {
        value = 'true';
      }
    }

    if (key in out) out[key] = value;
  }

  out.includeFullData = parseBool(out.includeFullData, true);
  out.includeSparkLines = parseBool(out.includeSparkLines, false);
  out.raw = parseBool(out.raw, false);
  out.json = parseBool(out.json, true);
  out.pretty = parseBool(out.pretty, true);
  out.timeoutMs = Number(out.timeoutMs) || 25000;
  out.count = Number(out.count) || 500;

  return out;
}

async function fetchJson(url, timeoutMs) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);

  try {
    const response = await fetch(url, {
      method: 'GET',
      headers: {
        accept: 'application/json,*/*',
        'user-agent': 'Mozilla/5.0 (OpenClaw; fxempire-live-data)',
      },
      signal: controller.signal,
      redirect: 'follow',
    });

    const text = await response.text();
    if (!response.ok) {
      throw new Error(`HTTP ${response.status} for ${url}: ${text.slice(0, 240)}`);
    }

    try {
      return JSON.parse(text);
    } catch (err) {
      throw new Error(`JSON parse failed for ${url}: ${err.message}`);
    }
  } finally {
    clearTimeout(timeout);
  }
}

function buildCandlesUrl(opts) {
  if (opts.provider === 'oanda') {
    const url = new URL('https://p.fxempire.com/oanda/candles/latest');
    url.searchParams.set('instrument', opts.instrument);
    url.searchParams.set('granularity', opts.granularity);
    url.searchParams.set('count', String(opts.count));
    url.searchParams.set('alignmentTimezone', opts.alignmentTimezone);
    if (opts.to) url.searchParams.set('to', String(opts.to));
    return url.toString();
  }

  const url = new URL(`https://www.fxempire.com/api/v1/${opts.locale}/${opts.market}/chart/candles`);
  url.searchParams.set('instrument', opts.instrument);
  url.searchParams.set('granularity', opts.granularity);
  url.searchParams.set('count', String(opts.count));
  url.searchParams.set('price', opts.price);
  url.searchParams.set('weeklyAlignment', opts.weeklyAlignment);
  url.searchParams.set('alignmentTimezone', opts.alignmentTimezone);
  url.searchParams.set('dailyAlignment', String(opts.dailyAlignment));
  url.searchParams.set('vendor', opts.vendor);
  if (opts.from) url.searchParams.set('from', String(opts.from));
  return url.toString();
}

function buildRatesUrl(opts) {
  const slugs = String(opts.slugs || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
    .join(',');

  if (!slugs) throw new Error('rates mode requires --slugs <csv>');

  const base = `https://www.fxempire.com/api/v1/${opts.locale}`;
  if (opts.market === 'currencies') {
    return `${base}/currencies/rates?category=&includeSparkLines=${opts.includeSparkLines}&includeFullData=${opts.includeFullData}&instruments=${encodeURIComponent(slugs)}`;
  }

  return `${base}/${opts.market}/rates?instruments=${encodeURIComponent(slugs)}&includeFullData=${opts.includeFullData}&includeSparkLines=${opts.includeSparkLines}`;
}

function normalizeFxEmpireCandles(rows) {
  if (!Array.isArray(rows)) return [];
  return rows
    .map((row) => ({
      time: row?.Date || row?.date || null,
      open: Number(row?.Open),
      high: Number(row?.High),
      low: Number(row?.Low),
      close: Number(row?.Close),
      volume: Number(row?.Volume ?? 0),
      complete: true,
    }))
    .filter((c) => c.time && Number.isFinite(c.open) && Number.isFinite(c.high) && Number.isFinite(c.low) && Number.isFinite(c.close));
}

function normalizeOandaCandles(payload) {
  const rows = Array.isArray(payload?.candles) ? payload.candles : [];
  return rows
    .map((row) => ({
      time: row?.time || null,
      open: Number(row?.mid?.o),
      high: Number(row?.mid?.h),
      low: Number(row?.mid?.l),
      close: Number(row?.mid?.c),
      volume: Number(row?.volume ?? 0),
      complete: Boolean(row?.complete),
    }))
    .filter((c) => c.time && Number.isFinite(c.open) && Number.isFinite(c.high) && Number.isFinite(c.low) && Number.isFinite(c.close));
}

function normalizeRates(payload, slugsCsv) {
  const entities = payload?.entities || {};
  const prices = payload?.prices || {};
  const slugs = String(slugsCsv || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);

  return slugs.map((slug) => {
    const entity = entities[slug] || {};
    const price = prices[slug] || {};
    return {
      slug,
      name: entity.name || slug,
      symbol: entity.symbol || null,
      last: price.last ?? entity.last ?? null,
      change: price.change ?? entity.change ?? null,
      percentChange: price.percentChange ?? entity.percentChange ?? null,
      lastUpdate: price.lastUpdate || entity.lastUpdate || null,
      currency: entity.currency || null,
      vendor: entity.vendor || null,
    };
  });
}

function printResult(result, pretty) {
  process.stdout.write(`${JSON.stringify(result, null, pretty ? 2 : 0)}\n`);
}

async function main() {
  const opts = parseArgv(process.argv.slice(2));

  if (!['candles', 'rates'].includes(opts.mode)) {
    throw new Error(`unsupported --mode ${opts.mode}; use candles|rates`);
  }

  if (!['fxempire', 'oanda'].includes(opts.provider)) {
    throw new Error(`unsupported --provider ${opts.provider}; use fxempire|oanda`);
  }

  if (!['commodities', 'indices', 'currencies', 'crypto-coin'].includes(opts.market)) {
    throw new Error(`unsupported --market ${opts.market}; use commodities|indices|currencies|crypto-coin`);
  }

  if (opts.mode === 'candles') {
    const requestUrl = buildCandlesUrl(opts);
    const payload = await fetchJson(requestUrl, opts.timeoutMs);

    const candles = opts.provider === 'oanda'
      ? normalizeOandaCandles(payload)
      : normalizeFxEmpireCandles(payload);

    const result = {
      ok: true,
      mode: 'candles',
      provider: opts.provider,
      market: opts.market,
      instrument: opts.instrument,
      granularity: opts.granularity,
      requestUrl,
      count: candles.length,
      candles,
    };

    if (opts.raw) result.raw = payload;
    return printResult(result, opts.pretty);
  }

  const requestUrl = buildRatesUrl(opts);
  const payload = await fetchJson(requestUrl, opts.timeoutMs);
  const rates = normalizeRates(payload, opts.slugs);

  const result = {
    ok: true,
    mode: 'rates',
    market: opts.market,
    slugs: String(opts.slugs || '').split(',').map((s) => s.trim()).filter(Boolean),
    requestUrl,
    count: rates.length,
    rates,
  };

  if (opts.raw) result.raw = payload;
  return printResult(result, opts.pretty);
}

main().catch((error) => {
  process.stderr.write(`fxempire_live_data error: ${error.message}\n`);
  process.exitCode = 1;
});
