import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, ClockAlert, Download, Plug, ShieldCheck, TriangleAlert, UserRound, Users } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { roleLabel } from "../approvals/words";
import { save } from "../reports/AccountantExports";
import { SettingsPage } from "./SettingsPage";


type Lists<T> = { [K in keyof T]-?: NonNullable<T[K]> extends (infer E)[] ? E[] : T[K] };
type Person = Lists<Omit<components["schemas"]["AccessReviewMember"], "conflicts">> & { conflicts: Lists<components["schemas"]["AccessReviewConflict"]>[] };
type Review = Omit<Lists<components["schemas"]["AccessReview"]>, "members" | "connectors"> & {
  members: Person[];
  connectors: Lists<components["schemas"]["AccessReviewConnector"]>[];
};

// review reads the review, every list an array: the API sends Go's nil slices as null.
async function review(): Promise<{ review: Review; etag: string }> {
  const r = await api.GET("/v1/access-review");
  if (r.error) throw r.error;
  const d = r.data;
  const members = (d.members ?? []).map((m) => ({
    ...m,
    roles: m.roles ?? [],
    expired_roles: m.expired_roles ?? [],
    conflicts: (m.conflicts ?? []).map((c) => ({ ...c, roles: c.roles ?? [] })),
  })) as Person[];
  const connectors = (d.connectors ?? []).map((c) => ({ ...c, fyniq_scopes: c.fyniq_scopes ?? [], data_classes: c.data_classes ?? [] })) as Review["connectors"];
  return { review: { ...d, members, connectors, invitations: d.invitations ?? [], attestations: d.attestations ?? [] } as Review, etag: etagOf(r.response) };
}

const factors: Record<string, string> = { passkey: "Passkey", sign_in: "Authenticator app", none: "Not yet" };

// flag is the one thing to review about a person, the most serious first, or null when there is none.
function flag(p: Person): { tone: string; icon: typeof TriangleAlert; label: string } | null {
  if (p.past_expiry) {
    const last = p.expired_roles.map((r) => r.expires_at ?? "").sort().pop();
    return { tone: "fy-chip--critical", icon: ClockAlert, label: `Ended ${date(last)}, still a member` };
  }
  if (p.conflicts.some((c) => c.covered === "no")) return { tone: "fy-chip--critical", icon: TriangleAlert, label: "Role conflict, not covered" };
  if (p.conflicts.length > 0) return { tone: "fy-chip--warning", icon: TriangleAlert, label: "Role conflict" };
  if (p.dormant) return { tone: "fy-chip--warning", icon: ClockAlert, label: "Not signed in for 30 days" };
  const ends = p.roles.map((r) => r.expires_at).filter(Boolean) as string[];
  if (ends.length > 0) return { tone: "fy-chip--neutral", icon: ClockAlert, label: `Until ${date(ends.sort()[0])}` };
  return null;
}

const covered: Record<string, string> = {
  founder_mode: "Founder mode allows it: another founder reviews what they do alone each week (FM-3).",
  override: "Standard mode blocks it; an approved override lets them keep it.",
  no: "Nothing covers it: the team mode blocks it and there is no override. End one of the roles.",
};

function Attest({ r, etag }: { r: Review; etag: string }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const { session } = useSignedIn();
  const noteId = useId();
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("access-review.attest");
  const q = r.to_attest;
  const owner = (session.roles ?? []).includes("owner");
  if (!q) return null;
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/access-review/attestations", {
        params: { header: { "If-Match": etag, "Idempotency-Key": key } },
        body: { quarter: q.quarter, ...(note.trim() ? { note: note.trim() } : {}) },
      }),
    );
    setBusy(false);
    if (out.error) {
      setProblem(problemOf(out.error));
      return;
    }
    setNote("");
    await Promise.all(["access-review", "checks"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
  };
  return (
    <section className="fy-card" id="attest" aria-labelledby="attest-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="attest-title">
          Attest {q.label}
        </h2>
      </div>
      <form className="fy-card__body fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <p className="fy-small fy-secondary">
          Attesting says you have reviewed everyone's access above as it stands today, and that it is right, or that you have asked for the changes it needs. It is
          recorded in the audit trail with your name and the report you saw.
        </p>
        {problem && <Refusal problem={problem} title="The quarter was not attested" />}
        <div className="fy-field">
          <label className="fy-label" htmlFor={noteId}>
            What you checked or asked to change (optional)
          </label>
          <textarea className="fy-textarea" id={noteId} value={note} maxLength={1000} onChange={(e) => setNote(e.target.value)} disabled={!owner} />
        </div>
        <div className="fy-row fy-row--wrap justify-end gap-3">
          {!owner && <span className="fy-small fy-secondary">Only an Owner can attest</span>}
          <Button variant="primary" type="submit" disabled={busy || !owner} title={h.tooltip}>
            <Icon of={ShieldCheck} />
            {busy ? "Attesting…" : `${h.label} ${q.label}`}
          </Button>
        </div>
      </form>
    </section>
  );
}

