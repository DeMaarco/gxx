const files = import.meta.glob<string>('../content/docs/**/*.md', {
	query: '?raw',
	import: 'default',
	eager: true,
});

export function rawDocMarkdown(id: string): string {
	const key = `../content/docs/${id}.md`;
	const raw = files[key];
	if (typeof raw !== 'string') {
		throw new Error(`Markdown source not found: ${id}`);
	}
	return raw;
}

export const markdownHeaders = {
	'Content-Type': 'text/markdown; charset=utf-8',
	'Cache-Control': 'public, max-age=3600',
};
