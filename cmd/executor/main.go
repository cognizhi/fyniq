// Command executor executes approved proposals under its own identity and queue (PRD-001 §12.3, §12.11). It is a shell until P1.
package main

import (
	"os"

	"github.com/cognizhi/fyniq/internal/app"
)

func main() { os.Exit(app.Serve("executor", nil)) }
