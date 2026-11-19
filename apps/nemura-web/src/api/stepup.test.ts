import { describe, expect, it } from "vitest";
import { answer, bodies, calls } from "../test/setup";
import { confirmWithPasskey } from "./stepup";

const bytes = (...n: number[]): ArrayBuffer => new Uint8Array(n).buffer;

// req: NM-ARC-06
describe("confirming the session with a passkey", () => {
  it("passes the API's challenge to the authenticator and sends back its answer, base64url, then reports success", async () => {
    answer("POST /v1/session/step-up", { status: 200, body: { publicKey: { challenge: "AQID", allowCredentials: [{ id: "BAUG", type: "public-key" }], userVerification: "required" } } });
    let seen: PublicKeyCredentialRequestOptions | undefined;
    Object.defineProperty(globalThis.navigator, "credentials", {
      configurable: true,
      value: {
        get: async ({ publicKey }: { publicKey: PublicKeyCredentialRequestOptions }) => {
          seen = publicKey;
          return { id: "cred", rawId: bytes(7, 8), type: "public-key", response: { authenticatorData: bytes(1), clientDataJSON: bytes(2), signature: bytes(3), userHandle: null } };
        },
      },
    });
    answer("POST /v1/session/step-up/confirm", { status: 204 });
    expect(await confirmWithPasskey()).toBeUndefined();
    expect(new Uint8Array(seen?.challenge as ArrayBuffer)).toEqual(new Uint8Array([1, 2, 3]));
    expect(new Uint8Array((seen?.allowCredentials?.[0]?.id as ArrayBuffer) ?? new ArrayBuffer(0))).toEqual(new Uint8Array([4, 5, 6]));
    expect(bodies.get("POST /v1/session/step-up/confirm")).toEqual({ credential: { id: "cred", rawId: "Bwg", type: "public-key", response: { authenticatorData: "AQ", clientDataJSON: "Ag", signature: "Aw", userHandle: null } } });
    expect(calls).toContain("POST /v1/session/step-up/confirm");
  });

  it("returns the API's words when there is no passkey, and sends nothing", async () => {
    answer("POST /v1/session/step-up", { status: 409, body: { code: "NM_NO_PASSKEY", detail: "This person has registered no passkey to confirm with." } });
    expect(await confirmWithPasskey()).toBe("This person has registered no passkey to confirm with.");
    expect(calls).not.toContain("POST /v1/session/step-up/confirm");
  });
});
