#!/usr/bin/env node

import fs from 'node:fs';
import path from 'node:path';

function env(name, fallback = undefined) {
  const value = process.env[name];
  return value === undefined || value === '' ? fallback : value;
}

function intEnv(name, fallback) {
  const raw = env(name);
  if (raw === undefined) return fallback;
  const parsed = Number.parseInt(raw, 10);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function pickRandom(items) {
  if (!Array.isArray(items) || items.length === 0) return null;
  const index = Math.floor(Math.random() * items.length);
  return items[index] ?? null;
}

function userAgentPoolFromEnv(raw) {
  if (!raw || !String(raw).trim()) return [];
  return String(raw)
    .split('||')
    .map((entry) => entry.trim())
    .filter(Boolean);
}

function buildDefaultUserAgentPool() {
  const osTokens = [
    'Windows NT 10.0; Win64; x64',
    'Windows NT 10.0; WOW64',
    'Macintosh; Intel Mac OS X 10_15_7',
    'Macintosh; Intel Mac OS X 14_3_1',
    'X11; Linux x86_64',
  ];
  const chromeMajors = [126, 127, 128, 129, 130, 131, 132, 133, 134, 135];

  const pool = [];
  for (const osToken of osTokens) {
    for (const major of chromeMajors) {
      pool.push(`Mozilla/5.0 (${osToken}) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/${major}.0.0.0 Safari/537.36`);
    }
  }
  return pool;
}

function resolveUserAgentSettings() {
  const defaultPool = buildDefaultUserAgentPool();

  const mode = String(env('TRUTHSOCIAL_USER_AGENT_MODE', 'random')).trim().toLowerCase();
  const fixedUserAgent = String(env('TRUTHSOCIAL_USER_AGENT', '')).trim();
  const envPool = userAgentPoolFromEnv(env('TRUTHSOCIAL_USER_AGENT_POOL', ''));
  const pool = envPool.length > 0 ? envPool : defaultPool;

  if (fixedUserAgent) {
    return {
      userAgent: fixedUserAgent,
      mode: 'fixed',
    };
  }

  if (mode === 'off' || mode === 'none' || mode === 'disabled') {
    return {
      userAgent: null,
      mode: 'off',
    };
  }

  return {
    userAgent: pickRandom(pool),
    mode: 'random',
  };
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, Math.max(0, ms)));
}

function nowUtcMinute() {
  const iso = new Date().toISOString();
  return `${iso.slice(0, 10)} ${iso.slice(11, 16)} UTC`;
}

function decodeHtmlEntities(text) {
  return String(text)
    .replace(/&#039;/g, "'")
    .replace(/&quot;/g, '"')
    .replace(/&amp;/g, '&')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&nbsp;/g, ' ');
}

function stripHtmlTags(text) {
  return decodeHtmlEntities(String(text).replace(/<[^>]*>/g, ' ')).replace(/\s+/g, ' ').trim();
}

function extractNumericId(url) {
  const value = String(url || '').trim();
  const m = value.match(/\/(\d+)(?:\D*)$/);
  return m ? m[1] : '';
}

function monthNameToNumber(name) {
  const months = {
    january: 1,
    february: 2,
    march: 3,
    april: 4,
    may: 5,
    june: 6,
    july: 7,
    august: 8,
    september: 9,
    october: 10,
    november: 11,
    december: 12,
  };
  return months[String(name || '').trim().toLowerCase()] ?? null;
}

function parseArchiveTimestampParts(timestampText) {
  const raw = String(timestampText || '').trim();
  const m = raw.match(/^([A-Za-z]+)\s+(\d{1,2}),\s*(\d{4}),\s*(\d{1,2}):(\d{2})\s*([AP]M)$/i);
  if (!m) {
    return null;
  }

  const month = monthNameToNumber(m[1]);
  if (!month) {
    return null;
  }

  const day = Number.parseInt(m[2], 10);
  const year = Number.parseInt(m[3], 10);
  let hour = Number.parseInt(m[4], 10);
  const minute = Number.parseInt(m[5], 10);
  const meridiem = m[6].toUpperCase();

  if (hour === 12) {
    hour = meridiem === 'AM' ? 0 : 12;
  } else if (meridiem === 'PM') {
    hour += 12;
  }

  return { year, month, day, hour, minute };
}

