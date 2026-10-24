import { Dialog as Primitive } from "@base-ui/react/dialog";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Archive, Bell, CheckCheck, ListChecks, Mail, MailOpen, Settings, ShieldAlert, Stamp, X, type LucideIcon } from "lucide-react";
import { useContext, useId, useState } from "react";
import { api, newKey } from "../api/client";
import type { components } from "../api/schema";
import { Refusal } from "../components/Refusal";
import { buttonVariants } from "../components/ui/button";
import { DeviceRoot } from "../components/ui/dialog";
import { dateTime } from "../format/date";
import { helpText } from "../i18n";
import { Icon, problemOf } from "../ui";

type Notification = components["schemas"]["Notification"];
type Show = "unread" | "all" | "archived";

// The bell's count is read again every minute, so a notification fanned out meanwhile shows without a reload.
const every = 60_000;

const categoryIcon: Record<string, LucideIcon> = { security: ShieldAlert, approvals: Stamp, email: Mail, operations: ListChecks };

function iconClass(n: Notification): string {
  if (n.severity === "critical") return "fy-list__icon fy-list__icon--critical";
  if (n.severity === "warning") return "fy-list__icon fy-list__icon--warning";
  return n.actionable ? "fy-list__icon fy-list__icon--brand" : "fy-list__icon";
}

async function listNotifications(show: Show) {
  const query = show === "archived" ? { archived: true, page_size: 100 } : show === "unread" ? { unread: true, page_size: 100 } : { page_size: 100 };
  const { data, error } = await api.GET("/v1/notifications", { params: { query } });
  if (error) throw error;
  return data;
}

function Item({ n, onChanged }: { n: Notification; onChanged: () => void }) {
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<unknown>(null);
  const act = async (what: "read" | "unread" | "archive") => {
    setBusy(true);
    setFailed(null);
    const path = `/v1/notifications/{notification_id}/${what}` as const;
    const r = await api.POST(path, { params: { path: { notification_id: n.id }, header: { "Idempotency-Key": newKey() } } });
    setBusy(false);
    if (r.error) setFailed(r.error);
    else onChanged();
  };
  const read = helpText(n.read ? "notification.unread" : "notification.read");
  const archive = helpText("notification.archive");
  return (
    <li className="fy-list__item items-start" data-unread={!n.read || undefined}>
      <span className={iconClass(n)}>
        <Icon of={categoryIcon[n.category] ?? Bell} size={20} />
      </span>
      <span className="fy-list__main min-w-0 flex-1 gap-1">
        <span className="fy-row fy-row--wrap gap-1.5">
          <span className="fy-tag">{n.label}</span>
          <span className="fy-small fy-muted">{dateTime(n.created_at)}</span>
        </span>
        {n.link ? (
          <Link to={n.link} className="fy-list__title whitespace-normal" onClick={() => !n.read && act("read")}>
            {n.title}
          </Link>
        ) : (
          <span className="fy-list__title whitespace-normal">{n.title}</span>
        )}
        {n.actionable && !n.archived && <span className="fy-small fy-secondary">It waits for you in your Inbox.</span>}
        {failed !== null && <Refusal problem={problemOf(failed)} title="That did not work" />}
      </span>
      <span className="fy-row shrink-0 gap-1">
        {!n.read && <span role="img" aria-label="Unread" className="size-2 shrink-0 rounded-full bg-primary" />}
        {!n.archived && (
          <>
            <button type="button" className={buttonVariants({ variant: "ghost", size: "icon" })} disabled={busy} onClick={() => act(n.read ? "unread" : "read")}
              aria-label={`${read.label}: ${n.title}`} title={read.tooltip}>
              <Icon of={n.read ? Mail : MailOpen} />
            </button>
            <button type="button" className={buttonVariants({ variant: "ghost", size: "icon" })} disabled={busy} onClick={() => act("archive")}
              aria-label={`${archive.label}: ${n.title}`} title={archive.tooltip}>
              <Icon of={Archive} />
            </button>
          </>
        )}
      </span>
    </li>
  );
}

