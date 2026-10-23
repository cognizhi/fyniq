import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { TemplateEditor } from "./TemplateEditor";
import { Templates } from "./Templates";

const founder = { ...ownerSession, roles: ["owner", "controller", "accountant"] } as Session;
const accountant = { ...ownerSession, roles: ["accountant"] } as Session;

const page = { size: "A4", margins_mm: { top: 20, right: 18, bottom: 20, left: 18 } };
const theme = { logo: true, accent: "#0F766E", font: "Inter", style: "classic" };
const blocks = [
  { kind: "header", shown: true },
  { kind: "our_details", shown: true },
  { kind: "totals", shown: true },
  { kind: "notes", shown: false, texts: { label: "Notes" } },
];
const draft = { schema: "fyniq.template/1", page, theme, blocks };
const template = {
  id: "t-1",
  kind: "document",
  document_type: "invoice",
  name: "Standard invoice",
  version: 4,
  draft,
  published_version: 3,
  draft_changed: true,
  saved_by: { kind: "user", id: ownerSession.user_id, name: "Mei Lin Tan" },
  saved_at: "2026-10-12T08:42:00Z",
};
// slow is how long a query waits: the editor loads three resources, and the live preview waits 600 ms after the last change.
const slow = { timeout: 3000 };

const choice = (kind: string, type: string, extra = {}) => ({ kind, document_type: type, etag: '"none"', ...extra });

function listPage(extra = {}) {
  return answer({
    "GET /v1/document-templates": { status: 200, body: { items: [template] } },
    "GET /v1/document-template-defaults": {
      status: 200,
      body: { items: [choice("document", "invoice", { etag: '"c-1"', template: { id: "t-1", name: "Standard invoice", published_version: 3 } }), choice("email", "invoice")] },
    },
    "GET /v1/approvals": { status: 200, body: { items: [] } },
    ...extra,
  });
}

// req: FR-TPL-05
// req: FR-TPL-07
describe("Templates", () => {
  it("lists each type's default, its templates and the starter gallery, and axe finds nothing", async () => {
    listPage();
    renderPage(Templates, { path: "/settings/templates", session: founder, mode: "basic" });
    const docs = await screen.findByRole("region", { name: "Document templates" }, slow);
    await waitFor(() => expect(within(docs).getAllByRole("row")[1]?.textContent).toContain("Standard invoiceVersion 3"), slow);
    expect(within(docs).getAllByText("Version 0").length).toBe(7);
    expect(screen.getByRole("link", { name: /Standard invoice/ }).getAttribute("href")).toBe("/settings/templates/t-1");
    expect(screen.getByText("Draft changes")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Use the Band style" })).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Template bundle" })).toBeNull(); // Expert only
    await noViolations(document.body);
  });

  it("chooses a default among the published templates, over the choice read", async () => {
    const seen = listPage({ "PUT /v1/document-template-defaults/document/invoice": { status: 200, body: choice("document", "invoice") } });
    renderPage(Templates, { path: "/settings/templates", session: founder });
    const docs = await screen.findByRole("region", { name: "Document templates" }, slow);
    await waitFor(() => expect(within(docs).getAllByRole("row")[1]?.textContent).toContain("Version 3"), slow);
    fireEvent.click(await within(docs).findByRole("button", { name: "Change default for invoice documents" }, slow));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Template"), { target: { value: "" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Use this default" }));
    await waitFor(() => expect(seen.some((s) => s.method === "PUT")).toBe(true), slow);
    const put = seen.find((s) => s.method === "PUT");
    expect(put?.headers.get("If-Match")).toBe('"c-1"');
    expect(put?.body).toEqual({});
  });

  it("makes a draft from a starter; a preparer cannot choose defaults", async () => {
    const seen = listPage({ "POST /v1/document-templates": { status: 201, body: { ...template, id: "t-2" } } });
    renderPage(Templates, { path: "/settings/templates", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Use the Band style" }, slow));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("radio", { name: "Band" }).getAttribute("aria-checked")).toBe("true");
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Navy invoice" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Make the draft" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST")).toBe(true), slow);
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ kind: "document", document_type: "invoice", name: "Navy invoice", starter: "band" });
    expect(screen.queryByRole("button", { name: /Change default/ })).toBeNull();
  });
});

