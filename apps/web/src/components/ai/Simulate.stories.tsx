import type { Meta, StoryObj } from "@storybook/react-vite";
import { InApp } from "../../test/story";
import { CashSimulate } from "./Simulate";

// Simulate (FR-PLN-08): the bill asked about paid on another day, or not in the next 90 days, through the cash
// projection's what-if, which saves nothing.
const meta = { title: "AI/CashSimulate", component: CashSimulate } satisfies Meta<typeof CashSimulate>;
export default meta;
type Story = StoryObj<typeof meta>;

const projection = {
  today: "2026-11-15", days: 90, currency: "SGD", opening: { amount: "50000.00", currency: "SGD" }, lowest: { amount: "12000.00", currency: "SGD" },
  lowest_on: "2026-11-20", below_buffer_on: "2026-11-20", buffer: {}, daily: [], weekly: [], not_included: [],
  movements: [{ key: "b-1", kind: "bill", id: "b-1", number: "S-77", expected_on: "2026-11-20", basis: "due", amount: { amount: "-8000.00", currency: "SGD" } }],
};

export const ABill: Story = {
  args: { context: { kind: "bill", id: "b-1", label: "Bill S-77" } },
  render: (args) => (
    <InApp data={[[["cash-projection", 90], projection]]}>
      <CashSimulate {...args} />
    </InApp>
  ),
};

export const NothingAskedAbout: Story = {
  args: {},
  render: (args) => (
    <InApp>
      <CashSimulate {...args} />
    </InApp>
  ),
};
