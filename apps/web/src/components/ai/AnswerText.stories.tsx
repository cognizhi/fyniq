import type { Meta, StoryObj } from "@storybook/react-vite";
import { InApp } from "../../test/story";
import { AnswerText } from "./AnswerText";

// An answer's text (AI-08): words in Fyniq's Markdown subset, each figure from its fact, and what could not be established.
const meta = { title: "AI/AnswerText", component: AnswerText } satisfies Meta<typeof AnswerText>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Answer: Story = {
  args: {
    segments: [
      { text: "Revenue in **October** was " },
      { fact: "f-1", figure: "12,400.00 SGD" },
      { text: ", up on *September*.\n- see `4010`\n- open [the statement](/reports/statements/pl)" },
      { missing: "the November budget." },
    ],
  },
  render: (args) => (
    <InApp>
      <AnswerText {...args} />
    </InApp>
  ),
};
