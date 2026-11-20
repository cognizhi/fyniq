import { useQuery } from "@tanstack/react-query";
import { Camera, CloudOff, RefreshCw, Trash2, Upload } from "lucide-react";
import { useCallback, useEffect, useId, useMemo, useState, useSyncExternalStore } from "react";
import { api } from "../api/client";
import { Button } from "../components/ui/button";
import { Icon, problemOf } from "../ui";
import { DEFAULT_HOURS, indexedDbStore, isSharedDevice, Queue, QueueRefused, setSharedDevice, type Outcome, type QueuedMeta, type Store } from "./queue";

// useOnline is whether the browser says there is a signal. It only says so: an upload that fails is queued whatever it said.
export function useOnline(): boolean {
  return useSyncExternalStore(
    (notify) => {
      window.addEventListener("online", notify);
      window.addEventListener("offline", notify);
      return () => {
        window.removeEventListener("online", notify);
        window.removeEventListener("offline", notify);
      };
    },
    () => navigator.onLine,
    () => true,
  );
}

// uploadReceipt is the document inbox's own upload, with the queued receipt's ID as its Idempotency-Key (FR-UX-16): the API re-checks the person's rights and compares the file's hash, so a
// receipt already in the inbox answers duplicate and one the person may no longer capture is refused. A network failure, a 429 or a 5xx is a retry.
export async function uploadReceipt(file: File, id: string): Promise<Outcome> {
  const body = { file: file.name, source: "upload" as const };
  const bodySerializer = () => {
    const form = new FormData();
    form.append("file", file, file.name);
    form.append("source", "upload");
    return form;
  };
  try {
    const r = await api.POST("/v1/captures", { params: { header: { "Idempotency-Key": id } }, body, bodySerializer });
    if (r.data) return r.data.status === "duplicate" ? { kind: "duplicate" } : { kind: "uploaded" };
    if (r.response.status >= 500 || r.response.status === 429 || r.response.status === 401) return { kind: "retry" }; // signed out meanwhile: the receipt waits for the next sign-in
    const p = problemOf(r.error);
    return { kind: "refused", reason: [p.detail, p.fix].filter(Boolean).join(" ") || "The server did not accept it." };
  } catch {
    return { kind: "retry" };
  }
}

// useReceiptQueue is the person's queue on this device: what waits, a way to add to it, and a flush that runs by itself when the signal comes back.
export function useReceiptQueue(user: string, onUploaded: () => void, store?: Store) {
  const hours = useQuery({
    queryKey: ["setting", "pwa.offline_queue_hours"],
    retry: false,
    staleTime: 3600_000,
    queryFn: async () => {
      const r = await api.GET("/v1/organisation/settings/{key}", { params: { path: { key: "pwa.offline_queue_hours" } } });
      const v = Number(r.data?.value);
      return Number.isFinite(v) && v >= 1 ? v : DEFAULT_HOURS;
    },
  });
  const queue = useMemo(() => new Queue(store ?? indexedDbStore(), user, hours.data ?? DEFAULT_HOURS), [store, user, hours.data]);
  const [waiting, setWaiting] = useState<QueuedMeta[]>([]);
  const [said, setSaid] = useState("");
  const [busy, setBusy] = useState(false);
  const refresh = useCallback(async () => {
    try {
      setWaiting(await queue.list());
    } catch {
      setWaiting([]);
    }
  }, [queue]);
  const flush = useCallback(async () => {
    setBusy(true);
    try {
      const r = await queue.flush(uploadReceipt);
      const parts = [
        r.uploaded ? `${r.uploaded} receipt${r.uploaded === 1 ? "" : "s"} uploaded` : "",
        r.duplicates ? `${r.duplicates} already in the inbox` : "",
        ...r.refused.map((x) => `${x.name} was not accepted: ${x.reason}`),
      ].filter(Boolean);
      if (parts.length) setSaid(parts.join(". ") + ".");
      if (r.uploaded || r.duplicates) onUploaded();
    } finally {
      setBusy(false);
      await refresh();
    }
  }, [queue, refresh, onUploaded]);
  useEffect(() => {
    void refresh();
    const online = () => void flush();
    window.addEventListener("online", online);
    return () => window.removeEventListener("online", online);
  }, [refresh, flush]);
  const enqueue = useCallback(
    async (file: File): Promise<string> => {
      try {
        const m = await queue.enqueue(file);
        await refresh();
        return `${m.name} is waiting on this device. It uploads by itself when you are back online.`;
      } catch (e) {
        if (e instanceof QueueRefused) return e.message;
        return "This browser could not keep the receipt on the device. Capture it again when you are back online.";
      }
    },
    [queue, refresh],
  );
  const remove = useCallback(
    async (id: string) => {
      await queue.remove(id);
      await refresh();
    },
    [queue, refresh],
  );
  return { waiting, said, busy, enqueue, flush, remove, hours: hours.data ?? DEFAULT_HOURS };
}

