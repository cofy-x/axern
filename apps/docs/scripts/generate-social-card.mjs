import { createHash } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';

import sharp from 'sharp';

const sourceUrl = new URL('../src/assets/social-card.svg', import.meta.url);
const outputUrl = new URL('../public/social-card.png', import.meta.url);
const manifestUrl = new URL('../src/assets/social-card.generated.json', import.meta.url);
const renderSpec = 'social-card-v1:1200x630:png-compression-9';

function sha256(value) {
  return createHash('sha256').update(value).digest('hex');
}

const source = await readFile(sourceUrl);
const sourceSha256 = sha256(source);

if (process.argv.includes('--check')) {
  const [output, manifestSource] = await Promise.all([
    readFile(outputUrl),
    readFile(manifestUrl, 'utf8'),
  ]);
  const manifest = JSON.parse(manifestSource);
  const metadata = await sharp(output).metadata();

  if (
    manifest.renderSpec !== renderSpec ||
    manifest.sourceSha256 !== sourceSha256 ||
    manifest.outputSha256 !== sha256(output) ||
    metadata.format !== 'png' ||
    metadata.width !== 1200 ||
    metadata.height !== 630
  ) {
    throw new Error('social-card.png is stale; run `make docs-social-card`');
  }

  console.log('social_card_check_ok=true');
  process.exit(0);
}

const output = await sharp(source, { density: 144 })
  .resize(1200, 630)
  .png({ compressionLevel: 9, adaptiveFiltering: true })
  .toBuffer();
const manifest = {
  renderSpec,
  sourceSha256,
  outputSha256: sha256(output),
};

await Promise.all([
  writeFile(outputUrl, output),
  writeFile(manifestUrl, `${JSON.stringify(manifest, null, 2)}\n`),
]);

console.log('social_card_generated=true');
