#!/usr/bin/env node
/**
 * FXEmpire enrichment orchestrator.
 *
 * Responsibility:
 * - Compose outputs from focused concerns:
 *   1) fxempire_rates.mjs
 *   2) fxempire_articles.mjs
 */

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const execFileAsync = promisify(execFile);

function parseArgs(argv) {
  const out = {
    locale: 'en',
    tz: 'Europe/Berlin',
    hours: null,
    commodities: ['brent-crude-oil', 'natural-gas', 'gold', 'silver'],
    focus: 'brent-crude-oil',
    maxItems: 6,
    pageSize: 50,
    maxPages: 10,
    tags: null,
    json: false,
    outputFile: null,
    fullText: true,
    maxTextChars: 12000,
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
    else if (key === 'tz' && val) out.tz = val;
    else if (key === 'hours' && val) out.hours = Number(val);
    else if (key === 'commodities' && val)
      out.commodities = val.split(',').map((s) => s.trim()).filter(Boolean);
    else if (key === 'focus' && val) out.focus = val;
    else if (key === 'max-items' && val) out.maxItems = Number(val);
    else if (key === 'page-size' && val) out.pageSize = Number(val);
    else if (key === 'max-pages' && val) out.maxPages = Number(val);
    else if (key === 'tags' && val) out.tags = val;
    else if (key === 'json') out.json = true;
    else if (key === 'output-file' && val) out.outputFile = val;
    else if (key === 'full-text') out.fullText = true;
    else if (key === 'no-full-text') out.fullText = false;
    else if (key === 'max-text-chars' && val) out.maxTextChars = Number(val);
  }

  if (!Number.isFinite(out.maxTextChars) || out.maxTextChars <= 0) out.maxTextChars = 12000;
  return out;
}

function mdEscape(s) {
  return String(s).replace(/\|/g, '\\|');
}

function markdownLinkText(s) {
  return String(s)
    .replace(/\[/g, '\\[')
    .replace(/\]/g, '\\]');
}

function markdownLinkUrl(url) {
  return encodeURI(String(url || ''))
    .replace(/\(/g, '%28')
    .replace(/\)/g, '%29');
}

