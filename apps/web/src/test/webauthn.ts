import { vi } from "vitest";

// A browser authenticator for component tests: jsdom has no WebAuthn, so the tests give the page
// navigator.credentials and PublicKeyCredential, and see what the page asked for.

export const credentialJSON = { id: "a2V5", rawId: "a2V5", type: "public-key", response: { clientDataJSON: "e30", attestationObject: "AA" }, clientExtensionResults: {} };

export function authenticator({ refuse }: { refuse?: string } = {}) {
  const answer = async () => {
    if (refuse) throw new DOMException("The operation either timed out or was not allowed.", refuse);
    return { toJSON: () => credentialJSON };
  };
  const credentials = { create: vi.fn(answer), get: vi.fn(answer) };
  Object.defineProperty(window.navigator, "credentials", { value: credentials, configurable: true });
  Object.defineProperty(window, "PublicKeyCredential", { value: function PublicKeyCredential() {}, configurable: true, writable: true });
  return credentials;
}

export function noAuthenticator() {
  Object.defineProperty(window, "PublicKeyCredential", { value: undefined, configurable: true, writable: true });
}