// OfflineQueuePanel shows what waits on this device (FR-UX-16): each receipt, how long it keeps, Upload now and Remove, and the choice to keep nothing on a shared device.
export function OfflineQueuePanel({ q }: { q: ReturnType<typeof useReceiptQueue> }) {
  const id = useId();
  const online = useOnline();
  const [shared, setShared] = useState(isSharedDevice());
  return (
    <section className="fy-card fy-stack fy-stack--3 fy-card--pad" aria-labelledby={`${id}-h`}>
      <div className="fy-row fy-row--between fy-row--wrap gap-2">
        <h2 className="fy-h3" id={`${id}-h`}>
          <Icon of={CloudOff} /> Waiting on this device
        </h2>
        {q.waiting.length > 0 && (
          <Button size="sm" variant="primary" onClick={() => void q.flush()} disabled={!online || q.busy}>
            <Icon of={Upload} />
            Upload now
          </Button>
        )}
      </div>
      {q.said && (
        <p className="fy-small" role="status">
          {q.said}
        </p>
      )}
      {q.waiting.length === 0 ? (
        <p className="fy-secondary">Nothing waits. A receipt you capture with no signal waits here, sealed on this device, and uploads when you are back online.</p>
      ) : (
        <ul className="fy-list">
          {q.waiting.map((m) => (
            <li key={m.id} className="fy-list__item fy-row fy-row--between fy-row--wrap gap-2">
              <span className="fy-list__main">
                <span className="fy-list__title">{m.name}</span>
                <span className="fy-list__meta">Kept until {new Date(m.expiresAt).toLocaleString("en-SG", { dateStyle: "medium", timeStyle: "short" })}</span>
              </span>
              <Button size="sm" variant="ghost" onClick={() => void q.remove(m.id)} aria-label={`Remove ${m.name} from this device`}>
                <Icon of={Trash2} />
                Remove
              </Button>
            </li>
          ))}
        </ul>
      )}
      <label className="fy-row gap-2">
        <input
          type="checkbox"
          checked={shared}
          onChange={(e) => {
            setShared(e.target.checked);
            void setSharedDevice(e.target.checked);
          }}
        />
        <span className="fy-small">This is a shared device: keep nothing on it while I am offline</span>
      </label>
      <p className="fy-help">A receipt waits at most {q.hours} hours, is removed when you sign out, and is checked by Fyniq when it uploads, as if you had just captured it.</p>
    </section>
  );
}

// CameraButton takes a photo with the device's camera, as a receipt to capture.
export function CameraButton({ onFile, disabled }: { onFile: (f: File) => void; disabled?: boolean }) {
  const id = useId();
  return (
    <>
      <label className={`fy-btn fy-btn--secondary${disabled ? " fy-btn--disabled" : ""}`} htmlFor={id}>
        <Icon of={Camera} />
        Take a photo
      </label>
      <input
        id={id}
        className="fy-sr"
        type="file"
        accept="image/*"
        capture="environment"
        disabled={disabled}
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) onFile(f);
          e.target.value = "";
        }}
      />
    </>
  );
}

// OfflineNotice tells the person, at the top of the app, that there is no signal: what still works and what waits.
export function OfflineNotice() {
  const online = useOnline();
  if (online) return null;
  return (
    <div className="fy-banner fy-banner--warning fy-banner--strip" role="status">
      <Icon of={RefreshCw} size={20} />
      <div className="fy-banner__text">
        <span>You are offline. Approvals and the books need a signal; a receipt you capture waits on this device and uploads when you are back.</span>
      </div>
    </div>
  );
}
