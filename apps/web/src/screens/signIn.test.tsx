import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { go } from "../route";
import { answer } from "../test/api";
import { noViolations } from "../test/axe";
import { SignIn } from "./SignIn";

// go replaces the page, which jsdom cannot do: the tests read where it would go.
vi.mock("../route", async (original) => ({ ...(await original<Record<string, unknown>>()), go: vi.fn() }));

const code = "ABCDE-FGHIJ-KLMNO-PQRST";
const signedIn = { "GET /v1/setup": { status: 200, body: { done: true } }, "GET /v1/session": { status: 200, body: { provider: "Google" } } };

const accept = async () => {
  fireEvent.change(await screen.findByLabelText("Invitation code"), { target: { value: code } });
  fireEvent.change(screen.getByLabelText("Your name"), { target: { value: "Wei Ling Tan" } });
  fireEvent.click(screen.getByRole("button", { name: "Accept the invitation" }));
};

// req: FR-ADM-01
// req: FR-ADM-06
describe("joining by invitation", () => {
  beforeEach(() => vi.mocked(go).mockClear());

  it("joins the company that invited a person signed in without an account, and opens Fyniq; axe finds nothing", async () => {
    const seen = answer({ ...signedIn, "POST /v1/members": { status: 201, body: { organisation_id: "o1", user_id: "u1", member_id: "m1", roles: ["owner"] } } });
    render(<SignIn />);
    expect(await screen.findByRole("heading", { name: "Joining by invitation" })).toBeTruthy();
    expect(screen.getByText(/to join a company's books/)).toBeTruthy();
    await noViolations(document.body);
    await accept();
    await waitFor(() => expect(go).toHaveBeenCalledWith("/"));
    expect(seen.find((s) => s.path === "/v1/members")!.body).toEqual({ invitation_code: code, display_name: "Wei Ling Tan" });
    expect(seen.some((s) => s.path === "/v1/super-admins")).toBe(false);
  });

  it("takes a Super admin's invitation the company's does not know, and shows the recovery codes", async () => {
    const refused = { status: 422, body: { code: "API-SUPERADMIN-01", detail: "The code is not valid." } };
    const seen = answer({ ...signedIn, "POST /v1/members": refused, "POST /v1/super-admins": { status: 201, body: { recovery_codes: ["aaaa-bbbb"] } } });
    render(<SignIn />);
    await accept();
    expect(await screen.findByText("aaaa-bbbb")).toBeTruthy();
    expect(seen.find((s) => s.path === "/v1/super-admins")!.body).toEqual({ invitation_code: code, display_name: "Wei Ling Tan" });
    expect(go).not.toHaveBeenCalled();
  });

  it("says a code neither knows is not valid, and links the message to the field", async () => {
    const refused = { status: 422, body: { code: "API-SUPERADMIN-01", detail: "The code is not valid." } };
    answer({ ...signedIn, "POST /v1/members": refused, "POST /v1/super-admins": refused });
    render(<SignIn />);
    await accept();
    const message = await screen.findByRole("link", { name: /That invitation code is not valid/ });
    expect(message.getAttribute("href")).toBe("#invite-code");
    expect(screen.getByLabelText("Invitation code").getAttribute("aria-invalid")).toBe("true");
  });
});