function getWallClockParts(utcMs, timeZone) {
  const formatter = new Intl.DateTimeFormat('en-US', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });

  const parts = formatter.formatToParts(new Date(utcMs));
  const map = {};
  for (const p of parts) {
    if (p.type !== 'literal') {
      map[p.type] = p.value;
    }
  }

  return {
    year: Number.parseInt(map.year, 10),
    month: Number.parseInt(map.month, 10),
    day: Number.parseInt(map.day, 10),
    hour: Number.parseInt(map.hour, 10),
    minute: Number.parseInt(map.minute, 10),
  };
}

function zonedDateTimeToUTC(parts, timeZone) {
  let guess = Date.UTC(parts.year, parts.month - 1, parts.day, parts.hour, parts.minute, 0, 0);

  for (let i = 0; i < 4; i += 1) {
    const wall = getWallClockParts(guess, timeZone);
    const wallAsUTC = Date.UTC(wall.year, wall.month - 1, wall.day, wall.hour, wall.minute, 0, 0);
    const targetAsUTC = Date.UTC(parts.year, parts.month - 1, parts.day, parts.hour, parts.minute, 0, 0);
    const diff = targetAsUTC - wallAsUTC;
    if (diff === 0) {
      return guess;
    }
    guess += diff;
  }

  return guess;
}

function parseTimestampBestEffort(timestampText, sourceTimeZone) {
  const parsed = parseArchiveTimestampParts(timestampText);
  if (!parsed) {
    return null;
  }

  try {
    const utcMs = zonedDateTimeToUTC(parsed, sourceTimeZone);
    if (!Number.isFinite(utcMs)) {
      return null;
    }
    return new Date(utcMs).toISOString();
  } catch (_) {
    return null;
  }
}

function ensureDirFor(filePath) {
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
}

function readState(filePath) {
  try {
    const raw = fs.readFileSync(filePath, 'utf8');
    const parsed = JSON.parse(raw);
    const ids = Array.isArray(parsed?.seenPostIds) ? parsed.seenPostIds.map(String) : [];
    return {
      seenPostIds: ids,
      latestPostId: parsed?.latestPostId ? String(parsed.latestPostId) : null,
      latestPostUrl: parsed?.latestPostUrl ? String(parsed.latestPostUrl) : null,
      lastPollAt: parsed?.lastPollAt ?? null,
      lastSeenAt: parsed?.lastSeenAt ?? null,
      lastTitle: parsed?.lastTitle ?? null,
    };
  } catch (_) {
    return {
      seenPostIds: [],
      latestPostId: null,
      latestPostUrl: null,
      lastPollAt: null,
      lastSeenAt: null,
      lastTitle: null,
    };
  }
}

function writeState(filePath, state) {
  ensureDirFor(filePath);
  const tmp = `${filePath}.tmp`;
  fs.writeFileSync(tmp, JSON.stringify(state, null, 2), 'utf8');
  fs.renameSync(tmp, filePath);
}

function writePostsSnapshot(filePath, payload) {
  ensureDirFor(filePath);
  const tmp = `${filePath}.tmp`;
  fs.writeFileSync(tmp, JSON.stringify(payload, null, 2), 'utf8');
  fs.renameSync(tmp, filePath);
}

async function createTarget(cdpBaseUrl, profileUrl) {
  const endpoint = `${cdpBaseUrl.replace(/\/+$/, '')}/json/new?${encodeURIComponent(profileUrl)}`;
  const res = await fetch(endpoint, { method: 'PUT' });
  if (!res.ok) {
    throw new Error(`Failed to create CDP target: HTTP ${res.status}`);
  }
  const data = await res.json();
  const wsUrl = data?.webSocketDebuggerUrl;
  const targetId = data?.id;
  if (!wsUrl || !targetId) {
    throw new Error('CDP target response missing webSocketDebuggerUrl/id');
  }
  return { wsUrl, targetId };
}

async function closeTarget(cdpBaseUrl, targetId) {
  const endpoint = `${cdpBaseUrl.replace(/\/+$/, '')}/json/close/${encodeURIComponent(targetId)}`;
  try {
    await fetch(endpoint, { method: 'GET' });
  } catch (_) {
    // ignore cleanup errors
  }
}

