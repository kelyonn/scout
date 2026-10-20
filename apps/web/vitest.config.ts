import { defineConfig } from "vitest/config";
import path from "node:path";

// Unit tests for the pure parts of apps/web: lib/* and the API proxy
// helper. No DOM, no React rendering — the Go API's own tests cover the
// data, and the browser walkthrough covers the screens. What's here is
// the logic that would fail silently: token comparison, redirect
// sanitizing, error mapping, rate limiting, the proxy's failure modes.
export default defineConfig({
  test: {
    environment: "node",
    include: ["**/*.test.ts"],
    exclude: ["node_modules/**", ".next/**"],
  },
  resolve: {
    alias: { "@": path.resolve(__dirname) },
  },
});
