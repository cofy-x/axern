import { access, readFile, readdir } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = fileURLToPath(new URL('../dist/', import.meta.url));

async function exists(target) {
  try {
    await access(target);
    return true;
  } catch {
    return false;
  }
}

async function walk(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const target = path.join(directory, entry.name);
    if (entry.isDirectory()) files.push(...await walk(target));
    else files.push(target);
  }
  return files;
}

function localTarget(urlPath) {
  const decoded = decodeURIComponent(urlPath.split(/[?#]/, 1)[0]);
  const relative = decoded.replace(/^\//, '');
  if (decoded.endsWith('/')) return path.join(dist, relative, 'index.html');
  const exact = path.join(dist, relative);
  return path.extname(exact) ? exact : path.join(exact, 'index.html');
}

for (const required of ['index.html', '404.html', 'zh-cn/index.html', 'sitemap-index.xml']) {
  if (!await exists(path.join(dist, required))) {
    throw new Error(`missing build artifact: ${required}`);
  }
}

const pagefindCandidates = ['pagefind/pagefind.js', '_pagefind/pagefind.js'];
if (!(await Promise.all(pagefindCandidates.map((candidate) => exists(path.join(dist, candidate))))).some(Boolean)) {
  throw new Error('missing built Pagefind search index');
}

const files = await walk(dist);
const htmlFiles = files.filter((file) => file.endsWith('.html'));
const failures = [];
let englishPages = 0;
let chinesePages = 0;

for (const file of htmlFiles) {
  const html = await readFile(file, 'utf8');
  if (/<html[^>]+lang="en"/.test(html)) englishPages += 1;
  if (/<html[^>]+lang="zh-CN"/.test(html)) chinesePages += 1;

  for (const match of html.matchAll(/(?:href|src)="([^"]+)"/g)) {
    const value = match[1];
    if (/^(?:[a-z]+:|\/\/|#)/i.test(value)) continue;
    const resolved = value.startsWith('/')
      ? new URL(value, 'https://axern.cofy-x.space').pathname
      : new URL(value, `https://axern.cofy-x.space/${path.relative(dist, path.dirname(file))}/`).pathname;
    if (!await exists(localTarget(resolved))) {
      failures.push(`${path.relative(dist, file)} -> ${value}`);
    }
  }
}

if (englishPages === 0 || chinesePages === 0) {
  throw new Error(`expected indexed English and Chinese pages, found en=${englishPages} zh-CN=${chinesePages}`);
}
if (failures.length > 0) {
  throw new Error(`broken built links:\n${failures.slice(0, 30).join('\n')}`);
}

console.log(`built_link_check_ok=true html=${htmlFiles.length} en=${englishPages} zh_cn=${chinesePages}`);
