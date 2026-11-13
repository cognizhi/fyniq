package aipolicy

// Where the model providers process in Singapore (PRD-001 §12.12, ADR-T2, AI-88): per connector and endpoint, the
// models whose calls are processed in asia-southeast1, with the official source and the day it was read. It chooses
// the presets' Singapore endpoints (preset A, p1-ai-providers task 5.1) and prefills a declaration, which still needs
// its own dated evidence; a model it does not list has no evidenced Singapore processing.

// SingaporeEndpoint is an endpoint that processes in Singapore, the models it does so for, and its source.
type SingaporeEndpoint struct {
	Connector string
	Endpoint  string // the host
	Models    []string
	Evidence  Evidence
}

// Singapore are the endpoints known to process in Singapore, and for which models.
var Singapore = []SingaporeEndpoint{
	{Connector: "anthropic", Endpoint: "asia-southeast1-aiplatform.googleapis.com",
		// Claude on Google Cloud in asia-southeast1: Opus 4.6, Sonnet 4.6, Opus 4.5 and Sonnet 4.5; the 4.7 and later and
		// the 5.x models only on the global endpoint (PRD-001 §12.5 and §12.12, the rev 0.19 sweep of 14 Oct 2026)
		// [verify: re-read the locations page before a preset pins them].
		Models: []string{"claude-opus-4-6", "claude-sonnet-4-6", "claude-opus-4-5", "claude-sonnet-4-5"},
		Evidence: Evidence{Kind: "source", URL: "https://cloud.google.com/vertex-ai/generative-ai/docs/learn/locations",
			Title: "Google Cloud: Generative AI on Vertex AI locations (Claude in asia-southeast1)", CheckedOn: "2026-10-14"}},
}

// OnlyStoredInSingapore are endpoints that keep data at rest in Singapore but process it elsewhere: never evidence of
// Singapore processing (AI-88's example, checked 14 Oct 2026) [verify: the data-residency page].
var OnlyStoredInSingapore = []SingaporeEndpoint{
	{Connector: "openai", Endpoint: "api.openai.com",
		Evidence: Evidence{Kind: "source", URL: "https://platform.openai.com/docs/guides/your-data",
			Title: "OpenAI: Data controls in the OpenAI platform (Singapore stores at rest, processing elsewhere)", CheckedOn: "2026-10-14"}},
}

// SingaporeModels are the models a connector's endpoint processes in Singapore, by this table.
func SingaporeModels(connector, endpoint string) []string {
	for _, s := range Singapore {
		if s.Connector == connector && s.Endpoint == endpoint {
			return s.Models
		}
	}
	return nil
}
