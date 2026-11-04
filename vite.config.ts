import { readFileSync, rmSync, writeFileSync } from 'node:fs';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// The Go server embeds web/dist, and the desktop app (`--mode desktop`) embeds
// its own copy, so each build goes next to the program that embeds it.
// `go:embed` needs the directory to exist even before the first build, hence
// the kept .gitkeep.
export default defineConfig(({ mode }) => {
  const outDir = mode === 'desktop' ? 'companion/cmd/vrcove-desktop/frontend' : 'web/dist';
  const version = readFileSync('VERSION', 'utf8').trim();
  return {
    define: {
      'import.meta.env.APP_VERSION': JSON.stringify(version),
    },
    plugins: [
      react(),
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
