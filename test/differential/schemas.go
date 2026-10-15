// Package differential embeds the shared scenario and result schemas, so the Go side of the
// differential tests validates against the same files as the oracle (task 2.1).
package differential

import _ "embed"

// ScenarioSchema is scenario.schema.json (fyniq-scenario@1).
//
//go:embed scenario.schema.json
var ScenarioSchema []byte

// ResultSchema is result.schema.json (fyniq-scenario-result@1).
//
//go:embed result.schema.json
var ResultSchema []byte
