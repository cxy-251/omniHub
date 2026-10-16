import { defineConfig } from 'vite';
import tailwindcss from '@tailwindcss/vite';
import { fileURLToPath } from 'node:url';

export default defineConfig({
  root: 'src',
  plugins: [tailwindcss()],
  build: { outDir: '../dist', emptyOutDir: true, rolldownOptions: { input: ['src/index.html', 'src/players/retro.html'].map(value => fileURLToPath(new URL(value, import.meta.url))) } },
  server: { proxy: { '/api': 'http://127.0.0.1:8998', '/vendor': 'http://127.0.0.1:8998', '/players': 'http://127.0.0.1:8998', '/game-content': 'http://127.0.0.1:8998' } },
});
