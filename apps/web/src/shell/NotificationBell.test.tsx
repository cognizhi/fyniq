import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { Banners } from "./Banners";
import { NotificationBell } from "./NotificationBell";

const approval = {
  id: "n-1", type: "approval.requested", label: "An approval waits for you", category: "approvals", severity: "info", actionable: true,
  title: "Post journal JNL-0042: waits for your approval", link: "/approvals/a-1", read: false, archived: false, created_at: "2026-10-24T01:00:00Z",
};
const roles = { ...approval, id: "n-2", type: "security.roles", label: "Your roles changed", category: "security", severity: "warning", actionable: false, title: "Your roles changed: auditor assigned", link: "/settings/profile", read: true };

// req: FR-NTF-01
describe("the notification bell", () => {
  it("counts the unread, opens the centre, marks one read and all read, and axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/notifications": (_b, req) => ({ status: 200, body: { items: new URL(req.url).searchParams.get("unread") ? [approval] : [approval, roles], unread: 1 } }),
      "POST /v1/notifications/n-1/read": { status: 200, body: { ...approval, read: true } },
      "POST /v1/notifications/read-all": { status: 200, body: { marked: 1 } },
    });
    renderPage(NotificationBell);
    const bell = await screen.findByRole("button", { name: "Notifications, 1 unread" });
    expect(screen.getByRole("status").textContent).toBe("1 unread notification");
    fireEvent.click(bell);
    const sheet = await screen.findByRole("dialog");
    expect(await within(sheet).findByText("Post journal JNL-0042: waits for your approval")).toBeTruthy();
    expect(within(sheet).getByText("It waits for you in your Inbox.")).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(within(sheet).getByRole("button", { name: "Mark read: Post journal JNL-0042: waits for your approval" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/notifications/n-1/read"));
    fireEvent.click(within(sheet).getByRole("tab", { name: "All" }));
    expect(await within(sheet).findByText("Your roles changed: auditor assigned")).toBeTruthy();
    fireEvent.click(within(sheet).getByRole("button", { name: "Mark all read" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/notifications/read-all"));
    expect(seen.find((s) => s.path === "/v1/notifications/read-all")?.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("shows no count when everything is read", async () => {
    answer({ "GET /v1/notifications": { status: 200, body: { items: [], unread: 0 } } });
    renderPage(NotificationBell);
    expect(await screen.findByRole("button", { name: "Notifications" })).toBeTruthy();
  });
});

const info = { id: "b-1", scope: "organisation", message: "The office is closed on 3 November.", link: "/help/organisation-settings", severity: "info", dismissible: true, ends_at: "2026-11-03T16:00:00Z" };
const critical = { id: "b-2", scope: "installation", message: "Fyniq is read-only until 23:30.", link: "https://status.example.com", severity: "critical", dismissible: false, ends_at: "2026-10-24T15:30:00Z" };

// req: FR-NTF-03
describe("the banners", () => {
  it("shows critical first as an alert, the rest politely, dismisses one, says an outside link opens in a new tab, and axe finds nothing", async () => {
    answer({
      "GET /v1/banners": { status: 200, body: { items: [critical, info] } },
      "POST /v1/banners/b-1/dismiss": { status: 200, body: { items: [critical] } },
    });
    renderPage(Banners);
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Fyniq is read-only until 23:30.");
    expect(within(alert).getByRole("link", { name: /opens in a new tab/ }).getAttribute("rel")).toBe("noopener noreferrer");
    expect(within(alert).queryByRole("button")).toBeNull();
    const polite = screen.getByRole("status");
    expect(within(polite).getByRole("link", { name: "Details" }).getAttribute("href")).toBe("/help/organisation-settings");
    await noViolations(document.body);
    fireEvent.click(within(polite).getByRole("button", { name: "Dismiss: The office is closed on 3 November." }));
    await waitFor(() => expect(calls()).toContain("POST /v1/banners/b-1/dismiss"));
  });

  it("renders nothing without banners", async () => {
    answer({ "GET /v1/banners": { status: 200, body: { items: [] } } });
    const { container } = renderPage(Banners);
    await waitFor(() => expect(calls()).toContain("GET /v1/banners"));
    expect(container.querySelector("section")).toBeNull();
  });
});
