import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { EmployeeRecord } from "./EmployeeRecord";

const owner = { ...ownerSession, roles: ["owner"] } as Session;
const ana = { person_id: "p1", display_name: "Ana Lim", status: "active", held: true, removed: false, department_id: "d1" };

// req: FR-PPL-06
// part of FR-PPL-13, FR-PPL-14 (the page; the hold's effect on payments is the API's)
describe("a person from Nemura", () => {
  it("shows what Nemura told Fyniq, a hold in words, and maps the person to a user chosen by a person; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/people-link/people": { status: 200, body: { items: [{ person_id: "p0", display_name: "Bo Tan", held: false, removed: false }, ana] } },
      "GET /v1/members": { status: 200, body: { items: [{ user_id: "u1", display_name: "Ana Lim", email: "ana@example.test", removed: false }, { user_id: "u2", display_name: "Gone", removed: true }] } },
      "POST /v1/people-link/people/p1/user": { status: 200, body: { person_id: "p1", user_id: "u1" } },
    });
    renderPage(EmployeeRecord, { path: "/payroll/employees/p1", route: "/payroll/employees/$personId", session: owner });
    expect(await screen.findByText("Held for tax clearance.")).toBeTruthy();
    expect(screen.getByText(/Not mapped to a Fyniq user yet/)).toBeTruthy();
    await noViolations(document.body);
    const form = screen.getByRole("form", { name: "Map to a Fyniq user" });
    const pick = within(form).getByLabelText("Fyniq user") as HTMLSelectElement;
    await waitFor(() => expect(within(pick).getAllByRole("option").length).toBe(2)); // a removed member is not offered
    const submit = within(form).getByRole("button", { name: "Map to this user" }) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    fireEvent.change(pick, { target: { value: "u1" } });
    fireEvent.click(submit);
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/people-link/people/p1/user")).toBe(true));
    expect(seen.find((s) => s.method === "POST")!.body).toEqual({ user_id: "u1" });
    expect(await screen.findByText(/Ana Lim is now this Fyniq user/)).toBeTruthy();
  });

  it("says so when Nemura never told Fyniq about the person", async () => {
    answer({ "GET /v1/people-link/people": { status: 200, body: { items: [] } } });
    renderPage(EmployeeRecord, { path: "/payroll/employees/p9", route: "/payroll/employees/$personId", session: owner });
    expect(await screen.findByText(/Nemura has not told Fyniq about this person/)).toBeTruthy();
  });
});
