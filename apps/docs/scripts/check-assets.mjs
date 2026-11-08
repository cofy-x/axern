import { stat } from 'node:fs/promises';

const required = [
  ['../public/favicon.svg', 100],
  ['../public/social-card.png', 10_000],
  ['../public/terminal/axern.svg', 1_000],
];

for (const [relativePath, minimumBytes] of required) {
  const url = new URL(relativePath, import.meta.url);
  const info = await stat(url);
  if (!info.isFile() || info.size < minimumBytes) {
    throw new Error(`${relativePath} must be a file of at least ${minimumBytes} bytes`);
  }
}

console.log('static_asset_check_ok=true');
