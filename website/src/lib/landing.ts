export type LandingLocale = 'en' | 'es';

export const buildSizes = [
	{ platform: 'Linux', arch: 'amd64', size: '17.8 MiB' },
	{ platform: 'Linux', arch: 'arm64', size: '16.6 MiB' },
	{ platform: 'macOS', arch: 'amd64', size: '18.3 MiB' },
	{ platform: 'macOS', arch: 'arm64', size: '17.1 MiB' },
	{ platform: 'Windows', arch: 'amd64', size: '18.4 MiB' },
	{ platform: 'Windows', arch: 'arm64', size: '16.9 MiB' },
];

export function landingContent(locale: LandingLocale) {
	const shared = {
		version: 'v0.0.25',
		installCmd: 'curl -fsSL https://raw.githubusercontent.com/DeMaarco/gxx/main/install.sh | sh',
	};

	if (locale === 'es') {
		return {
			...shared,
			lang: 'es',
			title: 'gxx — Un agente de código pequeño para la terminal',
			description:
				'Abre un repo, escribe lo que quieres. Un workspace, OpenAI o Claude, sin TUI.',
			headline: 'Un agente de código pequeño para la terminal.',
			about: [
				'gxx es un agente de código en CLI escrito en Go. Funciona dentro de un directorio — lista y busca archivos, lee y parchea código, y ejecuta comandos desde un solo prompt.',
				'Binario estático, sin CGO, ~17 MiB en la mayoría de plataformas. Open source bajo Apache-2.0. OpenAI o Claude, sin TUI.',
			],
			specsTitle: 'Especificaciones',
			specsIntro: 'Build de release con Go 1.27+, CGO_ENABLED=0, -trimpath y -ldflags "-s -w".',
			specsBinary: 'Tamaño del binario',
			specsRuntime: 'Runtime',
			specsRuntimeValue: 'Un solo proceso Go, sin daemon',
			specsPlatforms: 'Plataformas',
			specsPlatformsValue: 'macOS, Linux, Windows · amd64 y arm64',
			specsLicense: 'Licencia',
			specsLicenseValue: 'Apache-2.0',
			diagramTitle: 'Un directorio, un sandbox',
			diagramIntro:
				'El directorio desde el que inicias es el límite. Sin traversal hacia arriba, sin symlinks externos.',
			interfaceTitle: 'Interfaz',
			interfaceIntro:
				'Menús en la terminal — Tab para completar, flechas para navegar. Sin TUI a pantalla completa.',
			screenshotsTitle: 'Capturas reales',
			screenshotsIntro: 'Fotos del REPL en acción, incluidos modos ask/plan y el menú post-plan.',
			featuresLabel: 'Capacidades',
			principlesTitle: 'Cómo piensa gxx',
			footerLicense: 'Apache 2.0',
			footerSource: 'source',
			footerDocs: 'docs',
			footerUpdates: 'actualizaciones',
			copy: 'copiar',
			copied: 'copiado',
			failed: 'error',
			mockups: {
				repl: 'Prompt REPL con línea de estado',
				model: 'Selector de modelos (/model)',
				adjust: 'Ajuste de contexto, effort y fast',
				eco: 'Modos eco (/eco)',
				context: 'Ventana de contexto (/context)',
				usage: 'Uso y cuota (/usage)',
				mode: 'Modos de permiso (/mode)',
				plan: 'Menú post-plan',
			},
			screens: {
				repl: 'REPL en modo agente',
				modes: 'Modos ask, plan y agent',
				plan: 'Menú tras generar un plan',
			},
			heroImageAlt: 'Captura del REPL de gxx: prompt, badge de versión y línea de estado',
		};
	}

	return {
		...shared,
		lang: 'en',
		title: 'gxx — A small coding agent for the terminal',
		description: 'Open a repo, type what you want. One workspace, OpenAI or Claude, no TUI.',
		headline: 'A small coding agent for the terminal.',
		about: [
			'gxx is a coding agent CLI written in Go. It runs inside one directory — lists and searches files, reads and patches code, and runs shell commands from a single prompt.',
			'Static binary, no CGO, ~17 MiB on most platforms. Open source under Apache-2.0. OpenAI or Claude, no TUI.',
		],
		specsTitle: 'Specifications',
		specsIntro: 'Release build uses Go 1.27+, CGO_ENABLED=0, -trimpath, and -ldflags "-s -w".',
		specsBinary: 'Binary size',
		specsRuntime: 'Runtime',
		specsRuntimeValue: 'Single Go process, no daemon',
		specsPlatforms: 'Platforms',
		specsPlatformsValue: 'macOS, Linux, Windows · amd64 and arm64',
		specsLicense: 'License',
		specsLicenseValue: 'Apache-2.0',
		diagramTitle: 'One directory, one sandbox',
		diagramIntro:
			'The directory you start in is the boundary. No parent traversal, no outside symlinks.',
		interfaceTitle: 'Interface',
		interfaceIntro:
			'Terminal menus — Tab to complete, arrows to navigate. No full-screen TUI.',
		screenshotsTitle: 'Real screenshots',
		screenshotsIntro: 'Photos of the REPL in action, including ask/plan modes and the post-plan menu.',
		featuresLabel: 'Capabilities',
		principlesTitle: 'How gxx thinks',
		footerLicense: 'Apache 2.0',
		footerSource: 'source',
		footerDocs: 'docs',
		footerUpdates: 'updates',
		copy: 'copy',
		copied: 'copied',
		failed: 'failed',
		mockups: {
			repl: 'REPL prompt with status line',
			model: 'Model picker (/model)',
			adjust: 'Context, effort, and fast tuning',
			eco: 'Eco modes (/eco)',
			context: 'Context window (/context)',
			usage: 'Usage and quota (/usage)',
			mode: 'Permission modes (/mode)',
			plan: 'Post-plan menu',
		},
		screens: {
			repl: 'REPL in agent mode',
			modes: 'Ask, plan, and agent modes',
			plan: 'Menu after generating a plan',
		},
		heroImageAlt: 'gxx REPL screenshot: prompt, version badge, and status line',
	};
}
