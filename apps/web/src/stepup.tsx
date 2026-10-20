import { useQueryClient } from "@tanstack/react-query";
import { Fingerprint, LogIn, ShieldCheck } from "lucide-react";
import { createContext, useContext, useState, type ReactNode } from "react";
import { api } from "./api/client";
import { Button } from "./components/ui/button";
import { Dialog } from "./components/ui/dialog";
import { Refusal } from "./components/Refusal";
import { helpText } from "./i18n";
import * as passkey from "./passkey";
import { here, signInURL } from "./route";
import { Icon, problemOf, type ApiProblem } from "./ui";

// Confirming it's you (FR-ADM-02, SEC-01; rev 0.43). Asking for or approving a change needs a second
// factor in the last 5 minutes: a passkey, or signing in again with the provider's authenticator.
// The API says when one is needed (API-AUTHZ-05); the page then asks, and runs the action once more.
// A session that passed no second factor at all can do nothing else until it does (API-AUTHZ-07).

export const CodeStepUpRequired = "API-AUTHZ-05";
export const CodeSecondFactor = "API-AUTHZ-07";
export const CodeNoPasskey = "API-PASSKEY-02";

type Outcome = { ok: true } | { ok: false; problem?: ApiProblem; noPasskey?: boolean; cancelled?: boolean };

// stepUp runs the passkey ceremony: a challenge from the API, the authenticator's answer, and the
// API's check of it. The session then counts as stepped up for 5 minutes.
export async function stepUp(): Promise<Outcome> {
  const started = await api.POST("/v1/session/step-up/challenge");
  if (!started.data) {
    const p = problemOf(started.error);
    return { ok: false, problem: p, noPasskey: p.code === CodeNoPasskey };
  }
  let credential: Record<string, unknown>;
  try {
    credential = await passkey.get(started.data.options);
  } catch (e) {
    if (passkey.cancelled(e)) return { ok: false, cancelled: true };
    return { ok: false, problem: { detail: e instanceof Error ? e.message : "Your browser refused the passkey.", fix: "Try again, or sign in again instead." } };
  }
  const answered = await api.POST("/v1/session/step-up", { body: { challenge_id: started.data.challenge_id, credential } });
  return answered.data ? { ok: true } : { ok: false, problem: problemOf(answered.error) };
}

// ConfirmItsYou is the body of the dialog and of the second-factor page: a passkey first, and
// signing in again when there is no passkey (or the browser has none).
export function ConfirmItsYou({ provider, onConfirmed, onCancel }: { provider?: string; onConfirmed: () => void; onCancel?: () => void }) {
  const queries = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [noPasskey, setNoPasskey] = useState(!passkey.supported());
  const [note, setNote] = useState("");
  const usePasskey = helpText("session.step-up");
  const again = helpText("session.sign-in-again");

  const run = async () => {
    setBusy(true);
    setProblem(null);
    setNote("");
    const out = await stepUp();
    setBusy(false);
    if (out.ok) {
      await queries.invalidateQueries({ queryKey: ["shell"] });
      onConfirmed();
      return;
    }
    if (out.noPasskey) setNoPasskey(true);
    else if (out.cancelled) setNote("The passkey was cancelled, or took too long. Try again when you are ready.");
    else if (out.problem) setProblem(out.problem);
  };

  return (
    <div className="fy-stack fy-stack--3">
      {problem && <Refusal problem={problem} title="Your passkey did not confirm it's you" />}
      {note && (
        <p className="fy-small fy-secondary" role="status">
          {note}
        </p>
      )}
      {noPasskey ? (
        <p className="fy-small fy-secondary">
          {passkey.supported()
            ? "You have no passkey yet. Sign in again with your authenticator app to confirm it's you; then add a passkey in Profile and security, so next time takes a touch."
            : "This browser cannot use passkeys. Sign in again with your authenticator app to confirm it's you."}
        </p>
      ) : (
        <p className="fy-small fy-secondary">{usePasskey.tooltip}</p>
      )}
      <div className="fy-row fy-row--wrap">
        {!noPasskey && (
          <Button variant="primary" onClick={run} disabled={busy}>
            <Icon of={Fingerprint} />
            {busy ? "Waiting for your passkey…" : usePasskey.label}
          </Button>
        )}
        {provider && (
          <a className={noPasskey ? "fy-btn fy-btn--primary" : "fy-btn fy-btn--ghost"} href={signInURL(provider, here())} title={again.tooltip}>
            <Icon of={LogIn} />
            {again.label}
          </a>
        )}
        {onCancel && (
          <Button variant="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
        )}
      </div>
    </div>
  );
}

interface Pending {
  resolve: (ok: boolean) => void;
}

const Confirm = createContext<(() => Promise<boolean>) | null>(null);

// StepUpProvider gives every signed-in page the "Confirm it's you" dialog.
export function StepUpProvider({ provider, children }: { provider?: string; children: ReactNode }) {
  const [pending, setPending] = useState<Pending | null>(null);
  const confirm = () => new Promise<boolean>((resolve) => setPending({ resolve }));
  const done = (ok: boolean) => {
    pending?.resolve(ok);
    setPending(null);
  };
  return (
    <Confirm.Provider value={confirm}>
      {children}
      <Dialog
        open={pending !== null}
        onOpenChange={(o) => !o && done(false)}
        eyebrow="Confirm it's you"
        title="This needs a second factor"
        description="Changes and approvals need you to confirm it's you in the last 5 minutes. It counts for 5 minutes."
        icon={
          <span className="fy-list__icon fy-list__icon--brand">
            <Icon of={ShieldCheck} size={20} />
          </span>
        }
        closeLabel="Cancel"
      >
        {pending && <ConfirmItsYou provider={provider} onConfirmed={() => done(true)} onCancel={() => done(false)} />}
      </Dialog>
    </Confirm.Provider>
  );
}

// useConfirm asks the person to confirm it's you; it resolves true once they have.
export function useConfirm(): () => Promise<boolean> {
  const c = useContext(Confirm);
  return c ?? (() => Promise.resolve(false));
}

// guarded runs an API call; when the API answers that it needs a recent second factor, it asks the
// person to confirm it's you and runs the call once more (with the same Idempotency-Key, which the
// caller keeps in the closure: the API saved nothing for the refusal).
export async function guarded<T extends { error?: unknown }>(confirm: () => Promise<boolean>, call: () => Promise<T>): Promise<T> {
  const first = await call();
  if (first.error && problemOf(first.error).code === CodeStepUpRequired && (await confirm())) return call();
  return first;
}
