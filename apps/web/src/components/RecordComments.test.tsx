import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, problem } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { RecordComments } from "./RecordComments";

const comment = { id: "c1", record_type: "bill", record_id: "b1", author: { kind: "user", id: "u1", name: "Ah Kow" }, body: "The GST on this line looks high.", mentions: [], created_at: "2026-11-20T01:30:00Z" };
const thread = { "GET /v1/records/bill/b1/comments": { status: 200, body: { items: [comment] } } };
const members = (query: string) => (query.includes("q=we") ? [{ id: "u2", name: "Wei Ling" }] : [{ id: "u2", name: "Wei Ling" }, { id: "u3", name: "Mei Lim" }]);

function page() {
  return renderPage(() => <RecordComments recordType="bill" recordId="b1" />, { path: "/purchases/bills/b1" });
}

// req: FR-UX-13
describe("RecordComments", () => {
  it("shows the thread and says when it is empty, and axe finds nothing", async () => {
    answer(thread);
    page();
    const list = await screen.findByRole("list", { name: "Comments" });
    expect(within(list).getByText("Ah Kow")).toBeTruthy();
    expect(within(list).getByText("The GST on this line looks high.")).toBeTruthy();
    await noViolations(document.body);
  });

  it("says there are no comments yet", async () => {
    answer({ "GET /v1/records/bill/b1/comments": { status: 200, body: { items: [] } } });
    page();
    expect(await screen.findByText(/No comments yet/)).toBeTruthy();
  });

  it("offers only the members the API names while typing @, mentions the one chosen, and adds the comment", async () => {
    const seen = answer({
      ...thread,
      "GET /v1/records/bill/b1/mentionable": (_b, req) => ({ status: 200, body: { items: members(new URL(req.url).search) } }),
      "POST /v1/records/bill/b1/comments": { status: 201, body: { ...comment, id: "c2", body: "@Wei Ling please check.", mentions: [{ kind: "user", id: "u2", name: "Wei Ling" }] } },
    });
    page();
    const box = await screen.findByRole("textbox", { name: "Add a comment" });
    fireEvent.change(box, { target: { value: "@we", selectionStart: 3 } });
    const list = await screen.findByRole("listbox", { name: "Members to mention" });
    expect(within(list).getAllByRole("option").map((o) => o.textContent)).toEqual(["Wei Ling"]);
    expect(seen.some((s) => s.path.endsWith("/mentionable") && s.query.includes("q=we"))).toBe(true);
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect((box as HTMLTextAreaElement).value).toBe("@Wei Ling "));
    fireEvent.change(box, { target: { value: "@Wei Ling please check.", selectionStart: 23 } });
    fireEvent.click(screen.getByRole("button", { name: "Add comment" }));
    expect(await screen.findByText(/Comment added; 1 person was told/)).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ body: "@Wei Ling please check.", mention_ids: ["u2"] });
    expect(seen.find((s) => s.method === "POST")?.headers.get("Idempotency-Key")).toBeTruthy();
    expect((box as HTMLTextAreaElement).value).toBe("");
  });

  it("mentions nobody whose name was taken out of the text", async () => {
    const seen = answer({
      ...thread,
      "GET /v1/records/bill/b1/mentionable": { status: 200, body: { items: members("") } },
      "POST /v1/records/bill/b1/comments": { status: 201, body: comment },
    });
    page();
    const box = await screen.findByRole("textbox", { name: "Add a comment" });
    fireEvent.change(box, { target: { value: "@", selectionStart: 1 } });
    await screen.findByRole("listbox", { name: "Members to mention" });
    fireEvent.keyDown(box, { key: "ArrowDown" });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect((box as HTMLTextAreaElement).value).toBe("@Mei Lim "));
    fireEvent.change(box, { target: { value: "Never mind", selectionStart: 10 } });
    fireEvent.click(screen.getByRole("button", { name: "Add comment" }));
    await screen.findByText("Comment added");
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ body: "Never mind" });
  });

  it("shows what the API refused, and keeps the text", async () => {
    answer({
      ...thread,
      "POST /v1/records/bill/b1/comments": problem(422, "CMT-02", "A mention does not fit.", "Mention a member who may see this record."),
    });
    page();
    const box = await screen.findByRole("textbox", { name: "Add a comment" });
    fireEvent.change(box, { target: { value: "Hello" } });
    fireEvent.click(screen.getByRole("button", { name: "Add comment" }));
    expect(await screen.findByText("The comment was not added")).toBeTruthy();
    expect((box as HTMLTextAreaElement).value).toBe("Hello");
  });

  it("says when a record cannot be read, which is also how one the person may not see answers", async () => {
    answer({ "GET /v1/records/bill/b1/comments": problem(404, "NOT-FOUND", "No such record exists in this organisation.", "Check its ID.") });
    page();
    expect(await screen.findByText("The comments could not be read")).toBeTruthy();
  });
});
