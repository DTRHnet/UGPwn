import { defineConfig } from "vite";

// Output is embedded into the Go binary by browser/dashboard.go, so it must be
// written inside the module and served under /dashboard/.
export default defineConfig({
  base: "/dashboard/",
  build: {
    outDir: "../browser/dashboard/dist",
    emptyOutDir: true,
    assetsDir: "assets",
    target: "es2022",
    sourcemap: false,
  },
  esbuild: { jsx: "automatic", jsxImportSource: "preact" },
  resolve: { alias: { react: "preact/compat", "react-dom": "preact/compat" } },
});
