#!/usr/bin/env node

import fs from 'node:fs';
import path from 'node:path';

function usage() {
  console.log(`briefing-publisher

Usage:
  node skills/briefing-publisher/scripts/publish_briefing.mjs --input-file <path> [options]

Required:
  --input-file <path>          Markdown file to publish

Options:
  --series <path>              Series path under docs/reports (default: market)
  --repo <owner/name>          GitHub repo (default: mfittko/ai-briefings)
  --branch <name>              Branch (default: main)
  --site-base-url <url>        Pages base URL (default: https://mfittko.github.io/ai-briefings)
  --token-env <ENV_NAME>       Token env var name (default: GH_TOKEN)
  --timestamp <ISO_8601>       Override timestamp used for archive path
  --dry-run                    Compute and print result without writing to GitHub
  -h, --help                   Show this help
`);
}

function parseArgs(argv) {
  const args = {
    inputFile: '',
    series: 'market',
    repo: 'mfittko/ai-briefings',
    branch: 'main',
    siteBaseUrl: 'https://mfittko.github.io/ai-briefings',
    tokenEnv: 'GH_TOKEN',
    timestamp: '',
    dryRun: false,
  };

  for (let index = 0; index < argv.length; index++) {
    const token = argv[index];
    if (token === '-h' || token === '--help') {
      usage();
      process.exit(0);
    }
    if (token === '--dry-run') {
      args.dryRun = true;
      continue;
    }
    if (!token.startsWith('--')) continue;

    const eq = token.indexOf('=');
    let key = '';
    let value = '';

    if (eq > -1) {
      key = token.slice(2, eq);
      value = token.slice(eq + 1);
    } else {
      key = token.slice(2);
      const next = argv[index + 1];
      if (!next || next.startsWith('--')) {
        throw new Error(`Missing value for --${key}`);
      }
      value = next;
      index++;
    }

    if (key === 'input-file') args.inputFile = value;
    else if (key === 'series') args.series = value;
    else if (key === 'repo') args.repo = value;
    else if (key === 'branch') args.branch = value;
    else if (key === 'site-base-url') args.siteBaseUrl = value;
    else if (key === 'token-env') args.tokenEnv = value;
    else if (key === 'timestamp') args.timestamp = value;
    else throw new Error(`Unknown option --${key}`);
  }

  if (!args.inputFile) {
    throw new Error('--input-file is required');
  }

  return args;
}

function sanitizeSeries(series) {
  const normalized = String(series || '')
    .replace(/\\/g, '/')
    .replace(/^\/+/, '')
    .replace(/\/+$/, '');

  if (!normalized) {
    throw new Error('series cannot be empty');
  }
  if (normalized.includes('..')) {
    throw new Error('series cannot contain ..');
  }

  return normalized;
}

function toUtcStamp(date) {
  const year = date.getUTCFullYear();
  const month = String(date.getUTCMonth() + 1).padStart(2, '0');
  const day = String(date.getUTCDate()).padStart(2, '0');
  const hour = String(date.getUTCHours()).padStart(2, '0');
  const minute = String(date.getUTCMinutes()).padStart(2, '0');
  const second = String(date.getUTCSeconds()).padStart(2, '0');
  return `${year}-${month}-${day}T${hour}${minute}${second}Z`;
}

function resolveToken(preferredEnvName) {
  return (
    process.env[preferredEnvName] ||
    process.env.BRIEFINGS_GH_TOKEN ||
    process.env.GITHUB_TOKEN ||
    ''
  );
}

function htmlEscape(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function xmlEscape(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;');
}

function cdataEscape(value) {
  return String(value).replace(/\]\]>/g, ']]]]><![CDATA[>');
}

function stampToRfc822(stamp) {
  const match = String(stamp || '').match(/^(\d{4})-(\d{2})-(\d{2})T(\d{2})(\d{2})(\d{2})Z$/);
  if (!match) return new Date().toUTCString();
  const [, year, month, day, hour, minute, second] = match;
  const d = new Date(Date.UTC(Number(year), Number(month) - 1, Number(day), Number(hour), Number(minute), Number(second)));
  return d.toUTCString();
}

