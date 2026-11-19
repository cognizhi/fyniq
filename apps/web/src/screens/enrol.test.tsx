import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { Enrol } from "./Enrol";

const key = (id: string, revoked?: string) => ({ id, name: `Key ${id}`, authenticator: "0".repeat(8), backed_up: false, backup_eligible: false, created_at: "2026-10-01T02:00:00Z", transports: ["internal"], revoked_at: revoked });

// req: SEC-01
// req: SEC-35
describe("the enrolment page", () => {
  it("walks through a passkey, the recovery codes and a second authenticator, each done from what the API holds; axe finds nothing", async () => {
    answer({
      "GET /v1/session/passkeys": { status: 200, body: { items: [key("a")] } },
      "GET /v1/session/recovery-codes": { status: 200, body: { unused: 0 } },
    });
    renderPage(Enrol, { path: "/sign-in/enrol" });
    const steps = await screen.findByRole("list", { name: "Steps" });
    const step = (n: number) => within(steps).getByRole("heading", { level: 2, name: new RegExp(`^Step ${n}`) }).closest("li")!;
    await within(step(1)).findByText("Done");
    expect(within(step(2)).getByText("To do")).toBeTruthy();
    expect(within(step(3)).getByText("To do")).toBeTruthy();
    expect(screen.getByRole("heading", { level: 2, name: "Not finished yet" })).toBeTruthy();
    await noViolations(document.body);
  });

  it("says the person is set up once a passkey and recovery codes are held, and marks a second passkey done", async () => {
    answer({
      "GET /v1/session/passkeys": { status: 200, body: { items: [key("a"), key("b"), key("c", "2026-10-02T02:00:00Z")] } },
      "GET /v1/session/recovery-codes": { status: 200, body: { unused: 10 } },
    });
    renderPage(Enrol, { path: "/sign-in/enrol" });
    expect(await screen.findByRole("heading", { level: 2, name: "You are set up" })).toBeTruthy();
    const steps = screen.getByRole("list", { name: "Steps" });
    expect(within(steps).getAllByText("Done")).toHaveLength(3); // the revoked passkey does not count
  });
});
