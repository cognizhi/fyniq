//go:build local

package app

// Local builds link the local adapters (ADR-T4); release builds never do (DEV-07). Services
// take the adapters through Deps as each port gains its first user.
import _ "github.com/cognizhi/fyniq/internal/adapters/local"
