import { describe, expect, it } from "vitest";
import { buildTarget, engines, supported } from "../browsers";
import { projects } from "../playwright.config";

// req: NFR-BROWSER-01
describe("the supported browsers", () => {
  it("are NFR-BROWSER-01's, each built for and each engine tested", () => {
    // The PRD's list: the last two versions of Chrome, Edge, Safari and Firefox; iOS Safari and Android Chrome.
    expect(supported.map((b) => b.browser).sort()).toEqual(["Android Chrome", "Chrome", "Edge", "Firefox", "Safari", "iOS Safari"].sort());
    // Every browser's family is in the build's target, and nothing else is.
    const families = new Set(buildTarget.map((t) => t.replace(/\d+$/, "")));
    expect([...families].sort()).toEqual(["chrome", "edge", "firefox", "ios", "safari"]);
    // Every engine has its Playwright project, and the three engines are all there.
    expect(engines.sort()).toEqual(["chromium", "firefox", "webkit"]);
    for (const e of engines) expect(projects[e]?.name).toBeTruthy();
    expect(projects.chromium.use.channel).toBe("chrome");
  });
});
