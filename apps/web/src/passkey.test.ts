import { describe, expect, it } from "vitest";
import { creationOptions, fromBase64url, requestOptions, toBase64url, toJSON } from "./passkey";

const bytes = (b: ArrayBuffer | BufferSource) => Array.from(new Uint8Array(b as ArrayBuffer));

// req: FR-ADM-02
// req: SEC-01
describe("the browser's half of a passkey ceremony", () => {
  it("reads and writes base64url without padding", () => {
    expect(toBase64url(new Uint8Array([251, 255, 0, 1]))).toBe("-_8AAQ");
    expect(bytes(fromBase64url("-_8AAQ"))).toEqual([251, 255, 0, 1]);
    expect(toBase64url(fromBase64url("AQIDBAU"))).toBe("AQIDBAU");
  });

  it("decodes the API's options when the browser has no JSON parser", () => {
    const create = creationOptions({
      publicKey: { challenge: "AQID", rp: { id: "localhost", name: "Fyniq" }, user: { id: "BAU", name: "ann", displayName: "Ann" }, excludeCredentials: [{ type: "public-key", id: "Bgc" }] },
    });
    expect(bytes(create.challenge)).toEqual([1, 2, 3]);
    expect(bytes(create.user.id)).toEqual([4, 5]);
    expect(bytes(create.excludeCredentials?.[0]?.id as ArrayBuffer)).toEqual([6, 7]);
    expect(create.rp.id).toBe("localhost");
    const get = requestOptions({ publicKey: { challenge: "CAk", rpId: "localhost", allowCredentials: [{ type: "public-key", id: "Cg" }] } });
    expect(bytes(get.challenge)).toEqual([8, 9]);
    expect(bytes(get.allowCredentials?.[0]?.id as ArrayBuffer)).toEqual([10]);
    expect(() => requestOptions({})).toThrow(/publicKey/);
  });

  it("writes the answer as the API takes it when the browser has no toJSON", () => {
    const enc = (s: string) => new TextEncoder().encode(s).buffer;
    const answer = {
      id: "a2V5",
      rawId: enc("key"),
      type: "public-key",
      authenticatorAttachment: "platform",
      getClientExtensionResults: () => ({}),
      response: { clientDataJSON: enc("{}"), authenticatorData: enc("ad"), signature: enc("sig"), userHandle: enc("u") },
    } as unknown as PublicKeyCredential;
    expect(toJSON(answer)).toEqual({
      id: "a2V5",
      rawId: "a2V5",
      type: "public-key",
      authenticatorAttachment: "platform",
      clientExtensionResults: {},
      response: { clientDataJSON: "e30", authenticatorData: "YWQ", signature: "c2ln", userHandle: "dQ" },
    });
  });
});
