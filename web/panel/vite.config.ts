import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";
import { fileURLToPath } from "node:url";

const panelDir = fileURLToPath(new URL(".", import.meta.url));

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(panelDir, "src"),
    },
  },
  server: {
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        ws: true,
      },
      "/phpmyadmin": "http://127.0.0.1:8080",
    },
  },
  build: {
    outDir: path.resolve(panelDir, "../dist"),
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          const normalizedId = id.split(path.sep).join("/");
          if (!normalizedId.includes("node_modules")) {
            return;
          }

          if (
            normalizedId.includes("/node_modules/codemirror/") ||
            normalizedId.includes("/node_modules/@codemirror/") ||
            normalizedId.includes("/node_modules/@lezer/")
          ) {
            return;
          }

          if (normalizedId.includes("@xterm")) {
            return "vendor-terminal";
          }
          if (normalizedId.includes("recharts") || normalizedId.includes("d3-")) {
            return "vendor-charts";
          }
          if (normalizedId.includes("@radix-ui")) {
            return "vendor-radix";
          }
          if (normalizedId.includes("@tanstack")) {
            return "vendor-tanstack";
          }
          if (normalizedId.includes("lucide-react")) {
            return "vendor-icons";
          }
          if (
            normalizedId.includes("/node_modules/react/") ||
            normalizedId.includes("/node_modules/react-dom/") ||
            normalizedId.includes("/node_modules/scheduler/")
          ) {
            return "vendor-react";
          }

          return "vendor";
        },
      },
    },
  },
});
