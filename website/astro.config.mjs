// @ts-check
import { defineConfig } from 'astro/config';
import tailwindcss from '@tailwindcss/vite';

const isDev = process.argv.includes('dev');

// https://astro.build/config
export default defineConfig({
	site: 'https://demaarco.github.io',
	base: isDev ? '/' : '/gxx/',
	markdown: {
		syntaxHighlight: false,
	},
	vite: {
		plugins: [tailwindcss()],
	},
});
