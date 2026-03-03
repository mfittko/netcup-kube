#!/usr/bin/env node
/**
 * Watch Strait of Hormuz AIS traffic using aisstream.io (WebSocket) and print alerts.
 *
 * No external deps: uses Node.js built-in WebSocket (Node 20+ / 22+).
 *
 * Env vars
 * - AISSTREAM_API_KEY (required)
 * - HORMUZ_LAT_MIN/HORMUZ_LAT_MAX/HORMUZ_LON_MIN/HORMUZ_LON_MAX (optional)
 * - MIN_SOG (optional, default 1.5)
 * - WINDOW_SECONDS (optional, default 90)
 * - RETRY_MAX_ATTEMPTS (optional, default 5)
 * - RETRY_BASE_MS (optional, default 1000)
 * - RETRY_MAX_MS (optional, default 8000)
 * - WS_CONNECT_TIMEOUT_MS (optional, default 15000)
 * - RETRY_LOG_VERBOSE=1 (optional; print per-attempt retry logs)
 * - STATE_FILE (optional; default is OpenClaw-workspace friendly)
 * - NO_DEDUPE=1 (optional)
 */

import fs from 'node:fs';
import path from 'node:path';

function env(name, def = undefined) {
  const v = process.env[name];
  return (v === undefined || v === '') ? def : v;
}

function nowUtc() {
  const d = new Date();
  // YYYY-MM-DD HH:MM UTC
  const iso = d.toISOString();
  return `${iso.slice(0, 10)} ${iso.slice(11, 16)} UTC`;
}

function loadSeen(stateFile) {
  try {
    const raw = fs.readFileSync(stateFile, 'utf8');
    const data = JSON.parse(raw);
    if (Array.isArray(data)) return new Set(data.map(String));
  } catch (_) {
    // ignore
  }
  return new Set();
}

function saveSeen(stateFile, seenSet) {
  const dir = path.dirname(stateFile);
  fs.mkdirSync(dir, { recursive: true });
  const tmp = `${stateFile}.tmp`;
  const arr = Array.from(seenSet);
  arr.sort();
  fs.writeFileSync(tmp, JSON.stringify(arr), 'utf8');
  fs.renameSync(tmp, stateFile);
}

function inBox(cfg, lat, lon) {
  return cfg.latmin <= lat && lat <= cfg.latmax && cfg.lonmin <= lon && lon <= cfg.lonmax;
}

function parsePositionReport(msg) {
  const mtype = msg?.MessageType;
  const okTypes = new Set(['PositionReport', 'StandardClassBPositionReport', 'ExtendedClassBPositionReport']);
  if (!okTypes.has(mtype)) return null;

  const meta = msg?.MetaData ?? {};
  const mmsi = meta?.MMSI;
  if (!mmsi) return null;

  const message = msg?.Message ?? {};
  const pr = message?.PositionReport ?? message?.StandardClassBPositionReport ?? message?.ExtendedClassBPositionReport;
  if (!pr || typeof pr !== 'object') return null;

  const lat = Number(pr?.Latitude);
  const lon = Number(pr?.Longitude);
  if (!Number.isFinite(lat) || !Number.isFinite(lon)) return null;

  const sog = Number(pr?.Sog ?? 0);
  const name = String(meta?.ShipName ?? meta?.NAME ?? '').trim() || 'UNKNOWN';

  return { mmsi: String(mmsi), name, lat, lon, sog };
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, Math.max(0, ms)));
}

function isRetryableWebSocketError(err) {
  const text = String(err?.message ?? err ?? '').toLowerCase();
  if (!text) return true;
  return (
    text.includes('abort') ||
    text.includes('aborted') ||
    text.includes('closed before it was established') ||
    text.includes('closed before stream opened') ||
    text.includes('closed before') ||
    text.includes('network') ||
    text.includes('socket') ||
    text.includes('timed out') ||
    text.includes('timeout') ||
    text.includes('econn')
  );
}

function backoffDelayMs(cfg, attempt) {
  const expo = cfg.retryBaseMs * Math.pow(2, Math.max(0, attempt - 1));
  const jitter = Math.floor(Math.random() * Math.max(250, Math.floor(cfg.retryBaseMs / 2)));
  return Math.min(cfg.retryMaxMs, expo + jitter);
}

