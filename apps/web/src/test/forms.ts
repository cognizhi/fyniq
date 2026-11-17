// The forms QA-UI-01 asks a test of (p1-launch-readiness task 4.2): every screen file that can mark a field invalid
// (aria-invalid), the validation messages it writes itself, and whether the tests that reach it trigger each message
// and check an invalid field. The checks read the files' text by path, so their own test can plant a form.

export interface Files {
  [path: string]: string; // path relative to src, with forward slashes → its text
}

// A message the form writes itself: a text of at least 8 characters given to an error map, to setErrors or setError,
// or to a list of problems (not to any list: a rule's summary is no message). A message built with ${…} is known by its fixed start.
const MESSAGE = /(?:\b(?:errs|errors|e|err|next|problems|bad)\s*(?:\.\s*[A-Za-z_]\w*|\[[^\]]+\])\s*=\s*|setErrors?\(\s*\{[^}]*?:\s*|set(?:Error|Missing|Invalid)\(\s*|\b(?:problems|errs|errors)\.(?:push|add)\(\s*)[`"]([^`"]{8,}?)[`"]/g;

export function messages(text: string): string[] {
  const out = new Set<string>();
  for (const m of text.matchAll(MESSAGE)) {
    const fixed = (m[1] ?? "").split("${")[0]!.slice(0, 40).trim();
    if (fixed.length >= 8) out.add(fixed);
  }
  return [...out].sort();
}

export function forms(files: Files): string[] {
  return Object.keys(files)
    .filter((p) => p.endsWith(".tsx") && !p.endsWith(".test.tsx") && !p.endsWith(".stories.tsx") && files[p]!.includes("aria-invalid"))
    .sort();
}

const IMPORT = /from "(\.{1,2}\/[^"]+)"/g;

// resolve is the file an import from `from` names, among files, or undefined.
function resolve(files: Files, from: string, spec: string): string | undefined {
  const parts = from.split("/").slice(0, -1);
  for (const s of spec.split("/")) {
    if (s === "..") parts.pop();
    else if (s !== ".") parts.push(s);
  }
  const base = parts.join("/");
  return [base, `${base}.tsx`, `${base}.ts`, `${base}/index.tsx`, `${base}/index.ts`].find((p) => p in files);
}

// reaches are the files a file imports, and what they import, down to depth.
function reaches(files: Files, from: string, depth: number, seen = new Set<string>()): Set<string> {
  if (depth < 0 || seen.has(from)) return seen;
  seen.add(from);
  for (const m of (files[from] ?? "").matchAll(IMPORT)) {
    const to = resolve(files, from, m[1]!);
    if (to) reaches(files, to, depth - 1, seen);
  }
  return seen;
}

export interface Coverage {
  form: string;
  untriggered: string[]; // its messages no test that reaches it holds
  invalidChecked: boolean; // a test that reaches it checks aria-invalid, itself or with invalid() from test/invalid
}

const CHECKS_INVALID = /aria-invalid|from "(?:\.\.?\/)+test\/invalid"/;

// coverage is each form's: the tests that reach it (through up to three imports) must hold each of its messages and
// check that a field is marked invalid.
export function coverage(files: Files): Coverage[] {
  const tests = Object.keys(files).filter((p) => /\.test\.tsx?$/.test(p));
  const reached = new Map(tests.map((t) => [t, reaches(files, t, 3)]));
  return forms(files).map((form) => {
    const mine = tests.filter((t) => reached.get(t)!.has(form));
    const text = mine.map((t) => files[t]).join("\n");
    return {
      form,
      untriggered: messages(files[form]!).filter((m) => !text.includes(m)),
      invalidChecked: mine.some((t) => CHECKS_INVALID.test(files[t]!)),
    };
  });
}
