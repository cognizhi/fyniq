import { useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";
import { api } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { helpText } from "../../i18n";
import { problemOf } from "../../ui";
import { PackFigures } from "../reports/PackFigures";

// BoardPortal is where a Board viewer or an investor reads the numbers (docs/design/screens BoardPortal; FR-EQ-24, FR-RPT-06, FR-EQ-23): the
// published board packs, the investor updates sent to them, and their own holding. It needs their own sign-in; the pack's figures are the frozen
// ones, never recomputed. The full cap table opens only when an Owner has granted it (not built yet).
export function BoardPortal() {
  const id = useId();
  const h = helpText("board.portal");
  const packs = useQuery({
    queryKey: ["board-packs", "portal"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/board-packs", { params: { query: { page_size: 50 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const updates = useQuery({
    queryKey: ["investor-updates", "portal"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/investor-updates", { params: { query: { page_size: 50 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const holding = useQuery({
    queryKey: ["my-holding"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/me/holding", {});
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const whole = useQuery({
    queryKey: ["cap-table", "granted"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/cap-table/granted", { params: { query: {} } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const [packId, setPackId] = useState("");
  const [updateId, setUpdateId] = useState(() => new URLSearchParams(window.location.search).get("update") ?? "");
  const pack = useQuery({
    queryKey: ["board-pack", packId],
    enabled: packId !== "",
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/board-packs/{id}", { params: { path: { id: packId } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const update = useQuery({
    queryKey: ["investor-update", updateId],
    enabled: updateId !== "",
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/investor-updates/{id}", { params: { path: { id: updateId } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Board and investor portal</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>
      {(packs.error || updates.error) && <Refusal problem={problemOf(packs.error ?? updates.error)} title="The portal could not be shown" />}
      <section className="fy-card fy-card--pad" aria-labelledby={`${id}-updates`}>
        <h2 className="fy-h3" id={`${id}-updates`}>
          Investor updates
        </h2>
        {(updates.data ?? []).length === 0 && <p className="fy-secondary">No update has been sent to you.</p>}
        <ul className="fy-list">
          {(updates.data ?? []).map((x) => (
            <li key={x.id}>
              <button type="button" className="fy-link" onClick={() => setUpdateId(x.id)}>
                {x.title}
              </button>{" "}
              {x.read === false && <span className="fy-badge">New</span>}
            </li>
          ))}
        </ul>
        {update.data && (
          <article aria-label={update.data.title}>
            <h3 className="fy-h3">{update.data.title}</h3>
            {update.data.highlights && <p>{update.data.highlights}</p>}
            {update.data.asks && (
              <p>
                <span className="fy-strong">Asks: </span>
                {update.data.asks}
              </p>
            )}
            {update.data.pack && <PackFigures pack={update.data.pack} />}
          </article>
        )}
      </section>
      <section className="fy-card fy-card--pad" aria-labelledby={`${id}-packs`}>
        <h2 className="fy-h3" id={`${id}-packs`}>
          Board packs
        </h2>
        {(packs.data ?? []).length === 0 && <p className="fy-secondary">No board pack is published yet.</p>}
        <ul className="fy-list">
          {(packs.data ?? []).map((x) => (
            <li key={x.id}>
              <button type="button" className="fy-link" onClick={() => setPackId(x.id)}>
                {x.month}, version {x.version}
              </button>
            </li>
          ))}
        </ul>
        {pack.data && (
          <article aria-label={`Board pack ${pack.data.month}`}>
            {pack.data.highlights && <p>{pack.data.highlights}</p>}
            {pack.data.commentary && <p>{pack.data.commentary}</p>}
            <PackFigures pack={pack.data} />
          </article>
        )}
      </section>
      <section className="fy-card fy-card--pad" aria-labelledby={`${id}-holding`}>
        <h2 className="fy-h3" id={`${id}-holding`}>
          My holding
        </h2>
        {holding.data && !holding.data.linked && <p className="fy-secondary">You do not sign in as a shareholder, so no holding is shown.</p>}
        {holding.data?.linked && (
          <ul className="fy-list">
            {holding.data.lines.map((l, i) => (
              <li key={i}>
                {l.class_name ?? "Shares"}: {l.issued} shares
              </li>
            ))}
          </ul>
        )}
      </section>
      <section className="fy-card fy-card--pad" aria-labelledby={`${id}-whole`}>
        <h2 className="fy-h3" id={`${id}-whole`}>
          The full cap table
        </h2>
        {whole.isError && <p className="fy-secondary">An Owner has not opened the full cap table to you. You can read your own holding above.</p>}
        {whole.data && (
          <p>
            Open to you by an Owner: {whole.data.holders.length} holder(s), {whole.data.issued} shares issued, as at {whole.data.at}.
          </p>
        )}
      </section>
    </div>
  );
}
