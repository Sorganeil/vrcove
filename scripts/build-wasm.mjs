// Builds the website's VRChat worker (cmd/vrcove-wasm) into public/vrcove.
// Go's wasm_exec.js glue, from the same toolchain, goes into
// app/lib/generated so Vite bundles it into the worker, where the worker's
// own hash covers it. The build leaves out version control details, so the
// same commit builds the same file.
import { execFileSync } from 'node:child_process';
import { copyFileSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';

const out = join('public', 'vrcove');
const generated = join('app', 'lib', 'generated');
mkdirSync(out, { recursive: true });
mkdirSync(generated, { recursive: true });
execFileSync(
  'go',
  [
    'build',
    '-trimpath',
    '-buildvcs=false',
    '-ldflags=-s -w',
    '-o',
    join(out, 'vrcove.wasm'),
    './cmd/vrcove-wasm',
  ],
  { stdio: 'inherit', env: { ...process.env, GOOS: 'js', GOARCH: 'wasm' } },
);
const goroot = execFileSync('go', ['env', 'GOROOT'], { encoding: 'utf8' }).trim();
copyFileSync(join(goroot, 'lib', 'wasm', 'wasm_exec.js'), join(generated, 'wasm_exec.js'));
