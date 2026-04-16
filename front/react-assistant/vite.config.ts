import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        // SSE requires: no response buffering, identity encoding, and
        // HTTP/1.1 chunked passthrough. http-proxy defaults forward the
        // browser's `Accept-Encoding: gzip, br` upstream — if anything in
        // the chain honors it, gzip's DEFLATE window holds SSE frames
        // until EOF and the UI sees only the final response. Force
        // identity on the upstream hop and keep the connection open.
        ws: true,
        configure: (proxy) => {
          proxy.on('proxyReq', (proxyReq) => {
            proxyReq.setHeader('Accept-Encoding', 'identity')
            proxyReq.setHeader('X-Accel-Buffering', 'no')
          })
        },
      },
    },
  },
})
