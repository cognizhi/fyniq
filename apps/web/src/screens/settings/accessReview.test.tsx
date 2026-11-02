import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import type { Session } from "../../api/client";
import { AccessReview } from "./AccessReview";

const ann = {
  member_id: "m-1",
  user_id: ownerSession.user_id,
  display_name: "Ann Tan",
  roles: [{ role: "administrator" }, { role: "controller" }, { role: "owner" }],
  expired_roles: [],
  external: false,
  past_expiry: false,
  joined_at: "2026-09-02T01:00:00Z",
  last_sign_in: "2026-10-05T00:41:00Z",
  second_factor: "passkey",
  dormant: false,
  conflicts: [{ sod: "SOD-6", roles: ["controller", "owner", "administrator"], covered: "founder_mode" }],
};
const joanne = {
  ...ann,
  member_id: "m-2",
  user_id: "u-2",
  display_name: "Joanne Lee",
  roles: [],
  expired_roles: [{ role: "company_secretary", expires_at: "2026-09-30T15:59:59Z" }],
  external: true,
  past_expiry: true,
  second_factor: "sign_in",
  conflicts: [],
};
const q3 = { quarter: "2026-07-01", label: "Q3 2026", due_by: "2026-10-14" };
const review = {
  as_at: "2026-10-05T02:00:00Z",
  team_mode: "founder",
  members: [ann, joanne],
  invitations: [],
  connectors: [{ id: "c-1", connector: "wise", name: "Wise", state: "enabled", health: "ok", fyniq_scopes: ["fyniq.read.banking"], data_classes: [], installed_by: "user:u-1", installed_at: "2026-10-04T00:00:00Z" }],
  to_attest: q3,
  overdue: q3,
  attestations: [],
};

function page(extra = {}) {
  return answer({ "GET /v1/access-review": { status: 200, body: review, headers: { ETag: '"ab12"' } }, ...extra });
}

// req: FR-CTL-06
// req: SEC-06
describe("Access review", () => {
  it("shows who can do what, what to review, and the quarter to attest, and axe finds nothing", async () => {
    page();
    renderPage(AccessReview, { path: "/settings/access-review" });
    expect(await screen.findByText("Q3 2026 is not attested")).toBeTruthy();
    const people = screen.getByRole("table", { name: "People and their access" });
    expect(within(people).getByText(/^Ended 30 Sept? 2026, still a member$/)).toBeTruthy();
    expect(within(people).getByText("Role conflict")).toBeTruthy();
    expect(within(people).getByText("Company secretary (ended)")).toBeTruthy();
    expect(screen.getByText(/Founder mode allows it/)).toBeTruthy();
    expect(screen.getByText("Healthy")).toBeTruthy();
    await noViolations(document.body);
  });

  it("attests the quarter over the version read, with the note", async () => {
    const seen = page({ "POST /v1/access-review/attestations": { status: 200, body: { ...review, to_attest: undefined, overdue: undefined }, headers: { ETag: '"cd34"' } } });
    renderPage(AccessReview, { path: "/settings/access-review" });
    fireEvent.change(await screen.findByLabelText(/What you checked/), { target: { value: "Asked to remove Joanne" } });
    fireEvent.click(screen.getByRole("button", { name: "Attest Q3 2026" }));
    await screen.findByRole("button", { name: "Attest Q3 2026" });
    const post = seen.find((s) => s.method === "POST");
    expect(post?.headers.get("If-Match")).toBe('"ab12"');
    expect(post?.body).toEqual({ quarter: "2026-07-01", note: "Asked to remove Joanne" });
  });

  it("lets only an Owner attest", async () => {
    page();
    renderPage(AccessReview, { path: "/settings/access-review", session: { ...ownerSession, roles: ["controller"] } as Session });
    expect(await screen.findByText("Only an Owner can attest")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Attest Q3 2026" }) as HTMLButtonElement).disabled).toBe(true);
  });
});
