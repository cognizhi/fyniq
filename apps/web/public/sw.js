/* The Fyniq service worker (PRD-001 FR-UX-16; p2-mobile-approvals-automation design D4).
 *
 * It does one thing: it keeps the app's own files (the shell and its assets) so the installed app opens with no signal. It never touches the API: every request
 * to /api goes to the network, so no figure, approval or personal data is ever stored here, and it never handles a POST, PUT, PATCH or DELETE, so no approval or any
 * other write can be replayed later. Its caches hold public static files only, and the page clears them at sign-out. */
"use strict";

var SHELL = "fyniq-shell-v1";
var KEEP = [SHELL];

// shellRequest says whether a request is one of the app's own files to keep: a GET on this origin that is the page itself or a built asset, never the API or anything else.
function shellRequest(method, url, origin) {
  if (method !== "GET") return false;
  var u = new URL(url, origin);
  if (u.origin !== origin) return false;
  if (u.pathname.indexOf("/api/") === 0 || u.pathname === "/api") return false;
  return u.pathname === "/" || u.pathname === "/index.html" || u.pathname.indexOf("/assets/") === 0 || u.pathname === "/manifest.webmanifest" || u.pathname === "/icon.svg" || u.pathname === "/icon-maskable.svg";
}

if (typeof self !== "undefined" && typeof self.addEventListener === "function") {
  self.addEventListener("install", function () {
    self.skipWaiting();
  });
  self.addEventListener("activate", function (event) {
    event.waitUntil(
      caches.keys().then(function (names) {
        return Promise.all(names.filter(function (n) { return KEEP.indexOf(n) < 0; }).map(function (n) { return caches.delete(n); }));
      }).then(function () { return self.clients.claim(); })
    );
  });
  self.addEventListener("fetch", function (event) {
    var req = event.request;
    if (!shellRequest(req.method, req.url, self.location.origin)) return; // the network answers, as if there were no worker
    var navigation = req.mode === "navigate";
    event.respondWith(
      (navigation
        ? fetch(req).then(function (res) { return keep(req, res, "/index.html"); }).catch(function () { return caches.match("/index.html"); })
        : caches.match(req).then(function (hit) { return hit || fetch(req).then(function (res) { return keep(req, res, null); }); })
      )
    );
  });
  // The page asks it to forget at sign-out.
  self.addEventListener("message", function (event) {
    if (event.data && event.data.type === "clear") {
      event.waitUntil(caches.keys().then(function (names) { return Promise.all(names.map(function (n) { return caches.delete(n); })); }));
    }
  });
}

function keep(req, res, as) {
  if (!res || !res.ok || res.type === "opaque") return res;
  var copy = res.clone();
  caches.open(SHELL).then(function (c) { return c.put(as || req, copy); });
  return res;
}

if (typeof module !== "undefined") module.exports = { shellRequest: shellRequest };
