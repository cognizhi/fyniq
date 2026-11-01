// The parts of ECharts the charts use (MASTER.md §1: ECharts 6), registered once: line and bar charts with their grid,
// tooltip and legend, drawn on canvas so a chart exports to PNG. Only Chart imports this module, and only on demand,
// so ECharts stays out of the first load.
import { BarChart, LineChart } from "echarts/charts";
import { GridComponent, LegendComponent, TooltipComponent } from "echarts/components";
import { init, use } from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";

use([LineChart, BarChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer]);

export { init };
