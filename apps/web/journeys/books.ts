// What several journeys do on the way to the step they are about: each through the screens, as a person does it.
import { expect, type Page } from "@playwright/test";
import { contact } from "./harness";

// pdf is a one-page PDF that says what it is: the synthetic document a journey attaches as evidence (DEV-08).
export function pdf(text: string): Buffer {
  const safe = text.replace(/[()\\]/g, " ");
  const stream = `BT /F1 12 Tf 72 720 Td (${safe}) Tj ET`;
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
    `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`,
  ];
  let out = "%PDF-1.4\n";
  const at: number[] = [];
  objects.forEach((o, i) => {
    at.push(out.length);
    out += `${i + 1} 0 obj\n${o}\nendobj\n`;
  });
  const xref = out.length;
  out += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n${at.map((n) => `${String(n).padStart(10, "0")} 00000 n \n`).join("")}`;
  out += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(out, "latin1");
}

// attach attaches a document to the record whose page is open, in its Evidence panel, as what it is (such as invoice).
export async function attach(page: Page, name: string, kind: string, text: string): Promise<void> {
  const panel = page.getByRole("region", { name: "Evidence" });
  await panel.getByLabel("File", { exact: true }).setInputFiles({ name, mimeType: "application/pdf", buffer: pdf(text) });
  await panel.getByLabel("What it is").fill(kind);
  await panel.getByRole("button", { name: /^Attach/ }).click();
  await expect(panel.getByRole("list", { name: "Files attached" }).getByText(name)).toBeVisible();
}

export interface BillLine {
  account: string; // as the account list shows it, such as "6135 SaaS tools"
  description: string;
  amount: string;
}

// enterBill adds the supplier and enters their bill as a draft, from a supplier not registered for GST, with their
// invoice attached as its evidence, and returns the bill's ID and the supplier's.
export async function enterBill(page: Page, supplier: string, theirNumber: string, lines: BillLine[]): Promise<{ bill: string; supplier: string }> {
  const supplierId = await contact(page, supplier, { vendor: true });
  await page.goto("/purchases/bills/new");
  await page.getByLabel("Supplier", { exact: true }).selectOption({ label: supplier });
  await page.getByLabel("Their invoice number").fill(theirNumber);
  await page.getByRole("checkbox", { name: /The supplier is GST-registered/ }).uncheck();
  for (const [i, l] of lines.entries()) {
    if (i > 0) await page.getByRole("button", { name: "Add a line" }).click();
    const line = page.getByRole("group", { name: `Line ${i + 1}` });
    await line.getByLabel("What it is for").selectOption({ label: l.account });
    await line.getByLabel("Description").fill(l.description);
    await line.getByLabel("GST code").selectOption("NR");
    await line.getByLabel(/^Amount before GST/).fill(l.amount);
  }
  await page.getByRole("button", { name: "Save as a draft" }).click();
  await expect(page).toHaveURL(/\/purchases\/bills\/[0-9a-f-]{36}$/);
  await attach(page, `${theirNumber}.pdf`, "invoice", `${supplier} invoice ${theirNumber}`);
  return { bill: page.url().split("/").pop()!, supplier: supplierId };
}

// postBill opens a draft bill from the Bills list's drafts, as whoever posts bills does, and posts it, returning the
// number it took.
export async function postBill(page: Page, supplier: string, theirNumber: string): Promise<string> {
  await page.goto("/purchases/bills");
  await page.getByRole("button", { name: "Drafts" }).click();
  await page.getByRole("row", { name: new RegExp(`${supplier} ${theirNumber}`) }).getByRole("link").first().click();
  await page.getByRole("button", { name: "Post", exact: true }).click();
  const posted = page.getByRole("status").filter({ hasText: /^Posted as BILL-\d+\.$/ });
  await expect(posted).toBeVisible();
  return (await posted.textContent())!.replace(/^Posted as (BILL-\d+)\.$/, "$1");
}

// recordPayment records, on a posted bill's page, its payment that left the bank: from the account, the amount, with
// the bank's reference.
export async function recordPayment(page: Page, from: string, amount: string, reference: string): Promise<void> {
  await page.getByRole("button", { name: "Record payment…" }).click();
  const dialog = page.getByRole("dialog", { name: "Record a payment of this bill" });
  await dialog.getByLabel("Paid from").selectOption({ label: from });
  await dialog.getByLabel(/^Amount paid of the bill/).fill(amount);
  await dialog.getByLabel("Bank reference (optional)").fill(reference);
  await dialog.getByRole("button", { name: "Record payment" }).click();
  await expect(dialog).toBeHidden();
}
