#!/usr/bin/env node
import { spawn, spawnSync } from 'node:child_process';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join, normalize, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const docsDir = fileURLToPath(new URL('..', import.meta.url));
const distDir = join(docsDir, 'dist');

const CHROME_CANDIDATES = [
  process.env.CHROME_BIN,
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
  '/usr/bin/chromium-browser',
].filter(Boolean);

function findChrome() {
  for (const candidate of CHROME_CANDIDATES) {
    const probe = spawnSync(candidate, ['--version'], { stdio: 'ignore' });
    if (!probe.error && probe.status === 0) return candidate;
  }
  return null;
}

const CHECKS = {
  'no-overflow': `
    const root = doc.scrollingElement;
    if (root.scrollWidth > root.clientWidth + 1) {
      fail('no-overflow', 'scrollWidth=' + root.scrollWidth + ' clientWidth=' + root.clientWidth);
    }`,
  'cards-aligned': `
    const cards = [...doc.querySelectorAll('.ax-workload-grid--paths article')];
    const tops = cards.map((card) => Math.round(card.getBoundingClientRect().top));
    if (cards.length > 1 && Math.max(...tops) - Math.min(...tops) > 1) {
      fail('cards-aligned', 'card tops: ' + tops.join(','));
    }`,
  'code-gutter': `
    const panel = doc.querySelector('.ax-code__panel:not([hidden])');
    if (panel) {
      const gutter = panel.querySelectorAll('.ax-code-gutter__line');
      const lines = panel.querySelectorAll('.ax-code-line');
      if (gutter.length !== lines.length) {
        fail('code-gutter', 'gutter=' + gutter.length + ' lines=' + lines.length);
      } else if (gutter.length > 0) {
        const drift = Math.abs(
          gutter[0].getBoundingClientRect().top - lines[0].getBoundingClientRect().top);
        if (drift > 1) fail('code-gutter', 'first-line drift=' + drift);
      }
    }`,
  'sidebar-groups': `
    const nav = doc.querySelector('nav');
    const text = nav ? nav.textContent : '';
    for (const label of ['Tools', 'Workloads', 'SDK Guides', 'Data and Config', 'Identity and Access']) {
      if (!text.includes(label)) fail('sidebar-groups', 'missing group: ' + label);
    }`,
};

const CASES = [
  { path: '/', width: 390, checks: ['no-overflow'] },
  { path: '/', width: 1500, checks: ['no-overflow', 'cards-aligned', 'code-gutter'] },
  { path: '/', width: 2560, checks: ['no-overflow', 'cards-aligned', 'code-gutter'] },
  { path: '/guides/run/', width: 390, checks: ['no-overflow'] },
  { path: '/guides/run/', width: 2560, checks: ['no-overflow', 'sidebar-groups'] },
  { path: '/zh-cn/guides/run/', width: 2560, checks: ['no-overflow'] },
];

function wrapperPage(caseDef) {
  const checks = caseDef.checks.map((name) => CHECKS[name]).join('\n');
  return `<!doctype html>
<html><body>
<iframe id="f" src="${caseDef.path}" style="width:${caseDef.width}px;height:900px;border:0"></iframe>
<pre id="out">pending</pre>
<script>
const failures = [];
const fail = (check, detail) => failures.push(check + ': ' + detail);
const frame = document.getElementById('f');
frame.addEventListener('load', () => {
  setTimeout(() => {
    const doc = frame.contentDocument;
    ${checks}
    document.getElementById('out').textContent = JSON.stringify({ failures });
  }, 1000);
});
</script>
</body></html>`;
}

const MIME = { '.html': 'text/html', '.css': 'text/css', '.js': 'text/javascript', '.mjs': 'text/javascript', '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png', '.gif': 'image/gif', '.webp': 'image/webp', '.ico': 'image/x-icon', '.woff2': 'font/woff2', '.txt': 'text/plain', '.xml': 'application/xml' };

