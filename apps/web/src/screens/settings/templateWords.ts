import type { components } from "../../api/schema";

// The words the template screens use for what the API returns (FR-TPL-05; PRD-001 rev 0.99).

export type DocumentTemplate = components["schemas"]["DocumentTemplate"];
export type TemplateContent = components["schemas"]["TemplateContent"];
export type TemplateBlock = components["schemas"]["TemplateBlock"];
export type TemplateCatalogue = components["schemas"]["TemplateCatalogue"];
export type DocType = DocumentTemplate["document_type"];
export type Kind = DocumentTemplate["kind"];

export const docTypes: DocType[] = ["invoice", "quote", "credit_note", "receipt", "statement", "reminder", "remittance", "purchase_order"];

export const typeNames: Record<DocType, string> = {
  invoice: "Invoice",
  quote: "Quote",
  credit_note: "Credit note",
  receipt: "Receipt",
  statement: "Customer statement",
  reminder: "Reminder letter",
  remittance: "Remittance advice",
  purchase_order: "Purchase order",
};

// The document types whose templates keep the IRAS particulars (FR-TPL-03).
export const lockedTypes: DocType[] = ["invoice", "credit_note"];

export const blockNames: Record<TemplateBlock["kind"], string> = {
  header: "Header and logo",
  our_details: "Our details",
  recipient: "Customer details",
  document_details: "Document details",
  line_items: "Line items",
  totals: "Totals and GST summary",
  payment_details: "Payment details",
  notes: "Notes and terms",
  signature: "Signature or stamp",
  footer: "Footer",
  text: "Free text",
};

// blockName names a block for the document type: a remittance's or purchase order's recipient is the supplier.
export function blockName(kind: TemplateBlock["kind"], type: DocType): string {
  if (kind === "recipient" && (type === "remittance" || type === "purchase_order")) return "Supplier details";
  return blockNames[kind];
}

export const styles = [
  { key: "classic", name: "Classic", description: "Inter, the accent on the title and the table's rule" },
  { key: "band", name: "Band", description: "An accent band behind the header and the table's head" },
  { key: "minimal", name: "Minimal", description: "No rules, generous margins" },
  { key: "compact", name: "Compact", description: "Smaller type, for long line tables" },
] as const;

export type StyleKey = (typeof styles)[number]["key"];

// The roles that prepare templates (C on "Document templates") and those that publish them and choose defaults (A).
export const templatePreparers = ["accountant", "ar_specialist", "ap_specialist"];
export const templateApprovers = ["owner", "controller"];

// contrastOnWhite is a colour's contrast ratio against white (WCAG 2.2), for the hint beside the accent; the API checks it
// again (TPL-05). Display only: colours are not money.
export function contrastOnWhite(hex: string): number | null {
  if (!/^#[0-9A-Fa-f]{6}$/.test(hex)) return null;
  const channel = (i: number) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  const l = 0.2126 * channel(1) + 0.7152 * channel(3) + 0.0722 * channel(5);
  return 1.05 / (l + 0.05);
}
