import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { defaultMode, forgetMode, setMode, setPinnedBasic, useMode, usePinnedBasic } from "./mode";

describe("the experience mode", () => {
  // req: FR-UX-27
  it("starts by role, and the Owner wins", () => {
    expect(defaultMode(["owner"])).toBe("basic");
    expect(defaultMode(["bookkeeper", "approver", "employee"])).toBe("basic");
    expect(defaultMode(["accountant"])).toBe("expert");
    expect(defaultMode(["controller", "administrator"])).toBe("expert");
    expect(defaultMode(["tax_agent"])).toBe("expert");
    expect(defaultMode(["auditor"])).toBe("expert");
    expect(defaultMode(["owner", "payroll_administrator"])).toBe("basic");
    expect(defaultMode(["owner", "controller", "administrator"])).toBe("basic"); // the founders
    expect(defaultMode([])).toBe("basic");
    expect(defaultMode(null)).toBe("basic");
  });

  // req: FR-UX-23
  it("keeps the person's choice over the default", () => {
    const { result } = renderHook(() => useMode(["owner"]));
    expect(result.current).toBe("basic");
    act(() => setMode("expert"));
    expect(result.current).toBe("expert");
    expect(window.localStorage.getItem("fyniq.mode")).toBe("expert");
  });

  // req: FR-UX-28
  it("shows a pinned person Basic whatever they chose, and ignores a switch", () => {
    forgetMode();
    const { result } = renderHook(() => ({ mode: useMode(["accountant"]), pinned: usePinnedBasic() }));
    act(() => setMode("expert"));
    expect(result.current).toEqual({ mode: "expert", pinned: false });
    act(() => setPinnedBasic(true));
    expect(result.current).toEqual({ mode: "basic", pinned: true });
    act(() => setMode("expert")); // no choice while pinned
    expect(result.current.mode).toBe("basic");
    act(() => setPinnedBasic(false));
    expect(result.current.mode).toBe("expert"); // their own choice returns when the pin is lifted
    forgetMode();
  });
});
