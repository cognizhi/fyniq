import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { noViolations } from "../../test/axe";
import { Chart } from "./Chart";
import { chartOption, escaped, fold, plotted, type Series, type Theme } from "./option";

// ECharts draws on a canvas, which jsdom lacks: the chart's module is replaced by a recorder of what it is asked to draw.
const drawn: { option: ReturnType<typeof chartOption> | null; actions: unknown[] } = { option: null, actions: [] };
vi.mock("./echarts", () => ({
  init: () => ({
    setOption: (o: ReturnType<typeof chartOption>) => (drawn.option = o),
    dispatchAction: (a: unknown) => drawn.actions.push(a),
    getDataURL: () => "data:image/png;base64,iVBORw0KGgo=",
    resize: () => undefined,
    dispose: () => undefined,
  }),
}));

afterEach(() => {
  drawn.option = null;
  drawn.actions = [];
});

const theme: Theme = { series: ["#c1", "#c2", "#c3", "#c4", "#c5", "#c6", "#c7", "#c8"], text: "#t", axis: "#a", grid: "#g", surface: "#s", font: "Inter" };
const months = ["Jul 2026", "Aug 2026", "Sep 2026"];
const revenue: Series = { key: "revenue", name: "Revenue", values: ["12000.00", "13500.50", "-250.00"] };
const costs: Series = { key: "costs", name: "Costs & <expenses>", values: ["9000.00", "", "11000.25"], slot: 4 };

// req: FR-VIZ-01
// req: FR-VIZ-02
// req: FR-VIZ-03
// req: FR-VIZ-06
// req: FR-VIZ-08
describe("a chart's option", () => {
  it("plots the exact figures on one value axis, in each entity's fixed colour, with a legend and named lines", () => {
    const o = chartOption("line", months, [revenue, costs], theme);
    expect(o.series.map((s) => s.data)).toEqual([[12000, 13500.5, -250], [9000, null, 11000.25]]);
    expect(o.color).toEqual(["#c1", "#c5"]); // costs keeps its slot whatever comes before it
    expect([o.xAxis.type, o.yAxis.type]).toEqual(["category", "value"]);
    expect(o.legend).toMatchObject({ top: 0 });
    expect(o.series.map((s) => ("endLabel" in s ? s.endLabel.show : null))).toEqual([true, true]);
    expect(o.textStyle.color).toBe("#t"); // text in text colours, never a series colour
    const tip = o.tooltip.formatter([{ seriesIndex: 0, dataIndex: 2, marker: "" }, { seriesIndex: 1, dataIndex: 2, marker: "" }]);
    expect(tip).toBe("Sep 2026<br/>Revenue: <b>−250.00</b><br/>Costs &amp; &lt;expenses&gt;: <b>11,000.25</b>");
  });

  it("ranks named things on horizontal bars with their values, and a single series has no legend", () => {
    const o = chartOption("bar-horizontal", ["Rent", "Salaries"], [{ key: "x", name: "Expenses", values: ["545.00", "300.00"] }], theme);
    expect([o.xAxis.type, o.yAxis.type]).toEqual(["value", "category"]);
    expect(o.legend).toEqual({ show: false });
    const s = o.series[0]!;
    expect("label" in s && s.label.show && "formatter" in s.label ? s.label.formatter?.({ dataIndex: 0 }) : null).toBe("545.00");
  });

  it("folds more than eight series into Other, added up exactly", () => {
    const many = Array.from({ length: 10 }, (_, i): Series => ({ key: `s${i}`, name: `S${i}`, values: ["0.10", "0.20"] }));
    const shown = fold(many, 2);
    expect(shown.map((s) => s.name)).toEqual(["S0", "S1", "S2", "S3", "S4", "S5", "S6", "Other"]);
    expect(shown[7]?.values).toEqual(["0.30", "0.60"]); // 0.1 + 0.1 + 0.1 is 0.30, not 0.30000000000000004
    expect(plotted("")).toBeNull();
    expect(escaped(`<img src=x onerror="alert(1)">`)).toBe("&lt;img src=x onerror=&quot;alert(1)&quot;&gt;");
  });
});

// req: FR-VIZ-06
describe("the chart", () => {
  it("draws the chart, moves its tooltip with the keys, and shows the same figures in its table view; axe finds nothing", async () => {
    render(<Chart title="Revenue and costs" summary="Revenue rose in August." form="line" label="Month" categories={months} series={[revenue, costs]} unit="SGD" file="trend" />);
    const plot = screen.getByRole("img", { name: /Revenue and costs\. Revenue rose in August\./ });
    await vi.waitFor(() => expect(drawn.option).not.toBeNull());
    await noViolations(document.body);
    fireEvent.keyDown(plot, { key: "ArrowRight" });
    fireEvent.keyDown(plot, { key: "ArrowRight" });
    fireEvent.keyDown(plot, { key: "Escape" });
    expect(drawn.actions).toEqual([{ type: "showTip", seriesIndex: 0, dataIndex: 0 }, { type: "showTip", seriesIndex: 0, dataIndex: 1 }, { type: "hideTip" }]);

    const plottedFigures = drawn.option!.series.map((s) => s.data);
    fireEvent.click(screen.getByRole("radio", { name: "Table" }));
    const table = screen.getByRole("table", { name: "Revenue and costs" });
    const rows = within(table).getAllByRole("row").map((r) => Array.from(r.children).map((c) => c.textContent));
    expect(rows).toEqual([
      ["Month", "Revenue", "Costs & <expenses>"],
      ["Jul 2026", "12,000.00", "9,000.00"],
      ["Aug 2026", "13,500.50", "–"],
      ["Sep 2026", "−250.00", "11,000.25"],
    ]);
    // The table's figures are the chart's: each shown figure, read back, is the one plotted.
    const read = (t: string | null) => (t === "–" ? null : Number((t ?? "").replace(/,/g, "").replace("−", "-")));
    expect([0, 1].map((col) => rows.slice(1).map((r) => read(r[col + 1] ?? null)))).toEqual(plottedFigures);
    expect(screen.getByRole("button", { name: "Download Revenue and costs as PNG" }).hasAttribute("disabled")).toBe(true);
    await noViolations(document.body);
  });

  it("downloads its data as CSV with plain decimals and inert text, and the chart as PNG", async () => {
    const blobs: Blob[] = [];
    Object.assign(URL, { createObjectURL: (b: Blob) => (blobs.push(b), "blob:x"), revokeObjectURL: vi.fn() });
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
    const formula: Series = { key: "f", name: "=HYPERLINK()", values: ["-1234.50", "0.00", "1.00"] };
    render(<Chart title="Odd names" summary="A test." form="bar" label="Month" categories={months} series={[formula]} unit="SGD" file="odd" />);
    await vi.waitFor(() => expect(drawn.option).not.toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Download Odd names's data as CSV" }));
    expect(await blobs[0]!.text()).toBe("Month,'=HYPERLINK() SGD\r\nJul 2026,-1234.50\r\nAug 2026,0.00\r\nSep 2026,1.00\r\n");
    fireEvent.click(screen.getByRole("button", { name: "Download Odd names as PNG" }));
    expect(blobs[1]?.type).toBe("image/png");
    expect(click).toHaveBeenCalledTimes(2);
  });
});
