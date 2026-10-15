// The theme follows the operating system (MASTER.md): dark, more contrast and reduced motion. tokens.css
// switches colours with the .dark class and data-contrast on <html>; the kit's animations run only
// with data-motion="on" on the .fy-device wrapper (FR-UX-38).
export function followSystemTheme(): void {
  const root = document.documentElement;
  const dark = window.matchMedia("(prefers-color-scheme: dark)");
  const contrast = window.matchMedia("(prefers-contrast: more)");
  const apply = () => {
    root.classList.toggle("dark", dark.matches);
    if (contrast.matches) root.dataset.contrast = "more";
    else delete root.dataset.contrast;
  };
  apply();
  dark.addEventListener("change", apply);
  contrast.addEventListener("change", apply);
}

export function motion(): "on" | "reduce" {
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "reduce" : "on";
}
