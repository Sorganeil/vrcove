import { createHash } from 'node:crypto';
import { existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// The Go server embeds web/dist, and the desktop app (`--mode desktop`) embeds
// its own copy, so each build goes next to the program that embeds it.
// `go:embed` needs the directory to exist even before the first build, hence
// the kept .gitkeep.
export default defineConfig(({ mode, command }) => {
  const outDir = mode === 'desktop' ? 'companion/cmd/vrcove-desktop/frontend' : 'web/dist';
  const version = readFileSync('VERSION', 'utf8').trim();
  // The VRChat worker fetches its WebAssembly with this integrity hash, so a
  // changed file on the server fails to load. Development rebuilds it often, so
  // only production builds pin it.
  const wasm = 'public/vrcove/vrcove.wasm';
  const wasmIntegrity =
    command === 'build' && existsSync(wasm)
      ? 'sha384-' + createHash('sha384').update(readFileSync(wasm)).digest('base64')
      : '';
  return {
    define: {
      'import.meta.env.APP_VERSION': JSON.stringify(version),
      'import.meta.env.VRCOVE_WASM_INTEGRITY': JSON.stringify(wasmIntegrity),
    },
    plugins: [
      react(),
      {
        // Subresource Integrity: the page refuses entry scripts and styles
        // that do not match what this build produced.
        name: 'subresource-integrity',
        apply: 'build',
        enforce: 'post',
        generateBundle(_, bundle) {
          const page = bundle['index.html'];
          if (!page || page.type !== 'asset') return;
          const integrity = (file: string) => {
            const item = bundle[file];
            const data = item.type === 'chunk' ? item.code : item.source;
            return 'sha384-' + createHash('sha384').update(data).digest('base64');
          };
          page.source = String(page.source).replace(
            /<(script|link)\b([^>]*?)\s(src|href)="\/([^"]+)"([^>]*)>/g,
            (tag, name, before, attribute, file, after) =>
              bundle[file] && !tag.includes('integrity=')
                ? `<${name}${before} ${attribute}="/${file}" integrity="${integrity(file)}"${after}>`
                : tag,
          );
        },
      },
      {
        name: 'keep-embed-placeholder',
        closeBundle: () => {
          writeFileSync(`${outDir}/.gitkeep`, '');
          // The desktop app talks to VRChat itself and never loads the
          // website's VRChat worker.
          if (mode === 'desktop') rmSync(`${outDir}/vrcove`, { recursive: true, force: true });
        },
      },
    ],
    build: { outDir, emptyOutDir: true },
    // The VRChat worker loads Go's wasm_exec.js with a dynamic import.
    worker: { format: 'es' },
    server: {
      // Run the Go server with PUBLIC_ORIGIN=http://localhost:5173 alongside this.
      proxy: { '/api': { target: process.env.VRCOVE_API_TARGET || 'http://127.0.0.1:8080', ws: true } },
    },
  };
});
