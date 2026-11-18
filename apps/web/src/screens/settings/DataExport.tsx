import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, CircleX, ClockAlert, Download, FileArchive, Hourglass, Lock, MapPin, ShieldCheck, Users } from "lucide-react";
import { useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { SettingsPage } from "./SettingsPage";

type Export = components["schemas"]["TenantExport"];
type Setting = components["schemas"]["Setting"];

async function listExports(): Promise<Export[]> {
  const { data, error } = await api.GET("/v1/tenant-exports", { params: { query: { page_size: 100 } } });
  if (error) throw error;
  return data.items ?? [];
}

// regionAndRetention are the organisation's "Data region and retention" settings, as Settings › Organisation keeps them.
async function regionAndRetention(): Promise<Setting[]> {
  const { data, error } = await api.GET("/v1/organisation/settings", { params: { query: { page_size: 200 } } });
  if (error) throw error;
  return (data.items ?? []).filter((s) => s.group === "Data region and retention");
}

const regions: Record<string, string> = { "asia-southeast1": "Google Cloud, Singapore (asia-southeast1)" };

// valueOf is a setting's value in words.
function valueOf(s: Setting): string {
  switch (s.kind) {
    case "switch":
      return s.value === "true" ? "On" : "Off";
    case "years":
      return `${s.value} years after the end of their financial year`;
    case "days":
      return `${s.value} days`;
  }
  return regions[s.value] ?? s.value;
}

// size is a file's size in words.
function size(n: number): string {
  if (n < 1024) return `${n} bytes`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

const states: Record<Export["status"], { tone: string; icon: typeof Hourglass; label: string }> = {
  awaiting_approval: { tone: "fy-chip--warning", icon: Hourglass, label: "Awaiting approval" },
  pending: { tone: "fy-chip--neutral", icon: Hourglass, label: "Being prepared" },
  ready: { tone: "fy-chip--good", icon: CircleCheck, label: "Ready to save" },
  failed: { tone: "fy-chip--critical", icon: CircleX, label: "Failed" },
  expired: { tone: "fy-chip--neutral", icon: ClockAlert, label: "Expired" },
};

function ExportRow({ x, mine }: { x: Export; mine: boolean }) {
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("tenant-export.save");
  const s = states[x.status];
  const save = async () => {
    if (!x.export_id) return;
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/tenant-exports/{export_id}/download", { params: { path: { export_id: x.export_id } } });
    setBusy(false);
    if (out.error) {
      setProblem(problemOf(out.error));
      return;
    }
    window.location.assign(out.data.url); // served as an attachment: the page stays
  };
  return (
    <li className="fy-list__item flex-wrap items-start">
      <span className={`fy-list__icon ${x.status === "ready" ? "fy-list__icon--good" : ""}`}>
        <Icon of={FileArchive} size={20} />
      </span>
      <span className="fy-list__main min-w-52 flex-1">
        <span className="fy-list__title whitespace-normal">{x.file ? x.file.name : `Asked on ${dateTime(x.requested_at)}`}</span>
        <span className="fy-list__meta whitespace-normal">
          {mine ? "You asked for it" : "Another member asked for it"}
          {x.file && ` · ${size(x.file.size)} · ${x.file.rows.toLocaleString("en-SG")} rows · ready until ${dateTime(x.file.expires_at)}`}
          {x.failure && ` · ${x.failure}`}
        </span>
        {x.file && <span className="fy-small fy-muted fy-mono fy-expert-only">SHA-256 {x.file.sha256.slice(0, 16)}…</span>}
        <span className={`fy-chip ${s.tone} mt-1 self-start`}>
          <Icon of={s.icon} />
          {s.label}
        </span>
        {problem && <Refusal problem={problem} title="The link was not made" />}
      </span>
      {x.status === "awaiting_approval" && (
        <Link className="fy-btn fy-btn--secondary fy-btn--sm" to="/approvals/$approvalId" params={{ approvalId: x.approval_id }}>
          Open the approval
        </Link>
      )}
      {x.status === "ready" && mine && (
        <Button variant="primary" size="sm" onClick={() => void save()} disabled={busy} title={h.tooltip}>
          <Icon of={Download} />
          {busy ? "Making the link…" : h.label}
        </Button>
      )}
    </li>
  );
}

// DataExport is Settings › Data (docs/design/screens DataExport; FR-MIG-04, design D10; rev 0.273): where the
// organisation's data lives and how long it is kept, read only here, and the full export: asked for by one person,
// approved by another, saved by the person who asked within 24 hours.
export function DataExport() {
  const { session } = useSignedIn();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const settings = useQuery({ queryKey: ["settings", "data"], queryFn: regionAndRetention });
  const exports = useQuery({ queryKey: ["tenant-exports"], queryFn: listExports });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [asked, setAsked] = useState<string | null>(null);
  const h = helpText("tenant-export.request");
  const mayAsk = (session.roles ?? []).includes("administrator");
  const ask = async () => {
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () => api.POST("/v1/tenant-exports", { params: { header: { "Idempotency-Key": key } } }));
    setBusy(false);
    if (out.error) {
      setProblem(problemOf(out.error));
      return;
    }
    setAsked(out.data.id);
    await queries.invalidateQueries({ queryKey: ["tenant-exports"] });
  };
  const xs = exports.data ?? [];

  return (
    <SettingsPage title="Data and export" screen="data-export" lead="Where your data lives, how long it is kept, and a full copy you can take with you.">
      <section className="fy-card" aria-labelledby="where-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="where-title">
            Where your data lives and how long it is kept
          </h2>
        </div>
        {settings.isError && <Refusal problem={problemOf(settings.error)} title="The data region and retention could not be read" />}
        {settings.isPending && <p className="fy-card__body fy-secondary">Reading the settings…</p>}
        {settings.data && (
          <ul className="fy-list" aria-labelledby="where-title">
            {settings.data.map((s) => (
              <li key={s.key} className="fy-list__item">
                <span className="fy-list__icon">
                  <Icon of={s.key === "data.region" ? MapPin : ShieldCheck} size={20} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">{s.label}</span>
                  <span className="fy-list__meta whitespace-normal">{valueOf(s)}</span>
                </span>
                {s.key === "data.region" && (
                  <span className="fy-chip fy-chip--neutral">
                    <Icon of={Lock} />
                    Fixed
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
        <p className="fy-card__body fy-small fy-secondary">
          Change how long records are kept in <Link to="/settings/organisation">Settings › Organisation</Link>, under Data region and retention. They can be
          lengthened, never shortened.
        </p>
      </section>

      <section className="fy-card" aria-labelledby="export-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="export-title">
            Full export
          </h2>
          <span className="fy-tag">
            <Icon of={Users} />
            Two people
          </span>
        </div>
        <div className="fy-card__body fy-stack fy-stack--3">
          <p className="fy-small fy-secondary">
            Everything the organisation keeps in Fyniq in one ZIP file: each table as CSV, every version of every document, the evidence and payment files, the
            audit trail with its hashes, and a manifest with each file's SHA-256. It holds your vendors' bank account numbers in clear, so another person who
            holds the approval right approves it first, and only the person who asked can save it, within 24 hours of it being ready.
          </p>
          {asked && (
            <div className="fy-banner fy-banner--info" role="status">
              <Icon of={Hourglass} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong">Asked: waiting for approval</span>
                <span className="fy-small">
                  An Owner other than you approves it in <Link to="/approvals/$approvalId" params={{ approvalId: asked }}>Approvals</Link>. It is then prepared in the background,
                  and you are told when it is ready.
                </span>
              </div>
            </div>
          )}
          {problem && <Refusal problem={problem} title="The export was not asked for" />}
          <div className="fy-row fy-row--wrap justify-end gap-3">
            {!mayAsk && <span className="fy-small fy-secondary">Only an Administrator can ask for it</span>}
            <Button variant="primary" onClick={() => void ask()} disabled={busy || !mayAsk} title={h.tooltip}>
              <Icon of={FileArchive} />
              {busy ? "Asking…" : h.label}
            </Button>
          </div>
        </div>
        {exports.isError && <Refusal problem={problemOf(exports.error)} title="The exports could not be read" />}
        {exports.data &&
          (xs.length === 0 ? (
            <p className="fy-card__body fy-small fy-secondary">No full export yet.</p>
          ) : (
            <ul className="fy-list" aria-label="Full exports">
              {xs.map((x) => (
                <ExportRow key={x.approval_id + (x.export_id ?? "")} x={x} mine={x.requested_by === `user:${session.user_id}`} />
              ))}
            </ul>
          ))}
      </section>
    </SettingsPage>
  );
}
