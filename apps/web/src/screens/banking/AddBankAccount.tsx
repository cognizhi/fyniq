import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Check, ChevronRight, Clock, Copy, FileSpreadsheet, Mail, Plug, ShieldCheck } from "lucide-react";
import { useId, useState } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { timeZone } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { kinds, makers, type MoneyAccount } from "./BankAccounts";

type Draft = components["schemas"]["MoneyAccountDraft"];
type Source = "file" | "email" | "connector";
type Format = NonNullable<components["schemas"]["MoneySourceDraft"]["report_format"]>;

// setters are the roles that say where an account's data comes from and name the bank-ingest address: C on "Bank
// data… connections" (Appendix D), the Administrator; the Owner approves connectors themselves.
export const setters = ["administrator"];

const formats: [Format, string][] = [
  ["csv", "CSV"],
  ["xlsx", "Excel (XLSX)"],
  ["mt940", "MT940"],
  ["camt053", "camt.053"],
  ["ofx", "OFX"],
];

// todayInSingapore is the organisation's today, as a source's first day.
function todayInSingapore(): string {
  return new Date().toLocaleDateString("en-CA", { timeZone });
}

export interface AccountForm {
  kind: MoneyAccount["kind"];
  name: string;
  institution: string;
  account_id: string;
  identifier: string;
  opened_on: string;
}

// checkAccount reads the first step's form: every missing field at once (ENG-23), or the draft to send.
export function checkAccount(f: AccountForm): { draft?: Draft; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  if (!f.name.trim()) errors.name = "Name the account, such as DBS current.";
  if (!f.account_id) errors.account_id = "Choose the account in the chart it posts to.";
  if (Object.keys(errors).length > 0) return { errors };
  return {
    errors,
    draft: {
      kind: f.kind,
      name: f.name.trim(),
      account_id: f.account_id,
      institution: f.institution.trim() || undefined,
      identifier: f.identifier.trim() || undefined,
      opened_on: f.opened_on || undefined,
    },
  };
}

// senders reads the bank's addresses, one a line or separated by commas: at most five.
export function senders(text: string): string[] {
  return text
    .split(/[\s,;]+/)
    .map((s) => s.trim().toLowerCase())
    .filter(Boolean);
}

// AddBankAccount is Banking › Add an account (docs/design/screens AddBankAccount; FR-BNK-01, FR-BNK-15, FR-BNK-17,
// FR-BNK-18, FR-INT-22; p1-bank-import-reconcile task 10.1): the account, where its data comes from, and what happens
// next. Connectors are read-only; nothing here can move money.
export function AddBankAccount() {
  const { session } = useSignedIn();
  const roles = session.roles ?? [];
  const [account, setAccount] = useState<MoneyAccount | null>(null);
  const [tag, setTag] = useState("");
  const [done, setDone] = useState<Source | null>(null);
  const step = done ? 3 : account ? 2 : 1;
  const h = helpText("bank-account.new");

  return (
    <div className="fy-page fy-enter">
      <nav className="fy-breadcrumb" aria-label="Breadcrumb">
        <Link to="/banking">Banking</Link>
        <Icon of={ChevronRight} />
        <span>Add an account</span>
      </nav>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Add a money account</h1>
          <p className="fy-secondary">
            Step {step} of 3: {step === 1 ? "the account." : step === 2 ? "where its data comes from." : "what happens next."}
          </p>
        </div>
      </div>
      <ReviewBanner screen="add-bank-account" />
      <ol className="fy-steps" aria-label="Steps">
        {["Account", "Data source", "Check"].map((label, i) => (
          <li key={label} className={`fy-step${i + 1 < step ? " fy-step--done" : ""}`} aria-current={i + 1 === step ? "step" : undefined}>
            <span className="fy-step__n">{i + 1 < step ? <Icon of={Check} /> : i + 1}</span>
            {label}
          </li>
        ))}
      </ol>
      {!roles.some((r) => makers.includes(r)) && !account ? (
        <p className="fy-secondary">A Bookkeeper or an Accountant adds money accounts. {h.tooltip}</p>
      ) : step === 1 ? (
        <AccountStep
          onDone={(a, t) => {
            setAccount(a);
            setTag(t);
          }}
        />
      ) : step === 2 && account ? (
        <SourceStep account={account} etag={tag} mayConnect={roles.some((r) => setters.includes(r))} onDone={(s) => setDone(s)} />
      ) : (
        account && done && <CheckStep account={account} source={done} />
      )}
      <p className="fy-small fy-muted fy-row gap-2">
        <Icon of={ShieldCheck} />
        Connections are read-only: Fyniq never moves money through them. Payments go out from your bank, as approved files or by hand.
      </p>
    </div>
  );
}

