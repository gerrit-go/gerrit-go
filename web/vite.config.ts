import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "^/(login|register|logout|accounts|projects|changes|git|a)($|/)": {
        target: "http://localhost:8080",
        changeOrigin: true,
        bypass(req) {
          if (req.method !== "GET" && req.method !== "HEAD") return;
          const p = (req.url ?? "").split("?")[0];
          const apiGet =
            p.startsWith("/git/") ||
            p.startsWith("/a/") ||
            p.startsWith("/accounts") ||
            p === "/projects/" ||
            /^\/projects\/.+\/(branches|commits|tree|file)$/.test(p) ||
            p.startsWith("/changes/");
          if (apiGet) return;
          return p;
        },
      },
    },
  },
});
