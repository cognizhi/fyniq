import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import { EntryGrid, type GridColumn } from "./EntryGrid";

// The spreadsheet-like entry grid (FR-UX-07): keyboard-first, with paste preview, = arithmetic, fill down, undo and a hundred thousand
// rows as light as ten.
type Row = { account: string; memo: string; debit: string; credit: string };
const columns: GridColumn<Row>[] = [
  { key: "account", title: "Account", kind: "choice", required: true, choices: [{ value: "6200", label: "6200 Office rent" }, { value: "2010", label: "2010 Accounts payable" }, { value: "4010", label: "4010 Subscriptions" }] },
  { key: "memo", title: "Memo", kind: "text" },
  { key: "debit", title: "Debit", kind: "amount" },
  { key: "credit", title: "Credit", kind: "amount" },
];
const blank = (): Row => ({ account: "", memo: "", debit: "", credit: "" });

function Grid({ initial, readOnly }: { initial: Row[]; readOnly?: boolean }) {
  const [rows, setRows] = useState(initial);
  return <EntryGrid label="Journal lines" columns={columns} rows={rows} onChange={setRows} blank={blank} readOnly={readOnly} viewHeight={240} />;
}

const meta = { title: "Shared/EntryGrid", component: Grid } satisfies Meta<typeof Grid>;
export default meta;
type Story = StoryObj<typeof meta>;

export const JournalLines: Story = {
  args: {
    initial: [
      { account: "6200", memo: "October rent", debit: "3000.00", credit: "" },
      { account: "2010", memo: "October rent", debit: "", credit: "3000.00" },
      { account: "", memo: "A line to finish", debit: "12.345", credit: "" },
      blank(),
    ],
  },
};

export const AHundredThousandRows: Story = {
  args: { initial: Array.from({ length: 100_000 }, (_, i) => ({ account: "4010", memo: `Subscription ${i + 1}`, debit: "", credit: "49.00" })) },
};

export const ReadOnly: Story = {
  args: { readOnly: true, initial: [{ account: "6200", memo: "October rent", debit: "3000.00", credit: "" }] },
};
