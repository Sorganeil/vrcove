// Writes web/dist/build-manifest.json: the SHA-256 of every file the website
// serves. Anyone who rebuilds the same commit can compare it, and a signed
// copy lets browsers check what they were served.
import { createHash } from 'node:crypto';
import { readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs';
import { join, relative, sep } from 'node:path';

const root = join('web', 'dist');
const name = 'build-manifest.json';
const files = {};
const walk = (dir) => {
  for (const entry of readdirSync(dir).sort()) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) walk(path);
    else {
      const key = relative(root, path).split(sep).join('/');
      if (key !== name && key !== '.gitkeep')
        files[key] = createHash('sha256').update(readFileSync(path)).digest('hex');
    }
  }
};
walk(root);
const version = readFileSync('VERSION', 'utf8').trim();
writeFileSync(join(root, name), JSON.stringify({ version, files }, null, 2) + '\n');
console.log(`${name}: ${Object.keys(files).length} files`);
