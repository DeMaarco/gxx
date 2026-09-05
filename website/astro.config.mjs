// @ts-check
import { defineConfig } from 'astro/config';
import tailwindcss from '@tailwindcss/vite';

const isDev = process.argv.includes('dev');
const baseFromEnv = process.env.BASE_PATH || process.env.PUBLIC_BASE_PATH;
const onCloudflare = Boolean(
	process.env.CF_PAGES ||
		process.env.CF_PAGES_URL ||
		process.env.WORKERS_CI ||
		process.env.CLOUDFLARE_ACCOUNT_ID,
);

const base = baseFromEnv || (isDev || onCloudflare ? '/' : '/gxx/');
const site =
	process.env.SITE_URL ||
	process.env.CF_PAGES_URL ||
	(onCloudflare ? undefined : 'https://demaarco.github.io');

// https://astro.build/config
export default defineConfig({
	site,
	base,
	output: 'static',
	markdown: {
		syntaxHighlight: false,
	},
	vite: {
		plugins: [tailwindcss()],
	},
});
