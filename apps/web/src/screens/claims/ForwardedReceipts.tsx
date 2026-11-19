import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Mail, Paperclip, UserCheck } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Message = components["schemas"]["InboundMessage"];
type MessageFile = NonNullable<Message["files"]>[number];
const filesOf = (m: Message): MessageFile[] => (m.files ?? []).filter((f) => f.kind === "attachment" && !f.removed);

// sizeOf shows a file's size in KB or MB.
const sizeOf = (bytes: number) => (bytes < 1024 * 1024 ? `${Math.max(1, Math.round(bytes / 1024))} KB` : `${(bytes / (1024 * 1024)).toFixed(1)} MB`);

// ForwardedReceipts is the claimant's own email-in address and the receipts forwarded to it (FR-AP-14, FR-DOC-12): mail
// from their own sign-in email is accepted at once, mail from anyone else waits for them to accept its sender, and an
// accepted message's attachment attaches to this draft claim as a receipt, checked again as any file is. The server decides
// everything: this only lists, asks and shows its answer. Nothing here is a ledger figure.
export function ForwardedReceipts({ claimId, etag, onAttached }: { claimId: string; etag: string; onAttached: (said: string) => void }) {
  const queries = useQueryClient();
  const nameId = useId();
  const help = helpText("claim.forwarded-receipts");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);

  const addresses = useQuery({
    queryKey: ["capture-addresses"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/organisation/email/capture-addresses");
      if (r.error || !r.data) throw r.error ?? new Error("no addresses");
      return { ...r.data, etag: r.response.headers.get("ETag") ?? "" };
    },
  });
  const mine = addresses.data?.mine;
  const mail = useQuery({
    queryKey: ["my-inbound-messages"],
    enabled: Boolean(mine),
    queryFn: async () => {
      const r = await api.GET("/v1/organisation/email/inbound-messages/mine", { params: { query: { page_size: 20 } } });
      if (r.error || !r.data) throw r.error ?? new Error("no mail");
      return r.data.items;
    },
  });

  // The installation has no email-in, or the page cannot read the addresses: leave the card out, the upload above stays.
  if (addresses.isError || (addresses.data && !addresses.data.domain)) return null;
  if (!addresses.data) return null;

  const fail = (e: unknown) => {
    setProblem(problemOf(e));
    setBusy("");
  };

  const makeAddress = async () => {
    setBusy("address");
    setProblem(null);
    const r = await api.PUT("/v1/organisation/email/capture-addresses/mine", {
      params: { header: { "If-Match": addresses.data?.mine_etag ?? '"none"' } },
      body: { name: name.trim() },
    });
    if (r.error) return fail(r.error);
    setBusy("");
    onAttached(`Your address is ${r.data?.mine?.address ?? "made"}: forward receipts to it.`);
    await queries.invalidateQueries({ queryKey: ["capture-addresses"] });
  };

  const accept = async (m: Message) => {
    setBusy(m.id);
    setProblem(null);
    const r = await api.POST("/v1/organisation/email/capture-senders/mine", { params: { header: { "Idempotency-Key": newKey() } }, body: { sender: m.sender, decision: "accepted" } });
    if (r.error) return fail(r.error);
    setBusy("");
    onAttached(`Mail from ${m.sender} is accepted from now on.`);
    await queries.invalidateQueries({ queryKey: ["my-inbound-messages"] });
  };

  const attach = async (m: Message, file: MessageFile) => {
    setBusy(file.id);
    setProblem(null);
    const r = await api.POST("/v1/expense-claims/{claim_id}/evidence:from-message", {
      params: { path: { claim_id: claimId }, header: { "If-Match": etag, "Idempotency-Key": newKey() } },
      body: { message_id: m.id, file_id: file.id },
    });
    if (r.error) return fail(r.error);
    setBusy("");
    onAttached(`${file.file_name} is attached: pick it on its line.`);
    await queries.invalidateQueries({ queryKey: ["claim-evidence", claimId] });
  };

  const waiting = (mail.data ?? []).filter((m) => m.status === "quarantined");
  const accepted = (mail.data ?? []).filter((m) => m.status === "accepted" && filesOf(m).length > 0);

  return (
    <section className="fy-card" aria-labelledby={`${nameId}-h`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${nameId}-h`} title={help.tooltip}>
          <Icon of={Mail} /> {help.label}
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {mine ? (
          <p className="fy-small">
            Forward a receipt to <span className="fy-mono fy-strong">{mine.address}</span>. Mail from your own sign-in email arrives here at once; mail from anyone else waits for you to accept its sender.
          </p>
        ) : (
          <div className="fy-stack fy-stack--2">
            <p className="fy-small">Make an address of your own to forward receipts to, then attach them here without saving them first.</p>
            <div className="fy-row fy-row--wrap gap-2">
              <label className="fy-sr" htmlFor={nameId}>
                A name for your address
              </label>
              <input id={nameId} className="fy-input max-w-64" placeholder="A name, such as ah-kow" value={name} onChange={(e) => setName(e.target.value)} />
              <span className="fy-small fy-muted">@{addresses.data.domain}</span>
              <Button variant="secondary" onClick={() => void makeAddress()} disabled={busy !== "" || name.trim().length < 3}>
                Make my address
              </Button>
            </div>
          </div>
        )}
        {problem && <Refusal problem={problem} title="That could not be done" />}
        {waiting.length > 0 && (
          <ul className="fy-list" aria-label="Mail waiting for you">
            {waiting.map((m) => (
              <li key={m.id} className="fy-row gap-2">
                <span className="fy-grow fy-small">
                  Mail from <span className="fy-mono">{m.sender}</span>, {dateTime(m.received_at)}, waits for you to accept its sender.
                </span>
                <Button variant="ghost" size="sm" onClick={() => void accept(m)} disabled={busy !== ""} aria-label={`Accept mail from ${m.sender}`}>
                  <Icon of={UserCheck} />
                  Accept sender
                </Button>
              </li>
            ))}
          </ul>
        )}
        {accepted.length > 0 && (
          <ul className="fy-list" aria-label="Receipts forwarded to you">
            {accepted.flatMap((m) =>
              filesOf(m).map((f) => (
                <li key={f.id} className="fy-row gap-2">
                  <span className="fy-grow fy-small">
                    <span className="fy-mono">{f.file_name}</span> · {sizeOf(f.size_bytes)} · from {m.sender}, {dateTime(m.received_at)}
                  </span>
                  <Button variant="ghost" size="sm" onClick={() => void attach(m, f)} disabled={busy !== ""} aria-label={`Attach ${f.file_name} from ${m.sender} to this claim`} title={helpText("claim.attach-forwarded").tooltip}>
                    <Icon of={Paperclip} />
                    Attach
                  </Button>
                </li>
              )),
            )}
          </ul>
        )}
        {mine && mail.data && waiting.length === 0 && accepted.length === 0 && <p className="fy-small fy-muted">No receipt has been forwarded to you yet.</p>}
      </div>
    </section>
  );
}
