import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, CircleX, ExternalLink, Hourglass, Scale } from "lucide-react";
import { useState } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import type { Approval } from "../approvals/words";
import { SettingsPage } from "./SettingsPage";

type Pack = components["schemas"]["StatutoryPack"];

async function listPacks() {
  const { data, error } = await api.GET("/v1/statutory-packs", { params: { query: { page_size: 200 } } });
  if (error) throw error;
  return data.items ?? [];
}

async function waitingActivations() {
  const { data, error } = await api.GET("/v1/approvals", { params: { query: { status: "pending", page_size: 200 } } });
  if (error) throw error;
  return (data.items ?? []).filter((a) => a.object_type === "statutory_pack");
}

function Tests({ passed, total }: { passed: number; total: number }) {
  const ok = passed === total;
  return (
    <span className={ok ? "fy-chip fy-chip--good" : "fy-chip fy-chip--critical"}>
      <Icon of={ok ? CircleCheck : CircleX} />
      {passed} of {total} pinned tests pass
    </span>
  );
}

function PackVersion({ id, waiting, onClose, onAsked }: { id: string | null; waiting?: Approval; onClose: () => void; onAsked: (a: Approval) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const activate = helpText("pack.activate");
  const q = useQuery({
    queryKey: ["statutory-pack", id],
    enabled: id !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/statutory-packs/{version_id}", { params: { path: { version_id: id ?? "" } } });
      if (r.error) throw r.error;
      return { pack: r.data, etag: etagOf(r.response) || '"0"' };
    },
  });
  const close = () => {
    setProblem(null);
    onClose();
  };
  const run = async () => {
    if (!q.data) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const etag = q.data.etag;
    const out = await guarded(confirm, () =>
      api.POST("/v1/statutory-packs/{version_id}/activate", { params: { path: { version_id: q.data.pack.id }, header: { "If-Match": etag, "Idempotency-Key": key } } }),
    );
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    await Promise.all(["statutory-packs", "statutory-pack", "approvals"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    onAsked(out.data as unknown as Approval);
    close();
  };
  const p = q.data?.pack;
  const can = p && !p.activation && !waiting && p.readable && p.pinned_passed === p.pinned_total;
  return (
    <Dialog
      open={id !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      wide
      eyebrow="Statutory parameter packs"
      title={p ? p.title : "Pack version"}
      description={p ? `${p.id}: in force from ${date(p.effective_from)}${p.effective_to ? ` to ${date(p.effective_to)}` : ""}. Verified by ${p.verifier}; due for review ${date(p.review_date)}.` : undefined}
      footer={
        p && (
          <>
            <Button variant="ghost" onClick={close} disabled={busy}>
              Close
            </Button>
            {can && (
              <Button variant="primary" onClick={run} disabled={busy} title={activate.tooltip}>
                {activate.label}
              </Button>
            )}
          </>
        )
      }
    >
      {q.isError && <Refusal problem={problemOf(q.error)} title="The version could not be read" />}
      {problem && <Refusal problem={problem} title="The version was not activated" />}
      {p && (
        <>
          <div className="fy-row fy-row--wrap gap-2">
            <Tests passed={p.pinned_passed} total={p.pinned_total} />
            {p.activation && (
              <span className="fy-chip fy-chip--good">
                <Icon of={CircleCheck} />
                Active since {date(p.activation.activated_at)}
              </span>
            )}
            {!p.readable && <span className="fy-chip fy-chip--critical">This release cannot read it</span>}
          </div>
          {p.conditions && <p className="fy-small fy-secondary">{p.conditions}</p>}
          <section className="fy-stack fy-stack--2" aria-labelledby="pack-sources">
            <h3 className="fy-h3" id="pack-sources">
              Official sources
            </h3>
            <ul className="fy-list rounded-md border border-border" aria-labelledby="pack-sources">
              {(p.sources ?? []).map((s) => (
                <li key={s.id} className="fy-list__item">
                  <span className="fy-list__main">
                    <a className="fy-list__title whitespace-normal" href={s.url} target="_blank" rel="noreferrer noopener">
                      {s.title} <Icon of={ExternalLink} />
                    </a>
                    <span className="fy-list__meta whitespace-normal">
                      {s.kind}
                      {s.section ? ` · ${s.section}` : ""} · read {date(s.retrieved)}
                    </span>
                  </span>
                </li>
              ))}
            </ul>
          </section>
          <section className="fy-stack fy-stack--2" aria-labelledby="pack-tests">
            <h3 className="fy-h3" id="pack-tests">
              Pinned tests
            </h3>
            <div className="fy-tablewrap">
              <table className="fy-table fy-table--compact">
                <thead>
                  <tr>
                    <th scope="col">Case</th>
                    <th scope="col">Wants</th>
                    <th scope="col">Gets</th>
                    <th scope="col">Result</th>
                  </tr>
                </thead>
                <tbody>
                  {(p.pinned ?? []).map((t) => (
                    <tr key={`${t.table}-${t.case}`}>
                      <th scope="row" title={t.why}>
                        {t.case}
                      </th>
                      <td className="fy-num">{t.want}</td>
                      <td className="fy-num">{t.got}</td>
                      <td>{t.pass ? "Passes" : "Fails"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
          <p className="fy-mono fy-small break-all">SHA-256 {p.digest}</p>
        </>
      )}
    </Dialog>
  );
}

// StatutoryPacks is Settings › Statutory parameter packs (docs/design/screens StatutoryPacks; ENG-30,
// ENG-32, FR-ORG-05): each loaded version with its dates, its pinned tests and the organisation's
// activation. Activating one is a request that an Owner or Controller who did not ask approves (SOD-10).
export function StatutoryPacks() {
  const packs = useQuery({ queryKey: ["statutory-packs"], queryFn: listPacks });
  const waiting = useQuery({ queryKey: ["approvals", "packs"], queryFn: waitingActivations });
  const [open, setOpen] = useState<string | null>(null);
  const [asked, setAsked] = useState<Approval | null>(null);
  const pendingFor = (p: Pack) => (waiting.data ?? []).find((a) => a.object_key === p.id);
  return (
    <SettingsPage title="Statutory parameter packs" screen="statutory-packs" lead="GST rates and codes, and later CPF, SDL and SHG, from official sources. A new version is used only once the company activates it.">
      {asked && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={Hourglass} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Waiting for approval</span>
            <span className="fy-small">
              {asked.summary}. The version is not active yet.{" "}
              <Link to="/approvals/$approvalId" params={{ approvalId: asked.id }}>
                View the request
              </Link>
            </span>
          </div>
        </div>
      )}
      {packs.isError && <Refusal problem={problemOf(packs.error)} title="The packs could not be listed" />}
      {packs.isPending && <p className="fy-secondary">Loading the packs…</p>}
      {packs.data && (
        <section className="fy-card" aria-labelledby="packs-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="packs-title">
              Versions
            </h2>
            <span className="fy-badge fy-badge--quiet">{packs.data.length}</span>
          </div>
          <ul className="fy-list" aria-labelledby="packs-title">
            {packs.data.map((p) => {
              const w = pendingFor(p);
              return (
                <li key={p.id}>
                  <button type="button" className="fy-list__item w-full border-0 bg-transparent text-start font-[inherit]" onClick={() => setOpen(p.id)}>
                    <span className="fy-list__icon">
                      <Icon of={Scale} size={20} />
                    </span>
                    <span className="fy-list__main">
                      <span className="fy-list__title whitespace-normal">{p.title}</span>
                      <span className="fy-list__meta whitespace-normal">
                        <span className="fy-mono">{p.id}</span> · from {date(p.effective_from)}
                        {p.effective_to ? ` to ${date(p.effective_to)}` : ""}
                      </span>
                    </span>
                    {p.activation ? (
                      <span className="fy-chip fy-chip--good">
                        <Icon of={CircleCheck} />
                        Active
                      </span>
                    ) : w ? (
                      <span className="fy-chip fy-chip--neutral">
                        <Icon of={Hourglass} />
                        Waiting for approval
                      </span>
                    ) : (
                      <Tests passed={p.pinned_passed} total={p.pinned_total} />
                    )}
                  </button>
                </li>
              );
            })}
          </ul>
        </section>
      )}
      <PackVersion id={open} waiting={(packs.data ?? []).map(pendingFor).find((w) => w?.object_key === open) ?? undefined} onClose={() => setOpen(null)} onAsked={setAsked} />
    </SettingsPage>
  );
}
