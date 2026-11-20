import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { FileDown, Plus, Save, Send } from "lucide-react";
import { useId, useMemo, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listPeriods } from "../accounting/data";
import { PackFigures } from "./PackFigures";

type Pack = components["schemas"]["BoardPack"];

// BoardPack is Reports › Board pack (docs/design/screens BoardPack; FR-RPT-06): a month's pack is a draft whose figures read as they stand, with
// the highlights, commentary and asks a person writes; publishing freezes the statements and KPIs at one ledger revision, and a correction is a
// new version. A Board viewer reads the published pack after signing in (the Board portal); nothing here is a link that opens without it.
export function BoardPack() {
  const id = useId();
  const client = useQueryClient();
  const packs = useQuery({
    queryKey: ["board-packs"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/board-packs", { params: { query: { page_size: 200 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const periods = useQuery({ queryKey: ["periods"], queryFn: listPeriods });
  const months = useMemo(() => [...(periods.data?.items ?? [])].map((p) => p.starts_on.slice(0, 7)).reverse(), [periods.data]);
  const [month, setMonth] = useState("");
  const [openId, setOpenId] = useState("");
  const pack = useQuery({
    queryKey: ["board-pack", openId],
    enabled: openId !== "",
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/board-packs/{id}", { params: { path: { id: openId } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const [texts, setTexts] = useState<{ highlights: string; commentary: string; asks: string } | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const h = helpText("reports.board-pack");
  const p = pack.data;
  const draft = p?.status === "draft";
  const t = texts ?? { highlights: p?.highlights ?? "", commentary: p?.commentary ?? "", asks: p?.asks ?? "" };
  const refresh = async () => {
    await client.invalidateQueries({ queryKey: ["board-packs"] });
    await client.invalidateQueries({ queryKey: ["board-pack"] });
  };
  const choose = (next: string) => {
    setOpenId(next);
    setTexts(null);
    setProblem(null);
  };

  const start = async () => {
    setBusy(true);
    const { data, error } = await api.POST("/v1/board-packs", { body: { month: month || months[0]! } });
    setBusy(false);
    if (error) return setProblem(problemOf(error));
    setProblem(null);
    choose(data.id);
    await refresh();
  };
  const save = async () => {
    if (!p) return;
    setBusy(true);
    const { error } = await api.PUT("/v1/board-packs/{id}", { params: { path: { id: p.id }, header: { "If-Match": `"${p.edit}"` } }, body: t });
    setBusy(false);
    if (error) return setProblem(problemOf(error));
    setProblem(null);
    setTexts(null);
    await refresh();
  };
  // downloadPdf asks for the published pack's PDF and saves it under the name the server gave it (FR-RPT-06).
  const downloadPdf = async () => {
    if (!p) return;
    setBusy(true);
    const { data, error } = await api.GET("/v1/board-packs/{id}/pdf", { params: { path: { id: p.id } } });
    setBusy(false);
    if (error || !data) return setProblem(problemOf(error));
    setProblem(null);
    const bytes = Uint8Array.from(atob(data.content_base64), (c) => c.charCodeAt(0));
    const url = URL.createObjectURL(new Blob([bytes], { type: "application/pdf" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = data.file_name;
    a.click();
    URL.revokeObjectURL(url);
  };
  const publish = async () => {
    if (!p) return;
    setBusy(true);
    const { error } = await api.POST("/v1/board-packs/{id}/publish", { params: { path: { id: p.id }, header: { "If-Match": `"${p.edit}"` } } });
    setBusy(false);
    if (error) return setProblem(problemOf(error));
    setProblem(null);
    setTexts(null);
    await refresh();
  };

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/reports">Reports</Link>
          </nav>
          <h1 className="fy-h1">Board pack</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>
      {packs.error && <Refusal problem={problemOf(packs.error)} title="The board packs could not be shown" />}
      <div className="fy-card fy-card--pad fy-row fy-row--wrap gap-3">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-open`}>
            Open a pack
          </label>
          <select className="fy-select" id={`${id}-open`} value={openId} onChange={(e) => choose(e.target.value)}>
            <option value="">Choose a pack</option>
            {(packs.data ?? []).map((x) => (
              <option key={x.id} value={x.id}>
                {x.month}, version {x.version} ({x.status})
              </option>
            ))}
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-month`}>
            Start the pack of
          </label>
          <select className="fy-select" id={`${id}-month`} value={month} onChange={(e) => setMonth(e.target.value)}>
            {months.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
        </div>
        <Button variant="secondary" onClick={start} disabled={busy || months.length === 0}>
          <Icon of={Plus} /> Start the pack
        </Button>
      </div>
      {problem && <Refusal problem={problem} title="The board pack could not be saved" />}
      {p && (
        <>
          <section className="fy-card fy-card--pad" aria-labelledby={`${id}-texts`}>
            <h2 className="fy-h3" id={`${id}-texts`}>
              {p.month}, version {p.version}: {draft ? "a draft" : `published at ledger revision ${p.ledger_revision}`}
            </h2>
            {(["highlights", "commentary", "asks"] as const).map((k) => (
              <div className="fy-field" key={k}>
                <label className="fy-label" htmlFor={`${id}-${k}`}>
                  {k === "asks" ? "Asks of the board" : k === "highlights" ? "Highlights" : "Commentary"}
                </label>
                <textarea className="fy-textarea" id={`${id}-${k}`} rows={k === "commentary" ? 6 : 3} value={t[k]} readOnly={!draft} onChange={(e) => setTexts({ ...t, [k]: e.target.value })} />
              </div>
            ))}
            {draft && (
              <div className="fy-row gap-3">
                <Button variant="secondary" onClick={save} disabled={busy}>
                  <Icon of={Save} /> Save the texts
                </Button>
                <Button onClick={publish} disabled={busy || texts !== null}>
                  <Icon of={Send} /> Publish
                </Button>
              </div>
            )}
            {draft && <p className="fy-small fy-muted">Publishing freezes the figures below as they read at that moment. Save your texts first.</p>}
            {!draft && (
              <div className="fy-row gap-3">
                <Button variant="secondary" onClick={downloadPdf} disabled={busy}>
                  <Icon of={FileDown} /> Download the PDF
                </Button>
              </div>
            )}
            {!draft && <p className="fy-small fy-muted">A published pack never changes. To correct it, start the pack of this month again: that is the next version.</p>}
          </section>
          <PackFigures pack={p} />
        </>
      )}
    </div>
  );
}
