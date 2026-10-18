import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // Dev: proxy WebSockets to the Go server so the browser sees one origin.
    proxy: { "/ws": { target: "ws://localhost:8080", ws: true } },
  },
});
