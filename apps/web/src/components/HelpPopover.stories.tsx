import type { Meta, StoryObj } from "@storybook/react-vite";
import { HelpPopover } from "./HelpPopover";

// Help beside what it explains (FR-UX-30): an ⓘ beside a label, or a glossary term's own words (FR-UX-11).
const meta = { title: "Shared/HelpPopover", component: HelpPopover } satisfies Meta<typeof HelpPopover>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Info: Story = {
  args: {
    title: "Books health",
    children: <span className="fy-small">How the books stand by the checks that ran.</span>,
    learnMore: <a href="#checks">Learn more</a>,
  },
};

export const GlossaryTerm: Story = {
  args: {
    title: "Suspense",
    term: "suspense",
    children: <span className="fy-small">The account where money waits until someone says what it is for.</span>,
  },
};
