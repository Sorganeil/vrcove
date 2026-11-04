// Builds the website's VRChat worker (cmd/vrcove-wasm) into public/vrcove,
// with the wasm_exec.js glue from the same Go toolchain. Vite then copies
// both into the site build.
import { execFileSync } from 'node:child_process';
import { copyFileSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';

const out = join('public', 'vrcove');
mkdirSync(out, { recursive: true });
execFileSync(
  'go',
  ['build', '-trimpath', '-ldflags=-s -w', '-o', join(out, 'vrcove.wasm'), './cmd/vrcove-wasm'],
  {
    stdio: 'inherit',
    env: { ...process.env, GOOS: 'js', GOARCH: 'wasm' },
  },
);
const goroot = execFileSync('go', ['env', 'GOROOT'], { encoding: 'utf8' }).trim();
copyFileSync(join(goroot, 'lib', 'wasm', 'wasm_exec.js'), join(out, 'wasm_exec.js'));