async function runAttempt(cfg, seen, alerts, deadline) {
  if (typeof WebSocket === 'undefined') {
    throw new Error('Global WebSocket is not available in this Node runtime. Need Node 20+ (works on Node 22).');
  }

  const url = 'wss://stream.aisstream.io/v0/stream';

  const subscribe = {
    APIKey: cfg.apiKey,
    BoundingBoxes: [[[cfg.latmin, cfg.lonmin], [cfg.latmax, cfg.lonmax]]],
    FilterMessageTypes: ['PositionReport', 'StandardClassBPositionReport', 'ExtendedClassBPositionReport'],
  };

  await new Promise((resolve, reject) => {
    const ws = new WebSocket(url);
    let settled = false;
    let opened = false;

    const settleResolve = () => {
      if (settled) return;
      settled = true;
      clearInterval(tick);
      clearTimeout(connectTimeout);
      resolve(null);
    };

    const settleReject = (err) => {
      if (settled) return;
      settled = true;
      clearInterval(tick);
      clearTimeout(connectTimeout);
      reject(err);
    };

    const connectTimeout = setTimeout(() => {
      try { ws.close(); } catch (_) {}
      settleReject(new Error(`WebSocket connect timeout after ${cfg.wsConnectTimeoutMs}ms`));
    }, cfg.wsConnectTimeoutMs);

    ws.addEventListener('open', () => {
      opened = true;
      clearTimeout(connectTimeout);
      ws.send(JSON.stringify(subscribe));
    });

    ws.addEventListener('message', (ev) => {
      if (Date.now() > deadline) {
        try { ws.close(); } catch (_) {}
        return;
      }

      let msg;
      try {
        msg = JSON.parse(typeof ev.data === 'string' ? ev.data : Buffer.from(ev.data).toString('utf8'));
      } catch (_) {
        return;
      }

      const pr = parsePositionReport(msg);
      if (!pr) return;
      if (pr.sog < cfg.minSog) return;
      if (!inBox(cfg, pr.lat, pr.lon)) return;

      if (!cfg.noDedupe && seen.has(pr.mmsi)) return;

      const text = [
        'AIS Hormuz match',
        `Name: ${pr.name}`,
        `MMSI: ${pr.mmsi}`,
        `SOG: ${pr.sog.toFixed(1)} kn`,
        `Pos: ${pr.lat}, ${pr.lon}`,
        `Time: ${nowUtc()}`,
        `Track: https://www.marinetraffic.com/en/ais/details/ships/mmsi:${pr.mmsi}`,
      ].join('\n');

      alerts.push({ mmsi: pr.mmsi, text });
      if (!cfg.noDedupe) seen.add(pr.mmsi);
    });

    ws.addEventListener('error', (e) => {
      const err = e?.error ?? e;
      settleReject(err instanceof Error ? err : new Error(String(err || 'websocket error')));
    });

    ws.addEventListener('close', () => {
      if (!opened && Date.now() < deadline) {
        settleReject(new Error('WebSocket closed before stream opened'));
        return;
      }
      settleResolve();
    });

    // hard timeout
    const tick = setInterval(() => {
      if (Date.now() > deadline) {
        clearInterval(tick);
        try { ws.close(); } catch (_) {}
      }
    }, 250);
  });
}

async function runOnce(cfg) {
  const seen = cfg.noDedupe ? new Set() : loadSeen(cfg.stateFile);
  const alerts = [];
  const deadline = Date.now() + cfg.windowSeconds * 1000;
  let lastError = null;

  let attempt = 0;
  while (Date.now() < deadline) {
    attempt += 1;
    try {
      await runAttempt(cfg, seen, alerts, deadline);
      lastError = null;
      break;
    } catch (err) {
      lastError = err;
      const retryable = isRetryableWebSocketError(err);
      const retriesLeft = attempt < cfg.retryMaxAttempts;
      const timeLeftMs = deadline - Date.now();
      if (!retryable || !retriesLeft || timeLeftMs <= 0) {
        break;
      }

      const waitMs = Math.min(backoffDelayMs(cfg, attempt), Math.max(0, timeLeftMs));
      if (cfg.retryLogVerbose) {
        process.stderr.write(`hormuz_watch: websocket attempt ${attempt} failed (${String(err?.message ?? err)}), retrying in ${waitMs}ms\n`);
      }
      await sleep(waitMs);
    }
  }

  if (alerts.length === 0 && lastError) {
    process.stderr.write(`AISSTREAM_ERROR: connection unavailable after ${attempt} attempts (${String(lastError?.message ?? lastError)})\n`);
  }

  if (alerts.length) {
    for (const a of alerts) {
      process.stdout.write(a.text + '\n---\n');
    }
    if (!cfg.noDedupe) saveSeen(cfg.stateFile, seen);
  }
}

function parseConfig() {
  const apiKey = env('AISSTREAM_API_KEY');
  if (!apiKey) {
    throw new Error('AISSTREAM_API_KEY is required (set env var)');
  }

  const f = (name, def) => {
    const v = env(name);
    return v === undefined ? def : Number(v);
  };
  const i = (name, def) => {
    const v = env(name);
    return v === undefined ? def : parseInt(v, 10);
  };

  return {
    apiKey,
    latmin: f('HORMUZ_LAT_MIN', 25.5),
    latmax: f('HORMUZ_LAT_MAX', 27.5),
    lonmin: f('HORMUZ_LON_MIN', 56.0),
    lonmax: f('HORMUZ_LON_MAX', 57.5),
    minSog: f('MIN_SOG', 1.5),
    windowSeconds: i('WINDOW_SECONDS', 90),
    retryMaxAttempts: i('RETRY_MAX_ATTEMPTS', 5),
    retryBaseMs: i('RETRY_BASE_MS', 1000),
    retryMaxMs: i('RETRY_MAX_MS', 8000),
    wsConnectTimeoutMs: i('WS_CONNECT_TIMEOUT_MS', 15000),
    retryLogVerbose: env('RETRY_LOG_VERBOSE', '0') === '1',
    stateFile: env('STATE_FILE', '/home/node/.openclaw/workspace/state/hormuz-ais-watch/seen_vessels.json'),
    noDedupe: env('NO_DEDUPE', '0') === '1',
  };
}

const cfg = parseConfig();
await runOnce(cfg);
