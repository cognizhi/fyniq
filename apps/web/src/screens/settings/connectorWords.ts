import { CircleCheck, CircleHelp, CircleX, Pause, PackagePlus, Power, Settings2, FlaskConical, Trash2, type LucideIcon } from "lucide-react";
import type { components } from "../../api/schema";

// The words and chips of the connector screens (FR-INT-25…27): one place for the labels of kinds, data classes,
// regions, states and health, so the catalogue and a connector's page say the same thing.

export type CatalogueEntry = components["schemas"]["CatalogueEntry"];
export type Connector = components["schemas"]["Connector"];
export type InstallationChange = components["schemas"]["InstallationChange"];
export type Kind = components["schemas"]["ConnectorCapability"]["kind"];
export type DataClass = NonNullable<CatalogueEntry["data_classes"]>[number];
export type Scope = "installation" | "organisation";

// kinds is every kind of service in the catalogue's order (docs/design/screens ConnectorCatalogue).
export const kinds: [Kind, string][] = [
  ["sign-in", "Sign-in"],
  ["outbound-email", "Outbound email"],
  ["inbound-email", "Inbound email"],
  ["bank-data", "Bank data"],
  ["payment-provider", "Payment providers"],
  ["e-invoicing", "E-invoicing"],
  ["document-ai", "Document AI"],
  ["messaging", "Messaging"],
  ["ticketing", "Ticketing"],
];

export function kindLabel(k: string): string {
  return kinds.find(([x]) => x === k)?.[1] ?? k;
}

const dataClasses: Record<DataClass, string> = {
  "know-how": "know-how",
  "business-figures": "business figures",
  "personal-data": "personal data",
  payroll: "payroll",
  restricted: "restricted data",
};

// dataClassesLabel is what a connector sends out of Fyniq, in words: "personal data and business figures".
export function dataClassesLabel(cs: readonly string[] | null | undefined): string {
  const words = (cs ?? []).map((c) => dataClasses[c as DataClass] ?? c);
  if (words.length === 0) return "nothing";
  return words.length === 1 ? (words[0] ?? "") : `${words.slice(0, -1).join(", ")} and ${words.at(-1) ?? ""}`;
}

const regions: Record<string, string> = {
  SG: "Singapore",
  US: "United States",
  EU: "European Union",
  BR: "Brazil",
  JP: "Japan",
  IN: "India",
  AU: "Australia",
  configured: "where its server is",
};

export function regionsLabel(rs: readonly string[] | null | undefined): string {
  return (rs ?? []).map((r) => regions[r] ?? r).join(", ") || "none";
}

// processedIn is where the provider processes what it is sent, as a phrase: "in United States", or "where its server
// is" for a server the person chooses.
export function processedIn(rs: readonly string[] | null | undefined): string {
  const named = (rs ?? []).filter((r) => r !== "configured");
  const configured = (rs ?? []).includes("configured");
  if (named.length === 0) return configured ? "where its server is" : "nowhere";
  return `in ${regionsLabel(named)}${configured ? ", or where its server is" : ""}`;
}

export const stateChip: Record<Connector["state"], { label: string; tone: string; icon: LucideIcon }> = {
  installed: { label: "Installed", tone: "neutral", icon: PackagePlus },
  configured: { label: "Configured", tone: "neutral", icon: Settings2 },
  tested: { label: "Tested", tone: "info", icon: FlaskConical },
  enabled: { label: "Enabled", tone: "good", icon: CircleCheck },
  paused: { label: "Paused", tone: "warning", icon: Pause },
  disabled: { label: "Off", tone: "neutral", icon: Power },
  uninstalled: { label: "Uninstalled", tone: "neutral", icon: Trash2 },
};

export const healthChip: Record<Connector["health"], { label: string; tone: string; icon: LucideIcon }> = {
  ok: { label: "OK", tone: "good", icon: CircleCheck },
  failing: { label: "Failing", tone: "critical", icon: CircleX },
  unknown: { label: "Not tested", tone: "neutral", icon: CircleHelp },
};

// upgradeOf says whether the release ships a newer manifest, and whether it asks for more than was consented to.
export function upgradeOf(c: Connector): "none" | "consent" | "plain" {
  if (c.release_version === c.manifest_version) return "none";
  return c.upgrade_widens ? "consent" : "plain";
}

// missingSecrets is the secrets the manifest names that have no value yet.
export function missingSecrets(c: Connector): string[] {
  return (c.secrets ?? []).filter((s) => !s.set).map((s) => s.name);
}
