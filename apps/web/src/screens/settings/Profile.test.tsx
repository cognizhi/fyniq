import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { authenticator, credentialJSON } from "../../test/webauthn";
import { Profile } from "./Profile";

const laptop = { id: "k-1", name: "Work laptop", authenticator: "00000000-0000-0000-0000-000000000000", backed_up: true, backup_eligible: true, created_at: "2026-10-01T02:00:00Z", last_used_at: "2026-10-20T01:00:00Z", transports: ["internal"] };
const revoked = { ...laptop, id: "k-0", name: "Old phone", revoked_at: "2026-10-02T02:00:00Z" };
const here = { id: "d-1", current: true, browser: "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_6) AppleWebKit/537.36 Chrome/141.0 Safari/537.36", provider: "local", signed_in_at: "2026-10-20T01:00:00Z", last_seen_at: "2026-10-20T02:00:00Z", expires_at: "2026-10-20T13:00:00Z", step_up_method: "sign_in" };
const phone = { ...here, id: "d-2", current: false, browser: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) Version/18.0 Mobile Safari/604.1" };

function page(extra = {}) {
  return answer({
    "GET /v1/session/passkeys": { status: 200, body: { items: [laptop, revoked] } },
    "GET /v1/session/devices": { status: 200, body: { items: [here, phone] } },
    ...extra,
  });
}

// req: FR-ADM-02
// req: FR-ADM-03
// req: SEC-01
describe("Profile and security", () => {
  it("lists your live passkeys and your devices, and axe finds nothing", async () => {
    page();
    renderPage(Profile, { path: "/settings/profile" });
    const keys = await screen.findByRole("list", { name: "Passkeys" });
    expect(within(keys).getAllByRole("listitem")).toHaveLength(1);
    expect(within(keys).getByText("Work laptop")).toBeTruthy();
    const devices = await screen.findByRole("list", { name: "Your devices" });
    expect(within(devices).getByText("Chrome on macOS")).toBeTruthy();
    expect(within(devices).getByText("This device")).toBeTruthy();
    expect(within(devices).getByRole("button", { name: /Sign out Safari on iOS/ })).toBeTruthy();
    await noViolations(document.body);
  });

  it("adds a passkey: a challenge, the device's answer, then the API's check", async () => {
    const credentials = authenticator();
    const seen = page({
      "POST /v1/session/passkeys/challenge": { status: 200, body: { challenge_id: "c-9", expires_at: "2026-10-20T06:05:00Z", options: { publicKey: { challenge: "AQID", user: { id: "BAU", name: "a", displayName: "A" }, rp: { id: "localhost", name: "Fyniq" } } } } },
      "POST /v1/session/passkeys": { status: 201, body: { ...laptop, id: "k-2", name: "Desk" } },
    });
    renderPage(Profile, { path: "/settings/profile" });
    fireEvent.click(await screen.findByRole("button", { name: "Add a passkey" }));
    const dialog = await screen.findByRole("dialog");
    const name = within(dialog).getByLabelText("Name");
    fireEvent.change(name, { target: { value: "Desk" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the passkey" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/session/passkeys"));
    expect(credentials.create).toHaveBeenCalledOnce();
    expect(seen.find((s) => s.path === "/v1/session/passkeys" && s.method === "POST")?.body).toEqual({ challenge_id: "c-9", credential: credentialJSON, name: "Desk" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("asks you to confirm it's you when adding needs a recent second factor", async () => {
    authenticator();
    page({ "POST /v1/session/passkeys/challenge": problem(403, "API-AUTHZ-05", "Step-up required.") });
    renderPage(Profile, { path: "/settings/profile" });
    fireEvent.click(await screen.findByRole("button", { name: "Add a passkey" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Add the passkey" }));
    expect(await screen.findByRole("heading", { name: "This needs a second factor" })).toBeTruthy();
  });

  it("refuses an empty name before asking the device", async () => {
    const credentials = authenticator();
    page();
    renderPage(Profile, { path: "/settings/profile" });
    fireEvent.click(await screen.findByRole("button", { name: "Add a passkey" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "  " } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the passkey" }));
    expect(await within(dialog).findByText(/1 to 100 characters/)).toBeTruthy();
    expect(within(dialog).getByLabelText("Name").getAttribute("aria-invalid")).toBe("true");
    expect(credentials.create).not.toHaveBeenCalled();
  });

  it("revokes a passkey after you confirm", async () => {
    page({ "DELETE /v1/session/passkeys/k-1": { status: 200, body: { ...laptop, revoked_at: "2026-10-20T03:00:00Z" } } });
    renderPage(Profile, { path: "/settings/profile" });
    fireEvent.click(await screen.findByRole("button", { name: "Revoke Work laptop" }));
    const dialog = await screen.findByRole("dialog", { name: "Revoke Work laptop?" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Revoke" }));
    await waitFor(() => expect(calls()).toContain("DELETE /v1/session/passkeys/k-1"));
  });

  it("signs out one device, or every other one", async () => {
    page({
      "DELETE /v1/session/devices/d-2": { status: 200, body: {} },
      "POST /v1/session/devices/sign-out-others": { status: 200, body: { ended: 1 } },
    });
    renderPage(Profile, { path: "/settings/profile" });
    fireEvent.click(await screen.findByRole("button", { name: /Sign out Safari on iOS/ }));
    expect(await screen.findByText("Safari on iOS is signed out.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Sign out everywhere else" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Sign out everywhere else" }));
    expect(await screen.findByText("1 other session is signed out.")).toBeTruthy();
    expect(calls()).toContain("POST /v1/session/devices/sign-out-others");
  });
});
