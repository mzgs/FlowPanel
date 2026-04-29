import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        ws: true,
      },
      "/phpmyadmin": "http://localhost:8080",
    },
  },
  build: {
    outDir: path.resolve(__dirname, "../dist"),
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          const normalizedId = id.split(path.sep).join("/");
          if (!normalizedId.includes("node_modules")) {
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
