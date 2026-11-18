import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { BookOpen, Building, CircleCheck, CircleX, GitCompare, Hourglass, ListChecks, Minus, Pencil, PencilLine, Plus, RotateCcw, Search, Settings2, Sparkles, TriangleAlert, Upload } from "lucide-react";
import { useId, useMemo, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { members, personName, type Names } from "./AISettings";
import { SettingsPage } from "./SettingsPage";

type Entry = components["schemas"]["KBPageSummary"];
type Version = components["schemas"]["KBPageVersion"];
type Finding = components["schemas"]["KBFindingView"];
type Note = components["schemas"]["KBNote"];

async function pages(): Promise<Entry[]> {
  const { data, error } = await api.GET("/v1/knowledge-base/pages");
  if (error) throw error;
  return data.items;
}
async function versions(key: string): Promise<Version[]> {
  const { data, error } = await api.GET("/v1/knowledge-base/pages/{key}/versions", { params: { path: { key } } });
  if (error) throw error;
  return data.items;
}
async function findings(): Promise<Finding[]> {
  const { data, error } = await api.GET("/v1/knowledge-base/findings");
  if (error) throw error;
  return data.items;
}
async function snapshots() {
  const { data, error } = await api.GET("/v1/knowledge-base/snapshots");
  if (error) throw error;
  return data.items;
}
async function notes(): Promise<Note[]> {
  const { data, error } = await api.GET("/v1/knowledge-base/notes");
  if (error) throw error;
  return data.items;
}

// stateWords is where an organisation page stands, as the index and the page head say it.
const stateWords: Record<string, { label: string; tone: string; icon: typeof CircleCheck }> = {
  published: { label: "Published", tone: "fy-chip--good", icon: CircleCheck },
  draft: { label: "Newer draft", tone: "fy-chip--neutral", icon: PencilLine },
  unpublished: { label: "Not published", tone: "fy-chip--neutral", icon: PencilLine },
  superseded: { label: "Superseded", tone: "fy-chip--outline fy-chip--neutral", icon: Hourglass },
  platform: { label: "From the release", tone: "fy-chip--good", icon: CircleCheck },
};

const originWords: Record<string, string> = {
  rendered: "Rendered from settings",
  compiled: "Compiled by the knowledge-base compiler",
  human_edited: "Edited by a person",
  overlay: "Overlay of a platform page",
  rollback: "Restored from an earlier version",
};

function State({ state }: { state: string }) {
  const s = stateWords[state] ?? stateWords.draft!;
  return (
    <span className={`fy-chip ${s.tone}`}>
      <Icon of={s.icon} />
      {s.label}
    </span>
  );
}

// diffLines is a line-by-line comparison of two bodies by their longest common run of lines.
export function diffLines(a: string, b: string): { kind: "same" | "add" | "del"; text: string }[] {
  const x = a.split("\n");
  const y = b.split("\n");
  const n = x.length;
  const m = y.length;
  const common = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) common[i]![j] = x[i] === y[j] ? common[i + 1]![j + 1]! + 1 : Math.max(common[i + 1]![j]!, common[i]![j + 1]!);
  const out: { kind: "same" | "add" | "del"; text: string }[] = [];
  let i = 0;
  let j = 0;
  while (i < n || j < m) {
    if (i < n && j < m && x[i] === y[j]) out.push({ kind: "same", text: x[i++]! }), j++;
    else if (j < m && (i === n || common[i]![j + 1]! >= common[i + 1]![j]!)) out.push({ kind: "add", text: y[j++]! });
    else out.push({ kind: "del", text: x[i++]! });
  }
  return out;
}

