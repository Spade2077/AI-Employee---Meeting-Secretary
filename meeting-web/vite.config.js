import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  base: "/meeting/",
  plugins: [react()],
  server: {
    proxy: {
      "/api": {
        target: "http://localhost:8090",
        changeOrigin: true
      },
      "/ws": {
        target: "ws://localhost:8090",
        ws: true
      },
      "/share": {
        target: "http://localhost:8090",
        changeOrigin: true
      }
    }
  }
});
