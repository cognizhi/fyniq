import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CircleX, Hourglass, ShieldAlert, Undo2, UserCheck } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";

type Succession = components["schemas"]["OwnerSuccession"];
type Member = components["schemas"]["Member"];

async function successions(): Promise<Succession[]> {
  const out: Succession[] = [];
  let cursor: string | undefined;
  do {
    const { data, error } = await api.GET("/v1/owner-successions", { params: { query: { page_size: 100, cursor } } });
    if (error) throw error;
    out.push(...(data.items ?? []));
    cursor = data.next_cursor;
  } while (cursor);
  return out;
}

const evidenceKinds: [string, string][] = [
  ["board_resolution", "A board resolution"],
  ["accountant_confirmation", "The external accountant's recorded confirmation"],
];

const statusChip: Record<Succession["status"], [string, string, typeof Hourglass]> = {
  notice: ["fy-chip fy-chip--warning", "In notice", Hourglass],
  objected: ["fy-chip fy-chip--neutral", "Objected", CircleX],
  withdrawn: ["fy-chip fy-chip--neutral", "Withdrawn", Undo2],
  completed: ["fy-chip fy-chip--good", "Completed", CircleCheck],
};

// StartSuccession asks to succeed an Owner who is permanently unavailable (FR-ADM-09): a reason, what it rests on, and
// optionally a member who becomes an Owner. The outgoing Owner is told at once and has 7 days to object.
export function StartSuccession({ outgoing, people, onClose }: { outgoing: Member | null; people: Member[]; onClose: () => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const [reason, setReason] = useState("");
  const [kind, setKind] = useState("board_resolution");
  const [successor, setSuccessor] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("member.start-succession");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!outgoing) return;
    if (!reason.trim()) return setProblem({ detail: "Say why the Owner is permanently unavailable.", fix: "Write the reason, such as a departure and the board's resolution." });
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/members/{member_id}/succession", {
        params: { path: { member_id: outgoing.id }, header: { "Idempotency-Key": newKey() } },
        body: { reason: reason.trim(), evidence_kind: kind as "board_resolution", successor_member_id: successor || undefined },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["owner-successions"] });
    setReason("");
    setSuccessor("");
    onClose();
  };
  return (
    <Dialog
      open={outgoing !== null}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={outgoing ? `Start a succession of ${outgoing.display_name}` : "Start a succession"}
      description="For an Owner who is permanently unavailable: they are told at once and may object for 7 days. Then a Super admin completes it with the evidence attached, and their membership ends."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" form={`${id}-form`} disabled={busy} title={h.tooltip}>
            <Icon of={ShieldAlert} />
            {h.label}
          </Button>
        </>
      }
    >
      <form id={`${id}-form`} className="fy-stack fy-stack--3" onSubmit={(e) => void submit(e)}>
        {problem && <Refusal problem={problem} title="The succession was not started" />}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-reason`}>
            Why they are permanently unavailable
          </label>
          <textarea className="fy-textarea" id={`${id}-reason`} rows={3} maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-kind`}>
            What it rests on
          </label>
          <select className="fy-select" id={`${id}-kind`} value={kind} onChange={(e) => setKind(e.target.value)}>
            {evidenceKinds.map(([k, label]) => (
              <option key={k} value={k}>
                {label}
              </option>
            ))}
          </select>
          <span className="fy-small fy-muted">Attach the file once it is started; it is needed before the succession completes.</span>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-successor`}>
            Who becomes an Owner (optional)
          </label>
          <select className="fy-select" id={`${id}-successor`} value={successor} onChange={(e) => setSuccessor(e.target.value)}>
            <option value="">Nobody</option>
            {people
              .filter((m) => m.id !== outgoing?.id)
              .map((m) => (
                <option key={m.id} value={m.id}>
                  {m.display_name}
                </option>
              ))}
          </select>
        </div>
      </form>
    </Dialog>
  );
}

// Successions lists the Owner successions with what the viewer may do: object (the outgoing Owner), withdraw (who
// started it), attach its evidence (another Owner) and complete it (a Super admin, once its notice is over).
export function Successions() {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const list = useQuery({ queryKey: ["owner-successions"], queryFn: successions });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState("");
  const all = list.data ?? [];
  if (list.isError || all.length === 0) return null;
  const act = async (s: Succession, op: "object" | "withdraw" | "complete", said: string) => {
    setBusy(true);
    setProblem(null);
    const params = { path: { succession_id: s.id }, header: { "Idempotency-Key": newKey(), "If-Match": `"${s.version}"` } };
    const out = await guarded(confirm, () =>
      op === "object"
        ? api.POST("/v1/owner-successions/{succession_id}/object", { params })
        : op === "withdraw"
          ? api.POST("/v1/owner-successions/{succession_id}/withdraw", { params })
          : api.POST("/v1/owner-successions/{succession_id}/complete", { params }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setDone(said);
    await Promise.all(["owner-successions", "members"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
  };
  return (
    <section className="fy-card" aria-labelledby="successions-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="successions-title">
          Owner successions
        </h2>
        <span className="fy-badge fy-badge--quiet">{all.length}</span>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The succession did not change" />}
        {done && (
          <p className="fy-small" role="status">
            {done}
          </p>
        )}
        <ul className="fy-list" aria-labelledby="successions-title">
          {all.map((s) => {
            const [chip, label, icon] = statusChip[s.status];
            return (
              <li key={s.id} className="fy-list__item fy-stack fy-stack--2" style={{ alignItems: "stretch" }}>
                <div className="fy-row fy-row--wrap gap-2">
                  <span className="fy-list__main">
                    <span className="fy-list__title whitespace-normal">
                      Succession of {s.outgoing.name ?? "a former member"}
                      {s.successor ? `, ${s.successor.name ?? "a member"} to become an Owner` : ""}
                    </span>
                    <span className="fy-list__meta">
                      Started by {s.started_by.name ?? "an Owner"}, {dateTime(s.started_at)} · {s.status === "notice" ? `notice ends ${date(s.due_at)}` : s.ended_at ? `ended ${dateTime(s.ended_at)}` : ""} ·{" "}
                      {s.files === 1 ? "1 file" : `${s.files} files`}
                    </span>
                  </span>
                  <span className={chip}>
                    <Icon of={icon} />
                    {label}
                  </span>
                </div>
                <p className="fy-small fy-secondary">{s.reason}</p>
                <div className="fy-row fy-row--wrap gap-2">
                  {s.you_may_object && (
                    <Button size="sm" variant="secondary" disabled={busy} onClick={() => void act(s, "object", "You objected: the succession ended, and every Owner was told.")}>
                      <Icon of={CircleX} />
                      Object: I am not unavailable
                    </Button>
                  )}
                  {s.you_may_withdraw && (
                    <Button size="sm" variant="ghost" disabled={busy} onClick={() => void act(s, "withdraw", "You withdrew the succession.")}>
                      <Icon of={Undo2} />
                      Withdraw it
                    </Button>
                  )}
                  {s.you_may_complete && (
                    <Button size="sm" variant="primary" disabled={busy} onClick={() => void act(s, "complete", "The succession completed: the outgoing Owner's membership ended.")}>
                      <Icon of={UserCheck} />
                      Complete it, as a Super admin
                    </Button>
                  )}
                </div>
                {s.you_may_attach && (
                  <EvidencePanel record={{ kind: "owner-succession", id: s.id, etag: `"${s.version}"` }} draft={false} mayAttach onChanged={() => void queries.invalidateQueries({ queryKey: ["owner-successions"] })} />
                )}
              </li>
            );
          })}
        </ul>
      </div>
    </section>
  );
}