// KnowledgeBase is Settings › AI › Knowledge base (docs/design/screens KnowledgeBase; AI-55, AI-56, AI-52, AI-54): the pages
// the assistant reads for know-how, Fyniq's and the organisation's, their versions, the notes the organisation's pages are
// compiled from, what the lint found, and publication. Nothing the assistant reads changes until a person publishes.
export function KnowledgeBase() {
  const id = useId();
  const h = helpText("settings.ai-knowledge-base");
  const queries = useQueryClient();
  const index = useQuery({ queryKey: ["kb-pages"], queryFn: pages });
  const found = useQuery({ queryKey: ["kb-findings"], queryFn: findings });
  const snaps = useQuery({ queryKey: ["kb-snapshots"], queryFn: snapshots });
  const noteList = useQuery({ queryKey: ["kb-notes"], queryFn: notes });
  const people = useQuery({ queryKey: ["members", "ai"], queryFn: members });
  const names: Names = { agents: [], connections: [], people: people.data ?? [] };
  const [which, setWhich] = useState<"all" | "platform" | "organisation">("all");
  const [search, setSearch] = useState("");
  const [open, setOpen] = useState<string | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [said, setSaid] = useState<string | null>(null);
  const refresh = () => Promise.all(["kb-pages", "kb-findings", "kb-snapshots", "kb-notes", "kb-versions"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
  const all = index.data ?? [];
  const shown = useMemo(() => {
    const q = search.trim().toLowerCase();
    return all.filter((e) => (which === "all" || e.source === which) && (q === "" || `${e.key} ${e.title} ${e.summary}`.toLowerCase().includes(q)));
  }, [all, which, search]);
  const own = all.filter((e) => e.source === "organisation");
  const drafts = own.filter((e) => e.state !== "published");
  const open_ = (found.data ?? []).filter((f) => f.status === "open");
  const live = snaps.data?.[0]?.number;
  return (
    <SettingsPage title={h.label} lead={h.tooltip} screen="knowledge-base"
      actions={
        <Link className="fy-btn fy-btn--secondary fy-btn--sm" to="/settings/ai">
          <Icon of={Settings2} />
          {helpText("settings.ai").label}
        </Link>
      }>
      {index.isError && <Refusal problem={problemOf(index.error)} title="The knowledge base could not be shown" />}
      {problem && <Refusal problem={problem} title="That was not done" />}
      {said && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={Hourglass} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{said}</span>
          </div>
        </div>
      )}

      <section className="fy-grid fy-grid--4 gap-3" aria-label="The knowledge base at a glance">
        <div className="fy-tile">
          <span className="fy-tile__label">
            <Icon of={BookOpen} />
            Platform pages
          </span>
          <span className="fy-tile__value fy-num">{all.filter((e) => e.source === "platform").length}</span>
          <span className="fy-tile__meta">From the release, reviewed in git</span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">
            <Icon of={Building} />
            Organisation pages
          </span>
          <span className="fy-tile__value fy-num">{own.length}</span>
          <span className="fy-tile__meta">
            {live ? `Snapshot ${live}` : "Nothing published yet"} · {drafts.length} waiting
          </span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">
            <Icon of={ListChecks} />
            Lint findings
          </span>
          <span className="fy-tile__value fy-num">{open_.length}</span>
          <span className="fy-tile__meta">{open_.length === 0 ? "None open" : "Open ones stop their page's publication"}</span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">
            <Icon of={Sparkles} />
            Notes
          </span>
          <span className="fy-tile__value fy-num">{noteList.data?.length ?? 0}</span>
          <span className="fy-tile__meta">The organisation's pages are made from these</span>
        </div>
      </section>

      <section className="fy-card" aria-labelledby={`${id}-index`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-index`}>
            Index
          </h2>
          <div className="fy-segmented" role="radiogroup" aria-label="Which pages">
            {(["all", "platform", "organisation"] as const).map((w) => (
              <button key={w} type="button" role="radio" aria-checked={which === w} onClick={() => setWhich(w)}>
                {w === "all" ? `All ${all.length}` : w === "platform" ? `Platform ${all.filter((e) => e.source === w).length}` : `Organisation ${own.length}`}
              </button>
            ))}
          </div>
        </div>
        <div className="fy-card__body">
          <div className="fy-field">
            <label className="fy-label fy-sr" htmlFor={`${id}-search`}>
              Search pages
            </label>
            <div className="fy-inputgroup">
              <span className="fy-inputgroup__addon">
                <Icon of={Search} />
              </span>
              <input className="fy-input" id={`${id}-search`} placeholder="Search keys, titles and summaries" value={search} onChange={(e) => setSearch(e.target.value)} />
            </div>
          </div>
        </div>
        <table className="fy-table fy-table--cards fy-table--compact">
          <thead>
            <tr>
              <th scope="col">Key and title</th>
              <th scope="col">Written by</th>
              <th scope="col">Data class</th>
              <th scope="col">Status</th>
            </tr>
          </thead>
          <tbody>
            {shown.map((e) => (
              <tr key={`${e.source}:${e.key}`}>
                <td data-card="title">
                  {e.source === "organisation" ? (
                    <button type="button" className="fy-link fy-mono" onClick={() => setOpen(e.key)} aria-label={`Open ${e.key}`}>
                      {e.key}
                    </button>
                  ) : (
                    <span className="fy-mono">{e.key}</span>
                  )}
                  <span className="fy-small fy-muted block font-normal">{e.title}</span>
                </td>
                <td data-card="meta" className="fy-small">
                  {e.source === "platform" ? (
                    <>
                      <span className="fy-ai-badge">
                        <Icon of={Sparkles} />
                        AI
                      </span>{" "}
                      compiled, reviewed in git
                    </>
                  ) : (
                    (originWords[e.origin] ?? e.origin)
                  )}
                </td>
                <td data-card="meta" className="fy-small">
                  {e.data_class}
                </td>
                <td data-card="status">
                  <State state={e.state} />
                  {e.source === "organisation" && <span className="fy-small fy-muted"> v{e.newest_version}</span>}
                  {e.open_findings > 0 && (
                    <span className="fy-chip fy-chip--warning">
                      <Icon of={TriangleAlert} />
                      {e.open_findings} open
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {shown.length === 0 && !index.isError && <p className="fy-small fy-muted p-4">{index.data ? "No page matches." : "Loading the pages…"}</p>}
        <div className="fy-card__foot fy-small fy-muted">
          <Icon of={Search} />
          <span>The assistant reads the index first, then searches pages, aliases and sources by their words. There are no embeddings. Each reader sees only the pages their roles and data rights allow.</span>
        </div>
      </section>

      {open && <PageCard pageKey={open} names={names} onClose={() => setOpen(null)} onDone={refresh} setProblem={setProblem} />}

      <PublishCard drafts={drafts} onOpen={setOpen} onDone={refresh} setProblem={setProblem} setSaid={setSaid} />

      <FindingsCard items={found.data ?? []} names={names} error={found.isError ? problemOf(found.error) : null} onDone={refresh} setProblem={setProblem} />

      <NotesCard items={noteList.data ?? []} onDone={refresh} setProblem={setProblem} />

      <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-how`}>
        <h2 className="fy-h3" id={`${id}-how`}>
          What the assistant may use
        </h2>
        <ul className="fy-stack fy-stack--2 fy-small fy-secondary">
          <li>Know-how only. Figures come from read tools, never from pages.</li>
          <li>Citations point people to the user guide or the record, not to these pages.</li>
          <li>Answers never become pages by themselves; a page the assistant reads is one a person published.</li>
        </ul>
        <p className="fy-small fy-muted">Owners can edit and publish, Administrators can edit and ask, Auditors can view. Every action is audited.</p>
      </section>
    </SettingsPage>
  );
}

type Setter = (p: ApiProblem | null) => void;

// PageCard is one organisation page: its versions, a comparison of any two, an edit that makes the next version and a roll back
// that makes a new version of an earlier one's content.
function PageCard({ pageKey, names, onClose, onDone, setProblem }: { pageKey: string; names: Names; onClose: () => void; onDone: () => Promise<unknown>; setProblem: Setter }) {
  const id = useId();
  const got = useQuery({ queryKey: ["kb-versions", pageKey], queryFn: () => versions(pageKey) });
  const vs = got.data ?? [];
  const newest = vs[0];
  const [from, setFrom] = useState<number | null>(null);
  const [editing, setEditing] = useState(false);
  const [body, setBody] = useState("");
  const [title, setTitle] = useState("");
  const [summary, setSummary] = useState("");
  const [busy, setBusy] = useState(false);
  const a = vs.find((v) => v.number === (from ?? vs[1]?.number ?? newest?.number));
  const lines = a && newest ? diffLines(a.body, newest.body) : [];
  const save = async (e: FormEvent) => {
    e.preventDefault();
    if (!newest) return;
    setBusy(true);
    setProblem(null);
    const { error } = await api.PUT("/v1/knowledge-base/pages/{key}", {
      params: { path: { key: pageKey }, header: { "If-Match": `"v${newest.number}"`, "Idempotency-Key": newKey() } },
      body: { type: newest.type as components["schemas"]["KBPageInput"]["type"], title, summary, aliases: newest.aliases, body, sources: newest.sources },
    });
    setBusy(false);
    if (error) return setProblem(problemOf(error));
    setEditing(false);
    await onDone();
  };
  const rollback = async (n: number) => {
    setBusy(true);
    setProblem(null);
    const { error } = await api.POST("/v1/knowledge-base/pages/{key}/rollback", { params: { path: { key: pageKey }, header: { "Idempotency-Key": newKey() } }, body: { version: n } });
    setBusy(false);
    if (error) return setProblem(problemOf(error));
    await onDone();
  };
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__body fy-stack">
        <div className="fy-row fy-row--between fy-row--wrap">
          <div className="fy-stack fy-stack--2">
            <span className="fy-eyebrow">Organisation page{newest ? ` · ${newest.type}` : ""}</span>
            <h2 className="fy-h2" id={`${id}-title`}>
              {newest?.title ?? pageKey}
            </h2>
            <div className="fy-row fy-row--wrap">
              <span className="fy-mono fy-small">{pageKey}</span>
              {newest && <State state={newest.state} />}
              {newest?.human_edited && <span className="fy-tag">Human-edited</span>}
            </div>
          </div>
          <div className="fy-row fy-row--wrap">
            {newest && !editing && (
              <Button size="sm" variant="secondary" onClick={() => (setBody(newest.body), setTitle(newest.title), setSummary(newest.summary), setEditing(true))}>
                <Icon of={Pencil} />
                Edit
              </Button>
            )}
            <Button size="sm" variant="ghost" onClick={onClose}>
              Close
            </Button>
          </div>
        </div>
        <p className="fy-help">A recompile never overwrites a human-edited version: the lint flags the difference for a person. Rolling back makes a new version.</p>
        {got.isError && <Refusal problem={problemOf(got.error)} title="The page could not be shown" />}
        {editing && newest && (
          <form className="fy-stack fy-stack--3" onSubmit={save}>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-t`}>
                Title
              </label>
              <input className="fy-input" id={`${id}-t`} value={title} maxLength={200} onChange={(e) => setTitle(e.target.value)} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-s`}>
                Summary
              </label>
              <input className="fy-input" id={`${id}-s`} value={summary} maxLength={160} onChange={(e) => setSummary(e.target.value)} />
              <span className="fy-help">One line the index shows, at most 160 characters.</span>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-b`}>
                Page text
              </label>
              <textarea className="fy-textarea fy-mono" id={`${id}-b`} rows={14} value={body} onChange={(e) => setBody(e.target.value)} />
              <span className="fy-help">Each section ends with (src: note:key#anchor), naming a source this page lists. Every number must be in a source it cites.</span>
            </div>
            <div className="fy-row">
              <Button type="submit" disabled={busy}>
                Save as a draft
              </Button>
              <Button type="button" variant="ghost" onClick={() => setEditing(false)}>
                Cancel
              </Button>
            </div>
          </form>
        )}
        {vs.length > 1 && newest && (
          <div className="fy-stack fy-stack--3">
            <div className="fy-row fy-row--between fy-row--wrap">
              <h3 className="fy-h3">
                <Icon of={GitCompare} /> Compare versions
              </h3>
              <div className="fy-row fy-row--wrap fy-small">
                <label className="fy-sr" htmlFor={`${id}-from`}>
                  From version
                </label>
                <select className="fy-select" id={`${id}-from`} value={a?.number} onChange={(e) => setFrom(Number(e.target.value))}>
                  {vs.slice(1).map((v) => (
                    <option key={v.number} value={v.number}>
                      v{v.number} · {v.state}
                    </option>
                  ))}
                </select>
                <span>to the newest, v{newest.number}</span>
              </div>
            </div>
            <div className="fy-card fy-card--flat fy-small" role="group" aria-label="What changed">
              {lines.map((l, i) => (
                <div key={i} className="fy-row fy-row--top px-3 py-1.5" style={{ background: l.kind === "add" ? "var(--status-good-subtle)" : l.kind === "del" ? "var(--status-critical-subtle)" : undefined }}>
                  {l.kind === "add" ? <Icon of={Plus} /> : l.kind === "del" ? <Icon of={Minus} /> : <span aria-hidden="true" className="w-4 flex-none" />}
                  <span className="fy-sr">{l.kind === "add" ? "Added:" : l.kind === "del" ? "Removed:" : "Unchanged:"}</span>
                  <span className="fy-mono whitespace-pre-wrap">{l.text || " "}</span>
                </div>
              ))}
            </div>
          </div>
        )}
        <div className="fy-stack fy-stack--2">
          <h3 className="fy-h3">Versions</h3>
          <ol className="fy-timeline pl-7">
            {vs.map((v) => (
              <li key={v.number} className="fy-timeline__item">
                <span className={`fy-timeline__dot${v.state === "published" ? " fy-timeline__dot--good" : v.origin === "compiled" ? " fy-timeline__dot--ai" : ""}`}>
                  <Icon of={v.state === "published" ? CircleCheck : v.origin === "compiled" ? Sparkles : Pencil} />
                </span>
                <span className="fy-small fy-strong">
                  v{v.number} · {stateWords[v.state]?.label.toLowerCase()} · {originWords[v.origin] ?? v.origin}
                </span>
                <span className="fy-small fy-muted">
                  {dateTime(v.created_at)} · {v.author === "kb-compiler" ? "the compiler" : v.author === "system" ? "Fyniq" : personName(v.author, names)}
                  {v.restores ? ` · restores v${v.restores}` : ""}
                </span>
                {v.number !== newest?.number && (
                  <Button size="sm" variant="ghost" disabled={busy} onClick={() => rollback(v.number)} aria-label={`Roll back to v${v.number}`}>
                    <Icon of={RotateCcw} />
                    Roll back to v{v.number}
                  </Button>
                )}
              </li>
            ))}
          </ol>
        </div>
        {newest && newest.sources.length > 0 && (
          <div className="fy-stack fy-stack--2">
            <h3 className="fy-h3">Sources</h3>
            <ul className="fy-stack fy-stack--2 fy-small">
              {newest.sources.map((s) => (
                <li key={`${s.kind}:${s.ref}`}>
                  <span className="fy-mono">
                    {s.kind}:{s.ref}
                  </span>
                  {s.title ? ` · ${s.title}` : ""}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </section>
  );
}

// PublishCard publishes the drafts a person chooses. In Founder mode an Owner publishes alone and it waits in the weekly review;
// in Team mode the API says another Owner signs, and the request is made.
function PublishCard({ drafts, onOpen, onDone, setProblem, setSaid }: { drafts: Entry[]; onOpen: (key: string) => void; onDone: () => Promise<unknown>; setProblem: Setter; setSaid: (s: string | null) => void }) {
  const id = useId();
  const [chosen, setChosen] = useState<Record<string, boolean>>({});
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const keys = drafts.filter((d) => chosen[d.key]);
  const publish = async () => {
    setBusy(true);
    setProblem(null);
    setSaid(null);
    const body = { pages: keys.map((d) => ({ key: d.key, version: d.newest_version })), ...(reason.trim() ? { reason: reason.trim() } : {}) };
    const key = newKey();
    const made = await api.POST("/v1/knowledge-base/publications", { params: { header: { "Idempotency-Key": key } }, body });
    if (made.error && problemOf(made.error).code === "KB-TEAM-01") {
      const asked = await api.POST("/v1/knowledge-base/publications/approval", { params: { header: { "Idempotency-Key": newKey() } }, body });
      setBusy(false);
      if (asked.error) return setProblem(problemOf(asked.error));
      setSaid("The publication waits for another Owner to sign it. It is in the approvals.");
    } else {
      setBusy(false);
      if (made.error) return setProblem(problemOf(made.error));
      setSaid(`Published as snapshot ${made.data.number}. The assistant reads these pages from now on.`);
    }
    setChosen({});
    setReason("");
    await onDone();
  };
  return (
    <section className="fy-card" aria-labelledby={`${id}-pub`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-pub`}>
          Publish
        </h2>
        <span className="fy-badge fy-badge--quiet">{drafts.length}</span>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {drafts.length === 0 ? (
          <p className="fy-small fy-muted">Every page is published. A new draft appears here when a note is compiled or a page is edited.</p>
        ) : (
          <>
            <p className="fy-small fy-secondary">The assistant reads a page only once it is published. Each draft is checked before it is: its sections cite their sources, every number is in them, and no finding is open.</p>
            <ul className="fy-stack fy-stack--2">
              {drafts.map((d) => (
                <li key={d.key} className="fy-row fy-row--wrap">
                  <input type="checkbox" id={`${id}-${d.key}`} checked={chosen[d.key] ?? false} onChange={(e) => setChosen({ ...chosen, [d.key]: e.target.checked })} />
                  <label htmlFor={`${id}-${d.key}`}>
                    <span className="fy-mono">{d.key}</span> v{d.newest_version}
                  </label>
                  <span className="fy-small fy-muted">{originWords[d.origin] ?? d.origin}</span>
                  <Button size="sm" variant="ghost" onClick={() => onOpen(d.key)} aria-label={`Open ${d.key}`}>
                    Open
                  </Button>
                </li>
              ))}
            </ul>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-why`}>
                Why (optional)
              </label>
              <input className="fy-input" id={`${id}-why`} value={reason} maxLength={1000} onChange={(e) => setReason(e.target.value)} />
              <span className="fy-help">Kept with the snapshot and shown in the weekly review.</span>
            </div>
            <div className="fy-row">
              <Button disabled={busy || keys.length === 0} onClick={publish}>
                <Icon of={Upload} />
                Publish {keys.length || ""} page{keys.length === 1 ? "" : "s"}
              </Button>
            </div>
          </>
        )}
      </div>
    </section>
  );
}

// FindingsCard lists what the lint and the compiler found; a person resolves one with what was done about it.
function FindingsCard({ items, names, error, onDone, setProblem }: { items: Finding[]; names: Names; error: ApiProblem | null; onDone: () => Promise<unknown>; setProblem: Setter }) {
  const id = useId();
  const [resolving, setResolving] = useState<string | null>(null);
  const [text, setText] = useState("");
  const open = items.filter((f) => f.status === "open");
  const resolve = async (f: Finding) => {
    setProblem(null);
    const { error: err } = await api.POST("/v1/knowledge-base/findings/{finding_id}/resolve", { params: { path: { finding_id: f.id }, header: { "Idempotency-Key": newKey() } }, body: { resolution: text } });
    if (err) return setProblem(problemOf(err));
    setResolving(null);
    setText("");
    await onDone();
  };
  return (
    <section className="fy-card" aria-labelledby={`${id}-lint`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-lint`}>
          Lint findings
        </h2>
        <span className="fy-badge fy-badge--quiet">{open.length}</span>
      </div>
      {error && <Refusal problem={error} title="The findings could not be shown" />}
      {open.length === 0 && !error && <p className="fy-small fy-muted p-4">No finding is open.</p>}
      <ul className="fy-list">
        {open.map((f) => (
          <li key={f.id} className="fy-list__item items-start">
            <span className="fy-list__icon fy-list__icon--warning">
              <Icon of={f.kind === "contradiction" ? CircleX : TriangleAlert} size={20} />
            </span>
            <span className="fy-list__main">
              <span className="fy-list__title whitespace-normal">{findingWords[f.kind] ?? f.kind}</span>
              <span className="fy-small fy-secondary">
                {f.page} v{f.version}: {f.detail}
              </span>
              <span className="fy-list__meta">{dateTime(f.created_at)}</span>
              {resolving === f.id ? (
                <span className="fy-stack fy-stack--2">
                  <label className="fy-label" htmlFor={`${id}-r-${f.id}`}>
                    What was done about it
                  </label>
                  <input className="fy-input" id={`${id}-r-${f.id}`} value={text} maxLength={1000} onChange={(e) => setText(e.target.value)} />
                  <span className="fy-row">
                    <Button size="sm" disabled={text.trim() === ""} onClick={() => resolve(f)}>
                      Resolve
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => setResolving(null)}>
                      Cancel
                    </Button>
                  </span>
                </span>
              ) : (
                <span className="fy-row fy-row--wrap">
                  <Button size="sm" variant="secondary" onClick={() => (setResolving(f.id), setText(""))} aria-label={`Resolve: ${f.detail}`}>
                    Resolve
                  </Button>
                </span>
              )}
            </span>
          </li>
        ))}
      </ul>
      {items.some((f) => f.status === "resolved") && (
        <div className="fy-card__foot fy-small fy-muted">
          {items.filter((f) => f.status === "resolved").length} resolved, the latest by {personName(items.find((f) => f.status === "resolved")?.resolved_by ?? "", names)}.
        </div>
      )}
      <div className="fy-card__foot fy-small fy-muted">Checked before every publication; the compiler adds a finding when a note changes under a page a person edited.</div>
    </section>
  );
}

