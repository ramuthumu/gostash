// GoStash Service Worker
const CACHE_NAME = 'gostash-v2';
const STATIC_ASSETS = [
  '/static/style.css',
  '/static/theme.js',
  '/static/reader.js',
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

  // For fonts and immutable media: Cache-First
  if (url.pathname.startsWith('/media/') || url.pathname.startsWith('/static/fonts/')) {
    event.respondWith(
      caches.match(event.request).then((cached) => {
        if (cached) return cached;
        return fetch(event.request).then((resp) => {
          if (resp && resp.status === 200) {
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
          if (networkResp && networkResp.status === 200) {
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

  // For HTML pages / articles: Network-First
  if (event.request.mode === 'navigate') {
    event.respondWith(
      fetch(event.request).catch(() => {
        return caches.match(event.request);
      })
    );
  }
});
