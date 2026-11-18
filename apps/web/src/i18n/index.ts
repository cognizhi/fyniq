import catalogue from "./en-SG.json";

// The help strings (FR-UX-31, NFR-L10N-01): referenced by the registry's help key, kept out of the
// code, ready for translation. One catalogue today (en-SG); others sit beside it later.

export interface HelpText {
  label: string;
  tooltip: string;
  // An info popover's (FR-UX-30): what it is and why it matters in two or three plain sentences, an example, and the
  // accounting detail Expert mode adds (account, posting, rule ID). The tooltip stands in for a missing detail.
  detail?: string;
  example?: string;
  expert?: string;
}

const help: Record<string, HelpText | undefined> = catalogue.help;

export function helpText(key: string): HelpText {
  const h = help[key];
  if (!h) throw new Error(`no help text for ${key} in src/i18n/en-SG.json`);
  return h;
}

export function hasHelpText(key: string): boolean {
  return Boolean(help[key]?.label && help[key]?.tooltip);
}
