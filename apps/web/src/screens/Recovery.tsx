import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Hourglass, KeyRound, LifeBuoy, ShieldAlert, TriangleAlert, UserCheck } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, type Session } from "../api/client";
import { Refusal } from "../components/Refusal";
import { Button } from "../components/ui/button";
import { Dialog } from "../components/ui/dialog";
import { date, dateTime } from "../format/date";
import { helpText } from "../i18n";
import { go, here } from "../route";
import { guarded, useConfirm } from "../stepup";
import { Icon, problemOf, type ApiProblem } from "../ui";
import { RecoveryCodes } from "./RecoveryCodes";

// Account recovery in the web app (SEC-35, PRD-001 rev 0.198): a person's own recovery codes on their profile, the form
// that recovers an account from a session without a second factor, the confirmation an Owner or Administrator gives on
// Members and roles, and the banner of the cooling-off that follows. Codes live only in the page's memory.

const myLead = "If you lose every authenticator, one of these codes and the confirmation of an Owner or Administrator of your organisation give you your account back. Each works once.";

async function codeStatus() {
  const { data, error } = await api.GET("/v1/session/recovery-codes");
  if (error) throw error;
  return data;
}

// MyRecoveryCodes is the Profile's card of the person's recovery codes: how many are left, and making new ones.
export function MyRecoveryCodes() {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const status = useQuery({ queryKey: ["recovery-codes"], queryFn: codeStatus });
  const [codes, setCodes] = useState<string[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const make = helpText("recovery.make");
  const unused = status.data?.unused ?? 0;

  const run = async () => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.POST("/v1/session/recovery-codes"));
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    setCodes(out.data.recovery_codes ?? []);
  };

  return (
    <div className="fy-stack fy-stack--3 border-t border-border p-4">
      <div className="fy-row fy-row--wrap">
        <h3 className="fy-h3 fy-grow" id="recovery-title">
          Recovery codes
        </h3>
        <Button size="sm" variant={unused ? "secondary" : "primary"} onClick={() => void run()} disabled={busy} title={make.tooltip}>
          <Icon of={KeyRound} />
          {unused ? "Make new codes" : make.label}
        </Button>
      </div>
      {problem && <Refusal problem={problem} title="No codes were made" />}
      {status.isError && <Refusal problem={problemOf(status.error)} title="Your recovery codes could not be read" />}
      {status.data && unused === 0 && (
        <div className="fy-banner fy-banner--warning">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Make your recovery codes</span>
            <span className="fy-small fy-secondary">Without them, losing every authenticator locks you out until a Super admin helps.</span>
          </div>
        </div>
      )}
      {status.data && unused > 0 && (
        <p className="fy-small fy-secondary" aria-labelledby="recovery-title">
          {unused} of 10 unused{status.data.created_at ? `, made ${date(status.data.created_at)}` : ""}. Making new codes stops the old ones working.
        </p>
      )}
      <Dialog open={codes !== null} onOpenChange={() => undefined} dismissible={false} title="Your recovery codes">
        {codes && (
          <RecoveryCodes
            codes={codes}
            lead={myLead}
            doneLabel="Done"
            onDone={() => {
              setCodes(null);
              void queries.invalidateQueries({ queryKey: ["recovery-codes"] });
            }}
          />
        )}
      </Dialog>
    </div>
  );
}

