// What QA-UI-01 and QA-UI-02 ask of every shared component in src/components/ (p1-launch-readiness task 4.2): a story
// file beside it that imports it, which Storybook runs in every theme with axe, and a component test that imports it.
// A component is a .tsx file there that is neither a test nor a story. The checks take the files' text by path, so
// their tests can plant a component without either.

export interface Files {
  [path: string]: string; // path relative to src/components, with forward slashes → its text
}

const dir = (p: string) => (p.includes("/") ? p.slice(0, p.lastIndexOf("/") + 1) : "");
const base = (p: string) => p.slice(p.lastIndexOf("/") + 1).replace(/\.tsx$/, "");

export function components(files: Files): string[] {
  return Object.keys(files)
    .filter((p) => p.endsWith(".tsx") && !p.endsWith(".test.tsx") && !p.endsWith(".stories.tsx"))
    .sort();
}

// imports reports whether a file's text imports the component at path from its own folder.
function imports(text: string, path: string): boolean {
  return new RegExp(`from "\\./${base(path)}"`).test(text);
}

// withoutStories are the components with no story file beside them that imports them.
export function withoutStories(files: Files): string[] {
  return components(files).filter((c) => {
    const story = files[`${dir(c)}${base(c)}.stories.tsx`];
    return story === undefined || !imports(story, c);
  });
}

// withoutTests are the components no test in their folder imports.
export function withoutTests(files: Files): string[] {
  return components(files).filter(
    (c) => !Object.entries(files).some(([p, text]) => p.endsWith(".test.tsx") && dir(p) === dir(c) && imports(text, c)),
  );
}
