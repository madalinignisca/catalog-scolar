// Default API base URL. In the browser, we use the same hostname the page was
// loaded from (works for both localhost and LAN IP access). On the server side
// (SSR), we fall back to localhost. Override via NUXT_PUBLIC_API_BASE env var.
const apiBase: string =
  (process as unknown as { env: Record<string, string | undefined> }).env['NUXT_PUBLIC_API_BASE'] ??
  'http://localhost:8080/api/v1';

export default defineNuxtConfig({
  compatibilityDate: '2025-03-01',

  modules: ['@nuxtjs/tailwindcss', '@vite-pwa/nuxt'],

  runtimeConfig: {
    public: {
      apiBase,
    },
  },

  pwa: {
    registerType: 'autoUpdate',
    manifest: {
      name: 'CatalogRO',
      short_name: 'CatalogRO',
      description: 'Catalog Școlar Digital',
      theme_color: '#1B5E8C',
      background_color: '#ffffff',
      display: 'standalone',
      lang: 'ro',
      icons: [
        { src: '/icon-192.png', sizes: '192x192', type: 'image/png' },
        { src: '/icon-512.png', sizes: '512x512', type: 'image/png' },
      ],
    },
    workbox: {
      navigateFallback: '/',
      runtimeCaching: [
        {
          urlPattern: /^https:\/\/.*\/api\/v1\/.*/,
          handler: 'NetworkFirst',
          options: {
            cacheName: 'api-cache',
            expiration: { maxEntries: 100, maxAgeSeconds: 60 * 60 },
            networkTimeoutSeconds: 5,
          },
        },
      ],
    },
  },

  app: {
    head: {
      title: 'CatalogRO',
      htmlAttrs: { lang: 'ro' },
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
        { name: 'description', content: 'Catalog Școlar Digital pentru România' },
      ],
    },
  },

  typescript: {
    strict: true,
  },

  // Proxy /api/* requests to the Go backend. This makes API calls same-origin,
  // which means httpOnly cookies are sent automatically without cross-origin
  // issues (SameSite, Secure flag problems in dev mode).
  // In production, Traefik handles routing — this proxy is for dev/SSR only.
  nitro: {
    routeRules: {
      '/api/**': {
        proxy: 'http://localhost:8080/api/**',
      },
    },
  },

  devtools: { enabled: true },

  // Listen on all interfaces so the dev server is accessible from
  // other machines on the local network (e.g. laptop → VM).
  devServer: {
    host: '0.0.0.0',
    port: 3000,
  },
});
