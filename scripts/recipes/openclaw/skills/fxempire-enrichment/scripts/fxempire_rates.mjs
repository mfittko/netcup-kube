#!/usr/bin/env node
/**
 * FXEmpire rates-only fetcher.
 *
 * Responsibility:
 * - Retrieve commodity rates/snapshot from FXEmpire API.
 */

function parseArgs(argv) {
  const out = {
    locale: 'en',
    commodities: ['brent-crude-oil', 'natural-gas', 'gold', 'silver'],
    json: false,
  };

  for (let i = 0; i < argv.length; i++) {
    const k = argv[i];
    if (!k.startsWith('--')) continue;
    const key = k.slice(2);
    const next = argv[i + 1];
    const hasValue = next && !next.startsWith('--');
    const val = hasValue ? next : null;
    if (hasValue) i++;

    if (key === 'locale' && val) out.locale = val;
    else if (key === 'commodities' && val)
      out.commodities = val.split(',').map((s) => s.trim()).filter(Boolean);
    else if (key === 'json') out.json = true;
  }

  if (!out.commodities.length) out.commodities = ['brent-crude-oil'];
  return out;
}

function classifyInstrument(slug) {
  const s = String(slug || '').trim().toLowerCase();

  const commoditySet = new Set([
    'brent-crude-oil',
    'wti-crude-oil',
    'natural-gas',
    'gold',
    'silver',
    'platinum',
  ]);

  const indexSet = new Set([
    'spx',
    'tech100-usd',
    'us30-usd',
    'de30-eur',
    'uk100-gbp',
    'jp225-usd',
    'fr40-eur',
    'vix',
  ]);

  const currencySet = new Set([
    'eur-usd',
    'usd-jpy',
    'gbp-usd',
    'usd-chf',
    'usd-cad',
    'aud-usd',
    'nzd-usd',
  ]);

  if (commoditySet.has(s)) return 'commodities';
  if (indexSet.has(s)) return 'indices';
  if (currencySet.has(s)) return 'currencies';

  if (s.includes('-') && s.split('-').length === 2) return 'currencies';
  return 'crypto-coin';
}

async function fetchText(url, { timeoutMs = 20000 } = {}) {
  const ac = new AbortController();
  const t = setTimeout(() => ac.abort(), timeoutMs);
  try {
    const res = await fetch(url, {
      headers: {
        'user-agent': 'Mozilla/5.0 (OpenClaw; fxempire-rates)',
        accept: '*/*',
      },
      redirect: 'follow',
      signal: ac.signal,
    });
    const text = await res.text();
    return { ok: res.ok, status: res.status, text, url: res.url };
  } finally {
    clearTimeout(t);
  }
}

async function fetchJson(url, opts) {
  const r = await fetchText(url, opts);
  if (!r.ok) throw new Error(`HTTP ${r.status} for ${url}`);
  try {
    return JSON.parse(r.text);
  } catch (e) {
    throw new Error(`JSON parse failed for ${url}: ${e.message}`);
  }
}

function fmtPct(x) {
  if (x === null || x === undefined || Number.isNaN(Number(x))) return null;
  return `${Number(x).toFixed(2)}%`;
}

function fmtNum(x) {
  if (x === null || x === undefined || Number.isNaN(Number(x))) return null;
  const n = Number(x);
  return n.toLocaleString('en-US', { maximumFractionDigits: 3 });
}

function mdEscape(s) {
  return String(s).replace(/\|/g, '\\|');
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const base = `https://www.fxempire.com/api/v1/${args.locale}`;

  const groups = {
    commodities: [],
    indices: [],
    currencies: [],
    'crypto-coin': [],
  };
  for (const slug of args.commodities) {
    groups[classifyInstrument(slug)].push(slug);
  }

  const ratesUrls = [];
  if (groups.commodities.length) {
    ratesUrls.push(
      `${base}/commodities/rates?instruments=${encodeURIComponent(groups.commodities.join(','))}&includeFullData=true&includeSparkLines=true`
    );
  }
  if (groups.indices.length) {
    ratesUrls.push(
      `${base}/indices/rates?instruments=${encodeURIComponent(groups.indices.join(','))}&includeFullData=true&includeSparkLines=true`
    );
  }
  if (groups.currencies.length) {
    ratesUrls.push(
      `${base}/currencies/rates?category=&includeSparkLines=true&includeFullData=true&instruments=${encodeURIComponent(groups.currencies.join(','))}`
    );
  }
  if (groups['crypto-coin'].length) {
    ratesUrls.push(
      `${base}/crypto-coin/rates?instruments=${encodeURIComponent(groups['crypto-coin'].join(','))}&includeFullData=true`
    );
  }

  const mergedRates = { entities: {}, prices: {} };
  let ratesError = null;
  for (const url of ratesUrls) {
    try {
      const partial = await fetchJson(url, { timeoutMs: 20000 });
      Object.assign(mergedRates.entities, partial?.entities || {});
      Object.assign(mergedRates.prices, partial?.prices || {});
    } catch (e) {
      ratesError = ratesError ? `${ratesError}; ${e.message}` : e.message;
    }
  }

  const prices = [];
  if (Object.keys(mergedRates.entities).length || Object.keys(mergedRates.prices).length) {
    for (const slug of args.commodities) {
      const e = mergedRates.entities?.[slug] || {};
      const p = mergedRates.prices?.[slug] || {};
      prices.push({
        slug,
        name: e.name || slug,
        last: p.last ?? e.last ?? null,
        change: p.change ?? e.change ?? null,
        pct: p.percentChange ?? e.percentChange ?? null,
        lastUpdate: p.lastUpdate || e.lastUpdate || null,
      });
    }
  }

  const payload = {
    meta: {
      now: new Date().toISOString(),
      locale: args.locale,
      commodities: args.commodities,
    },
    ratesUrl: ratesUrls.join(' | '),
    prices,
    pricesError: ratesError,
  };

  if (args.json) {
    process.stdout.write(JSON.stringify(payload, null, 2));
    return;
  }

  const lines = [];
  lines.push('## FXEmpire rates');
  if (ratesError) {
    lines.push(`- ERROR: ${mdEscape(ratesError)}`);
  } else {
    for (const row of prices) {
      const pct = fmtPct(row.pct);
      const ch = row.change === null || row.change === undefined ? null : fmtNum(row.change);
      lines.push(
        `- **${mdEscape(row.name)}** (${row.slug}): ${fmtNum(row.last)} ` +
          (ch && pct ? `(${ch}, ${pct})` : '') +
          (row.lastUpdate ? ` — lastUpdate ${row.lastUpdate}` : '')
      );
    }
  }

  process.stdout.write(lines.join('\n') + '\n');
}

main().catch((e) => {
  process.stderr.write(`fxempire_rates error: ${e.message}\n`);
  process.exitCode = 1;
});
