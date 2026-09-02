export type DocNavItem = {
	slug: string;
	label: string;
};

export type DocNavSection = {
	label: string;
	items: DocNavItem[];
};

export type DocLocale = 'en' | 'es';

export const docsNav: Record<DocLocale, DocNavSection[]> = {
	en: [
		{
			label: 'getting started',
			items: [
				{ slug: 'install', label: 'Install' },
				{ slug: 'quick-start', label: 'Quick start' },
			],
		},
		{
			label: 'using gxx',
			items: [
				{ slug: 'repl', label: 'REPL' },
				{ slug: 'permissions', label: 'Permissions' },
				{ slug: 'privacy', label: 'Privacy' },
				{ slug: 'cli', label: 'CLI' },
			],
		},
	],
	es: [
		{
			label: 'primeros pasos',
			items: [
				{ slug: 'install', label: 'Instalación' },
				{ slug: 'quick-start', label: 'Inicio rápido' },
			],
		},
		{
			label: 'usar gxx',
			items: [
				{ slug: 'repl', label: 'REPL' },
				{ slug: 'permissions', label: 'Permisos' },
				{ slug: 'privacy', label: 'Privacidad' },
				{ slug: 'cli', label: 'CLI' },
			],
		},
	],
};

export function flatNav(locale: DocLocale): DocNavItem[] {
	return docsNav[locale].flatMap((section) => section.items);
}

export function getAdjacent(slug: string, locale: DocLocale) {
	const items = flatNav(locale);
	const index = items.findIndex((item) => item.slug === slug);
	if (index === -1) return { prev: null, next: null };
	return {
		prev: index > 0 ? items[index - 1] : null,
		next: index < items.length - 1 ? items[index + 1] : null,
	};
}

export function docPath(slug: string, locale: DocLocale, base = import.meta.env.BASE_URL) {
	const prefix = locale === 'es' ? `${base}es/` : base;
	return `${prefix}${slug}/`;
}

export function isDocSlug(slug: string, locale: DocLocale): boolean {
	return flatNav(locale).some((item) => item.slug === slug);
}

export function collectionId(slug: string, locale: DocLocale): string {
	return locale === 'es' ? `es/${slug}` : slug;
}
