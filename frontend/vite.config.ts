import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import path from 'path';

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/ui/api': {
        target: 'http://localhost:3333',
        changeOrigin: true,
      },
      '/ui/logs/stream': {
        target: 'http://localhost:3333',
        changeOrigin: true,
      },
    },
  },
});