function resolveArticleUrl(article) {
  const articleUrl = String(article?.articleUrl || '').trim();
  if (articleUrl) {
    if (/^https?:\/\//i.test(articleUrl)) return articleUrl;
    return `https://www.fxempire.com${articleUrl}`;
  }

  const fullUrl = String(article?.fullUrl || '').trim();
  if (fullUrl) return fullUrl;

  const slug = String(article?.slug || '').trim();
  const id = article?.id;
  const type = article?.type === 'news' ? 'news' : article?.type === 'forecasts' ? 'forecasts' : null;
  if (slug && id && type) {
    return `https://www.fxempire.com/${type}/article/${slug}-${id}`;
  }
  return null;
}

function formatArticleMarkdownLink(article) {
  const title = markdownLinkText(article?.title || 'Untitled');
  const url = resolveArticleUrl(article);
  if (!url) return { label: mdEscape(title), hasLink: false };
  return { label: `[${title}](${markdownLinkUrl(url)})`, hasLink: true };
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

function normalizeTakeaway(text) {
  const snippetRaw = text ? String(text).replace(/\s+/g, ' ').trim() : '';
  if (!snippetRaw) return '';

  const maxChars = 700;
  if (snippetRaw.length <= maxChars) return snippetRaw;

  const window = snippetRaw.slice(0, maxChars);
  const boundaryRegex = /(?<!\b[A-Z])[.!?](?=\s+[A-Z]|$)/g;
  let match = null;
  let lastGoodBoundary = -1;
  while ((match = boundaryRegex.exec(window)) !== null) {
    if (match.index >= 280) lastGoodBoundary = match.index;
  }

  if (lastGoodBoundary !== -1) {
    return `${window.slice(0, lastGoodBoundary + 1).trim()}…`;
  }

  const lastSpace = window.lastIndexOf(' ');
  const cut = lastSpace > 200 ? lastSpace : maxChars;
  return `${window.slice(0, cut).trim()}…`;
}

function toNum(v) {
  const n = Number(v);
  return Number.isFinite(n) ? n : null;
}

function scoreKeywords(text, words) {
  const t = String(text || '').toLowerCase();
  let score = 0;
  for (const word of words) {
    score += t.split(word).length - 1;
  }
  return score;
}

function outlookLabel({ pct, bull, bear }) {
  if (pct !== null && pct >= 1 && bull >= bear) return 'Bullish (momentum + narrative aligned)';
  if (pct !== null && pct <= -1 && bear >= bull) return 'Bearish (momentum + narrative aligned)';
  if (bull - bear >= 3) return 'Bullish bias (narrative-led)';
  if (bear - bull >= 3) return 'Bearish bias (narrative-led)';
  if (pct !== null && pct > 0.4) return 'Mild bullish bias (price-led)';
  if (pct !== null && pct < -0.4) return 'Mild bearish bias (price-led)';
  return 'Neutral / mixed';
}

function confidenceLevel({ articleCount, pct, bull, bear }) {
  let score = 1;
  score += Math.min(3, articleCount);
  score += Math.min(3, Math.abs(pct || 0));
  score += Math.min(3, Math.abs(bull - bear));
  if (score >= 7) return 'High';
  if (score >= 4) return 'Medium';
  return 'Low';
}

function buildCommodityAnalysis(slug, payload) {
  const price = payload.prices.find((p) => p.slug === slug) || {};
  const items = payload.articles
    .filter((a) => a.commodity === slug)
    .sort((a, b) => (b.timestamp || 0) - (a.timestamp || 0));

  const body = items
    .slice(0, 3)
    .map((a) => a.textFull || a.textSnippet || a.description || '')
    .join(' ');

  const bull = scoreKeywords(body, ['rise', 'rally', 'gain', 'upside', 'support', 'higher', 'boost', 'bull']);
  const bear = scoreKeywords(body, ['fall', 'drop', 'dive', 'fade', 'downside', 'pressure', 'lower', 'selloff', 'bear']);
  const geo = scoreKeywords(body, ['iran', 'hormuz', 'middle east', 'strike', 'military', 'tension', 'war']);

  const pct = toNum(price.pct);
  const change = toNum(price.change);

  return {
    slug,
    name: price.name || slug,
    last: toNum(price.last),
    change,
    pct,
    lastUpdate: price.lastUpdate || null,
    articleCount: items.length,
    bullScore: bull,
    bearScore: bear,
    geoScore: geo,
    outlook: outlookLabel({ pct, bull, bear }),
    confidence: confidenceLevel({ articleCount: items.length, pct, bull, bear }),
    topArticles: items.slice(0, 3).map((a) => ({
      id: a.id || null,
      title: a.title,
      slug: a.slug || null,
      type: a.type || null,
      iso: a.iso,
      author: a.author || null,
      articleUrl: a.articleUrl || null,
      fullUrl: resolveArticleUrl(a),
      takeaway: normalizeTakeaway(a.textSnippet || a.description || a.excerpt || ''),
    })),
  };
}

function buildDetailedMarkdown(payload, analyses) {
  const lines = [];
  const now = payload.meta.now || new Date().toISOString();
  const hours = payload.meta.hours || 'N/A';
  const tz = payload.meta.tz || 'UTC';

  lines.push('# Commodity Market Analysis (FXEmpire)');
  lines.push('');
  lines.push(`- Generated: ${now}`);
  lines.push(`- Window: last ${hours}h (${tz})`);
  lines.push(`- Locale: ${payload.meta.locale}`);
  lines.push('');

  lines.push('## Market Snapshot');
  lines.push('');
  lines.push('| Commodity | Last | Change | % | Outlook | Confidence |');
  lines.push('|---|---:|---:|---:|---|---|');
  for (const a of analyses) {
    lines.push(
      `| ${mdEscape(a.name)} | ${fmtNum(a.last) || 'n/a'} | ${fmtNum(a.change) || 'n/a'} | ${fmtPct(a.pct) || 'n/a'} | ${mdEscape(a.outlook)} | ${a.confidence} |`
    );
  }

  for (const a of analyses) {
    lines.push('');
    lines.push(`## ${mdEscape(a.name)} (${a.slug})`);
    lines.push('');
    lines.push(`- Price action: ${fmtNum(a.last) || 'n/a'} (${fmtNum(a.change) || 'n/a'}, ${fmtPct(a.pct) || 'n/a'})`);
    lines.push(`- Narrative signals: bull=${a.bullScore}, bear=${a.bearScore}, geopolitics=${a.geoScore}`);
    lines.push(`- Outlook: **${mdEscape(a.outlook)}**`);
    lines.push(`- Confidence: **${a.confidence}**`);

    if (a.topArticles.length) {
      lines.push('');
      lines.push('### Supporting Articles');
      for (const item of a.topArticles) {
        const when = item.iso ? item.iso.replace('T', ' ').replace('Z', 'Z') : '';
        const link = formatArticleMarkdownLink(item);
        const meta = `${when ? ` (${when}` : ''}${item.author ? `${when ? ', ' : ' ('}${mdEscape(item.author)}` : ''}${when || item.author ? ')' : ''}`;
        lines.push(`- ${link.label}${meta}${link.hasLink ? '' : ' — link unavailable'}`);
        if (item.takeaway) lines.push(`  - ${mdEscape(item.takeaway)}`);
      }
    }
  }

  return lines.join('\n') + '\n';
}

async function runNodeJson(scriptPath, args) {
  const cmdArgs = [scriptPath, ...args, '--json'];
  const { stdout } = await execFileAsync('node', cmdArgs, { maxBuffer: 8 * 1024 * 1024 });
  return JSON.parse(stdout);
}

async function main() {
  const args = parseArgs(process.argv.slice(2));

  const __filename = fileURLToPath(import.meta.url);
  const __dirname = path.dirname(__filename);
  const ratesScript = path.join(__dirname, 'fxempire_rates.mjs');
  const articlesScript = path.join(__dirname, 'fxempire_articles.mjs');

  const baseArgs = ['--locale', args.locale, '--commodities', args.commodities.join(',')];

  const articlesArgs = [...baseArgs, '--tz', args.tz, '--max-items', String(args.maxItems), '--page-size', String(args.pageSize), '--max-pages', String(args.maxPages)];
  if (args.hours && Number.isFinite(args.hours) && args.hours > 0) {
    articlesArgs.push('--hours', String(args.hours));
  }
  if (args.tags) {
    articlesArgs.push('--tags', args.tags);
  }
  if (args.fullText) {
    articlesArgs.push('--full-text', '--max-text-chars', String(args.maxTextChars));
  }

  const [rates, articles] = await Promise.all([
    runNodeJson(ratesScript, baseArgs),
    runNodeJson(articlesScript, articlesArgs),
  ]);

  const payload = {
    meta: {
      now: articles?.meta?.now || rates?.meta?.now || new Date().toISOString(),
      cutoff: articles?.meta?.cutoff || null,
      hours: articles?.meta?.hours || null,
      tz: articles?.meta?.tz || args.tz,
      locale: args.locale,
    },
    ratesUrl: rates?.ratesUrl || null,
    prices: rates?.prices || [],
    pricesError: rates?.pricesError || null,
    articles: articles?.articles || [],
  };

  const order = [...new Set([args.focus, ...args.commodities])].filter(Boolean);
  const analyses = order.map((slug) => buildCommodityAnalysis(slug, payload));
  const reportMarkdown = buildDetailedMarkdown(payload, analyses);

  if (args.outputFile) {
    const target = path.resolve(args.outputFile);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, reportMarkdown, 'utf8');
    payload.reportFile = target;
  }
  payload.analysis = analyses;
  payload.reportMarkdown = reportMarkdown;

  if (args.json) {
    process.stdout.write(JSON.stringify(payload, null, 2));
    return;
  }
  process.stdout.write(reportMarkdown);
}

main().catch((e) => {
  process.stderr.write(`fxempire_enrich error: ${e.message}\n`);
  process.exitCode = 1;
});
