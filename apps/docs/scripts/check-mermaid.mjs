import { readFile, readdir } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { JSDOM } from 'jsdom';

const docsRoot = fileURLToPath(new URL('../src/content/docs/', import.meta.url));

const dom = new JSDOM('<!doctype html><html><body></body></html>');
globalThis.window = dom.window;
globalThis.document = dom.window.document;

const { default: mermaid } = await import('mermaid');
mermaid.initialize({ startOnLoad: false, securityLevel: 'strict' });

async function walk(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const target = path.join(directory, entry.name);
    if (entry.isDirectory()) files.push(...await walk(target));
    else if (/\.mdx?$/.test(entry.name)) files.push(target);
  }
  return files;
}

const files = await walk(docsRoot);
let diagrams = 0;

for (const file of files) {
  const source = await readFile(file, 'utf8');
  const blocks = [...source.matchAll(/```mermaid\s*\n([\s\S]*?)```/g)];
  for (const [index, block] of blocks.entries()) {
    diagrams += 1;
    try {
      await mermaid.parse(block[1], { suppressErrors: false });
    } catch (error) {
      throw new Error(`${path.relative(docsRoot, file)} mermaid block ${index + 1}: ${error.message}`);
    }
  }
}

if (diagrams < 2) {
  throw new Error(`expected at least 2 Mermaid diagrams, found ${diagrams}`);
}

console.log(`mermaid_check_ok=true diagrams=${diagrams}`);
