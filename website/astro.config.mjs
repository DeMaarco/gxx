// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
	site: 'https://demaarco.github.io',
	base: '/gxx/',
	integrations: [
		starlight({
			title: 'gxx',
			description: 'A small coding agent for the terminal.',
			customCss: ['./src/styles/custom.css'],
			components: {
				ThemeProvider: './src/components/ThemeProvider.astro',
				ThemeSelect: './src/components/ThemeSelect.astro',
			},
			head: [
				{
					tag: 'link',
					attrs: {
						rel: 'preconnect',
						href: 'https://fonts.googleapis.com',
					},
				},
				{
					tag: 'link',
					attrs: {
						rel: 'preconnect',
						href: 'https://fonts.gstatic.com',
						crossorigin: true,
					},
				},
				{
					tag: 'link',
					attrs: {
						rel: 'stylesheet',
						href: 'https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&family=JetBrains+Mono:wght@400;500&display=swap',
					},
				},
			],
			locales: {
				root: {
					label: 'English',
					lang: 'en',
				},
				es: {
					label: 'Español',
					lang: 'es',
				},
			},
			social: [
				{
					icon: 'github',
					label: 'GitHub',
					href: 'https://github.com/DeMaarco/gxx',
				},
			],
			sidebar: [
				{
					label: 'Install',
					slug: 'install',
					translations: { es: 'Instalación' },
				},
				{
					label: 'Quick start',
					slug: 'quick-start',
					translations: { es: 'Inicio rápido' },
				},
				{
					label: 'REPL',
					slug: 'repl',
				},
				{
					label: 'Permissions',
					slug: 'permissions',
					translations: { es: 'Permisos' },
				},
				{
					label: 'Privacy',
					slug: 'privacy',
					translations: { es: 'Privacidad' },
				},
				{
					label: 'CLI',
					slug: 'cli',
				},
			],
		}),
	],
});