function runChrome(chrome, args, timeoutMs) {
  return new Promise((resolve) => {
    const child = spawn(chrome, args, { stdio: ['ignore', 'pipe', 'pipe'] });
    let stdout = '';
    let stderr = '';
    const timer = setTimeout(() => {
      child.kill('SIGKILL');
      resolve({ error: new Error('timeout'), stdout, stderr });
    }, timeoutMs);
    child.stdout.on('data', (chunk) => { stdout += chunk; });
    child.stderr.on('data', (chunk) => { stderr += chunk; });
    child.on('error', (error) => {
      clearTimeout(timer);
      resolve({ error, stdout, stderr });
    });
    child.on('close', () => {
      clearTimeout(timer);
      resolve({ error: null, stdout, stderr });
    });
  });
}

async function main() {
  try {
    await readFile(join(distDir, 'index.html'));
  } catch {
    console.error('layout_check_error: missing dist/index.html; run make docs-build first');
    process.exit(2);
  }

  const chrome = findChrome();
  if (!chrome) {
    console.error('layout_check_skipped: no Chrome/Chromium found (set CHROME_BIN)');
    process.exit(2);
  }

  const server = createServer(async (req, res) => {
    const url = new URL(req.url, 'http://localhost');
    if (url.pathname === '/__layout_check__') {
      const caseDef = CASES[Number(url.searchParams.get('case'))];
      if (!caseDef) {
        res.writeHead(400);
        res.end('unknown case');
        return;
      }
      res.writeHead(200, { 'content-type': 'text/html' });
      res.end(wrapperPage(caseDef));
      return;
    }
    const filePath = normalize(
      join(distDir, decodeURIComponent(url.pathname).replace(/\/$/, '/index.html')),
    );
    if (!filePath.startsWith(distDir + sep)) {
      res.writeHead(403);
      res.end('forbidden');
      return;
    }
    try {
      const body = await readFile(filePath);
      res.writeHead(200, { 'content-type': MIME[extname(filePath)] ?? 'application/octet-stream' });
      res.end(body);
    } catch {
      res.writeHead(404);
      res.end('not found');
    }
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const port = server.address().port;

  let failed = 0;
  try {
    for (const [index, caseDef] of CASES.entries()) {
      const target = `http://127.0.0.1:${port}/__layout_check__?case=${index}`;
      const result = await runChrome(
        chrome,
        ['--headless', '--disable-gpu', '--virtual-time-budget=10000',
          `--window-size=${caseDef.width + 110},1000`, '--dump-dom', target],
        60000,
      );
      const pre = /<pre id="out">([\s\S]*?)<\/pre>/.exec(result.stdout ?? '');
      if (result.error || !pre) {
        if (process.env.LAYOUT_CHECK_DEBUG) {
          console.error('debug:', {
            error: String(result.error), status: result.status, signal: result.signal,
            stdout: (result.stdout ?? '').length, stderr: (result.stderr ?? '').slice(0, 200),
          });
        }
        console.error(`FAIL ${caseDef.path} @${caseDef.width}: chrome did not return results`);
        failed += 1;
        continue;
      }
      let parsed;
      try {
        parsed = JSON.parse(pre[1].replace(/&quot;/g, '"').replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>'));
      } catch {
        console.error(`FAIL ${caseDef.path} @${caseDef.width}: unparseable result ${pre[1].slice(0, 120)}`);
        failed += 1;
        continue;
      }
      if (parsed.failures.length > 0) {
        for (const failure of parsed.failures) {
          console.error(`FAIL ${caseDef.path} @${caseDef.width}: ${failure}`);
        }
        failed += parsed.failures.length;
      } else {
        console.log(`ok ${caseDef.path} @${caseDef.width} (${caseDef.checks.join(', ')})`);
      }
    }
  } finally {
    server.close();
  }

  if (failed > 0) {
    console.error(`layout_check_failed failures=${failed}`);
    process.exit(1);
  }
  console.log(`layout_check_ok=true cases=${CASES.length}`);
}

main();
