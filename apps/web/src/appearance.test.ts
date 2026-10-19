import { describe, expect, it } from "vitest";
import { applyTo, defaults, parse, resolve, setAppearance, startAppearance, type Environment } from "./appearance";
import { media } from "./test/setup";

const os: Environment = { prefersDark: false, prefersMoreContrast: false, prefersReducedMotion: false, organisationMotion: "on" };
const root = () => document.documentElement;

describe("appearance", () => {
  // req: experience/visual-design Themes
  it("applies Light, Dark and System, and System follows the operating system", () => {
    expect(resolve({ ...defaults, theme: "light" }, { ...os, prefersDark: true }).dark).toBe(false);
    expect(resolve({ ...defaults, theme: "dark" }, os).dark).toBe(true);
    expect(resolve({ ...defaults, theme: "system" }, { ...os, prefersDark: true }).dark).toBe(true);
    expect(resolve({ ...defaults, theme: "system" }, os).dark).toBe(false);
  });

  // req: experience/visual-design Themes
  it("turns high contrast on from the person's choice or prefers-contrast: more", () => {
    expect(resolve({ ...defaults, contrast: "system" }, { ...os, prefersMoreContrast: true }).moreContrast).toBe(true);
    expect(resolve({ ...defaults, contrast: "more" }, os).moreContrast).toBe(true);
    expect(resolve({ ...defaults, contrast: "standard" }, { ...os, prefersMoreContrast: true }).moreContrast).toBe(false);
  });

  // req: FR-UX-38
  // req: experience/visual-design Motion
  it("reduces motion for the person, always for prefers-reduced-motion, and turns it off for the organisation", () => {
    expect(resolve(defaults, os).motion).toBe("on");
    expect(resolve({ ...defaults, motion: "reduce" }, os).motion).toBe("reduce");
    expect(resolve({ ...defaults, motion: "on" }, { ...os, prefersReducedMotion: true }).motion).toBe("reduce");
    expect(resolve({ ...defaults, motion: "on" }, { ...os, organisationMotion: "off" }).motion).toBe("off");
  });

  // req: experience/visual-design Themes
  // req: experience/visual-design Density
  it("sets the attributes tokens.css reads on <html>", () => {
    applyTo(root(), { dark: true, moreContrast: true, density: "compact", motion: "reduce" });
    expect(root().classList.contains("dark")).toBe(true);
    expect(root().dataset.contrast).toBe("more");
    expect(root().dataset.density).toBe("compact");
    expect(root().dataset.motion).toBe("reduce");
    applyTo(root(), { dark: false, moreContrast: false, density: "comfortable", motion: "on" });
    expect(root().classList.contains("dark")).toBe(false);
    expect(root().dataset.contrast).toBeUndefined();
    expect(root().dataset.density).toBeUndefined();
    expect(root().dataset.motion).toBe("on");
  });

  it("keeps the choice across page loads and ignores values it does not know", () => {
    media("(prefers-color-scheme: dark)", true);
    startAppearance();
    expect(root().classList.contains("dark")).toBe(true); // System by default
    setAppearance({ theme: "light", density: "compact" });
    expect(parse(window.localStorage.getItem("fyniq.appearance"))).toEqual({ ...defaults, theme: "light", density: "compact" });
    startAppearance();
    expect(root().classList.contains("dark")).toBe(false);
    expect(root().dataset.density).toBe("compact");
    expect(parse('{"theme":"neon","motion":"reduce"}')).toEqual({ ...defaults, motion: "reduce" });
    expect(parse("not json")).toEqual(defaults);
  });
});