const findingWords: Record<string, string> = {
  human_edit_conflict: "A recompile waits beside a person's edit",
  contradiction: "Contradicts a setting",
  stale_source: "A source changed",
  ungrounded_number: "A number is not in its source",
  uncompiled_note: "A note has no page",
};

const kinds = [
  ["note", "A note"],
  ["policy", "A policy"],
  ["decision", "A decision"],
] as const;

// NotesCard is where the organisation writes what its pages are compiled from: notes, policies and decisions, in words the
// assistant may explain. A note holding an identity number, a bank account number or a secret is refused (AI-02).
function NotesCard({ items, onDone, setProblem }: { items: Note[]; onDone: () => Promise<unknown>; setProblem: Setter }) {
  const id = useId();
  const [editing, setEditing] = useState<Note | "new" | null>(null);
  const [f, setF] = useState({ key: "", kind: "note" as Note["kind"], title: "", body: "", data_class: "general" as Note["data_class"] });
  const [busy, setBusy] = useState(false);
  const begin = (n: Note | "new") => {
    setEditing(n);
    setProblem(null);
    setF(n === "new" ? { key: "", kind: "note", title: "", body: "", data_class: "general" } : { key: n.key, kind: n.kind, title: n.title, body: n.body, data_class: n.data_class });
  };
  const save = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const body = { kind: f.kind, title: f.title, body: f.body, data_class: f.data_class };
    const out =
      editing === "new"
        ? await api.POST("/v1/knowledge-base/notes", { params: { header: { "Idempotency-Key": newKey() } }, body: { ...body, key: f.key } })
        : await api.PUT("/v1/knowledge-base/notes/{key}", { params: { path: { key: f.key }, header: { "If-Match": `"v${(editing as Note).version}"`, "Idempotency-Key": newKey() } }, body });
    setBusy(false);
    if (out.error) return setProblem(problemOf(out.error));
    setEditing(null);
    await onDone();
  };
  return (
    <section className="fy-card" aria-labelledby={`${id}-notes`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-notes`}>
          Notes, policies and decisions
        </h2>
        <Button size="sm" variant="secondary" onClick={() => begin("new")}>
          <Icon of={Plus} />
          Write a note
        </Button>
      </div>
      {items.length === 0 && editing === null && <p className="fy-small fy-muted p-4">No note yet. Write what a colleague would ask about, such as how expense claims are made, and the compiler turns it into a page for you to publish.</p>}
      <ul className="fy-list">
        {items.map((n) => (
          <li key={n.key} className="fy-list__item">
            <span className="fy-list__main">
              <span className="fy-list__title">
                {n.title} <span className="fy-mono fy-small fy-muted">{n.key}</span>
              </span>
              <span className="fy-list__meta">
                {n.kind} · {n.data_class} · v{n.version}
              </span>
            </span>
            <Button size="sm" variant="ghost" onClick={() => begin(n)} aria-label={`Edit ${n.title}`}>
              <Icon of={Pencil} />
              Edit
            </Button>
          </li>
        ))}
      </ul>
      {editing !== null && (
        <form className="fy-card__body fy-stack fy-stack--3" onSubmit={save}>
          {editing === "new" && (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-key`}>
                Key
              </label>
              <input className="fy-input fy-mono" id={`${id}-key`} value={f.key} maxLength={100} required onChange={(e) => setF({ ...f, key: e.target.value })} />
              <span className="fy-help">Lower-case words, dots and hyphens, such as expense-policy. It never changes.</span>
            </div>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-kind`}>
              What it is
            </label>
            <select className="fy-select" id={`${id}-kind`} value={f.kind} onChange={(e) => setF({ ...f, kind: e.target.value as Note["kind"] })}>
              {kinds.map(([k, l]) => (
                <option key={k} value={k}>
                  {l}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-title`}>
              Title
            </label>
            <input className="fy-input" id={`${id}-title`} value={f.title} maxLength={200} required onChange={(e) => setF({ ...f, title: e.target.value })} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-body`}>
              What it says
            </label>
            <textarea className="fy-textarea" id={`${id}-body`} rows={8} value={f.body} maxLength={20000} required onChange={(e) => setF({ ...f, body: e.target.value })} />
            <span className="fy-help">In words the assistant may explain to colleagues. No identity or bank account numbers, and no secrets.</span>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-class`}>
              Strictest data it holds
            </label>
            <select className="fy-select" id={`${id}-class`} value={f.data_class} onChange={(e) => setF({ ...f, data_class: e.target.value as Note["data_class"] })}>
              <option value="general">General</option>
              <option value="personal">Personal</option>
              <option value="payroll">Payroll</option>
              <option value="equity">Equity</option>
            </select>
            <span className="fy-help">The page made from it is shown only to people who may read that data.</span>
          </div>
          <div className="fy-row">
            <Button type="submit" disabled={busy}>
              Save the note
            </Button>
            <Button type="button" variant="ghost" onClick={() => setEditing(null)}>
              Cancel
            </Button>
          </div>
        </form>
      )}
    </section>
  );
}
