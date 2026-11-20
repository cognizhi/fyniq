import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { components } from "../../api/schema";
import { answer, ownerSession, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { IntegrationTasksToDo } from "./IntegrationTasks";

type Task = components["schemas"]["IntegrationTask"];

const letter: Task = {
  id: "it-1",
  client_id: "jira",
  external_ref: "OPS-142",
  title: "Chase the signed engagement letter",
  detail: "Raised from OPS-142.",
  due_on: "2026-12-15",
  link_url: "https://example.atlassian.net/browse/OPS-142",
  status: "open",
  created_at: "2026-11-20T01:00:00Z",
  updated_at: "2026-11-20T01:00:00Z",
  version: 2,
};
const early: Task = { ...letter, id: "it-2", external_ref: "OPS-7", title: "Renew the lease", due_on: "2026-11-30", link_url: undefined, version: 1 };

// req: FR-INT-24
describe("the Inbox's tasks from other systems", () => {
  it("lists the open tasks, the earliest due first, with the system's reference and its link, and axe finds nothing", async () => {
    answer({ "GET /v1/tasks": { status: 200, body: { items: [letter, early] } } });
    const { container } = renderPage(IntegrationTasksToDo, { path: "/inbox" });
    const items = within(await screen.findByRole("list", { name: "Tasks from other systems" })).getAllByRole("listitem");
    expect(items.map((i) => i.querySelector(".fy-list__title")?.textContent)).toEqual(["Renew the lease", "Chase the signed engagement letter"]);
    expect(items[1]?.textContent).toContain("from jira, reference OPS-142");
    const link = within(items[1]!).getByRole("link", { name: /Open in the other system/ }) as HTMLAnchorElement;
    expect(link.href).toBe("https://example.atlassian.net/browse/OPS-142");
    expect(link.rel).toContain("noopener");
    await noViolations(container);
  });

  it("finishes a task over the version it shows, and an Owner or Administrator may, a Controller may not", async () => {
    const seen = answer({
      "GET /v1/tasks": { status: 200, body: { items: [letter] } },
      "POST /v1/tasks/it-1/close": { status: 200, body: { ...letter, status: "done", version: 3 } },
    });
    renderPage(IntegrationTasksToDo, { path: "/inbox" });
    fireEvent.click(await screen.findByRole("button", { name: "Mark done: Chase the signed engagement letter" }));
    await waitFor(() => {
      const done = seen.find((s) => s.path === "/v1/tasks/it-1/close");
      expect(done?.body).toEqual({ status: "done" });
      expect(done?.headers.get("If-Match")).toBe('"2"');
      expect(done?.headers.get("Idempotency-Key")).toBeTruthy();
    });
  });

  it("cancels a task, and a Controller sees the task without the buttons", async () => {
    const seen = answer({ "GET /v1/tasks": { status: 200, body: { items: [letter] } }, "POST /v1/tasks/it-1/close": { status: 200, body: { ...letter, status: "cancelled", version: 3 } } });
    const view = renderPage(IntegrationTasksToDo, { path: "/inbox" });
    fireEvent.click(await screen.findByRole("button", { name: "Cancel the task: Chase the signed engagement letter" }));
    await waitFor(() => expect(seen.find((s) => s.path === "/v1/tasks/it-1/close")?.body).toEqual({ status: "cancelled" }));
    view.unmount();
    renderPage(IntegrationTasksToDo, { path: "/inbox", session: { ...ownerSession, roles: ["controller"] } });
    expect(await screen.findByText("Chase the signed engagement letter")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Mark done/ })).toBeNull();
  });

  it("shows nothing to those who may not see the tasks", async () => {
    answer({ "GET /v1/tasks": problem(403, "API-AUTHZ-01") });
    const { container } = renderPage(IntegrationTasksToDo, { path: "/inbox" });
    await waitFor(() => expect(container.querySelector("section")).toBeNull());
  });
});
