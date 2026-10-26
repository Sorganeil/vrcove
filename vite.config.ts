import { writeFileSync } from "node:fs";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The Go server embeds web/dist, so the build goes there. `go:embed` needs the
// directory to exist even before the first build, hence the kept .gitkeep.
const outDir = "web/dist";

export default defineConfig({
  plugins: [
    react(),
    {
      name: "keep-embed-placeholder",
      closeBundle: () => writeFileSync(`${outDir}/.gitkeep`, ""),
    },
  ],
  build: { outDir, emptyOutDir: true },
  server: {
    // Run the Go server with PUBLIC_ORIGIN=http://localhost:5173 alongside this.
    proxy: { "/api": process.env.VRCOVE_API_TARGET || "http://127.0.0.1:8080" },
  },
});