const catalogue = {
  document_type: "invoice",
  blocks: [
    { kind: "header", texts: [], locked: true },
    { kind: "our_details", texts: [], locked: true },
    { kind: "totals", texts: [], locked: true },
    { kind: "notes", texts: ["label", "body"], locked: false },
  ],
  fields: [
    { path: "customer.name", type: "text", label: "Customer's name" },
    { path: "document.due_date", type: "date", label: "Due date" },
  ],
  formats: [
    { type: "text", formats: [] },
    { type: "date", formats: ["medium", "long", "short", "iso"] },
    { type: "money", formats: ["code", "symbol", "plain"] },
    { type: "number", formats: [] },
  ],
  fonts: ["Inter", "IBM Plex Sans", "IBM Plex Serif"],
  page_sizes: ["A4", "Letter"],
  margin_min_mm: 10,
  margin_max_mm: 40,
  min_contrast: "4.5",
  locked_title: true,
  defaults: { document: draft, email: { schema: "fyniq.template/1", subject: "x", body: "y" } },
  starters: [],
};
const versions = [
  { number: 3, content_hash: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", note: "PayNow added", published_by: { kind: "user", id: "u", name: "Mei Lin Tan" }, published_at: "2026-09-02T02:00:00Z" },
  { number: 2, content_hash: "3b1c02e7884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", published_by: { kind: "user", id: "u2", name: "Arjun Rao" }, published_at: "2026-07-14T02:00:00Z" },
];
const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==";

function editorPage(extra = {}) {
  return answer({
    "GET /v1/document-templates/t-1": { status: 200, body: template, headers: { ETag: '"4"' } },
    "GET /v1/document-template-catalogue/invoice": { status: 200, body: catalogue },
    "GET /v1/document-templates/t-1/versions": { status: 200, body: { items: versions } },
    "POST /v1/document-template-previews": { status: 200, body: { kind: "document", document_type: "invoice", pages: [png], pdf: "JVBERi0=", sha256: "a".repeat(64), missing_particulars: [] } },
    ...extra,
  });
}

// req: FR-TPL-01
// req: FR-TPL-03
// req: FR-TPL-04
describe("The template editor", () => {
  it("shows the style, theme, locked blocks, a live preview and the versions, and axe finds nothing", async () => {
    editorPage();
    renderPage(TemplateEditor, { path: "/settings/templates/t-1", route: "/settings/templates/$templateId", session: founder, mode: "basic" });
    expect(await screen.findByRole("heading", { name: "Invoice template: Standard invoice" }, slow)).toBeTruthy();
    expect(screen.getByText("Version 3 in force")).toBeTruthy();
    expect(screen.getAllByText("Locked").length).toBe(3);
    expect(await screen.findByAltText(/Page 1 of the invoice/, {}, slow)).toBeTruthy();
    expect(screen.getByText("No IRAS particular missing")).toBeTruthy();
    expect(screen.getByText(/5\.5:1 against white: text in it reads/)).toBeTruthy();
    expect(await screen.findByRole("button", { name: "Roll back to version 2" }, slow)).toBeTruthy();
    await noViolations(document.body);
  });

  it("saves the draft over the version read, with a field inserted into a text", async () => {
    const seen = editorPage({ "PUT /v1/document-templates/t-1": { status: 200, body: template, headers: { ETag: '"5"' } } });
    renderPage(TemplateEditor, { path: "/settings/templates/t-1", route: "/settings/templates/$templateId", session: founder });
    fireEvent.click(await screen.findByRole("button", { name: "Edit texts of Notes and terms" }, slow));
    const body = screen.getByLabelText("Text");
    fireEvent.change(body, { target: { value: "Pay by " } });
    const picker = screen.getByRole("group", { name: "Insert a field into Text" });
    fireEvent.change(within(picker).getByLabelText("Field"), { target: { value: "document.due_date" } });
    fireEvent.change(within(picker).getByLabelText("Format"), { target: { value: "long" } });
    fireEvent.click(within(picker).getByRole("button", { name: "Insert field" }));
    fireEvent.change(screen.getByLabelText("Accent colour"), { target: { value: "#FFFF00" } });
    expect(screen.getByText(/too pale for text/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(seen.some((s) => s.method === "PUT")).toBe(true), slow);
    const put = seen.find((s) => s.method === "PUT");
    expect(put?.headers.get("If-Match")).toBe('"4"');
    const sent = put?.body as { content: { theme: { accent: string }; blocks: { texts?: { body?: string } }[] } };
    expect(sent.content.theme.accent).toBe("#FFFF00");
    expect(sent.content.blocks[3]?.texts?.body).toBe("Pay by {{document.due_date|long}}") // inserted at the cursor;
  });

  it("publishes; in Standard mode it asks another person to approve", async () => {
    const seen = editorPage({
      "POST /v1/document-templates/t-1/publish": {
        status: 409,
        body: { code: "TPL-14", detail: "In Team mode another person approves a template's publication (FR-TPL-04).", fix: "Ask for approval with requestDocumentTemplatePublication" },
      },
      "POST /v1/document-templates/t-1/approval": { status: 202, body: { id: "a-9", summary: "Publish the template", status: "pending" } },
    });
    renderPage(TemplateEditor, { path: "/settings/templates/t-1", route: "/settings/templates/$templateId", session: founder });
    fireEvent.change(await screen.findByLabelText("What changed (optional)", {}, slow), { target: { value: "New footer" } });
    fireEvent.click(screen.getByRole("button", { name: "Publish version 4" }));
    expect(await screen.findByText("Another person approves this publication", {}, slow)).toBeTruthy();
    fireEvent.click(screen.getAllByRole("button", { name: "Ask for approval" })[0] as HTMLElement);
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/document-templates/t-1/approval")).toBe(true), slow);
    const ask = seen.find((s) => s.path === "/v1/document-templates/t-1/approval");
    expect(ask?.body).toEqual({ note: "New footer" });
    expect(ask?.headers.get("If-Match")).toBe('"4"');
  });
});