// NotificationBell is the header's bell and the notification centre it opens (FR-NTF-01; docs/design/screens
// Notifications): the unread count, announced politely; the caller's notifications, unread, all or archived; each
// marked read, unread or archived, or all read at once; a link to preferences. An actionable one links to what waits
// for the person, and opening it marks it read.
export function NotificationBell() {
  const queries = useQueryClient();
  const device = useContext(DeviceRoot);
  const id = useId();
  const [open, setOpen] = useState(false);
  const [show, setShow] = useState<Show>("unread");
  const [failed, setFailed] = useState<unknown>(null);
  const bell = helpText("notification.centre");
  const readAll = helpText("notification.read-all");
  const prefs = helpText("settings.notifications");
  const count = useQuery({ queryKey: ["notifications", "count"], queryFn: () => listNotifications("unread"), refetchInterval: every });
  const list = useQuery({ queryKey: ["notifications", show], queryFn: () => listNotifications(show), enabled: open });
  const unread = count.data?.unread ?? 0;
  const changed = () => queries.invalidateQueries({ queryKey: ["notifications"] });
  const markAll = async () => {
    setFailed(null);
    const r = await api.POST("/v1/notifications/read-all", { params: { header: { "Idempotency-Key": newKey() } } });
    if (r.error) setFailed(r.error);
    else await changed();
  };
  const tabs: [Show, string][] = [
    ["unread", unread > 0 ? `Unread ${unread}` : "Unread"],
    ["all", "All"],
    ["archived", "Archived"],
  ];
  const items = list.data?.items ?? [];
  return (
    <>
      <button type="button" className={`${buttonVariants({ variant: "ghost", size: "icon" })} relative`} onClick={() => setOpen(true)} title={bell.tooltip}
        aria-label={unread > 0 ? `${bell.label}, ${unread} unread` : bell.label}>
        <Icon of={Bell} size={20} />
        {unread > 0 && (
          <span className="fy-badge absolute -top-0.5 -right-0.5" aria-hidden="true">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>
      <span className="fy-sr" role="status" aria-live="polite">
        {unread > 0 ? `${unread} unread ${unread === 1 ? "notification" : "notifications"}` : ""}
      </span>
      <Primitive.Root open={open} onOpenChange={setOpen}>
        <Primitive.Portal container={device ?? undefined}>
          <Primitive.Backdrop className="fixed inset-0 z-(--z-modal) bg-scrim" />
          <Primitive.Popup className="fy-sheet fixed inset-y-0 right-0 z-(--z-modal)" aria-labelledby={`${id}-title`}>
            <div className="fy-row px-4 pt-3 pb-2">
              <Primitive.Title className="fy-h2" id={`${id}-title`}>
                {bell.label}
              </Primitive.Title>
              {unread > 0 && (
                <span className="fy-badge" aria-label={`${unread} unread`}>
                  {unread}
                </span>
              )}
              <span className="fy-spacer" />
              <Link to="/settings/notifications" className={buttonVariants({ variant: "ghost", size: "icon" })} aria-label={prefs.label} title={prefs.tooltip}
                onClick={() => setOpen(false)}>
                <Icon of={Settings} size={20} />
              </Link>
              <Primitive.Close className={buttonVariants({ variant: "ghost", size: "icon" })} aria-label="Close notifications">
                <Icon of={X} size={20} />
              </Primitive.Close>
            </div>
            <div className="fy-row border-b border-border px-4">
              <div className="fy-tabs fy-grow border-0" role="tablist" aria-label="Show">
                {tabs.map(([key, label]) => (
                  <button key={key} type="button" role="tab" id={`${id}-tab-${key}`} aria-selected={show === key} aria-controls={`${id}-panel`} onClick={() => setShow(key)}>
                    {label}
                  </button>
                ))}
              </div>
              {unread > 0 && show !== "archived" && (
                <button type="button" className={buttonVariants({ variant: "ghost", size: "sm" })} onClick={markAll} title={readAll.tooltip}>
                  <Icon of={CheckCheck} />
                  {readAll.label}
                </button>
              )}
            </div>
            <div className="flex-auto overflow-auto" role="tabpanel" id={`${id}-panel`} aria-labelledby={`${id}-tab-${show}`}>
              {failed !== null && <Refusal problem={problemOf(failed)} title="The notifications were not marked read" />}
              {list.isError && <Refusal problem={problemOf(list.error)} title="Your notifications could not be read" />}
              {list.isPending && <p className="fy-secondary p-4">Loading your notifications…</p>}
              {list.data && items.length === 0 && (
                <p className="fy-secondary p-4">{show === "unread" ? "Nothing new. You are up to date." : show === "archived" ? "Nothing archived." : "No notifications yet."}</p>
              )}
              {items.length > 0 && (
                <ul className="fy-list" aria-label={tabs.find(([k]) => k === show)?.[1]}>
                  {items.map((n) => (
                    <Item key={n.id} n={n} onChanged={changed} />
                  ))}
                </ul>
              )}
            </div>
            <div className="fy-row fy-small gap-2 border-t border-border bg-surface-2 px-4 py-3">
              <Icon of={Mail} />
              <span className="fy-grow fy-secondary">Email follows your preferences: approvals and security at once, the rest in a daily digest at 08:00.</span>
              <Link to="/settings/notifications" onClick={() => setOpen(false)}>
                Preferences
              </Link>
            </div>
          </Primitive.Popup>
        </Primitive.Portal>
      </Primitive.Root>
    </>
  );
}
