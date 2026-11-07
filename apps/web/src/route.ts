// Where the browser goes. Screens are whole pages: moving between them is a page load, so nothing
// secret has to survive in memory or storage across a navigation.

// safeReturn keeps a return address on this origin: a path, never an absolute URL, "//host" or a
// backslash trick (the server's signin.SafeReturn applies the same rule).
export function safeReturn(s: string | null | undefined): string {
  if (!s || s.length > 2048 || !s.startsWith("/") || s.startsWith("//") || /[\\\r\n\t]/.test(s)) return "/";
  try {
    const u = new URL(s, window.location.origin);
    return u.origin === window.location.origin ? u.pathname + u.search + u.hash : "/";
  } catch {
    return "/";
  }
}

// go replaces the current page with path (a redirect, so Back does not return to it).
export function go(path: string): void {
  window.location.replace(path);
}

// signInURL is the address that signs in with a provider and comes back to returnTo.
export function signInURL(provider: string, returnTo: string): string {
  return `/api/auth/signin?${new URLSearchParams({ provider, return_to: safeReturn(returnTo) })}`;
}

// visit goes to path as a new page, so Back returns here.
export function visit(path: string): void {
  window.location.assign(path);
}

// linkURL is the address that links a sign-in with a provider to the signed-in person's account (FR-ADM-08); it comes
// back to the profile.
export function linkURL(provider: string): string {
  return `/api/auth/signin?${new URLSearchParams({ provider, link: "1" })}`;
}

// here is the current page, as a return address.
export function here(): string {
  return window.location.pathname + window.location.search;
}
