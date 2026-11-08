import { useQuery } from "@tanstack/react-query";
import { useId } from "react";
import { listAccounts } from "../accounting/data";
import { listHolders } from "./Holders";
import { accelerationWords, type OptionGrantDraft, type PoolResolutionDraft } from "./optionWords";

export const blankResolution: PoolResolutionDraft = { size: "", class_account_id: "", resolution_ref: "", resolved_on: "" };

export const blankGrant: OptionGrantDraft = {
  holder_id: "",
  number: "",
  exercise_price: "",
  granted_on: "",
  cliff_months: 12,
  vesting_months: 48,
  acceleration: "none",
  expires_on: "",
  class_account_id: "",
};

// resolutionBody is a resolution as the API takes it: whole numbers without separators, the note left out when empty.
export function resolutionBody(d: PoolResolutionDraft, classAccount: string): PoolResolutionDraft {
  const out: PoolResolutionDraft = { ...d, size: d.size.trim().replace(/,/g, ""), class_account_id: d.class_account_id || classAccount };
  if (!out.note?.trim()) delete out.note;
  return out;
}

// grantBody is a grant as the API takes it.
export function grantBody(d: OptionGrantDraft, classAccount: string): OptionGrantDraft {
  const out: OptionGrantDraft = { ...d, number: d.number.trim().replace(/,/g, ""), exercise_price: d.exercise_price.trim(),
    class_account_id: d.class_account_id || classAccount };
  if (!out.note?.trim()) delete out.note;
  return out;
}

// capitalOf are the active share-capital accounts the pool's options may be over.
function useCapital() {
  const chart = useQuery({ queryKey: ["accounts", "all"], queryFn: listAccounts });
  return (chart.data ?? []).filter((a) => a.role === "share-capital" && a.active && !a.group);
}

// ResolutionFields are a pool resolution's: the shares set aside, the class, the resolution and the day it was passed.
export function ResolutionFields({ form, set }: { form: PoolResolutionDraft; set: (d: PoolResolutionDraft) => void }) {
  const id = useId();
  const capital = useCapital();
  const field = (k: keyof PoolResolutionDraft) => (e: { target: { value: string } }) => set({ ...form, [k]: e.target.value });
  return (
    <div className="fy-form fy-form--2">
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-size`}>
          Shares set aside
        </label>
        <input className="fy-input fy-num" id={`${id}-size`} inputMode="numeric" value={form.size} onChange={field("size")} aria-describedby={`${id}-size-help`} />
        <span className="fy-help" id={`${id}-size-help`}>
          A whole number, at least the options granted by then, such as 1,000,000
        </span>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-class`}>
          Over the class of
        </label>
        <select className="fy-select" id={`${id}-class`} value={form.class_account_id || capital[0]?.id || ""} onChange={field("class_account_id")}>
          {capital.map((a) => (
            <option key={a.id} value={a.id}>
              {a.code} {a.name}
            </option>
          ))}
        </select>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-ref`}>
          Resolution
        </label>
        <input className="fy-input" id={`${id}-ref`} maxLength={1000} value={form.resolution_ref} onChange={field("resolution_ref")}
          aria-describedby={`${id}-ref-help`} />
        <span className="fy-help" id={`${id}-ref-help`}>
          Such as: Ordinary resolution 20 Jan 2026
        </span>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-on`}>
          Passed on
        </label>
        <input className="fy-input" id={`${id}-on`} type="date" value={form.resolved_on} onChange={field("resolved_on")} />
      </div>
      <div className="fy-field fy-field--full">
        <label className="fy-label" htmlFor={`${id}-note`}>
          Note (optional)
        </label>
        <input className="fy-input" id={`${id}-note`} maxLength={1000} value={form.note ?? ""} onChange={field("note")} />
      </div>
    </div>
  );
}

