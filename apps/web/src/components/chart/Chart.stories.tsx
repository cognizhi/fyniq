import type { Meta, StoryObj } from "@storybook/react-vite";
import { Chart } from "./Chart";

// A chart by the dataviz method (FR-VIZ-01 to FR-VIZ-08): its summary, its table view and its downloads beside it.
const meta = {
  title: "Shared/Chart",
  component: Chart,
  args: {
    title: "Revenue and costs",
    summary: "Revenue rose in August and fell below costs in September.",
    label: "Month",
    categories: ["Jul 2026", "Aug 2026", "Sep 2026"],
    unit: "SGD",
    file: "revenue-and-costs",
  },
} satisfies Meta<typeof Chart>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Line: Story = {
  args: {
    form: "line",
    series: [
      { key: "revenue", name: "Revenue", values: ["12000.00", "13500.50", "-250.00"] },
      { key: "costs", name: "Costs", values: ["9000.00", "", "11000.25"], slot: 4 },
    ],
  },
};

export const Bars: Story = {
  args: { form: "bar", series: [{ key: "revenue", name: "Revenue", values: ["12000.00", "13500.50", "9800.00"] }] },
};

export const Ranked: Story = {
  args: {
    title: "Spend by supplier",
    summary: "Three suppliers make up most of the quarter's spend.",
    form: "bar-horizontal",
    label: "Supplier",
    categories: ["Atlassian", "Amazon Web Services", "WeWork"],
    series: [{ key: "spend", name: "Spend", values: ["4200.00", "3100.75", "2500.00"] }],
    file: "spend-by-supplier",
  },
};
