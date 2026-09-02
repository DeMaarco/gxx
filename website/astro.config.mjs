// @ts-check
import { defineConfig } from 'astro/config';
import tailwindcss from '@tailwindcss/vite';

// https://astro.build/config
export default defineConfig({
	site: 'https://demaarco.github.io',
	base: '/gxx/',
	markdown: {
		syntaxHighlight: false,
	},
	vite: {
		plugins: [tailwindcss()],
	},
});
