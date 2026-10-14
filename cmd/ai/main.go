// Command ai runs the AI service: read tools and proposals, never a write credential (PRD-001 §9, golden rule 5). It is a shell until P1.
package main

import (
	"os"

	"github.com/cognizhi/fyniq/internal/app"
)

func main() { os.Exit(app.Serve("ai", nil)) }
