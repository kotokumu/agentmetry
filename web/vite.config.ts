import { defineConfig } from "vitest/config";

export default defineConfig({
  build: {
    outDir: "dist/generated",
    emptyOutDir: true,
  },
  test: {
    environment: "happy-dom",
  },
});
