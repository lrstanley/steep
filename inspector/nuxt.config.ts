import tailwindcss from '@tailwindcss/vite'

// A static, client-only app: `bun run build` writes .output/public, which
// any static host serves. Set NUXT_APP_BASE_URL when it is served from a
// subpath, such as a GitHub Pages project site.
export default defineNuxtConfig({
  ssr: false,
  compatibilityDate: '2025-01-01',
  devtools: { enabled: false },
  // Otherwise Nitro detects Workers Builds and uses its cloudflare-module preset, which points
  // wrangler at a generated config for a Worker entry that doesn't exist, not wrangler.jsonc.
  nitro: { preset: 'static' },
  css: ['~/assets/css/tailwind.css'],
  app: {
    head: {
      title: 'reel inspector',
      htmlAttrs: { lang: 'en', class: 'dark' },
      meta: [
        { name: 'description', content: 'Inspect reel and steep .cast recordings: replay frames, events, escape sequences, and annotations.' },
        { name: 'color-scheme', content: 'dark' },
      ],
      link: [{ rel: 'icon', type: 'image/svg+xml', href: 'favicon.svg' }],
    },
  },
  vite: {
    plugins: [tailwindcss()],
  },
  modules: ['shadcn-nuxt'],
  shadcn: {
    prefix: '',
    componentDir: '@/components/ui',
  },
})
