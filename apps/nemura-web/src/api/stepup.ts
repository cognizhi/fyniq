import { api } from "./client";

// Confirming the session with a passkey (NM-ARC-06): the API gives the options, the browser asks the person's authenticator to sign the one-time challenge, and the answer
// goes back. It resolves to an error's detail when something fails and to undefined when the session is confirmed for five minutes.
const b64 = (s: string): ArrayBuffer => {
  const pad = "=".repeat((4 - (s.length % 4)) % 4);
  const raw = atob((s + pad).replace(/-/g, "+").replace(/_/g, "/"));
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out.buffer;
};
const b64url = (buf: ArrayBuffer): string => btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");

export async function confirmWithPasskey(): Promise<string | undefined> {
  const begun = await api.POST("/v1/session/step-up");
  if (!begun.data) return (begun.error as { detail?: string } | undefined)?.detail ?? "";
  const options = (begun.data as { publicKey: { challenge: string; allowCredentials?: { id: string; type: string }[] } & Record<string, unknown> }).publicKey;
  const publicKey = { ...options, challenge: b64(options.challenge), allowCredentials: (options.allowCredentials ?? []).map((c) => ({ ...c, id: b64(c.id) })) } as unknown as PublicKeyCredentialRequestOptions;
  const got = (await navigator.credentials.get({ publicKey })) as PublicKeyCredential | null;
  if (!got) return "";
  const r = got.response as AuthenticatorAssertionResponse;
  const credential = {
    id: got.id,
    rawId: b64url(got.rawId),
    type: got.type,
    response: { authenticatorData: b64url(r.authenticatorData), clientDataJSON: b64url(r.clientDataJSON), signature: b64url(r.signature), userHandle: r.userHandle ? b64url(r.userHandle) : null },
  };
  const done = await api.POST("/v1/session/step-up/confirm", { body: { credential } });
  return done.error ? ((done.error as { detail?: string }).detail ?? "") : undefined;
}
