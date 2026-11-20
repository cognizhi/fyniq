package ports

import (
	"context"
	"errors"
	"time"
)

// Messaging is a chat connector's one write (PRD-001 FR-NTF-04, rev 0.620; p2-api-third-parties design D9): it posts a short text, which carries a deep link into the web
// app, to a channel. It is a port of its own with one method, held there by a test: a chat connector cannot read a channel, approve, pay or export, and approvals never
// happen in chat (FR-APR-03). The text is built by Fyniq at the content level the connection chose (minimal by default: no amount, name or title).
type Messaging interface {
	// Post sends the message. ErrChatRefused means the provider refused it for good (a channel that does not exist, a revoked token), so a retry would only repeat
	// the refusal; ErrChatLimited carries how long the provider asked to wait; any other error leaves it unknown whether it was posted.
	Post(ctx context.Context, m ChatMessage) (ChatReceipt, error)
}

// ChatMessage is a message to post.
type ChatMessage struct {
	// Channel is the provider's channel: a Slack channel ID or name, a Cliq channel's unique name.
	Channel string
	// Text is the plain text, with its link.
	Text string
}

// ChatReceipt is what the provider answered: its reference for the message, never its body.
type ChatReceipt struct {
	ID string
}

// ErrChatRefused is a provider's refusal of a message, with no body kept.
var ErrChatRefused = errors.New("the chat provider refused the message")

// ChatLimited is a provider's request to wait before posting again.
type ChatLimited struct{ After time.Duration }

func (e ChatLimited) Error() string { return "the chat provider asked to wait before posting again" }
