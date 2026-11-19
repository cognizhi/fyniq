import catalogue from "./en-SG.json";

// Every string a person reads comes from this catalogue (NM-NFR-04), never from a component: a test fails on a string literal in one. Help
// (NM-UX-04) is referenced by the registry's help key. One catalogue today (en-SG); others sit beside it later.
export interface HelpText {
  label: string;
  tooltip: string;
}

const text: Record<string, string | undefined> = catalogue.text;
const help: Record<string, HelpText | undefined> = catalogue.help;

// t is the catalogue's text for a key, with {name} placeholders filled: no string is built by joining pieces, so another language can reorder them.
export function t(key: string, values: Record<string, string> = {}): string {
  const s = text[key];
  if (s === undefined) throw new Error(`no text ${key} in src/i18n/en-SG.json`);
  return s.replace(/\{(\w+)\}/g, (_, name: string) => values[name] ?? "");
}

export function helpText(key: string): HelpText {
  const h = help[key];
  if (!h) throw new Error(`no help text for ${key} in src/i18n/en-SG.json`);
  return h;
}

export function hasHelpText(key: string): boolean {
  return Boolean(help[key]?.label && help[key]?.tooltip);
}

export const textKeys = Object.keys(text);
