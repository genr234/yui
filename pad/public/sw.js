// Yui Pad service worker: caches the app shell; API, cable and sim traffic always hits the network.
// Registered from /pad/sw.js with scope /play/ (Rails sends Service-Worker-Allowed: /play/).
const CACHE = "yui-pad-shell-v4";
const SHELL = [
	"/pad/",
	"/pad/manifest.webmanifest",
	"/pad/icon.svg",
	"/pad/icon-192.png",
	"/pad/icon-512.png",
	"/pad/icon-maskable-512.png",
	"/pad/apple-touch-icon.png",
];

// The built index.html names the hashed bundle files; cache those too so the
// whole shell is available after the first visit.
async function precache() {
  const cache = await caches.open(CACHE);
  await cache.addAll(SHELL);
  const index = await cache.match("/pad/");
  if (!index) return;
  const html = await index.text();
  const assets = [...html.matchAll(/(?:src|href)="(\/pad\/assets\/[^"]+)"/g)].map((m) => m[1]);
  await Promise.all(assets.map((a) => cache.add(a).catch(() => {})));
}

self.addEventListener("install", (e) => {
  e.waitUntil(precache().then(() => self.skipWaiting()));
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

  // Hashed build assets never change: cache-first.
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

  // Navigations to /play (the installed app's start page) and /play/<CODE>:
  // network first (Rails sends the CSP). The cached shell stands in when the
  // network is down or answers with an error page (a proxy's 502, an old
  // server's 404), so the app always opens on its own screens, which say
  // what went wrong and offer a box for a code.
  if (req.mode === "navigate" && (url.pathname === "/play" || url.pathname.startsWith("/play/"))) {
    e.respondWith(
      fetch(req)
        .then(async (res) => {
          if (res.ok || res.type === "opaqueredirect" || res.redirected) return res;
          return (await caches.match("/pad/")) || res;
        })
        .catch(async () => (await caches.match("/pad/")) || Response.error()),
    );
  }
});