function AccountStep({ onDone }: { onDone: (a: MoneyAccount, etag: string) => void }) {
  const id = useId();
  const queries = useQueryClient();
  const [form, setForm] = useState<AccountForm>({ kind: "bank", name: "", institution: "", account_id: "", identifier: "", opened_on: "" });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const chart = useQuery({
    queryKey: ["accounts", "money"],
    queryFn: async () => {
      const [accounts, money] = await Promise.all([api.GET("/v1/accounts", {}), api.GET("/v1/money-accounts", { params: { query: { page_size: 100 } } })]);
      if (accounts.error) throw accounts.error;
      if (money.error) throw money.error;
      const used = new Set((money.data.items ?? []).map((m) => m.account_id));
      return (accounts.data.items ?? []).filter((a) => a.money_account && a.active && !a.group && !used.has(a.id));
    },
  });
  const set = (k: keyof AccountForm) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setForm({ ...form, [k]: e.target.value });
  const field = (name: string) => (errors[name] ? { "aria-invalid": true as const, "aria-describedby": `${id}-${name}-error` } : {});
  const error = (name: string) =>
    errors[name] ? (
      <span className="fy-error" id={`${id}-${name}-error`}>
        {errors[name]}
      </span>
    ) : null;
  const send = async () => {
    const { draft, errors: e } = checkAccount(form);
    setErrors(e);
    if (!draft) return;
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/money-accounts", { params: { header: { "Idempotency-Key": newKey() } }, body: draft });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    void queries.invalidateQueries({ queryKey: ["money-accounts"] });
    onDone(out.data, etagOf(out.response));
  };
  const options = chart.data ?? [];
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-title`}>
          The account
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The account was not added" />}
        {chart.isError && <Refusal problem={problemOf(chart.error)} title="The chart of accounts could not be read" />}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-kind`}>
            Kind
          </label>
          <select className="fy-select" id={`${id}-kind`} value={form.kind} onChange={set("kind")}>
            {Object.entries(kinds).map(([k, v]) => (
              <option key={k} value={k}>
                {v.words}
              </option>
            ))}
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-name`}>
            Name
          </label>
          <input className="fy-input" id={`${id}-name`} value={form.name} maxLength={100} placeholder="Such as DBS current" onChange={set("name")} {...field("name")} />
          {error("name")}
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-institution`}>
            Bank or provider (optional)
          </label>
          <input className="fy-input" id={`${id}-institution`} value={form.institution} maxLength={100} placeholder="Such as DBS, Wise or Airwallex" onChange={set("institution")} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-account`}>
            Account in the chart
          </label>
          <select className="fy-select" id={`${id}-account`} value={form.account_id} onChange={set("account_id")} {...field("account_id")}>
            <option value="">Choose an account</option>
            {options.map((a) => (
              <option key={a.id} value={a.id}>
                {a.code} {a.name}
                {a.currency ? ` (${a.currency})` : ""}
              </option>
            ))}
          </select>
          {error("account_id")}
          <span className="fy-small fy-muted">
            {chart.data && options.length === 0
              ? "No account in the chart is free to use: an Accountant marks one as a money account in Accounting › Chart of accounts."
              : "Only an account marked as a money account can be chosen; its currency is the money account's."}
          </span>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-identifier`}>
            Account number or balance ID (optional)
          </label>
          <input className="fy-input fy-mono" id={`${id}-identifier`} value={form.identifier} maxLength={64} autoComplete="off" onChange={set("identifier")} />
          <span className="fy-small fy-muted">What statements name it by. Fyniq shows only its last four characters afterwards.</span>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-opened`}>
            Opened on (optional)
          </label>
          <input className="fy-input max-w-56" id={`${id}-opened`} type="date" value={form.opened_on} onChange={set("opened_on")} />
        </div>
      </div>
      <div className="fy-card__foot">
        <Link className="fy-btn fy-btn--ghost" to="/banking">
          Cancel
        </Link>
        <button type="button" className="fy-btn fy-btn--primary" onClick={() => void send()} disabled={busy}>
          Add the account
        </button>
      </div>
    </section>
  );
}

function SourceStep({ account, etag, mayConnect, onDone }: { account: MoneyAccount; etag: string; mayConnect: boolean; onDone: (s: Source) => void }) {
  const id = useId();
  const h = helpText("bank-account.source");
  const [source, setSource] = useState<Source>("file");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [addressName, setAddressName] = useState("");
  const [from, setFrom] = useState("");
  const [format, setFormat] = useState<Format>("csv");
  const [mapping, setMapping] = useState("");
  const [connector, setConnector] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const queries = useQueryClient();
  const addresses = useQuery({
    queryKey: ["capture-addresses"],
    enabled: mayConnect,
    queryFn: async () => {
      const { data, error, response } = await api.GET("/v1/organisation/email/capture-addresses", {});
      if (error) throw error;
      return { ...data, etag: etagOf(response) };
    },
  });
  const mappings = useQuery({
    queryKey: ["statement-mappings"],
    enabled: mayConnect && source === "email",
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/statement-mappings", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const connectors = useQuery({
    queryKey: ["organisation-connectors"],
    enabled: mayConnect && source === "connector",
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/organisation/connectors", {});
      if (error) throw error;
      return (data.items ?? []).filter((c) => c.state === "enabled");
    },
  });
  const bank = addresses.data?.bank;
  const choose = (s: Source) => () => {
    setSource(s);
    setErrors({});
    setProblem(null);
  };

  const send = async () => {
    if (source === "file") return onDone("file");
    const e: Record<string, string> = {};
    const list = senders(from);
    if (source === "email") {
      if (!bank && !addressName.trim()) e.address = "Name the bank-ingest address first, such as bank.";
      if (list.length === 0) e.senders = "Enter the address the bank sends its reports from.";
      if (list.length > 5) e.senders = "Enter at most five addresses.";
      if ((format === "csv" || format === "xlsx") && !mapping) e.mapping = "Choose the saved column mapping its reports follow.";
    }
    if (source === "connector" && !connector) e.connector = "Choose the connector that syncs it.";
    setErrors(e);
    if (Object.keys(e).length > 0) return;
    setBusy(true);
    setProblem(null);
    if (source === "email" && !bank) {
      const named = await api.PUT("/v1/organisation/email/capture-addresses/bank", {
        params: { header: { "If-Match": addresses.data?.bank_etag ?? `"none"` } },
        body: { name: addressName.trim() },
      });
      if (!named.data) {
        setBusy(false);
        return setProblem(problemOf(named.error));
      }
      void queries.invalidateQueries({ queryKey: ["capture-addresses"] });
    }
    const out = await api.POST("/v1/money-accounts/{money_account_id}/sources", {
      params: { path: { money_account_id: account.id }, header: { "Idempotency-Key": newKey(), "If-Match": etag || `"${account.version}"` } },
      body:
        source === "email"
          ? { kind: "email", from_date: todayInSingapore(), senders: list, report_format: format, mapping_id: mapping || undefined }
          : { kind: "connector", from_date: todayInSingapore(), connector_id: connector },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    void queries.invalidateQueries({ queryKey: ["money-accounts"] });
    onDone(source);
  };
  const field = (name: string) => (errors[name] ? { "aria-invalid": true as const, "aria-describedby": `${id}-${name}-error` } : {});
  const error = (name: string) =>
    errors[name] ? (
      <span className="fy-error" id={`${id}-${name}-error`}>
        {errors[name]}
      </span>
    ) : null;
  const choice = (s: Source, icon: typeof Mail, title: string, words: string, disabled = false) => (
    <button className="fy-choice" type="button" role="radio" aria-checked={source === s} aria-disabled={disabled || undefined} disabled={disabled} onClick={choose(s)}>
      <span className="fy-radio" />
      <span className="fy-stack fy-grow gap-1">
        <span className="fy-row fy-row--wrap gap-2">
          <Icon of={icon} />
          <span className="fy-strong">{title}</span>
        </span>
        <span className="fy-small fy-secondary">{words}</span>
      </span>
    </button>
  );

  return (
    <section className="fy-stack fy-stack--3" aria-labelledby={`${id}-title`}>
      <h2 className="fy-h2" id={`${id}-title`} title={h.tooltip}>
        Where does {account.name}'s data come from?
      </h2>
      {problem && <Refusal problem={problem} title="The data source was not set" />}
      <div className="fy-stack fy-stack--3" role="radiogroup" aria-labelledby={`${id}-title`}>
        {choice("file", FileSpreadsheet, "Statement files you import", "You export CSV, XLSX, OFX, camt.053 or MT940 from the bank and import it. Nothing more to set up.")}
        {choice("email", Mail, "Bank reports by email", "The bank emails its daily report, and an alert for each incoming payment, to your bank-ingest address. No extra bank fee.", !mayConnect)}
        {choice("connector", Plug, "A connector, such as Wise or Airwallex", "Reads the account's transactions every hour with a read-only key, through a connector installed under Settings › Integrations.", !mayConnect)}
        <button className="fy-choice" type="button" role="radio" aria-checked={false} aria-disabled disabled>
          <span className="fy-radio" />
          <span className="fy-stack fy-grow gap-1">
            <span className="fy-row fy-row--wrap gap-2">
              <span className="fy-strong">Bank API or host-to-host</span>
              <span className="fy-chip fy-chip--neutral">
                <Icon of={Clock} />
                Not available yet
              </span>
            </span>
            <span className="fy-small fy-secondary">Such as DBS RAPID: a later release.</span>
          </span>
        </button>
      </div>
      {!mayConnect && <p className="fy-small fy-secondary">An Administrator sets up bank reports by email and connectors. Statement files need nothing more.</p>}
      {source === "email" && (
        <div className="fy-card">
          <div className="fy-card__body fy-stack fy-stack--3">
            {bank ? (
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-ingest`}>
                  Your bank-ingest address
                </label>
                <div className="fy-row gap-2">
                  <input className="fy-input fy-mono fy-grow" id={`${id}-ingest`} value={bank.address} readOnly />
                  <button type="button" className="fy-btn fy-btn--secondary" onClick={() => void navigator.clipboard?.writeText(bank.address)}>
                    <Icon of={Copy} />
                    Copy
                  </button>
                </div>
                <span className="fy-small fy-muted">Ask the bank to send the account's daily report and its incoming-funds alerts here.</span>
              </div>
            ) : (
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-name`}>
                  Name the bank-ingest address
                </label>
                <div className="fy-row gap-2">
                  <input className="fy-input fy-mono" id={`${id}-name`} value={addressName} maxLength={40} placeholder="bank" onChange={(e) => setAddressName(e.target.value)} {...field("address")} />
                  {addresses.data?.domain && <span className="fy-mono fy-secondary">@{addresses.data.domain}</span>}
                </div>
                {error("address")}
              </div>
            )}
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-senders`}>
                The bank's sender addresses
              </label>
              <textarea className="fy-input fy-mono" id={`${id}-senders`} rows={2} value={from} onChange={(e) => setFrom(e.target.value)} {...field("senders")} />
              {error("senders")}
              <span className="fy-small fy-muted">Only these are accepted, and only when the bank's mail passes DMARC; anything else waits in quarantine.</span>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-format`}>
                Its reports' format
              </label>
              <select className="fy-select max-w-56" id={`${id}-format`} value={format} onChange={(e) => setFormat(e.target.value as Format)}>
                {formats.map(([f, words]) => (
                  <option key={f} value={f}>
                    {words}
                  </option>
                ))}
              </select>
            </div>
            {(format === "csv" || format === "xlsx") && (
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-mapping`}>
                  Column mapping
                </label>
                <select className="fy-select" id={`${id}-mapping`} value={mapping} onChange={(e) => setMapping(e.target.value)} {...field("mapping")}>
                  <option value="">Choose a saved mapping</option>
                  {(mappings.data ?? [])
                    .filter((m) => m.format === format)
                    .map((m) => (
                      <option key={m.id} value={m.id}>
                        {m.name}
                      </option>
                    ))}
                </select>
                {error("mapping")}
              </div>
            )}
          </div>
        </div>
      )}
      {source === "connector" && (
        <div className="fy-card">
          <div className="fy-card__body fy-stack fy-stack--3">
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-connector`}>
                Connector
              </label>
              <select className="fy-select" id={`${id}-connector`} value={connector} onChange={(e) => setConnector(e.target.value)} {...field("connector")}>
                <option value="">Choose a connector that is on</option>
                {(connectors.data ?? []).map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
              {error("connector")}
              <span className="fy-small fy-muted">
                {connectors.data && connectors.data.length === 0
                  ? "No connector is on yet: install Wise or Airwallex under Settings › Integrations first."
                  : "The account's balance ID, entered in the first step, tells the connector which balance it is."}
              </span>
            </div>
          </div>
        </div>
      )}
      <div className="fy-row gap-2">
        <button type="button" className="fy-btn fy-btn--primary" onClick={() => void send()} disabled={busy}>
          {source === "file" ? "Continue" : "Set the data source"}
        </button>
      </div>
    </section>
  );
}

function CheckStep({ account, source }: { account: MoneyAccount; source: Source }) {
  return (
    <section className="fy-card" aria-labelledby="check-title">
      <div className="fy-card__head">
        <span className="fy-list__icon fy-list__icon--good">
          <Icon of={Check} size={20} />
        </span>
        <h2 className="fy-h3" id="check-title">
          {account.name} is added
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {source === "file" && <p>Its statements come as files you import. Until the first one, it shows no statements yet.</p>}
        {source === "email" && (
          <p>
            <span className="fy-chip fy-chip--neutral">
              <Icon of={Clock} />
              Awaiting its first report
            </span>{" "}
            The bank's next daily report is imported when it arrives; its lines wait for Reconcile now.
          </p>
        )}
        {source === "connector" && <p>The connector reads its transactions within the hour, and then every hour.</p>}
      </div>
      <div className="fy-card__foot">
        <Link className="fy-btn fy-btn--primary" to="/banking">
          Back to Banking
        </Link>
      </div>
    </section>
  );
}