// AccessReview is Settings › Access review (docs/design/screens AccessReview; FR-CTL-06, SEC-06, rev 0.162): who can do
// what, live, for an Owner to review and attest once each calendar quarter ends. C.2 #7 warns until it is.
export function AccessReview() {
  const q = useQuery({ queryKey: ["access-review"], queryFn: review });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const exp = helpText("access-review.export");
  const r = q.data?.review;
  const download = async () => {
    setProblem(null);
    const out = await api.GET("/v1/access-review/export");
    if (out.error) {
      setProblem(problemOf(out.error));
      return;
    }
    save(out.data.file_name, out.data.media_type, out.data.content_base64);
  };
  const external = r?.members.filter((m) => m.external) ?? [];
  const conflicts = r?.members.flatMap((m) => m.conflicts.map((c) => ({ m, c }))) ?? [];

  return (
    <SettingsPage
      title="Access review"
      screen="access-review"
      lead="Who can do what in Fyniq, as it stands today. An Owner reviews it and attests it once a quarter."
      actions={
        <Button onClick={() => void download()} title={exp.tooltip} disabled={!r}>
          <Icon of={Download} />
          {exp.label}
        </Button>
      }
    >
      {problem && <Refusal problem={problem} title="The CSV was not made" />}
      {q.isError && <Refusal problem={problemOf(q.error)} title="The access review could not be read" />}
      {q.isPending && <p className="fy-secondary">Reading who can do what…</p>}
      {r && (
        <>
          {r.to_attest && (
            <div className={r.overdue ? "fy-banner fy-banner--warning" : "fy-banner fy-banner--info"} role="status">
              <Icon of={r.overdue ? TriangleAlert : ClockAlert} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong">{r.to_attest.label} is not attested</span>
                <span className="fy-small">
                  It is due by {date(r.to_attest.due_by)}.{" "}
                  {r.overdue ? `The check "Access review overdue" warns until an Owner attests it (C.2 #7).` : "An Owner attests it below."}
                </span>
              </div>
              <a className="fy-btn fy-btn--sm" href="#attest">
                Attest {r.to_attest.label}
              </a>
            </div>
          )}
          <div className="fy-grid fy-grid--4 gap-3">
            <div className="fy-tile">
              <span className="fy-tile__label">
                <Icon of={Users} />
                People with access
              </span>
              <span className="fy-tile__value fy-num">{r.members.length}</span>
              <span className="fy-tile__meta">
                {r.members.length - external.length} staff and {external.length} external
              </span>
            </div>
            <div className="fy-tile">
              <span className="fy-tile__label">
                <Icon of={TriangleAlert} />
                Role conflicts
              </span>
              <span className="fy-tile__value fy-num">{conflicts.length}</span>
              <span className="fy-tile__meta">
                {conflicts.some(({ c }) => c.covered === "no")
                  ? `${conflicts.filter(({ c }) => c.covered === "no").length} not covered`
                  : r.team_mode === "founder"
                    ? "Allowed in Founder mode"
                    : "Each with an override"}
              </span>
            </div>
            <div className="fy-tile">
              <span className="fy-tile__label">
                <Icon of={ClockAlert} />
                External access
              </span>
              <span className="fy-tile__value fy-num">{external.length}</span>
              <span className="fy-tile__meta">{external.filter((m) => m.past_expiry).length} ended, still a member</span>
            </div>
            <div className="fy-tile">
              <span className="fy-tile__label">
                <Icon of={Plug} />
                Integration clients
              </span>
              <span className="fy-tile__value fy-num">{r.connectors.length}</span>
              <span className="fy-tile__meta">{r.connectors.filter((c) => c.health === "failing").length} failing</span>
            </div>
          </div>

          <section className="fy-card" aria-labelledby="people-title">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="people-title">
                People and their access
              </h2>
              <span className="fy-badge fy-badge--quiet">{r.members.length}</span>
            </div>
            <div className="fy-tablewrap">
              <table className="fy-table fy-table--cards" aria-labelledby="people-title">
                <thead>
                  <tr>
                    <th scope="col">Person</th>
                    <th scope="col">Roles</th>
                    <th scope="col">Last sign-in</th>
                    <th scope="col" className="fy-expert-only">
                      Second factor
                    </th>
                    <th scope="col">Review</th>
                  </tr>
                </thead>
                <tbody>
                  {r.members.map((p) => {
                    const f = flag(p);
                    return (
                      <tr key={p.member_id}>
                        <td data-card="title" className="whitespace-normal">
                          <span className="fy-stack gap-0">
                            <span>{p.display_name}</span>
                            <span className="fy-small fy-secondary font-normal">{p.external ? "External" : `Since ${date(p.joined_at)}`}</span>
                          </span>
                        </td>
                        <td data-card="meta">
                          <span className="fy-row fy-row--wrap gap-1">
                            {p.roles.map((x) => (
                              <span key={x.role} className="fy-tag">
                                {roleLabel(x.role)}
                              </span>
                            ))}
                            {p.expired_roles.map((x) => (
                              <span key={`ended-${x.role}`} className="fy-tag fy-muted">
                                {roleLabel(x.role)} (ended)
                              </span>
                            ))}
                          </span>
                        </td>
                        <td data-card="meta" className="whitespace-nowrap">
                          {p.last_sign_in ? dateTime(p.last_sign_in) : "Never"}
                        </td>
                        <td data-card="meta" className="fy-expert-only">
                          {factors[p.second_factor] ?? p.second_factor}
                        </td>
                        <td data-card="status">
                          {f ? (
                            <span className={`fy-chip ${f.tone}`}>
                              <Icon of={f.icon} />
                              {f.label}
                            </span>
                          ) : (
                            <span className="fy-chip fy-chip--good">
                              <Icon of={CircleCheck} />
                              No issue
                            </span>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            <p className="fy-card__body fy-small fy-secondary">
              Change roles or remove someone in <Link to="/settings/members">Members and roles</Link>.
              {r.invitations.length > 0 && ` ${r.invitations.length} invitation${r.invitations.length === 1 ? "" : "s"} not yet used.`}
            </p>
          </section>

          <section className="fy-card" aria-labelledby="conflicts-title">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="conflicts-title">
                Role conflicts
              </h2>
              <span className="fy-badge fy-badge--quiet">{conflicts.length}</span>
            </div>
            {conflicts.length === 0 ? (
              <p className="fy-card__body fy-small fy-secondary">Nobody holds roles that conflict.</p>
            ) : (
              <ul className="fy-list" aria-labelledby="conflicts-title">
                {conflicts.map(({ m, c }) => (
                  <li key={`${m.member_id}-${c.sod}`} className="fy-list__item">
                    <span className={`fy-list__icon ${c.covered === "no" ? "fy-list__icon--critical" : ""}`}>
                      <Icon of={TriangleAlert} size={20} />
                    </span>
                    <span className="fy-list__main">
                      <span className="fy-list__title whitespace-normal">
                        {m.display_name}: {c.roles.map(roleLabel).join(" and ")}
                      </span>
                      <span className="fy-list__meta whitespace-normal">
                        {covered[c.covered]} {c.override_reason ? `Reason: ${c.override_reason}.` : ""} <span className="fy-mono fy-expert-only">({c.sod})</span>
                      </span>
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section className="fy-card" aria-labelledby="clients-title">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="clients-title">
                Integration clients
              </h2>
              <span className="fy-badge fy-badge--quiet">{r.connectors.length}</span>
            </div>
            {r.connectors.length === 0 ? (
              <p className="fy-card__body fy-small fy-secondary">No connector is installed.</p>
            ) : (
              <ul className="fy-list" aria-labelledby="clients-title">
                {r.connectors.map((c) => (
                  <li key={c.id} className="fy-list__item">
                    <span className="fy-list__icon">
                      <Icon of={Plug} size={20} />
                    </span>
                    <span className="fy-list__main">
                      <span className="fy-list__title whitespace-normal">{c.name}</span>
                      <span className="fy-list__meta whitespace-normal">
                        {c.fyniq_scopes.length > 0 ? c.fyniq_scopes.join(", ") : "No Fyniq scope"} · installed on {date(c.installed_at)}
                      </span>
                    </span>
                    <span className={`fy-chip ${c.health === "ok" ? "fy-chip--good" : c.health === "failing" ? "fy-chip--critical" : "fy-chip--neutral"}`}>
                      {c.health === "ok" ? "Healthy" : c.health === "failing" ? "Failing" : "Not tested"}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </section>

          <Attest r={r} etag={q.data?.etag ?? ""} />

          <section className="fy-card" aria-labelledby="earlier-title">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="earlier-title">
                Earlier quarters
              </h2>
            </div>
            {r.attestations.length === 0 ? (
              <p className="fy-card__body fy-small fy-secondary">None yet. Each attested quarter is kept here with the report as it was attested.</p>
            ) : (
              <ul className="fy-list" aria-labelledby="earlier-title">
                {r.attestations.map((a) => (
                  <li key={a.quarter.quarter} className="fy-list__item">
                    <span className="fy-list__icon fy-list__icon--brand">
                      <Icon of={UserRound} size={20} />
                    </span>
                    <span className="fy-list__main">
                      <span className="fy-list__title">{a.quarter.label}</span>
                      <span className="fy-list__meta whitespace-normal">
                        Attested by {a.attested_by_name ?? "a former member"} on {dateTime(a.attested_at)}
                        {a.note ? `: ${a.note}` : ""}
                        <span className="fy-mono fy-expert-only"> · SHA-256 {a.report_sha256.slice(0, 12)}…</span>
                      </span>
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </>
      )}
    </SettingsPage>
  );
}
