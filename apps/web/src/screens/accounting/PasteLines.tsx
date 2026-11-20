import { ClipboardPaste } from "lucide-react";
import { useMemo, useState } from "react";
import { Dialog } from "../../components/ui/dialog";
import { Button } from "../../components/ui/button";
import { EntryGrid, type GridColumn } from "../../components/EntryGrid";
import { rowStatus } from "../../components/entryGridModel";
import { formatCount } from "../../format/amount";
import { isZero, sign } from "../../format/decimal";
import { Icon } from "../../ui";
import type { Account } from "./data";
import { lineKey, type Line } from "./lines";

// PasteLines is the journal editor's bulk entry (PRD-001 FR-UX-07; p1-app-shell 3.1): the entry grid in a dialog, where lines are typed
// with the keyboard or pasted from Excel or Google Sheets with the paste preview, each row saying whether it is ready. "Add the lines"
// puts the ready rows into the editor, which still checks and balances them as it does every line.

type Row = { account: string; description: string; debit: string; credit: string };
const blankRow = (): Row => ({ account: "", description: "", debit: "", credit: "" });

export function PasteLines({ open, onClose, accounts, base, onAdd }: { open: boolean; onClose: () => void; accounts: Account[]; base: string; onAdd: (lines: Line[]) => void }) {
  const [rows, setRows] = useState<Row[]>(() => Array.from({ length: 6 }, blankRow));
  const columns = useMemo<GridColumn<Row>[]>(
    () => [
      { key: "account", title: "Account", kind: "choice", required: true, width: "minmax(14rem, 2fr)", choices: accounts.map((a) => ({ value: a.code, label: `${a.code} ${a.name}` })) },
      { key: "description", title: "Description", kind: "text", width: "minmax(10rem, 2fr)" },
      {
        key: "debit",
        title: "Debit",
        kind: "amount",
        check: (v, r) => (sign(v) < 0 ? "Enter the amount without a minus sign: a credit goes under Credit." : r.credit && !isZero(r.credit) && !isZero(v) ? "Enter a debit or a credit, not both." : null),
      },
      { key: "credit", title: "Credit", kind: "amount", check: (v) => (sign(v) < 0 ? "Enter the amount without a minus sign: a debit goes under Debit." : null) },
    ],
    [accounts],
  );
  const used = rows.filter((r) => rowStatus(columns, r, 2).state !== "empty");
  const ready = used.filter((r) => rowStatus(columns, r, 2).state === "ok" && (r.debit !== "" || r.credit !== ""));
  const stuck = used.length - ready.length;
  const close = () => {
    setRows(Array.from({ length: 6 }, blankRow));
    onClose();
  };
  const add = () => {
    const byCode = new Map(accounts.map((a) => [a.code, a.id]));
    onAdd(
      ready.map((r) => ({ key: lineKey(), account_id: byCode.get(r.account) ?? "", description: r.description, debit: r.debit, credit: r.credit, currency: base, fx_rate: "", party_id: "", origin: "", dimension_value_ids: [] })),
    );
    close();
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && close()}
      wide
      eyebrow="Journal"
      title="Enter lines like a spreadsheet"
      description="Type, or paste cells from Excel or Google Sheets: account code, description, debit, credit. You see what was read before it is set."
      icon={
        <span className="fy-list__icon fy-list__icon--brand">
          <Icon of={ClipboardPaste} size={20} />
        </span>
      }
      footer={
        <>
          <Button variant="ghost" type="button" onClick={close}>
            Cancel
          </Button>
          <Button variant="primary" type="button" onClick={add} disabled={ready.length === 0 || stuck > 0} title={stuck > 0 ? "Fix the rows that say they have problems first" : undefined}>
            Add {formatCount(ready.length)} {ready.length === 1 ? "line" : "lines"}
          </Button>
        </>
      }
    >
      <EntryGrid label="Journal lines to add" columns={columns} rows={rows} onChange={setRows} blank={blankRow} viewHeight={260} />
      {stuck > 0 && (
        <p className="fy-error" role="status">
          {stuck} {stuck === 1 ? "row has" : "rows have"} a problem or no amount yet; fix {stuck === 1 ? "it" : "them"} or clear {stuck === 1 ? "it" : "them"} to add the rest.
        </p>
      )}
    </Dialog>
  );
}
