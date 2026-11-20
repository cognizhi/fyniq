import { indexedDbStore } from "./queue";

// The service worker and the sign-out clean-up (FR-UX-16; p2-mobile-approvals-automation design D4). The worker keeps the app's own files so the installed app opens with no signal, and touches
// no API request. Registering it is skipped where there is no worker (a test, an old browser) and in the dev server, where Vite's own files must never be cached.

export function registerServiceWorker(): void {
  if (typeof navigator === "undefined" || !("serviceWorker" in navigator) || !import.meta.env.PROD) return;
  window.addEventListener("load", () => {
    void navigator.serviceWorker.register("/sw.js", { scope: "/" }).catch(() => undefined); // the app works without it
  });
}

// forget empties what this device kept for the person: the receipts queued and the worker's caches.
export async function forget(): Promise<void> {
  try {
    await indexedDbStore().clear();
  } catch {
    /* nothing was kept */
  }
  try {
    const reg = await navigator.serviceWorker?.getRegistration();
    reg?.active?.postMessage({ type: "clear" });
    if (typeof caches !== "undefined") await Promise.all((await caches.keys()).map((n) => caches.delete(n)));
  } catch {
    /* no worker */
  }
}

// installSignOutCleanup forgets before a sign-out form goes: sign-out is a form post to the API, so the page holds it until the device is clean, then sends it.
export function installSignOutCleanup(doc: Document = document): () => void {
  const onSubmit = (e: Event) => {
    const form = e.target;
    if (!(form instanceof HTMLFormElement) || !form.action.endsWith("/api/auth/signout") || form.dataset.forgotten === "1") return;
    e.preventDefault();
    void forget().finally(() => {
      form.dataset.forgotten = "1";
      form.requestSubmit();
    });
  };
  doc.addEventListener("submit", onSubmit, true);
  return () => doc.removeEventListener("submit", onSubmit, true);
}
