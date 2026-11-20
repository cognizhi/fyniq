import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Plus, Save, Send } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Update = components["schemas"]["InvestorUpdate"];
type Member = components["schemas"]["Member"];

// InvestorUpdates is Equity › Investor updates (docs/design/screens InvestorUpdates; FR-EQ-23): an update is built from a published board pack, with
// its own highlights and asks, and sent once to the members named. Each is told by a notification that links to the signed-in update, which carries
// no figure; the page shows who has opened it. A sent update never changes.
export function InvestorUpdates() {
  const id = useId();
  const client = useQueryClient();
  const list = useQuery({
    queryKey: ["investor-updates"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/investor-updates", { params: { query: { page_size: 200 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const packs = useQuery({
    queryKey: ["board-packs", "published"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/board-packs", { params: { query: { page_size: 200 } } });
      return r.error ? [] : (r.data.items ?? []).filter((p) => p.status === "published");
    },
  });
  const members = useQuery({
    queryKey: ["members", "investors"],
    queryFn: async () => {
      const r = await api.GET("/v1/members", { params: { query: { page_size: 200 } } });
      return r.error ? ([] as Member[]) : (r.data.items ?? []);
    },
  });
  const [openId, setOpenId] = useState("");
  const [packId, setPackId] = useState("");
  const [title, setTitle] = useState("");
  const [kind, setKind] = useState<"monthly" | "quarterly">("monthly");
  const [edit, setEdit] = useState<{ title: string; highlights: string; asks: string } | null>(null);
  const [to, setTo] = useState<string[]>([]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("equity.updates");
  const u: Update | undefined = (list.data ?? []).find((x) => x.id === openId);
  const draft = u?.status === "draft";
  const t = edit ?? { title: u?.title ?? "", highlights: u?.highlights ?? "", asks: u?.asks ?? "" };
  const refresh = () => client.invalidateQueries({ queryKey: ["investor-updates"] });

  const make = async () => {
    const { data, error } = await api.POST("/v1/investor-updates", { body: { pack_id: packId || packs.data?.[0]?.id || "", kind, title } });
    if (error) return setProblem(problemOf(error));
    setProblem(null);
    setOpenId(data.id);
    setEdit(null);
    await refresh();
  };
  const save = async () => {
    if (!u) return;
    const { error } = await api.PUT("/v1/investor-updates/{id}", { params: { path: { id: u.id }, header: { "If-Match": `"${u.edit}"` } }, body: t });
    if (error) return setProblem(problemOf(error));
    setProblem(null);
    setEdit(null);
    await refresh();
  };
  const send = async () => {
    if (!u) return;
    const { error } = await api.POST("/v1/investor-updates/{id}/send", { params: { path: { id: u.id }, header: { "If-Match": `"${u.edit}"` } }, body: { recipients: to } });
    if (error) return setProblem(problemOf(error));
    setProblem(null);
    await refresh();
  };

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/equity">Equity</Link>
          </nav>
          <h1 className="fy-h1">Investor updates</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>
      {list.error && <Refusal problem={problemOf(list.error)} title="The investor updates could not be shown" />}
      <section className="fy-card fy-card--pad" aria-labelledby={`${id}-new`}>
        <h2 className="fy-h3" id={`${id}-new`}>
          A new update
        </h2>
        <div className="fy-row fy-row--wrap gap-3">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-pack`}>
              From the published pack
            </label>
            <select className="fy-select" id={`${id}-pack`} value={packId} onChange={(e) => setPackId(e.target.value)}>
              {(packs.data ?? []).map((p) => (
                <option key={p.id} value={p.id}>
                  {p.month}, version {p.version}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-kind`}>
              Kind
            </label>
            <select className="fy-select" id={`${id}-kind`} value={kind} onChange={(e) => setKind(e.target.value as "monthly" | "quarterly")}>
              <option value="monthly">Monthly</option>
              <option value="quarterly">Quarterly</option>
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-title`}>
              Title
            </label>
            <input className="fy-input" id={`${id}-title`} value={title} maxLength={120} onChange={(e) => setTitle(e.target.value)} />
          </div>
          <Button variant="secondary" onClick={make} disabled={title.trim() === "" || (packs.data ?? []).length === 0}>
            <Icon of={Plus} /> Start the update
          </Button>
        </div>
        {(packs.data ?? []).length === 0 && <p className="fy-secondary">Publish a board pack first: an update is built from one.</p>}
      </section>
      {problem && <Refusal problem={problem} title="The investor update could not be saved" />}
      <section className="fy-card fy-card--pad" aria-labelledby={`${id}-list`}>
        <h2 className="fy-h3" id={`${id}-list`}>
          Updates
        </h2>
        {(list.data ?? []).length === 0 && <p className="fy-secondary">No update yet.</p>}
        <ul className="fy-list">
          {(list.data ?? []).map((x) => (
            <li key={x.id}>
              <button type="button" className="fy-link" aria-current={x.id === openId ? "true" : undefined} onClick={() => { setOpenId(x.id); setEdit(null); }}>
                {x.title}
              </button>{" "}
              <span className="fy-muted fy-small">
                {x.status === "sent" ? `sent to ${x.recipients.length}, read by ${x.read_by?.length ?? 0}` : "draft"}
              </span>
            </li>
          ))}
        </ul>
      </section>
      {u && (
        <section className="fy-card fy-card--pad" aria-labelledby={`${id}-edit`}>
          <h2 className="fy-h3" id={`${id}-edit`}>
            {u.title}
          </h2>
          {(["title", "highlights", "asks"] as const).map((k) => (
            <div className="fy-field" key={k}>
              <label className="fy-label" htmlFor={`${id}-f-${k}`}>
                {k === "title" ? "Title" : k === "highlights" ? "Highlights" : "Asks"}
              </label>
              {k === "title" ? (
                <input className="fy-input" id={`${id}-f-${k}`} value={t[k]} readOnly={!draft} onChange={(e) => setEdit({ ...t, [k]: e.target.value })} />
              ) : (
                <textarea className="fy-textarea" id={`${id}-f-${k}`} rows={3} value={t[k]} readOnly={!draft} onChange={(e) => setEdit({ ...t, [k]: e.target.value })} />
              )}
            </div>
          ))}
          {draft && (
            <>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-to`}>
                  Send to
                </label>
                <select className="fy-select" id={`${id}-to`} multiple value={to} onChange={(e) => setTo(Array.from(e.target.selectedOptions, (o) => o.value))}>
                  {(members.data ?? []).map((m) => (
                    <option key={m.user_id} value={m.user_id}>
                      {m.display_name}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fy-row gap-3">
                <Button variant="secondary" onClick={save}>
                  <Icon of={Save} /> Save the texts
                </Button>
                <Button onClick={send} disabled={to.length === 0 || edit !== null}>
                  <Icon of={Send} /> Send it
                </Button>
              </div>
              <p className="fy-small fy-muted">Each person is told by a notification that links to the update after they sign in. It carries no figure, and a sent update never changes.</p>
            </>
          )}
        </section>
      )}
    </div>
  );
}