class CdpClient {
  constructor(wsUrl) {
    if (typeof WebSocket === 'undefined') {
      throw new Error('Global WebSocket is unavailable. Node 20+ required.');
    }
    this.ws = new WebSocket(wsUrl);
    this.nextId = 1;
    this.pending = new Map();

    this.openPromise = new Promise((resolve, reject) => {
      this.ws.addEventListener('open', () => resolve());
      this.ws.addEventListener('error', (event) => reject(event?.error ?? new Error('WebSocket open error')));
    });

    this.ws.addEventListener('message', (event) => {
      let payload;
      try {
        const raw = typeof event.data === 'string' ? event.data : Buffer.from(event.data).toString('utf8');
        payload = JSON.parse(raw);
      } catch (_) {
        return;
      }

      if (!payload || typeof payload !== 'object') return;
      if (typeof payload.id !== 'number') return;

      const pending = this.pending.get(payload.id);
      if (!pending) return;
      this.pending.delete(payload.id);

      if (payload.error) {
        pending.reject(new Error(String(payload.error?.message ?? 'CDP command failed')));
      } else {
        pending.resolve(payload.result ?? {});
      }
    });

    this.ws.addEventListener('close', () => {
      for (const [, pending] of this.pending) {
        pending.reject(new Error('CDP WebSocket closed'));
      }
      this.pending.clear();
    });
  }

  async open() {
    await this.openPromise;
  }

  async send(method, params = {}) {
    const id = this.nextId++;
    const message = JSON.stringify({ id, method, params });

    const promise = new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
    });

    this.ws.send(message);
    return promise;
  }

  close() {
    try {
      this.ws.close();
    } catch (_) {
      // ignore
    }
  }
}

async function fetchRecentPosts({ cdpBaseUrl, profileUrl, username, maxPosts, waitAfterLoadMs, navTimeoutMs, userAgent, acceptLanguage }) {
  const { wsUrl, targetId } = await createTarget(cdpBaseUrl, profileUrl);
  const client = new CdpClient(wsUrl);

  try {
    await client.open();
    await client.send('Page.enable');
    await client.send('Runtime.enable');
    await client.send('Network.enable');

    if (userAgent) {
      await client.send('Network.setUserAgentOverride', {
        userAgent,
        acceptLanguage,
        platform: 'Win32',
      });
    }

    if (acceptLanguage) {
      await client.send('Network.setExtraHTTPHeaders', {
        headers: {
          'Accept-Language': acceptLanguage,
        },
      });
    }

    await client.send('Page.navigate', { url: profileUrl });
    await sleep(waitAfterLoadMs);

    const expression = `(() => {
      const uname = ${JSON.stringify(username)};
      const cap = ${JSON.stringify(maxPosts)};

      const entries = [...document.querySelectorAll('[id^="status-"]')]
        .filter((el, i, arr) => arr.findIndex((e) => e.id === el.id) === i)
        .map((el) => {
          const id = String(el.id || '').replace(/^status-/, '').trim();
          const text =
            el.querySelector('[data-testid="status-content"]')?.innerText?.trim() ||
            el.querySelector('[class*="whitespace-pre-wrap"]')?.innerText?.trim() ||
            '';
          return [id, text];
        })
        .filter(([id, text]) => id && text);

      entries.sort((a, b) => Number(b[0]) - Number(a[0]));
      const posts = entries.slice(0, cap).map(([id, text]) => ({
        id,
        url: 'https://truthsocial.com/@' + uname + '/posts/' + id,
        text: String(text).replace(/\\s+/g, ' ').trim().slice(0, 500),
      }));

      return {
        title: document.title || '',
        href: location.href,
        bodyPreview: String(document.body?.innerText || '').replace(/\s+/g, ' ').trim().slice(0, 240),
        posts,
        polledAt: new Date().toISOString(),
      };
    })();`;

    const evalResult = await client.send('Runtime.evaluate', {
      expression,
      returnByValue: true,
      awaitPromise: true,
      timeout: navTimeoutMs,
    });

    const value = evalResult?.result?.value;
    if (!value || !Array.isArray(value.posts)) {
      throw new Error('Failed to parse Truth Social posts from page');
    }

    return {
      targetId,
      title: String(value.title ?? ''),
      href: String(value.href ?? profileUrl),
      bodyPreview: String(value.bodyPreview ?? ''),
      polledAt: String(value.polledAt ?? new Date().toISOString()),
      posts: value.posts
        .map((p) => ({
          id: String(p?.id ?? '').trim(),
          url: String(p?.url ?? '').trim(),
          text: String(p?.text ?? '').trim(),
          timestampText: null,
          timestampISO: null,
        }))
        .filter((p) => p.id && p.url),
    };
  } finally {
    client.close();
    await closeTarget(cdpBaseUrl, targetId);
  }
}

