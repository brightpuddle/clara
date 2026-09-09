import { defineConfig } from "vite"
import path from "node:path"
import tailwindcss from "@tailwindcss/vite"

export default defineConfig({
  plugins: [tailwindcss()],
  content: ["**/*.templ", "**/*.ts"],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  build: {
    manifest: true,
    outDir: "dist",
    rollupOptions: {
      input: {
        app: "src/app.ts",
      },
    },
  },
  server: {
    port: 3001,
    host: "0.0.0.0",
    origin: "http://localhost:4444",
    watch: {
      usePolling: true,
    },
  },
  clearScreen: false,
})
