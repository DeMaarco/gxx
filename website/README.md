# gxx website

The official gxx website is a static Astro site with a Three.js layer, documented 3D credits, responsive product storytelling, and an explicit empty state for unpublished benchmark data.

## Development

```sh
npm install
npm run dev
```

## Production build

```sh
npm run build
npm run preview
```

The site is configured for GitHub Pages at `/gxx/` in production. The WebGL scenes lazy-load the GLB asset and fall back to the original art system when WebGL is unavailable or reduced motion is enabled.

## Asset notes

- `public/models/gxx-core-module.glb` is the selected Kenney Modular Space Kit 1.0 module. See [`public/credits/3d-credits.md`](public/credits/3d-credits.md).
- `public/images/` contains original procedural SVG art for the hero, workspace, security, context, open source, and final CTA compositions.
- The benchmark dashboard intentionally contains no fabricated measurements. It is ready for reproducible results when the repository publishes them.
