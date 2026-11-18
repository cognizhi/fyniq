import { describe, expect, it } from "vitest";
import { split } from "./hinge";

// req: FR-UX-37
// req: experience/devices-and-motion FR-UX-37 Responsive at every size Spanning a hinge
describe("two panes split at a hinge", () => {
  const two = [
    { x: 0, width: 680 },
    { x: 700, width: 680 },
  ];
  it("gives the list the width up to the hinge, and the hinge's width as the gap", () => {
    expect(split(two, 240, 1100)).toEqual({ list: 440, gap: 20 });
    expect(split([...two].reverse(), 0, 1380)).toEqual({ list: 680, gap: 20 });
  });
  it("leaves a screen in one piece, and a pane wholly on one side, as it is", () => {
    expect(split(null, 0, 1000)).toBeNull();
    expect(split([{ x: 0, width: 1380 }], 0, 1380)).toBeNull();
    expect(split(two, 720, 600)).toBeNull();
    expect(split(two, 10, 600)).toBeNull();
  });
});
