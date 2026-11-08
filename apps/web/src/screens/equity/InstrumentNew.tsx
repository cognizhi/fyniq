import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { problemOf, type ApiProblem } from "../../ui";
import { listAccounts } from "../accounting/data";
import { EquityTabs } from "./Holders";
import { blankInstrument, InstrumentFields, instrumentBody } from "./InstrumentForm";
import type { InstrumentDraft } from "./instrumentWords";

// InstrumentNew is Equity › SAFEs and notes › Add (docs/design/screens Instruments "Add instrument"; FR-EQ-18): the
// holder, the form and its terms. Saved as a draft, it takes the signed instrument and asks for approval on its own page.
export function InstrumentNew() {
  const navigate = useNavigate();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const chart = useQuery({ queryKey: ["accounts", "all"], queryFn: listAccounts });
  const capital = (chart.data ?? []).filter((a) => a.role === "share-capital" && a.active && !a.group);
  const [form, setForm] = useState<InstrumentDraft>(blankInstrument);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const draftH = helpText("instrument.draft");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/instruments", { params: { header: { "Idempotency-Key": key } }, body: instrumentBody(form, capital[0]?.id ?? "") }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["instruments"] });
    await navigate({ to: "/equity/instruments/$instrumentId", params: { instrumentId: out.data.id } });
  };
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/equity/instruments">SAFEs and notes</Link>
            <span aria-hidden="true">›</span>
            <span>New</span>
          </nav>
          <h1 className="fy-h1" title={draftH.tooltip}>
            New SAFE or note
          </h1>
          <p className="fy-secondary">Draft it, attach the signed instrument, then ask another person to approve it. Its money comes in on a bank line to 2700 that names it.</p>
        </div>
      </div>
      <EquityTabs current="instruments" />
      <form className="fy-card fy-card--pad fy-stack fy-stack--4" onSubmit={submit} aria-label="New SAFE or note">
        {problem && <Refusal problem={problem} title="The instrument was not drafted" />}
        <InstrumentFields form={form} set={setForm} holderFixed={false} />
        <div className="fy-row justify-end gap-2">
          <Link className="fy-btn fy-btn--ghost" to="/equity/instruments">
            Cancel
          </Link>
          <Button variant="primary" type="submit" disabled={busy} title={draftH.tooltip}>
            {draftH.label}
          </Button>
        </div>
      </form>
    </div>
  );
}
