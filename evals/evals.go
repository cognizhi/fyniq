// Package evals holds the AI eval suites (PRD-001 §16.2, QA-AI-*, AI-78; p1-ai-providers task 5.4): one directory per
// suite with its suite.json, embedded so that a qualification runs the suite the release ships. evals/cassettes holds
// the recorded answers `make evals` replays; it is not a suite.
package evals

import "embed"

// Suites are the suites' definitions, as evals/<suite>/suite.json.
//
//go:embed */suite.json
var Suites embed.FS
