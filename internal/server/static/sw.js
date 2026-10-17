// GoStash Service Worker
const CACHE_NAME = 'gostash-v5';
const STATIC_ASSETS = [
  '/static/style.css?v=4',
  '/static/theme.js',
  '/static/reader.js?v=4',
  '/static/library.js?v=4',
  '/static/vendor/htmx.min.js',
  '/static/icon.svg',
  '/static/fonts/fonts.css',
  '/static/fonts/inter-latin-400700.woff2',
  '/static/fonts/vollkorn-latin-400700.woff2',
  '/static/fonts/newsreader-latin-400700.woff2',
  '/static/fonts/ebgaramond-latin-400700.woff2',
  '/manifest.webmanifest'
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => {
      return cache.addAll(STATIC_ASSETS).catch(() => {});
    })
  );
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => {
      return Promise.all(
        keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key))
      );
    })
  );
  self.clients.claim();
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);
  // Fragments and mutations must never become offline page-cache entries.
  if (event.request.method !== 'GET' || url.origin !== self.location.origin ||
      url.pathname.startsWith('/partials/') || url.pathname.endsWith('/status')) return;

  // Archived article media remains private and must not enter an offline cache.
  if (url.pathname.startsWith('/media/')) {
    event.respondWith(fetch(event.request));
    return;
  }
  // Public fonts are immutable and safe to cache.
  if (url.pathname.startsWith('/static/fonts/')) {
    event.respondWith(
      caches.match(event.request).then((cached) => {
        if (cached) return cached;
        return fetch(event.request).then((resp) => {
          if (resp && resp.status === 200 && !resp.headers.get('Cache-Control')?.includes('no-store')) {
            const copy = resp.clone();
            caches.open(CACHE_NAME).then((cache) => cache.put(event.request, copy));
          }
          return resp;
        });
      })
    );
    return;
  }

  // For static app scripts/CSS: Stale-While-Revalidate
  if (url.pathname.startsWith('/static/') || url.pathname === '/manifest.webmanifest') {
    event.respondWith(
      caches.match(event.request).then((cached) => {
        const fetchPromise = fetch(event.request).then((networkResp) => {
          if (networkResp && networkResp.status === 200 && !networkResp.headers.get('Cache-Control')?.includes('no-store')) {
            const copy = networkResp.clone();
            caches.open(CACHE_NAME).then((cache) => cache.put(event.request, copy));
          }
          return networkResp;
        }).catch(() => null);

        return cached || fetchPromise;
      })
    );
    return;
  }

  // For HTML pages / articles: Network-First, caching successful responses so
  // previously visited pages remain readable offline.
  if (event.request.mode === 'navigate') {
    event.respondWith(
      fetch(event.request).then((resp) => {
        if (resp && resp.ok && !resp.headers.get('Cache-Control')?.includes('no-store')) {
          const copy = resp.clone();
          caches.open(CACHE_NAME).then((cache) => cache.put(event.request, copy));
        }
        return resp;
      }).catch(() => caches.match(event.request))
    );
  }
});
