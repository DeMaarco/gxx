import type { APIRoute, GetStaticPaths } from 'astro';
import { getCollection } from 'astro:content';
import { markdownHeaders, rawDocMarkdown } from '../lib/markdown';

export const prerender = true;

export const getStaticPaths = (async () => {
	const entries = await getCollection('docs', ({ id }) => !id.startsWith('es/'));
	return entries.map((entry) => ({
		params: { slug: entry.id },
		props: { markdown: rawDocMarkdown(entry.id) },
	}));
}) satisfies GetStaticPaths;

export const GET: APIRoute = ({ props }) => {
	const markdown = (props as { markdown?: string }).markdown;
	if (!markdown) {
		return new Response('Not found', { status: 404 });
	}
	return new Response(markdown, { headers: markdownHeaders });
};