// RecoverAccount is the form a person signed in without a second factor uses when they lost every authenticator: one of
// their recovery codes and the confirmation an Owner or Administrator gave them after speaking to them.
export function RecoverAccount() {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [code, setCode] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [done, setDone] = useState<{ codes: string[]; until: string } | null>(null);
  const h = helpText("recovery.recover");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/session/recovery", { body: { recovery_code: code.trim(), confirmation_code: confirmation.trim() } });
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    setDone({ codes: out.data.recovery_codes ?? [], until: out.data.cooling_off_until });
  };

  if (done)
    return (
      <div className="fy-stack">
        <p className="fy-secondary" role="status">
          Your account is back. Add a new passkey on your profile. Until {dateTime(done.until)}, approving, payments, bank details, roles and sign-ins wait.
        </p>
        <RecoveryCodes codes={done.codes} lead={myLead} doneLabel="Go to Fyniq" onDone={() => go(here())} />
      </div>
    );
  if (!open)
    return (
      <Button variant="ghost" onClick={() => setOpen(true)} title={h.tooltip}>
        <Icon of={LifeBuoy} />
        Lost every authenticator?
      </Button>
    );
  return (
    <form className="fy-card fy-card--pad fy-stack" onSubmit={submit} noValidate aria-labelledby={`${id}-title`}>
      <h2 className="fy-h3" id={`${id}-title`}>
        Recover your account
      </h2>
      <p className="fy-small fy-secondary">
        You need one of your recovery codes and a confirmation from an Owner or Administrator of your organisation. Ask them in person or call them: they
        give you the confirmation after speaking to you. Email alone never recovers an account.
      </p>
      {problem && <Refusal problem={problem} title="Your account was not recovered" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-code`}>
          Recovery code
        </label>
        <input className="fy-input fy-mono" id={`${id}-code`} value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" spellCheck={false} />
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-conf`}>
          Confirmation from your Owner or Administrator
        </label>
        <input className="fy-input fy-mono" id={`${id}-conf`} value={confirmation} onChange={(e) => setConfirmation(e.target.value)} autoComplete="off" spellCheck={false} />
      </div>
      <Button type="submit" variant="primary" disabled={busy || !code.trim() || !confirmation.trim()}>
        <Icon of={UserCheck} />
        {h.label}
      </Button>
    </form>
  );
}

// ConfirmRecovery is the Members page's action an Owner or Administrator takes after speaking to a member who lost every
// authenticator: a one-time confirmation, shown once, to give them.
export function ConfirmRecovery({ memberId, name, onClose }: { memberId: string; name: string; onClose: () => void }) {
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [code, setCode] = useState<{ code: string; expires_at: string } | null>(null);
  const h = helpText("recovery.confirm");
  const run = async () => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.POST("/v1/members/{member_id}/recovery-confirmations", { params: { path: { member_id: memberId } } }));
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    setCode(out.data);
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && !busy && onClose()}
      dismissible={!busy}
      title={`Confirm ${name}'s recovery?`}
      description="Only after speaking to them, in person or on a call you made to a number you know. With one of their recovery codes, it gives them their account back."
      footer={
        code ? (
          <Button variant="primary" onClick={onClose}>
            Done
          </Button>
        ) : (
          <>
            <Button variant="ghost" onClick={onClose} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" onClick={() => void run()} disabled={busy}>
              <Icon of={UserCheck} />
              {h.label}
            </Button>
          </>
        )
      }
    >
      {problem && <Refusal problem={problem} title="The recovery was not confirmed" />}
      {code && (
        <div className="fy-stack fy-stack--2">
          <p className="fy-small fy-secondary">Read this to {name} now; it is shown once and works until {dateTime(code.expires_at)}.</p>
          <p className="fy-mono fy-strong" aria-label="Confirmation code">
            {code.code}
          </p>
        </div>
      )}
    </Dialog>
  );
}

// CoolingOff is the banner of the cooling-off after the person's own recovery: what waits, and until when.
export function CoolingOff({ session }: { session: Pick<Session, "cooling_off_until"> }) {
  if (!session.cooling_off_until) return null;
  return (
    <div className="fy-banner fy-banner--warning fy-banner--strip" role="status">
      <Icon of={Hourglass} size={20} />
      <div className="fy-banner__text">
        <span className="fy-small">
          <Icon of={ShieldAlert} /> Your account was recovered. Until {dateTime(session.cooling_off_until)}, approving, releasing payments, changing bank details,
          roles or sign-ins, and break-glass wait.
        </span>
      </div>
    </div>
  );
}
