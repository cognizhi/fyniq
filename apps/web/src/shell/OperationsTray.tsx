import { Dialog as Primitive } from "@base-ui/react/dialog";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, CircleX, Download, FileSpreadsheet, Hourglass, ListChecks, LoaderCircle, X, type LucideIcon } from "lucide-react";
import { useContext, useId, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { Refusal } from "../components/Refusal";
import { Button, buttonVariants } from "../components/ui/button";
import { DeviceRoot } from "../components/ui/dialog";
import { dateTime } from "../format/date";
import { helpText } from "../i18n";
import { nameOf, save } from "../screens/reports/AccountantExports";
import { Icon, problemOf } from "../ui";

type Operation = components["schemas"]["Operation"];
type Export = components["schemas"]["ReportExportOut"];

// While any operation runs the tray reads them again every 5 seconds (design D10); otherwise every minute, so work asked
// for on another page or device shows without a reload.
const whileRunning = 5_000;
const otherwise = 60_000;

const kindIcon: Record<string, LucideIcon> = { report_export: FileSpreadsheet, checks_run: ListChecks };

async function listOperations() {
  const { data, error } = await api.GET("/v1/operations", { params: { query: { page_size: 50 } } });
  if (error) throw error;
  return data.items;
}

// exportOf is the export an operation made, from the caller's exports: its resource is /v1/report-exports/{id}.
function exportOf(o: Operation, exports: Export[] | undefined): Export | undefined {
  if (o.kind !== "report_export") return undefined;
  const id = o.resource.split("/").pop();
  return exports?.find((x) => x.id === id);
}

function titleOf(o: Operation, x: Export | undefined): string {
  if (o.kind === "checks_run") return "Run every check";
  return x ? `Export ${nameOf(x.report)} (${x.format.toUpperCase()})` : "Accountant export";
}

function Status({ o }: { o: Operation }) {
  switch (o.status) {
    case "succeeded":
      return (
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          Done
        </span>
      );
    case "failed":
      return (
        <span className="fy-chip fy-chip--critical">
          <Icon of={CircleX} />
          Failed
        </span>
      );
    default:
      return (
        <span className="fy-chip fy-chip--info">
          <Icon of={LoaderCircle} />
          Running
        </span>
      );
  }
}

function Item({ o, x, onClose }: { o: Operation; x: Export | undefined; onClose: () => void }) {
  const [failed, setFailed] = useState<unknown>(null);
  const title = titleOf(o, x);
  const download = async (e: Export) => {
    setFailed(null);
    const r = await api.GET("/v1/report-exports/{export_id}/file", { params: { path: { export_id: e.id } } });
    if (r.error) return setFailed(r.error);
    save(r.data.file_name, r.data.media_type, r.data.content_base64);
  };
  const when = o.finished_at ? `${o.status === "failed" ? "failed" : "finished"} ${dateTime(o.finished_at)}` : `started ${dateTime(o.requested_at)}`;
  const icon = o.status === "failed" ? "fy-list__icon fy-list__icon--critical" : o.status === "succeeded" ? "fy-list__icon fy-list__icon--good" : "fy-list__icon";
  const where = o.kind === "checks_run" ? "/checks" : "/reports";
  return (
    <li className="fy-card fy-card--flat fy-card--pad fy-stack fy-stack--3" aria-busy={o.status === "running" || undefined}>
      <div className="fy-row fy-row--top">
        <span className={icon}>
          <Icon of={kindIcon[o.kind] ?? Hourglass} size={20} />
        </span>
        <span className="fy-stack fy-stack--2 fy-grow min-w-0">
          <span className="fy-strong">{title}</span>
          <span className="fy-small fy-muted">{when}</span>
        </span>
        <Status o={o} />
      </div>
      {o.status === "running" && (
        <div className="fy-stack fy-stack--2">
          <div className="fy-progress fy-progress--indeterminate" role="progressbar" aria-label={`${title}: running`}>
            <span />
          </div>
          <span className="fy-small fy-muted">{o.kind === "report_export" ? "You can keep working: a notification says when the file is ready." : "You can keep working: Checks updates once they have run."}</span>
        </div>
      )}
      {o.status === "succeeded" && o.kind === "report_export" && x?.status === "ready" && (
        <div className="fy-row fy-row--between fy-row--wrap">
          <span className="fy-small fy-muted">Downloads until {dateTime(x.expires_at)}</span>
          <Button variant="secondary" size="sm" onClick={() => download(x)} aria-label={`Download ${x.file_name ?? title}`}>
            <Icon of={Download} />
            Download {x.format.toUpperCase()}
          </Button>
        </div>
      )}
      {o.status === "succeeded" && o.kind === "report_export" && x?.status === "expired" && (
        <span className="fy-small fy-muted">
          Its 24 hours are over.{" "}
          <Link to="/reports" onClick={onClose}>
            Ask for it again in Reports
          </Link>
        </span>
      )}
      {o.status === "succeeded" && o.kind === "checks_run" && (
        <Link to="/checks" className="fy-small" onClick={onClose}>
          See the checks
        </Link>
      )}
      {o.status === "failed" && (
        <p className="fy-error">
          <Icon of={CircleX} />
          <span>
            {x?.why ? `${x.why}. ` : "It stopped after its attempts. "}
            <Link to={where} onClick={onClose}>
              {o.kind === "checks_run" ? "Run the checks again" : "Ask for it again in Reports"}
            </Link>
          </span>
        </p>
      )}
      {failed !== null && <Refusal problem={problemOf(failed)} title="The export could not be downloaded" />}
    </li>
  );
}

// OperationsTray is the header's operations button and the panel it opens (FR-INT-11; design D10, docs/design/screens
// OperationsTray): the long work the person asked for, newest first, read again every 5 seconds while any runs. A
// running one's progress is indeterminate, since operations carry no percentage yet; a finished one links to what it
// made, such as the export's download; a failed one says why when its export knows, and how to ask again.
export function OperationsTray() {
  const device = useContext(DeviceRoot);
  const id = useId();
  const [open, setOpen] = useState(false);
  const help = helpText("shell.operations");
  const list = useQuery({
    queryKey: ["operations"],
    queryFn: listOperations,
    refetchInterval: (q) => ((q.state.data ?? []).some((o) => o.status === "running") ? whileRunning : otherwise),
  });
  const items = list.data ?? [];
  const anyExport = items.some((o) => o.kind === "report_export");
  const exports = useQuery({
    queryKey: ["report-exports"],
    queryFn: async () => {
      const r = await api.GET("/v1/report-exports");
      if (r.error) throw r.error;
      return r.data.items;
    },
    enabled: open && anyExport,
  });
  const running = items.filter((o) => o.status === "running").length;
  const runningSaid = `${running} ${running === 1 ? "operation" : "operations"} running`;
  const close = () => setOpen(false);
  return (
    <>
      <button type="button" className={`${buttonVariants({ variant: "ghost", size: "icon" })} relative`} onClick={() => setOpen(true)} title={help.tooltip}
        aria-label={running > 0 ? `${help.label}, ${running} running` : help.label}>
        <Icon of={Hourglass} size={20} />
        {running > 0 && (
          <span className="fy-badge absolute -top-0.5 -right-0.5" aria-hidden="true">
            {running > 99 ? "99+" : running}
          </span>
        )}
      </button>
      <span className="fy-sr" role="status" aria-live="polite">
        {running > 0 ? runningSaid : ""}
      </span>
      <Primitive.Root open={open} onOpenChange={setOpen}>
        <Primitive.Portal container={device ?? undefined}>
          <Primitive.Backdrop className="fixed inset-0 z-(--z-modal) bg-scrim" />
          <Primitive.Popup className="fy-sheet fixed inset-y-0 right-0 z-(--z-modal)" aria-labelledby={`${id}-title`} aria-describedby={`${id}-about`}>
            <div className="fy-row border-b border-border px-4 pt-3 pb-3">
              <span className="fy-stack fy-stack--2 fy-grow">
                <Primitive.Title className="fy-h2" id={`${id}-title`}>
                  {help.label}
                </Primitive.Title>
                <span className="fy-small fy-muted" id={`${id}-about`}>
                  Exports and check runs keep running when you leave the page.
                </span>
              </span>
              <Primitive.Close className={buttonVariants({ variant: "ghost", size: "icon" })} aria-label="Close operations">
                <Icon of={X} size={20} />
              </Primitive.Close>
            </div>
            <div className="flex-auto overflow-auto p-4">
              {list.isError && <Refusal problem={problemOf(list.error)} title="Your operations could not be read" />}
              {list.isPending && <p className="fy-secondary">Loading your operations…</p>}
              {list.data && items.length === 0 && <p className="fy-secondary">Nothing yet. Exports and check runs you ask for show here while they run.</p>}
              {items.length > 0 && (
                <ul className="fy-stack fy-stack--3" aria-label="Your operations">
                  {items.map((o) => (
                    <Item key={o.id} o={o} x={exportOf(o, exports.data)} onClose={close} />
                  ))}
                </ul>
              )}
            </div>
          </Primitive.Popup>
        </Primitive.Portal>
      </Primitive.Root>
    </>
  );
}
