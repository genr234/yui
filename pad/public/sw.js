// Yui Pad service worker: caches the app shell; API and cable traffic always hits the network.
const CACHE = "yui-pad-shell-v1";
const SHELL = ["/pad/", "/pad/manifest.webmanifest", "/pad/icon.svg"];

self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener("activate", (e) => {
  e.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("fetch", (e) => {
  const req = e.request;
  if (req.method !== "GET") return;
  const url = new URL(req.url);
  if (url.origin !== location.origin) return;

  // Hashed build assets: cache-first.
  if (url.pathname.startsWith("/pad/assets/")) {
    e.respondWith(
      caches.match(req).then(
        (hit) =>
          hit ||
          fetch(req).then((res) => {
            if (res.ok) {
              const copy = res.clone();
              caches.open(CACHE).then((c) => c.put(req, copy));
            }
            return res;
          }),
      ),
    );
    return;
  }

  // Navigations to /play/<CODE>: network first (session may be gone), shell as offline fallback.
  if (req.mode === "navigate" && url.pathname.startsWith("/play/")) {
    e.respondWith(fetch(req).catch(() => caches.match("/pad/")));
  }
});