async function fetchRecentPostsNode({ profileUrl, username, maxPosts, userAgent, acceptLanguage, navTimeoutMs, sourceTimeZone }) {
  const headers = {
    'Accept': 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8',
    'Accept-Language': acceptLanguage,
    'Cache-Control': 'no-cache',
    'Pragma': 'no-cache',
  };
  if (userAgent) {
    headers['User-Agent'] = userAgent;
  }

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), Math.max(1000, navTimeoutMs));

  let html = '';
  let finalUrl = profileUrl;
  try {
    const response = await fetch(profileUrl, {
      method: 'GET',
      headers,
      redirect: 'follow',
      signal: controller.signal,
    });
    html = await response.text();
    finalUrl = response.url || profileUrl;
  } finally {
    clearTimeout(timeout);
  }

  const posts = [];
  const seen = new Set();
  const statusRegex = /<div class="status"[^>]*data-status-url="([^"]+)"[\s\S]*?<div class="status__content">([\s\S]*?)<\/div>[\s\S]*?<div class="status__footer"><\/div>/g;
  let match;
  while ((match = statusRegex.exec(html)) !== null) {
    const block = match[0];
    const statusUrl = decodeHtmlEntities(match?.[1] || '').trim();
    const externalUrlMatch = block.match(/href="(https:\/\/truthsocial\.com\/@[^"\s]+\/\d+)"/i);
    const externalUrl = decodeHtmlEntities(externalUrlMatch?.[1] || '').trim();
    const timestampMatch = block.match(/href="https:\/\/www\.trumpstruth\.org\/statuses\/\d+"\s+class="status-info__meta-item">([\s\S]*?)<\/a>/i);
    const timestampText = stripHtmlTags(timestampMatch?.[1] || '');
    const timestampISO = parseTimestampBestEffort(timestampText, sourceTimeZone);
    const resolvedUrl = externalUrl || statusUrl;
    const id = extractNumericId(externalUrl) || extractNumericId(statusUrl);
    if (!id || !resolvedUrl || seen.has(id)) continue;

    const text = stripHtmlTags(match?.[2] || '').slice(0, 500);
    if (!text) continue;

    seen.add(id);
    posts.push({
      id,
      url: resolvedUrl,
      text,
      timestampText: timestampText || null,
      timestampISO,
      timestampTimeZone: sourceTimeZone,
    });
    if (posts.length >= maxPosts) break;
  }

  return {
    targetId: 'node-fetch',
    title: (() => {
      const m = html.match(/<title[^>]*>([\s\S]*?)<\/title>/i);
      return stripHtmlTags(m?.[1] || '');
    })(),
    href: finalUrl,
    bodyPreview: stripHtmlTags(html).slice(0, 240),
    polledAt: new Date().toISOString(),
    posts,
    source: `node:${username}`,
  };
}

function isLikelyChallengePage(title, bodyPreview) {
  const text = `${String(title || '')} ${String(bodyPreview || '')}`.toLowerCase();
  const markers = [
    'just a moment',
    'checking your browser',
    'verify you are human',
    'attention required',
    'cloudflare',
    'cf-challenge',
  ];
  return markers.some((marker) => text.includes(marker));
}

function formatAlert(posts, profileUrl) {
  const lines = [];
  lines.push('🟠 New Truth Social post(s): @realDonaldTrump');
  lines.push(`Profile: ${profileUrl}`);
  lines.push(`Detected: ${nowUtcMinute()}`);
  lines.push('');

  for (const post of posts) {
    lines.push(`• ${post.url}`);
    if (post.text) {
      lines.push(`  ${post.text}`);
    }
  }

  return lines.join('\n');
}

