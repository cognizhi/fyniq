import type { components } from "../../api/schema";

// The payment files a money account may give (FR-BNK-12, rev 0.157; p1-payment-files): what the screens call each, and
// how it reaches the bank. The server decides which an account may be set to (`payment_files` on the money account).
export type PaymentFile = components["schemas"]["MoneyAccount"]["payment_file"];

export const paymentFiles: Record<PaymentFile, { words: string; how: string; downloadHelp: string }> = {
  "payment-list": {
    words: "The payment list",
    how: "A CSV you key into your bank's bulk payment screen. It works for any bank.",
    downloadHelp: "batch.download",
  },
  "wise-batch": {
    words: "Wise's batch payment file",
    how: "A CSV in Wise's batch template, which you upload to Wise.",
    downloadHelp: "batch.download-wise",
  },
  "dbs-uff": {
    words: "DBS IDEAL's UFF",
    how: "The file you upload to DBS IDEAL to pay by FAST or GIRO.",
    downloadHelp: "batch.download-dbs",
  },
};

// fileOfWriter is the payment file a released batch's writer ("wise-batch@1") is, or undefined for one Fyniq does not list.
export function fileOfWriter(writer: string): PaymentFile | undefined {
  const key = writer.split("@")[0] ?? "";
  return key in paymentFiles ? (key as PaymentFile) : undefined;
}

// institutions are the banks and providers the account form suggests (FR-BNK-12, p1-payment-files design D2): the list
// internal/domain/banking/institutions.go recognises by name, which decides the files an account may give. A name on
// no list is another bank and gives the payment list. A Go test keeps the two lists the same.
export const institutions = [
  "DBS",
  "OCBC",
  "UOB",
  "Standard Chartered",
  "HSBC",
  "Citibank",
  "Maybank",
  "CIMB",
  "Bank of China",
  "ICBC",
  "RHB",
  "GXS Bank",
  "Trust Bank",
  "MariBank",
  "ANEXT Bank",
  "Aspire",
  "Airwallex",
  "Wise",
] as const;

// paysOut says whether a kind of money account is one a payment batch pays from.
export function paysOut(kind: components["schemas"]["MoneyAccount"]["kind"]): boolean {
  return kind === "bank" || kind === "wallet";
}
