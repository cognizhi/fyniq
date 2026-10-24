import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleX, ExternalLink, Info, Megaphone, TriangleAlert, X, type LucideIcon } from "lucide-react";
import { useState } from "react";
import { api, newKey } from "../api/client";
import type { components } from "../api/schema";
import { Refusal } from "../components/Refusal";
import { buttonVariants } from "../components/ui/button";
import { helpText } from "../i18n";
import { Icon, problemOf } from "../ui";

type Banner = components["schemas"]["Banner"];

// Banners are read again every five minutes, so one that starts or ends meanwhile shows or goes.
const every = 300_000;

const severityIcon: Record<Banner["severity"], LucideIcon> = { info: Megaphone, warning: TriangleAlert, critical: CircleX };

async function listBanners() {
  const { data, error } = await api.GET("/v1/banners", { params: { query: { page_size: 20 } } });
  if (error) throw error;
  return data.items ?? [];
}

// BannerLink is a banner's link: a path in Fyniq opens in place; an https address, in a new tab, said so.
function BannerLink({ to }: { to: string }) {
  if (to.startsWith("/")) return <Link to={to}>Details</Link>;
  return (
    <a href={to} target="_blank" rel="noopener noreferrer">
      Details <Icon of={ExternalLink} />
      <span className="fy-sr"> (opens in a new tab, outside Fyniq)</span>
    </a>
  );
}

function One({ b, onDismissed }: { b: Banner; onDismissed: () => void }) {
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<unknown>(null);
  const dismiss = helpText("banner.dismiss");
  const go = async () => {
    setBusy(true);
    setFailed(null);
    const r = await api.POST("/v1/banners/{announcement_id}/dismiss", { params: { path: { announcement_id: b.id }, header: { "Idempotency-Key": newKey() } } });
    setBusy(false);
    if (r.error) setFailed(r.error);
    else onDismissed();
  };
  return (
    // Info and warning are announced politely, critical at once (FR-NTF-03).
    <div className={`fy-banner fy-banner--${b.severity} fy-banner--strip`} role={b.severity === "critical" ? "alert" : "status"}>
      <Icon of={b.scope === "installation" && b.severity === "info" ? Info : severityIcon[b.severity]} size={20} />
      <div className="fy-banner__text">
        <span>
          {b.message} {b.link && <BannerLink to={b.link} />}
        </span>
        {failed !== null && <Refusal problem={problemOf(failed)} title="The banner was not dismissed" />}
      </div>
      {b.dismissible && (
        <button type="button" className={buttonVariants({ variant: "ghost", size: "icon" })} onClick={go} disabled={busy} aria-label={`${dismiss.label}: ${b.message}`}
          title={dismiss.tooltip}>
          <Icon of={X} />
        </button>
      )}
    </div>
  );
}

// Banners are the announcements under the header on every page (FR-NTF-03; docs/design/screens Banners): the
// organisation's and the installation's live banners meant for the person, critical first. A message is plain
// text; a dismissal is the person's own, kept on the server for every device; a critical banner stays until it ends.
export function Banners() {
  const queries = useQueryClient();
  const q = useQuery({ queryKey: ["banners"], queryFn: listBanners, refetchInterval: every });
  const items = q.data ?? [];
  if (items.length === 0) return null;
  return (
    <section className="fy-stack gap-0" aria-label="Messages for everyone">
      {items.map((b) => (
        <One key={b.id} b={b} onDismissed={() => queries.invalidateQueries({ queryKey: ["banners"] })} />
      ))}
    </section>
  );
}
