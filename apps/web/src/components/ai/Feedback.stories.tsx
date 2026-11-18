import type { Meta, StoryObj } from "@storybook/react-vite";
import { InApp } from "../../test/story";
import { TurnFeedback } from "./Feedback";

// Rating an answer (QA-AI-05): a thumbs up or down with a reason, once, then how the rating stands.
const meta = { title: "AI/TurnFeedback", component: TurnFeedback } satisfies Meta<typeof TurnFeedback>;
export default meta;
type Story = StoryObj<typeof meta>;

const args = { conversation: "c-1", turn: "t-1", onRated: () => {} };

export const NotRatedYet: Story = {
  args,
  render: (a) => (
    <InApp>
      <TurnFeedback {...a} />
    </InApp>
  ),
};

export const Rated: Story = {
  args: { ...args, rated: { id: "f-1", rating: "down", reason: "Wrong month.", state: "waiting" } },
  render: (a) => (
    <InApp>
      <TurnFeedback {...a} />
    </InApp>
  ),
};
