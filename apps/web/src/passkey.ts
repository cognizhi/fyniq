// The browser's half of a passkey ceremony (WebAuthn; FR-ADM-02, SEC-01). The API sends the options
// as JSON, with its binary values in base64url (the `publicKey` member of a WebAuthn options
// document); the browser's answer goes back as JSON the same way. Nothing here is secret: the
// private key never leaves the authenticator, and each challenge works once, for 5 minutes, on
// this site only.

type Json = Record<string, unknown>;

// supported says whether this browser can use passkeys at all.
export function supported(): boolean {
  return typeof window !== "undefined" && typeof window.PublicKeyCredential === "function" && !!navigator.credentials;
}

export function fromBase64url(s: string): ArrayBuffer {
  const b64 = s.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(s.length / 4) * 4, "=");
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}

export function toBase64url(b: ArrayBuffer | ArrayBufferView): string {
  const bytes = ArrayBuffer.isView(b) ? new Uint8Array(b.buffer, b.byteOffset, b.byteLength) : new Uint8Array(b);
  let bin = "";
  for (const x of bytes) bin += String.fromCharCode(x);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function publicKeyOf(options: Json): Json {
  const pk = options.publicKey;
  if (!pk || typeof pk !== "object") throw new Error("The passkey options have no publicKey member.");
  return pk as Json;
}

// parser is the browser's own JSON parser for WebAuthn options, when it has one.
function parser<T>(name: "parseCreationOptionsFromJSON" | "parseRequestOptionsFromJSON"): ((o: unknown) => T) | undefined {
  const pkc = (globalThis as { PublicKeyCredential?: unknown }).PublicKeyCredential as Record<string, unknown> | undefined;
  const f = pkc?.[name];
  return typeof f === "function" ? (f.bind(pkc) as (o: unknown) => T) : undefined;
}

function withIds(list: unknown): unknown {
  return Array.isArray(list) ? list.map((c: Json) => ({ ...c, id: fromBase64url(String(c.id)) })) : list;
}

// creationOptions turns the API's registration options into what navigator.credentials.create
// takes, with the browser's own parser when it has one.
export function creationOptions(options: Json): PublicKeyCredentialCreationOptions {
  const pk = publicKeyOf(options);
  const parse = parser<PublicKeyCredentialCreationOptions>("parseCreationOptionsFromJSON");
  if (parse) return parse(pk);
  const user = pk.user as Json;
  return {
    ...(pk as object),
    challenge: fromBase64url(String(pk.challenge)),
    user: { ...(user as object), id: fromBase64url(String(user.id)) },
    excludeCredentials: withIds(pk.excludeCredentials),
  } as PublicKeyCredentialCreationOptions;
}

// requestOptions turns the API's step-up or approval options into what navigator.credentials.get
// takes.
export function requestOptions(options: Json): PublicKeyCredentialRequestOptions {
  const pk = publicKeyOf(options);
  const parse = parser<PublicKeyCredentialRequestOptions>("parseRequestOptionsFromJSON");
  if (parse) return parse(pk);
  return { ...(pk as object), challenge: fromBase64url(String(pk.challenge)), allowCredentials: withIds(pk.allowCredentials) } as PublicKeyCredentialRequestOptions;
}

// toJSON is the browser's answer as the API takes it (the WebAuthn JSON form), with the browser's
// own serialiser when it has one.
export function toJSON(c: PublicKeyCredential): Json {
  const own = (c as unknown as { toJSON?: () => Json }).toJSON;
  if (typeof own === "function") return own.call(c);
  const r = c.response as AuthenticatorResponse & Partial<AuthenticatorAttestationResponse & AuthenticatorAssertionResponse>;
  const response: Json = { clientDataJSON: toBase64url(r.clientDataJSON) };
  if (r.attestationObject) {
    response.attestationObject = toBase64url(r.attestationObject);
    if (typeof r.getTransports === "function") response.transports = r.getTransports();
  }
  if (r.authenticatorData) response.authenticatorData = toBase64url(r.authenticatorData);
  if (r.signature) response.signature = toBase64url(r.signature);
  if (r.userHandle) response.userHandle = toBase64url(r.userHandle);
  return {
    id: c.id,
    rawId: toBase64url(c.rawId),
    type: c.type,
    response,
    clientExtensionResults: c.getClientExtensionResults(),
    authenticatorAttachment: c.authenticatorAttachment ?? undefined,
  };
}

// create asks the authenticator for a new passkey; get asks one to answer a challenge. Each returns
// the answer as JSON, or throws when the person cancels or the browser refuses.
export async function create(options: Json): Promise<Json> {
  const c = await navigator.credentials.create({ publicKey: creationOptions(options) });
  if (!c) throw new Error("No passkey was created.");
  return toJSON(c as PublicKeyCredential);
}

export async function get(options: Json): Promise<Json> {
  const c = await navigator.credentials.get({ publicKey: requestOptions(options) });
  if (!c) throw new Error("No passkey answered.");
  return toJSON(c as PublicKeyCredential);
}

// cancelled says whether a ceremony ended because the person cancelled it or let it time out
// (the browser does not say which), so the page says so instead of reporting an error.
export function cancelled(e: unknown): boolean {
  return e instanceof DOMException && (e.name === "NotAllowedError" || e.name === "AbortError");
}
