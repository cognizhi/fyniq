import { ClipboardPaste } from "lucide-react";
import { useMemo, useState } from "react";
import { EntryGrid, type GridColumn } from "../../components/EntryGrid";
import { rowStatus } from "../../components/entryGridModel";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatCount } from "../../format/amount";
import { month } from "../../format/date";
import { Icon } from "../../ui";
import type { Account } from "../accounting/data";

// PasteBudget enters budget lines in the entry grid (PRD-001 FR-UX-07; p1-app-shell 3.1): an account code and one amount for each month of
// the year, typed or pasted from Excel or Google Sheets with the paste preview. The ready rows are added to the draft, which still checks them.

type Row = Record<string, string>;

export function PasteBudget({ open, onClose, accounts, months, scale, onAdd }: { open: boolean; onClose: () => void; accounts: Account[]; months: string[]; scale: number; onAdd: (lines: { account_id: string; amounts: string[] }[]) => void }) {
  const blank = (): Row => ({ account: "", ...Object.fromEntries(months.map((_, i) => [`m${i}`, ""])) });
  const [rows, setRows] = useState<Row[]>(() => Array.from({ length: 5 }, blank));
  const columns = useMemo<GridColumn<Row>[]>(
    () => [
      { key: "account", title: "Account", kind: "choice", required: true, width: "minmax(14rem, 2fr)", choices: accounts.map((a) => ({ value: a.code, label: `${a.code} ${a.name}` })) },
      ...months.map((m, i): GridColumn<Row> => ({ key: `m${i}`, title: month(m), kind: "amount", width: "7rem" })),
    ],
    [accounts, months],
  );
  const used = rows.filter((r) => rowStatus(columns, r, scale).state !== "empty");
  const ready = used.filter((r) => rowStatus(columns, r, scale).state === "ok");
  const stuck = used.length - ready.length;
  const close = () => {
    setRows(Array.from({ length: 5 }, blank));
    onClose();
  };
  const add = () => {
    const byCode = new Map(accounts.map((a) => [a.code, a.id]));
    onAdd(ready.map((r) => ({ account_id: byCode.get(r.account ?? "") ?? "", amounts: months.map((_, i) => r[`m${i}`] ?? "") })));
    close();
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && close()}
      wide
      eyebrow="Budget"
      title="Enter lines like a spreadsheet"
      description="Type, or paste cells from Excel or Google Sheets: an account code and an amount for each month. You see what was read before it is set."
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
      <EntryGrid label="Budget lines to add" columns={columns} rows={rows} onChange={setRows} blank={blank} scale={scale} viewHeight={240} />
      {stuck > 0 && (
        <p className="fy-error" role="status">
          {stuck} {stuck === 1 ? "row has" : "rows have"} a problem; fix {stuck === 1 ? "it" : "them"} or clear {stuck === 1 ? "it" : "them"} to add the rest.
        </p>
      )}
    </Dialog>
  );
}
