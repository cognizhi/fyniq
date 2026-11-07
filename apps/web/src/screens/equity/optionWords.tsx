import { CircleCheck, Hourglass, PencilLine } from "lucide-react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { date } from "../../format/date";
import { Icon } from "../../ui";

export type OptionPool = components["schemas"]["OptionPoolView"];
export type PoolResolution = components["schemas"]["PoolResolutionView"];
export type PoolResolutionDraft = components["schemas"]["PoolResolutionDraft"];
export type OptionGrant = components["schemas"]["OptionGrantView"];
export type OptionGrantDraft = components["schemas"]["OptionGrantDraft"];
export type OptionEvent = components["schemas"]["OptionEventView"];
export type OptionEventDraft = components["schemas"]["OptionEventDraft"];

// The roles that draft and ask (the Accountant's C/P on the cap table), as Appendix D's "Cap table" row gives them.
export const drafters = ["accountant"];

// count shows a whole number of shares or options with thousands separators, as the server gave it.
export function count(n: string | undefined): string {
  if (n === undefined || n === "") return "–";
  return n.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

export const accelerationWords: Record<OptionGrantDraft["acceleration"], string> = {
  none: "no acceleration",
  single: "single-trigger acceleration",
  double: "double-trigger acceleration",
};

export const eventWords: Record<OptionEventDraft["kind"], string> = {
  forfeiture: "Forfeiture",
  expiry: "Expiry",
};

// vestingText is a grant's vesting in words: its cliff and months from the grant date, and its acceleration.
export function vestingText(g: OptionGrantDraft): string {
  if (g.vesting_months === 0) return `Vested on the grant date; ${accelerationWords[g.acceleration]}`;
  const years = g.vesting_months % 12 === 0 ? `${g.vesting_months / 12} year${g.vesting_months === 12 ? "" : "s"}` : `${g.vesting_months} months`;
  const cliff = g.cliff_months === 0 ? "no cliff" : `a ${g.cliff_months}-month cliff`;
  return `${years} monthly with ${cliff}, from ${date(g.granted_on)}; ${accelerationWords[g.acceleration]}`;
}

// RecordStatus is a draft or recorded record's status, with its icon and words.
export function RecordStatus({ status, asked }: { status: "draft" | "recorded"; asked?: string }) {
  if (status === "recorded") {
    return (
      <span className="fy-chip fy-chip--good">
        <Icon of={CircleCheck} />
        Recorded
      </span>
    );
  }
  return asked ? (
    <span className="fy-chip fy-chip--neutral">
      <Icon of={Hourglass} />
      Waiting for approval
    </span>
  ) : (
    <span className="fy-chip fy-chip--neutral">
      <Icon of={PencilLine} />
      Draft
    </span>
  );
}

export async function poolOf(): Promise<OptionPool> {
  const { data, error } = await api.GET("/v1/option-pool", {});
  if (error) throw error;
  return data;
}

export async function listGrants(): Promise<OptionGrant[]> {
  const { data, error } = await api.GET("/v1/option-grants", { params: { query: { page_size: 200 } } });
  if (error) throw error;
  return data.items ?? [];
}

export async function grantOf(id: string): Promise<{ g: OptionGrant; etag: string }> {
  const { data, error, response } = await api.GET("/v1/option-grants/{grant_id}", { params: { path: { grant_id: id } } });
  if (error) throw error;
  return { g: data, etag: response.headers.get("ETag") ?? `"${data.version}-${data.status === "draft" ? "draft" : "posted"}"` };
}

export async function resolutionOf(id: string): Promise<{ r: PoolResolution; etag: string }> {
  const { data, error, response } = await api.GET("/v1/option-pool/resolutions/{resolution_id}", { params: { path: { resolution_id: id } } });
  if (error) throw error;
  return { r: data, etag: response.headers.get("ETag") ?? `"${data.version}-${data.status === "draft" ? "draft" : "posted"}"` };
}

export async function eventOf(id: string): Promise<{ e: OptionEvent; etag: string }> {
  const { data, error, response } = await api.GET("/v1/option-events/{event_id}", { params: { path: { event_id: id } } });
  if (error) throw error;
  return { e: data, etag: response.headers.get("ETag") ?? `"${data.version}-${data.status === "draft" ? "draft" : "posted"}"` };
}
