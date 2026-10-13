import { defineConfig } from 'vite';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  root: 'src',
  plugins: [tailwindcss()],
  build: { outDir: '../dist', emptyOutDir: true },
  server: { proxy: { '/api': 'http://127.0.0.1:8998', '/vendor': 'http://127.0.0.1:8998', '/players': 'http://127.0.0.1:8998', '/standalone_games': 'http://127.0.0.1:8998' } },
});
