import { enterBill, postBill, recordPayment } from "./books";
import { api, expect, organisation, test } from "./harness";

// ofx is the bank's statement of the account for the day, in OFX: the payment that left it, as the bank describes it.
function ofx(account: string, day: string, amount: string, memo: string): Buffer {
  const d = day.replaceAll("-", "");
  return Buffer.from(
    [
      "OFXHEADER:100",
      "DATA:OFXSGML",
      "VERSION:102",
      "SECURITY:NONE",
      "ENCODING:USASCII",
      "CHARSET:1252",
      "COMPRESSION:NONE",
      "OLDFILEUID:NONE",
      "NEWFILEUID:NONE",
      "",
      "<OFX>",
      `<SIGNONMSGSRSV1><SONRS><STATUS><CODE>0<SEVERITY>INFO</STATUS><DTSERVER>${d}120000<LANGUAGE>ENG</SONRS></SIGNONMSGSRSV1>`,
      "<BANKMSGSRSV1><STMTTRNRS><TRNUID>1<STATUS><CODE>0<SEVERITY>INFO</STATUS>",
      "<STMTRS>",
      "<CURDEF>SGD",
      `<BANKACCTFROM><BANKID>7339<ACCTID>${account}<ACCTTYPE>CHECKING</BANKACCTFROM>`,
      "<BANKTRANLIST>",
      `<DTSTART>${d}`,
      `<DTEND>${d}`,
      `<STMTTRN><TRNTYPE>DEBIT<DTPOSTED>${d}<TRNAMT>-${amount}<FITID>JOURNEY-1<NAME>LUMEN PRINT HOUSE<MEMO>${memo}</STMTTRN>`,
      "</BANKTRANLIST>",
      "</STMTRS>",
      "</STMTTRNRS></BANKMSGSRSV1>",
      "</OFX>",
      "",
    ].join("\n"),
    "latin1",
  );
}

// QA-E2E-01's reconcile journey. The Accountant adds the operating bank account as a money account, pays a bill from
// it, and imports the bank's statement of the day as the bank's OFX file. The statement's line is the payment: the
// queue offers it as the payment already recorded, the Accountant accepts, and the line is matched to it, on the screen
// and in the API.
// Scenarios: FR-BNK-01 "Kinds of money account in any currency"; FR-BNK-02 "Structured statement formats"; FR-BNK-06
// "Suggested matches" and "Actions on a line"; FR-BNK-07 "Reference matching order".
// req: QA-E2E-01
test("reconcile: the bank's statement line of a recorded payment is matched to it", async ({ browser }) => {
  const j = await organisation(browser, "Journey Reconcile Pte Ltd", [
    { key: "bookkeeper", name: "Bea Bookkeeper", roles: ["bookkeeper"] },
    { key: "accountant", name: "Cal Accountant", roles: ["accountant"] },
  ]);
  const cal = j.people.accountant!.page;
  await cal.goto("/banking/accounts/new");
  await cal.getByLabel("Name", { exact: true }).fill("OCBC current");
  await cal.getByLabel("Bank or provider (optional)").fill("OCBC");
  await cal.getByLabel("Account in the chart").selectOption({ label: "1020 OCBC Business Growth" });
  await cal.getByLabel("Account number or balance ID (optional)").fill("601234567001");
  await cal.getByRole("button", { name: "Add the account" }).click();
  await expect(cal.getByText("Step 2 of 3")).toBeVisible();

  await enterBill(j.people.bookkeeper!.page, "Lumen Print House Pte Ltd", "LPH-0931", [
    { account: "6140 Printing and stationery", description: "Business cards and letterheads", amount: "642.80" },
  ]);
  await postBill(cal, "Lumen Print House Pte Ltd", "LPH-0931");
  await recordPayment(cal, "1020 OCBC Business Growth", "642.80", "OCBC FAST 7781");
  const day = (await api<{ payments: { date: string }[] }>(cal, `/v1/bills/${cal.url().split("/").pop()}`)).payments[0]!.date;

  await cal.goto("/banking/import");
  await cal.getByLabel("Statement files").setInputFiles({ name: "ocbc.ofx", mimeType: "application/x-ofx", buffer: ofx("601234567001", day, "642.80", "OCBC FAST 7781") });
  const file = cal.getByRole("region", { name: "ocbc.ofx" });
  await expect(file.getByLabel("Account")).toBeVisible();
  await cal.getByRole("button", { name: "Import", exact: true }).click();
  await expect(file.getByText("Imported")).toBeVisible();
  await expect(file.getByRole("list", { name: "What it does" }).getByText("1 added")).toBeVisible();

  // The queue: the line is the payment already recorded, which the Accountant accepts.
  await cal.goto("/banking/categorise");
  const line = cal.getByRole("region", { name: /^OCBC current · / });
  await expect(line.getByText("OCBC FAST 7781")).toBeVisible();
  await expect(line.getByRole("radio", { name: /Money paid, already recorded: PAY-0001/ })).toBeChecked();
  await line.getByRole("button", { name: "Accept" }).click();
  await expect(cal.getByText(/1 done · 0 lines wait/)).toBeVisible();

  const lines = await api<{ items: { amount: { amount: string }; status: string; kind?: string; document_id?: string }[] }>(cal, "/v1/bank-lines?page_size=50");
  expect(lines.items).toHaveLength(1);
  expect(lines.items[0]).toMatchObject({ amount: { amount: "-642.80" }, status: "matched" });
});