function buildRssFeed({ title, description, siteUrl, feedPath, items }) {
  const channelLink = `${siteUrl}/`;
  const atomSelf = `${siteUrl}/${feedPath}`;
  const body = items
    .map((item) => {
      const descriptionNode = item.descriptionHtml
        ? `      <description><![CDATA[${cdataEscape(item.descriptionHtml)}]]></description>`
        : item.description
          ? `      <description>${xmlEscape(item.description)}</description>`
          : null;

      const contentEncodedNode = item.contentHtml
        ? `      <content:encoded><![CDATA[${cdataEscape(item.contentHtml)}]]></content:encoded>`
        : null;

      return [
        '    <item>',
        `      <title>${xmlEscape(item.title)}</title>`,
        `      <link>${xmlEscape(item.link)}</link>`,
        `      <guid isPermaLink="true">${xmlEscape(item.guid || item.link)}</guid>`,
        `      <pubDate>${xmlEscape(item.pubDate)}</pubDate>`,
        descriptionNode,
        contentEncodedNode,
        '    </item>',
      ].filter(Boolean).join('\n');
    })
    .join('\n');

  return [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:content="http://purl.org/rss/1.0/modules/content/">',
    '  <channel>',
    `    <title>${xmlEscape(title)}</title>`,
    `    <description>${xmlEscape(description)}</description>`,
    `    <link>${xmlEscape(channelLink)}</link>`,
    '    <language>en-us</language>',
    '    <generator>briefing-publisher</generator>',
    '    <docs>https://www.rssboard.org/rss-specification</docs>',
    `    <atom:link href="${xmlEscape(atomSelf)}" rel="self" type="application/rss+xml" />`,
    `    <lastBuildDate>${new Date().toUTCString()}</lastBuildDate>`,
    body,
    '  </channel>',
    '</rss>',
    '',
  ].join('\n');
}

async function githubRequest({ token, method, urlPath, body }) {
  const response = await fetch(`https://api.github.com${urlPath}`, {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: 'application/vnd.github+json',
      'X-GitHub-Api-Version': '2022-11-28',
      'Content-Type': 'application/json',
    },
    body: body ? JSON.stringify(body) : undefined,
  });

  if (response.status === 204) {
    return {};
  }

  const text = await response.text();
  let data = {};
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = { message: text };
    }
  }

  if (!response.ok) {
    const msg = data.message || `GitHub API error ${response.status}`;
    const err = new Error(msg);
    err.status = response.status;
    throw err;
  }

  return data;
}

