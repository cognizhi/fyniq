package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeAIRoute is the database's refusal of a route (FY334): a version that is not the next, changed or removed, a step
// written apart from its version, a model not on its connection's allow-list, or a class the agent never handles
// (PRD-001 AI-74; p1-ai-providers task 5.1).
const CodeAIRoute = "STORAGE-AI-ROUTE-01"

// AIAgent is an agent that calls a model, as the release lists it (AI-74): the installation's, the same for every
// organisation.
type AIAgent struct {
	ID          string
	Name        string
	Description string
	Classes     []string // the data classes it may ever handle
	Line        string   // the model line its presets pin: main or classification
}

// AIRouteStep is one step of a route: 1 the primary, then the fallbacks in order.
type AIRouteStep struct {
	Position     int
	ConnectionID uuid.UUID
	Model        string
	Effort       string
	Cache        bool
}

// AIRoute is a version of an agent's route in an organisation (AI-74).
type AIRoute struct {
	ID       uuid.UUID
	Agent    string
	Version  int
	Classes  []string
	Preset   string
	Steps    []AIRouteStep
	Approval *uuid.UUID // the second approver's, for a version that widens where data goes
	MadeBy   string
	MadeAt   time.Time
}

// AIRoutes keeps each agent's route, version after version, and lists the agents.
type AIRoutes interface {
	// Agents are the release's agents, in their order.
	Agents(ctx context.Context) ([]AIAgent, error)
	// Current are the routes in force: each agent's latest version.
	Current(ctx context.Context) ([]AIRoute, error)
	// Versions are an agent's versions, newest first, at most limit.
	Versions(ctx context.Context, agent string, limit int) ([]AIRoute, error)
	// Save adds r as its agent's next version with its steps: audited as ai_route.changed. CodeAIRoute when the
	// database refuses it.
	Save(ctx context.Context, r AIRoute) (AIRoute, error)
}
