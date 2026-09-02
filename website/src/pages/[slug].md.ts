import type { APIRoute, GetStaticPaths } from 'astro';
import { getCollection } from 'astro:content';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

export const getStaticPaths = (async () => {
	const entries = await getCollection('docs', ({ id }) => !id.startsWith('es/'));
	return entries.map((entry) => ({
		params: { slug: entry.id },
		props: { entry },
	}));
}) satisfies GetStaticPaths;

export const GET: APIRoute = ({ props }) => {
	const { entry } = props as { entry: { id: string } };
	const filePath = join(process.cwd(), 'src/content/docs', `${entry.id}.md`);
	const raw = readFileSync(filePath, 'utf-8');
	return new Response(raw, {
		headers: {
			'Content-Type': 'text/markdown; charset=utf-8',
			'Cache-Control': 'public, max-age=3600',
		},
	});
};