async function main() {
  const sourceMode = String(env('TRUTHSOCIAL_SOURCE_MODE', 'node')).trim().toLowerCase();
  const defaultProfileUrl = sourceMode === 'node' ? 'https://www.trumpstruth.org/' : 'https://truthsocial.com/@realDonaldTrump';
  const profileUrl = env('TRUTHSOCIAL_PROFILE_URL', defaultProfileUrl);
  const username = env('TRUTHSOCIAL_USERNAME', 'realDonaldTrump');
  const cdpBaseUrl = env('CDP_BASE_URL', 'http://localhost:9222');
  const maxPosts = intEnv('MAX_POSTS', 8);
  const waitAfterLoadMs = intEnv('WAIT_AFTER_LOAD_MS', 3500);
  const navTimeoutMs = intEnv('NAV_TIMEOUT_MS', 30000);
  const acceptLanguage = env('TRUTHSOCIAL_ACCEPT_LANGUAGE', 'en-US,en;q=0.9');
  const sourceTimeZone = env('TRUTHSOCIAL_SOURCE_TIMEZONE', 'America/New_York');
  const stateFile = env('STATE_FILE', '/home/node/.openclaw/workspace/state/truthsocial-trump-watch/state.json');
  const postsFile = env('TRUTHSOCIAL_POSTS_FILE', '/home/node/.openclaw/workspace/state/truthsocial-trump-watch/latest-posts.json');
  const uaSettings = resolveUserAgentSettings();

  const state = readState(stateFile);

  const scraped = sourceMode === 'cdp'
    ? await fetchRecentPosts({
      cdpBaseUrl,
      profileUrl,
      username,
      maxPosts,
      waitAfterLoadMs,
      navTimeoutMs,
      userAgent: uaSettings.userAgent,
      acceptLanguage,
    })
    : await fetchRecentPostsNode({
      profileUrl,
      username,
      maxPosts,
      userAgent: uaSettings.userAgent,
      acceptLanguage,
      navTimeoutMs,
      sourceTimeZone,
    });

  const previousLatestId = state.latestPostId;
  const currentLatestPost = scraped.posts[0] ?? null;
  const currentLatestId = currentLatestPost?.id ?? null;
  const challengeDetected = scraped.posts.length === 0 && isLikelyChallengePage(scraped.title, scraped.bodyPreview);

  let newest = [];
  if (currentLatestId && previousLatestId && currentLatestId !== previousLatestId) {
    let foundPrevious = false;
    for (const post of scraped.posts) {
      if (post.id === previousLatestId) {
        foundPrevious = true;
        break;
      }
      newest.push(post);
    }
    if (!foundPrevious && currentLatestPost) {
      newest = [currentLatestPost];
    }
  }

  const updatedSeen = scraped.posts.map((p) => p.id);
  const nextState = {
    seenPostIds: updatedSeen,
    latestPostId: currentLatestId,
    latestPostUrl: currentLatestPost?.url ?? null,
    lastPollAt: scraped.polledAt,
    lastSeenAt: newest.length > 0 ? new Date().toISOString() : state.lastSeenAt,
    lastTitle: scraped.title,
  };
  writeState(stateFile, nextState);

  writePostsSnapshot(postsFile, {
    profileUrl: scraped.href || profileUrl,
    profileUser: username,
    polledAt: scraped.polledAt,
    latestPostId: currentLatestId,
    latestPostUrl: currentLatestPost?.url ?? null,
    pageTitle: scraped.title,
    bodyPreview: scraped.bodyPreview,
    challengeDetected,
    count: scraped.posts.length,
    posts: scraped.posts,
    newSinceLastSeen: newest,
  });

  if (challengeDetected) {
    throw new Error('TRUTHSOCIAL_CHALLENGE_PAGE: blocked by anti-bot challenge page (title/body indicates Cloudflare interstitial)');
  }

  if (newest.length === 0) {
    process.stdout.write('HEARTBEAT_OK\n');
    return;
  }

  process.stdout.write(formatAlert(newest, scraped.href || profileUrl) + '\n');
}

main().catch((error) => {
  process.stderr.write(`TRUTHSOCIAL_WATCH_ERROR: ${String(error?.message ?? error)}\n`);
  process.exitCode = 1;
});
