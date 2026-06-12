import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import { resolve } from "path";

// 4 个独立入口 (MPA), 对应 server.go 的 4 个路由:
//   /   -> index.html  (电脑端发送 dashboard)
//   /d  -> d.html      (手机端下载页)
//   /r  -> r.html      (电脑端接收 dashboard)
//   /u  -> u.html      (手机端上传表单)
//
// Vite build 产物在 dist/, server.go 通过 //go:embed web/dist 嵌入二进制.
//
// dev 模式 (npm run dev): proxy /api /qr /qr-recv /file /upload 到本机 8443 daemon,
// 这样可以一边跑 daemon 一边热重载前端.
export default defineConfig({
  plugins: [vue()],
  root: ".",
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: {
      input: {
        index: resolve(__dirname, "index.html"),
        d: resolve(__dirname, "d.html"),
        r: resolve(__dirname, "r.html"),
        u: resolve(__dirname, "u.html"),
        p: resolve(__dirname, "p.html"),
        v: resolve(__dirname, "v.html"),
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": "http://127.0.0.1:8443",
      "/qr": "http://127.0.0.1:8443",
      "/qr-recv": "http://127.0.0.1:8443",
      "/file": "http://127.0.0.1:8443",
      "/upload": "http://127.0.0.1:8443",
    },
  },
});