function buildSeriesIndexHtml({ title, entries, homePath, latestViewPath, feedPath }) {
  const items = entries
    .map((entry) => `      <li><a href="${htmlEscape(entry.link)}">${htmlEscape(entry.label)}</a></li>`)
    .join('\n');

  return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>${htmlEscape(title)}</title>
    <style>
      :root { color-scheme: dark; }
      body {
        font-family: -apple-system,BlinkMacSystemFont,Segoe UI,Helvetica,Arial,sans-serif;
        max-width: 860px;
        margin: 2rem auto;
        padding: 0 1rem;
        line-height: 1.5;
        background: #0d1117;
        color: #c9d1d9;
      }
      h1, h2 { color: #f0f6fc; }
      a { color: #58a6ff; }
      ul { padding-left: 1.2rem; }
    </style>
  </head>
  <body>
    <h1>${htmlEscape(title)}</h1>
    <p>Automated briefing archive.</p>
    <p>
      <a href="${htmlEscape(homePath)}">Home</a>
      ·
      <a href="${htmlEscape(latestViewPath)}">Latest report</a>
      ·
      <a href="${htmlEscape(feedPath)}">RSS feed</a>
    </p>
    <h2>Entries</h2>
    <ul>
${items}
    </ul>
  </body>
</html>
`;
}

function buildRootHomeHtml({ series, latestViewerUrl, latestMdUrl, seriesFeedUrl, seriesIndexUrl, entries }) {
  const seriesTitle = titleCaseSeries(series);
  const listItems = entries
    .slice(0, 5)
    .map((entry) => `        <li class="inline-links">${htmlEscape(entry.label)} (<a href="${htmlEscape(entry.viewerUrl)}">html</a>|<a href="${htmlEscape(entry.mdUrl)}">md</a>)</li>`)
    .join('\n');

  return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>AI Briefings</title>
    <style>
      :root { color-scheme: dark; }
      body {
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
        margin: 0;
        background: #0d1117;
        color: #c9d1d9;
      }
      .shell {
        margin: 2rem auto;
        max-width: 860px;
        line-height: 1.5;
        padding: 0 1rem;
      }
      h1, h2 { margin-bottom: 0.4rem; color: #f0f6fc; }
      h3, h4 { margin-bottom: 0.3rem; color: #f0f6fc; }
      ul { margin-top: 0.4rem; }
      a { color: #58a6ff; }
      code { background: #161b22; padding: 0.1rem 0.35rem; border-radius: 4px; color: #c9d1d9; }
      .inline-links a { margin: 0 0.2rem; }
      .muted { color: #8b949e; font-size: 0.95rem; }
    </style>
  </head>
  <body>
    <div class="shell">
      <h1>AI Briefings</h1>
      <p>Published briefings for market and geopolitics workflows.</p>

      <h2>Reports</h2>
      <h3>${htmlEscape(seriesTitle)}</h3>
      <ul>
        <li class="inline-links">latest (
          <a href="${htmlEscape(latestViewerUrl)}">html</a>|
          <a href="${htmlEscape(latestMdUrl)}">md</a>
        )</li>
        <li>RSS feed: <a href="${htmlEscape(seriesFeedUrl)}">feed.xml</a></li>
      </ul>

      <h4>archive (limit to 5 max)</h4>
      <ul>
${listItems || '        <li class="muted">No archive entries yet.</li>'}
      </ul>
      <p class="muted">Full archive: <a href="${htmlEscape(seriesIndexUrl)}">reports/${htmlEscape(series)}/index.html</a></p>
    </div>
  </body>
</html>
`;
}

function titleCaseSeries(series) {
  return String(series || '')
    .split('/')
    .filter(Boolean)
    .map((part) => part.replace(/[-_]+/g, ' '))
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' / ');
}

function buildRootSeriesSection({ series, latestViewerUrl, latestMdUrl, seriesIndexUrl, seriesFeedUrl, entries }) {
  const sectionTitle = titleCaseSeries(series);
  const lines = [];
  lines.push(`### ${sectionTitle}`);
  lines.push('');
  lines.push(`- latest ( [html](${latestViewerUrl})| [md](${latestMdUrl}) )`);
  lines.push(`- RSS feed: [feed.xml](${seriesFeedUrl})`);
  lines.push('');
  lines.push('#### archive (limit to 5 max)');
  lines.push('');

  for (const entry of entries.slice(0, 5)) {
    lines.push(`- ${entry.label} ([html](${entry.viewerUrl})|[md](${entry.mdUrl}))`);
  }

  lines.push('');
  lines.push(`Full archive: [reports/${series}/index.html](${seriesIndexUrl})`);
  lines.push('');
  return lines.join('\n');
}

function upsertRootIndexSeriesSection(oldContent, { series, section }) {
  const sectionTitle = titleCaseSeries(series);
  const normalized = String(oldContent || '').trim();

  if (!normalized) {
    return [
      '# AI Briefings',
      '',
      'Published briefings for market and geopolitics workflows.',
      '',
      '## Reports',
      '',
      section,
    ].join('\n');
  }

  const escapedTitle = sectionTitle.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const sectionPattern = new RegExp(
    `(^### ${escapedTitle}\\n[\\s\\S]*?)(?=\\n### |\\n## Naming convention|$)`,
    'm'
  );
  if (sectionPattern.test(oldContent)) {
    return oldContent.replace(sectionPattern, `${section}\n`);
  }

  const reportsHeader = /^## Reports\s*$/m;
  if (reportsHeader.test(oldContent)) {
    return oldContent.replace(reportsHeader, `## Reports\n\n${section}`);
  }

  return `${oldContent.replace(/\s*$/, '')}\n\n## Reports\n\n${section}`;
}

function branchRefPath(branch) {
  return String(branch || '')
    .split('/')
    .map((part) => encodeURIComponent(part))
    .join('/');
}

async function getBranchHeadSha({ token, owner, repo, branch }) {
  const data = await githubRequest({
    token,
    method: 'GET',
    urlPath: `/repos/${owner}/${repo}/git/ref/heads/${branchRefPath(branch)}`,
  });
  const sha = data?.object?.sha;
  if (!sha) throw new Error(`Unable to resolve branch head SHA for ${branch}`);
  return sha;
}

async function getCommit({ token, owner, repo, sha }) {
  return githubRequest({
    token,
    method: 'GET',
    urlPath: `/repos/${owner}/${repo}/git/commits/${encodeURIComponent(sha)}`,
  });
}

async function createBlob({ token, owner, repo, content }) {
  return githubRequest({
    token,
    method: 'POST',
    urlPath: `/repos/${owner}/${repo}/git/blobs`,
    body: {
      content,
      encoding: 'utf-8',
    },
  });
}

async function createTree({ token, owner, repo, baseTreeSha, tree }) {
  return githubRequest({
    token,
    method: 'POST',
    urlPath: `/repos/${owner}/${repo}/git/trees`,
    body: {
      base_tree: baseTreeSha,
      tree,
    },
  });
}

async function createCommit({ token, owner, repo, message, treeSha, parentSha }) {
  return githubRequest({
    token,
    method: 'POST',
    urlPath: `/repos/${owner}/${repo}/git/commits`,
    body: {
      message,
      tree: treeSha,
      parents: [parentSha],
    },
  });
}

async function updateBranchHead({ token, owner, repo, branch, sha }) {
  return githubRequest({
    token,
    method: 'PATCH',
    urlPath: `/repos/${owner}/${repo}/git/refs/heads/${branchRefPath(branch)}`,
    body: {
      sha,
      force: false,
    },
  });
}

async function commitFilesBatch({ token, owner, repo, branch, message, files }) {
  const headSha = await getBranchHeadSha({ token, owner, repo, branch });
  const headCommit = await getCommit({ token, owner, repo, sha: headSha });
  const baseTreeSha = headCommit?.tree?.sha;
  if (!baseTreeSha) throw new Error('Unable to resolve base tree SHA');

  const treeEntries = [];
  for (const file of files) {
    const blob = await createBlob({
      token,
      owner,
      repo,
      content: file.content,
    });
    treeEntries.push({
      path: file.path,
      mode: '100644',
      type: 'blob',
      sha: blob.sha,
    });
  }

  const tree = await createTree({
    token,
    owner,
    repo,
    baseTreeSha,
    tree: treeEntries,
  });

  const commit = await createCommit({
    token,
    owner,
    repo,
    message,
    treeSha: tree.sha,
    parentSha: headSha,
  });

  await updateBranchHead({
    token,
    owner,
    repo,
    branch,
    sha: commit.sha,
  });

  return commit.sha;
}

function buildIndex({ series, entries, feedPath }) {
  const lines = [];
  lines.push(`# ${series}`);
  lines.push('');
  lines.push('Automated briefing archive.');
  if (feedPath) {
    lines.push('');
    lines.push(`RSS feed: [feed.xml](${feedPath})`);
  }
  lines.push('');
  lines.push('## Entries');
  lines.push('');
  for (const entry of entries) {
    lines.push(`- [${entry.label}](${entry.link})`);
  }
  lines.push('');
  return lines.join('\n');
}

async function getTextFile({ token, owner, repo, branch, filePath }) {
  try {
    const data = await githubRequest({
      token,
      method: 'GET',
      urlPath: `/repos/${owner}/${repo}/contents/${encodeURIComponent(filePath).replace(/%2F/g, '/')}` + `?ref=${encodeURIComponent(branch)}`,
    });
    if (!data.content) return '';
    return Buffer.from(data.content, 'base64').toString('utf8');
  } catch (error) {
    if (error.status === 404) return '';
    throw error;
  }
}

function updateIndexContent(oldContent, archiveRelativePath, stamp) {
  const entry = `- [${stamp}](${archiveRelativePath})`;
  const feedPath = 'feed.xml';

  if (!oldContent.trim()) {
    return buildIndex({
      series: 'Briefings',
      entries: [{ label: stamp, link: archiveRelativePath }],
      feedPath,
    });
  }

  const lines = oldContent.split('\n');
  const existing = new Set(lines.filter((line) => line.startsWith('- [')));
  if (existing.has(entry)) return oldContent;

  const out = [];
  let inserted = false;
  for (const line of lines) {
    out.push(line);
    if (!inserted && line.trim() === '## Entries') {
      out.push('');
      out.push(entry);
      inserted = true;
    }
  }

  if (!inserted) {
    out.push('');
    out.push('## Entries');
    out.push('');
    out.push(entry);
  }

  return out.join('\n').replace(/\n{3,}/g, '\n\n');
}

function parseIndexEntries(content) {
  const entries = [];
  for (const line of String(content || '').split('\n')) {
    const m = line.match(/^- \[(.+?)\]\((.+?)\)$/);
    if (m) entries.push({ label: m[1], link: m[2] });
  }
  return entries;
}

function buildRssItemHtml({ series, label, viewerUrl, mdUrl }) {
  return [
    `<p><strong>${htmlEscape(series)} briefing</strong> — ${htmlEscape(label)}</p>`,
    `<p><a href="${htmlEscape(viewerUrl)}">Read in HTML viewer</a> · <a href="${htmlEscape(mdUrl)}">Raw Markdown</a></p>`,
  ].join('');
}

function derivePaths({ series, stamp }) {
  const year = stamp.slice(0, 4);
  const month = stamp.slice(5, 7);
  const archiveFile = `${stamp}.md`;

  const baseDir = `docs/reports/${series}`;
  const archivePath = `${baseDir}/${year}/${month}/${archiveFile}`;
  const latestPath = `${baseDir}/latest.md`;
  const indexPath = `${baseDir}/index.md`;
  const indexHtmlPath = `${baseDir}/index.html`;
  const seriesFeedPath = `${baseDir}/feed.xml`;
  const rootFeedPath = 'docs/feed.xml';
  const rootIndexPath = 'docs/index.md';
  const rootIndexHtmlPath = 'docs/index.html';

  const archiveRelativePath = `${year}/${month}/${stamp}.md`;

  return {
    archivePath,
    latestPath,
    indexPath,
    indexHtmlPath,
    seriesFeedPath,
    rootFeedPath,
    rootIndexPath,
    rootIndexHtmlPath,
    archiveRelativePath,
  };
}

async function main() {
  try {
    const args = parseArgs(process.argv.slice(2));
    const series = sanitizeSeries(args.series);

    const inputContent = fs.readFileSync(path.resolve(args.inputFile), 'utf8');
    if (!inputContent.trim()) {
      throw new Error('Input file is empty');
    }

    const date = args.timestamp ? new Date(args.timestamp) : new Date();
    if (Number.isNaN(date.getTime())) {
      throw new Error('Invalid --timestamp value');
    }
    const stamp = toUtcStamp(date);

    const [owner, repo] = args.repo.split('/');
    if (!owner || !repo) {
      throw new Error('Invalid --repo, expected owner/name');
    }

    const paths = derivePaths({ series, stamp });

    const base = args.siteBaseUrl.replace(/\/$/, '');
    const sitePath = new URL(base).pathname.replace(/\/$/, '');
    const archiveMdPath = `${sitePath}/reports/${series}/${paths.archiveRelativePath}`;
    const latestMdPath = `${sitePath}/reports/${series}/latest.md`;
    const archiveUrl = `${base}/viewer.html?src=${encodeURIComponent(archiveMdPath)}`;
    const latestUrl = `${base}/viewer.html?src=${encodeURIComponent(latestMdPath)}`;
    const seriesFeedUrl = `${base}/reports/${series}/feed.xml`;
    const rootFeedUrl = `${base}/feed.xml`;

    if (args.dryRun) {
      console.log(
        JSON.stringify(
          {
            dryRun: true,
            repo: args.repo,
            branch: args.branch,
            series,
            stamp,
            archivePath: paths.archivePath,
            latestPath: paths.latestPath,
            indexPath: paths.indexPath,
            seriesFeedPath: paths.seriesFeedPath,
            rootFeedPath: paths.rootFeedPath,
            rootIndexPath: paths.rootIndexPath,
            rootIndexHtmlPath: paths.rootIndexHtmlPath,
            archiveUrl,
            latestUrl,
            seriesFeedUrl,
            rootFeedUrl,
          },
          null,
          2
        )
      );
      return;
    }

    const token = resolveToken(args.tokenEnv);
    if (!token) {
      throw new Error(`Missing token env. Set ${args.tokenEnv} (or BRIEFINGS_GH_TOKEN / GITHUB_TOKEN)`);
    }

    const oldIndex = await getTextFile({
      token,
      owner,
      repo,
      branch: args.branch,
      filePath: paths.indexPath,
    });

    const oldRootIndex = await getTextFile({
      token,
      owner,
      repo,
      branch: args.branch,
      filePath: paths.rootIndexPath,
    });

    const newIndex = updateIndexContent(oldIndex, paths.archiveRelativePath, stamp);

    const entries = parseIndexEntries(newIndex).map((entry) => {
      const mdPath = `${sitePath}/reports/${series}/${entry.link}`;
      return {
        label: entry.label,
        link: `${base}/viewer.html?src=${encodeURIComponent(mdPath)}`,
        mdUrl: `${base}/reports/${series}/${entry.link}`,
      };
    });
    const indexHtml = buildSeriesIndexHtml({
      title: `${series} archive`,
      entries,
      homePath: `${base}/index.html`,
      latestViewPath: latestUrl,
      feedPath: `${base}/reports/${series}/feed.xml`,
    });

    const rssItems = parseIndexEntries(newIndex).slice(0, 100).map((entry) => {
      const mdPath = `${sitePath}/reports/${series}/${entry.link}`;
      const mdLink = `${base}/reports/${series}/${entry.link}`;
      const viewerLink = `${base}/viewer.html?src=${encodeURIComponent(mdPath)}`;
      const itemHtml = buildRssItemHtml({
        series,
        label: entry.label,
        viewerUrl: viewerLink,
        mdUrl: mdLink,
      });
      return {
        title: `${series} ${entry.label}`,
        link: viewerLink,
        guid: viewerLink,
        pubDate: stampToRfc822(entry.label),
        descriptionHtml: itemHtml,
        contentHtml: itemHtml,
      };
    });

    const seriesFeed = buildRssFeed({
      title: `${series} briefings`,
      description: `Automated ${series} briefing archive feed`,
      siteUrl: base,
      feedPath: `reports/${series}/feed.xml`,
      items: rssItems,
    });

    const rootFeed = buildRssFeed({
      title: `${series} briefings`,
      description: `Automated ${series} briefing archive feed`,
      siteUrl: base,
      feedPath: 'feed.xml',
      items: rssItems,
    });

    const rootSectionEntries = entries.map((entry) => ({
      label: entry.label,
      viewerUrl: entry.link,
      mdUrl: entry.mdUrl,
    }));
    const rootSeriesSection = buildRootSeriesSection({
      series,
      latestViewerUrl: latestUrl,
      latestMdUrl: `${base}/reports/${series}/latest.md`,
      seriesIndexUrl: `${base}/reports/${series}/index.html`,
      seriesFeedUrl,
      entries: rootSectionEntries,
    });
    const newRootIndex = upsertRootIndexSeriesSection(oldRootIndex, {
      series,
      section: rootSeriesSection,
    });
    const newRootIndexHtml = buildRootHomeHtml({
      series,
      latestViewerUrl: latestUrl,
      latestMdUrl: `${base}/reports/${series}/latest.md`,
      seriesFeedUrl,
      seriesIndexUrl: `${base}/reports/${series}/index.html`,
      entries: rootSectionEntries,
    });

    const commitSha = await commitFilesBatch({
      token,
      owner,
      repo,
      branch: args.branch,
      message: `publish: ${series} ${stamp}`,
      files: [
        { path: paths.archivePath, content: inputContent },
        { path: paths.latestPath, content: inputContent },
        { path: paths.indexPath, content: newIndex },
        { path: paths.indexHtmlPath, content: indexHtml },
        { path: paths.seriesFeedPath, content: seriesFeed },
        { path: paths.rootFeedPath, content: rootFeed },
        { path: paths.rootIndexPath, content: newRootIndex },
        { path: paths.rootIndexHtmlPath, content: newRootIndexHtml },
      ],
    });

    console.log(
      JSON.stringify(
        {
          ok: true,
          repo: args.repo,
          branch: args.branch,
          series,
          stamp,
          archivePath: paths.archivePath,
          latestPath: paths.latestPath,
          indexPath: paths.indexPath,
          seriesFeedPath: paths.seriesFeedPath,
          rootFeedPath: paths.rootFeedPath,
          rootIndexPath: paths.rootIndexPath,
          rootIndexHtmlPath: paths.rootIndexHtmlPath,
          commitSha,
          archiveUrl,
          latestUrl,
          seriesFeedUrl,
          rootFeedUrl,
        },
        null,
        2
      )
    );
  } catch (error) {
    console.error(`briefing-publisher error: ${error.message}`);
    process.exit(1);
  }
}

main();
