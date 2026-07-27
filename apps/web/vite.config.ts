import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The build output lands directly in the Go server's embed directory, so
// `make build` produces one binary with the PWA inside it.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../../server/internal/web/dist',
    emptyOutDir: true,
  },
  server: {
    // `npm run dev` talks to a server started with `make run`.
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8899',
        changeOrigin: true,
      },
    },
  },
})
