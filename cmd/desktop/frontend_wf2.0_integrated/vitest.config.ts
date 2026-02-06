import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte({ hot: !process.env.VITEST })],
  test: {
    globals: true,
    environment: 'jsdom',
    include: ['src/**/*.{test,spec}.{js,ts}'],
    alias: {
      '$lib': '/src/lib',
      '$app': '/src/app',
    },
  },
  resolve: {
    alias: {
      '$lib': '/src/lib',
      '$app/navigation': '/src/lib/__mocks__/navigation.ts',
    },
  },
});
