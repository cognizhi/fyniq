import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Announcements } from "./Announcements";
import { Notifications } from "./Notifications";

const types = [
  { type: "approval.requested", label: "An approval waits for you", category: "approvals", email: "daily", recommended: "immediate", locked: false, etag: '"3"' },
  { type: "bills.due", label: "Bills to pay soon", category: "operations", email: "daily", recommended: "daily", locked: false, etag: '"none"' },
  { type: "security.sign_in", label: "You signed in", category: "security", email: "off", recommended: "off", locked: true, etag: '"none"',
    note: "Every sign-in is emailed at once by the sign-in alert, so it is not emailed again here" },
  { type: "security.roles", label: "Your roles changed", category: "security", email: "immediate", recommended: "immediate", locked: true, etag: '"none"' },
];

function page(extra = {}) {
  return answer({
    "GET /v1/notification-preferences": { status: 200, body: { types, quiet_hours_etag: '"none"' } },
    ...extra,
  });
}

// req: FR-NTF-02
// req: FR-NTF-05
describe("Notification preferences", () => {
  it("groups each kind with its email choice against the recommendation, locks security notices, and axe finds nothing", async () => {
    page();
    renderPage(Notifications, { path: "/settings/notifications" });
    const approvals = await screen.findByRole("list", { name: "Approvals" });
    expect(within(approvals).getByText("Changed from recommended")).toBeTruthy();
    expect(within(approvals).getByText("Recommended: immediately")).toBeTruthy();
    const security = screen.getByRole("list", { name: "Security: always on" });
    expect(within(security).getAllByText("Always on")).toHaveLength(2);
    expect(within(security).getByText("Not by email here")).toBeTruthy();
    expect(within(security).getByText("Email, immediately")).toBeTruthy();
    expect(within(security).queryByRole("combobox")).toBeNull();
    await noViolations(document.body);
  });

  it("changes a kind's email over its ETag, resets it, and sets quiet hours", async () => {
    const seen = page({
      "PUT /v1/notification-preferences/approval.requested": { status: 200, body: { types, quiet_hours_etag: '"none"' } },
      "PUT /v1/notification-quiet-hours": { status: 200, body: { types, quiet_hours: { from: "22:00", to: "07:00" }, quiet_hours_etag: '"1"' } },
    });
    renderPage(Notifications, { path: "/settings/notifications" });
    const approvals = await screen.findByRole("list", { name: "Approvals" });
    fireEvent.change(within(approvals).getByLabelText("Email"), { target: { value: "weekly" } });
    await waitFor(() => expect(calls()).toContain("PUT /v1/notification-preferences/approval.requested"));
    const put = seen.find((s) => s.method === "PUT");
    expect(put?.headers.get("If-Match")).toBe('"3"');
    expect(put?.body).toEqual({ email: "weekly" });
    fireEvent.click(within(approvals).getByRole("button", { name: "Reset to recommended" }));
    await waitFor(() => expect(seen.filter((s) => s.method === "PUT")).toHaveLength(2));
    expect(seen.filter((s) => s.method === "PUT")[1]?.body).toEqual({ email: "immediate" });
    fireEvent.click(screen.getByRole("switch", { name: "Hold email during quiet hours" }));
    fireEvent.click(screen.getByRole("button", { name: "Save quiet hours" }));
    await waitFor(() => expect(calls()).toContain("PUT /v1/notification-quiet-hours"));
    expect(seen.find((s) => s.path === "/v1/notification-quiet-hours")?.body).toEqual({ from: "22:00", to: "07:00" });
  });

  it("sends a test notification and says how it goes by email", async () => {
    page({ "POST /v1/notifications/test-messages": { status: 202, body: { event_id: "e-1", title: "A test notification you sent at 10:05 Singapore time", email: "immediate" } } });
    renderPage(Notifications, { path: "/settings/notifications" });
    fireEvent.click(await screen.findByRole("button", { name: "Send me a test notification" }));
    expect(await screen.findByText(/goes by email at once/)).toBeTruthy();
  });
});

const live = { id: "an-1", message: "Closed on 3 November.", severity: "info", roles: [], starts_at: "2026-10-24T01:00:00Z", ends_at: "2026-11-03T16:00:00Z",
  dismissible: true, status: "live", published_by: "user:u-1", published_at: "2026-10-24T01:00:00Z" };
const ended = { ...live, id: "an-2", message: "The bank feed was down.", severity: "warning", roles: ["owner"], status: "ended", withdrawn_at: "2026-10-24T02:00:00Z" };

// req: FR-NTF-03
describe("Announcements", () => {
  it("lists each with its status, publishes a new one to chosen roles, ends a live one, and axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/announcements": { status: 200, body: { items: [live, ended] } },
      "POST /v1/announcements": { status: 201, body: { ...live, id: "an-3", message: "Submit September claims by 20 Oct.", roles: ["employee"] } },
      "POST /v1/announcements/an-1/withdraw": { status: 200, body: { ...live, status: "ended" } },
    });
    renderPage(Announcements, { path: "/settings/announcements" });
    expect(await screen.findByText("The bank feed was down.")).toBeTruthy();
    expect(screen.getByText("Owner")).toBeTruthy();
    await noViolations(document.body);
    fireEvent.change(screen.getByLabelText(/^Message/), { target: { value: "Submit September claims by 20 Oct." } });
    fireEvent.click(screen.getByRole("radio", { name: "Chosen roles" }));
    fireEvent.click(screen.getByRole("button", { name: "Employee" }));
    fireEvent.click(screen.getByRole("radio", { name: /Critical/ }));
    expect(screen.getByRole("switch", { name: "People can dismiss it" }).getAttribute("aria-checked")).toBe("false");
    fireEvent.click(screen.getByRole("radio", { name: /Info/ }));
    fireEvent.change(screen.getByLabelText(/^Ends/), { target: { value: "2026-10-31T18:00" } });
    fireEvent.click(screen.getByRole("button", { name: "Publish" }));
    expect(await screen.findByText("Announcement published")).toBeTruthy();
    const post = seen.find((s) => s.method === "POST" && s.path === "/v1/announcements");
    expect(post?.body).toEqual({ message: "Submit September claims by 20 Oct.", severity: "info", ends_at: "2026-10-31T10:00:00.000Z", dismissible: true, roles: ["employee"] });
    expect(post?.headers.get("Idempotency-Key")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "End now: Closed on 3 November." }));
    await waitFor(() => expect(calls()).toContain("POST /v1/announcements/an-1/withdraw"));
  });

  it("only lists them for the Auditor, who does not publish", async () => {
    answer({ "GET /v1/announcements": { status: 200, body: { items: [ended] } } });
    renderPage(Announcements, { path: "/settings/announcements", session: { provider: "local", roles: ["auditor"], super_admin: false, step_up_method: "sign_in", features: {} } as never });
    expect(await screen.findByText("The bank feed was down.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Publish" })).toBeNull();
  });
});