// GrantFields are a grant's terms (FR-EQ-19): its holder, options, exercise price, grant date, vesting and expiry, over
// the pool's class. The holder stays once drafted.
export function GrantFields({ form, set, holderFixed }: { form: OptionGrantDraft; set: (d: OptionGrantDraft) => void; holderFixed: boolean }) {
  const id = useId();
  const holders = useQuery({ queryKey: ["holders"], queryFn: listHolders, enabled: !holderFixed });
  const capital = useCapital();
  const field = (k: keyof OptionGrantDraft) => (e: { target: { value: string } }) => set({ ...form, [k]: e.target.value });
  const months = (k: "cliff_months" | "vesting_months") => (e: { target: { value: string } }) => set({ ...form, [k]: Number(e.target.value || 0) });
  return (
    <div className="fy-form fy-form--2">
      {!holderFixed && (
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-holder`}>
            Holder
          </label>
          <select className="fy-select" id={`${id}-holder`} value={form.holder_id ?? ""} onChange={field("holder_id")} aria-describedby={`${id}-holder-help`}>
            <option value="">Choose a holder</option>
            {(holders.data ?? [])
              .filter((h) => h.investor || h.shareholder)
              .map((h) => (
                <option key={h.contact_id} value={h.contact_id}>
                  {h.name}
                </option>
              ))}
          </select>
          <span className="fy-help" id={`${id}-holder-help`}>
            A contact marked investor or shareholder
          </span>
        </div>
      )}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-number`}>
          Options
        </label>
        <input className="fy-input fy-num" id={`${id}-number`} inputMode="numeric" value={form.number} onChange={field("number")}
          aria-describedby={`${id}-number-help`} />
        <span className="fy-help" id={`${id}-number-help`}>
          At most what the pool leaves on the grant date
        </span>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-price`}>
          Exercise price per share
        </label>
        <input className="fy-input fy-num" id={`${id}-price`} inputMode="decimal" value={form.exercise_price} onChange={field("exercise_price")}
          aria-describedby={`${id}-price-help`} />
        <span className="fy-help" id={`${id}-price-help`}>
          In the base currency, such as 0.10
        </span>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-granted`}>
          Granted on
        </label>
        <input className="fy-input" id={`${id}-granted`} type="date" value={form.granted_on} onChange={field("granted_on")} />
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-cliff`}>
          Cliff, in months
        </label>
        <input className="fy-input fy-num" id={`${id}-cliff`} type="number" min={0} max={120} value={form.cliff_months} onChange={months("cliff_months")} />
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-vesting`}>
          Vesting, in months
        </label>
        <input className="fy-input fy-num" id={`${id}-vesting`} type="number" min={0} max={120} value={form.vesting_months} onChange={months("vesting_months")}
          aria-describedby={`${id}-vesting-help`} />
        <span className="fy-help" id={`${id}-vesting-help`}>
          Evenly by month from the grant date; 0 vests it all at once
        </span>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-acceleration`}>
          On a change of control
        </label>
        <select className="fy-select" id={`${id}-acceleration`} value={form.acceleration}
          onChange={(e) => set({ ...form, acceleration: e.target.value as OptionGrantDraft["acceleration"] })}>
          {(Object.keys(accelerationWords) as OptionGrantDraft["acceleration"][]).map((k) => (
            <option key={k} value={k}>
              {accelerationWords[k].charAt(0).toUpperCase() + accelerationWords[k].slice(1)}
            </option>
          ))}
        </select>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-expires`}>
          Expires on
        </label>
        <input className="fy-input" id={`${id}-expires`} type="date" value={form.expires_on} onChange={field("expires_on")} />
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-class`}>
          Over the class of
        </label>
        <select className="fy-select" id={`${id}-class`} value={form.class_account_id || capital[0]?.id || ""} onChange={field("class_account_id")}>
          {capital.map((a) => (
            <option key={a.id} value={a.id}>
              {a.code} {a.name}
            </option>
          ))}
        </select>
      </div>
      <div className="fy-field fy-field--full">
        <label className="fy-label" htmlFor={`${id}-note`}>
          Note (optional)
        </label>
        <input className="fy-input" id={`${id}-note`} maxLength={1000} value={form.note ?? ""} onChange={field("note")} />
      </div>
    </div>
  );
}

export { useCapital };
