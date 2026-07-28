import { readFile, stat } from 'node:fs/promises';

const required = [
  ['../public/favicon.svg', 100],
  ['../public/social-card.png', 10_000],
  ['../public/terminal/axern.gif', 10_000],
  ['../public/terminal/axrun.gif', 10_000],
  ['../public/terminal/python-service.gif', 10_000],
  ['../vhs/axern.tape', 100],
  ['../vhs/axrun.tape', 100],
  ['../vhs/python-service.tape', 100],
];

for (const [relativePath, minimumBytes] of required) {
  const url = new URL(relativePath, import.meta.url);
  const info = await stat(url);
  if (!info.isFile() || info.size < minimumBytes) {
    throw new Error(`${relativePath} must be a file of at least ${minimumBytes} bytes`);
  }
}

const recordings = [
  ['axern', 960, 600],
  ['axrun', 960, 600],
  ['python-service', 960, 380],
];

for (const [name, expectedWidth, expectedHeight] of recordings) {
  const gif = await readFile(new URL(`../public/terminal/${name}.gif`, import.meta.url));
  if (gif.subarray(0, 6).toString('ascii') !== 'GIF89a') {
    throw new Error(`${name}.gif is not a GIF89a recording`);
  }
  const width = gif.readUInt16LE(6);
  const height = gif.readUInt16LE(8);
  if (width !== expectedWidth || height !== expectedHeight) {
    throw new Error(
      `${name}.gif must be ${expectedWidth}x${expectedHeight}, found ${width}x${height}`,
    );
  }
}

console.log('static_asset_check_ok=true');
