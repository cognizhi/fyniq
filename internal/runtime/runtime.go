// Package runtime decides at start-up whether this binary may run in this environment.
//
// Each image has two flavours built from the same commit (PRD-001 §12.8, ADR-T4): a release
// flavour for staging and production, and a local flavour, tagged `local`, that alone contains
// the local adapters. A release binary refuses to start outside staging and production or with a
// local adapter linked in (DEV-07); a local binary refuses to start in staging or production
// (SEC-16).
package runtime

import (
	"fmt"
	"os"
	"sort"
	"sync"
)

// Build flavours.
const (
	FlavourLocal   = "local"
	FlavourRelease = "release"
)

// Env is the value of FYNIQ_ENV.
type Env string

// The environments a Fyniq binary knows.
const (
	EnvLocal      Env = "local"
	EnvCI         Env = "ci"
	EnvDev        Env = "dev"
	EnvStaging    Env = "staging"
	EnvProduction Env = "production"
)

// Error is a start-up refusal with a stable code and a plain fix (golden rule 13).
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// ParseEnv reads a FYNIQ_ENV value.
func ParseEnv(s string) (Env, error) {
	switch e := Env(s); e {
	case EnvLocal, EnvCI, EnvDev, EnvStaging, EnvProduction:
		return e, nil
	case "":
		return "", &Error{"RUNTIME-ENV-01", "FYNIQ_ENV is not set: set it to local, ci, dev, staging or production"}
	default:
		return "", &Error{"RUNTIME-ENV-02", fmt.Sprintf("FYNIQ_ENV=%q is not an environment: use local, ci, dev, staging or production", s)}
	}
}

// EnvFromOS reads FYNIQ_ENV. A local build defaults to local so that `make dev` needs no setup.
func EnvFromOS() (Env, error) {
	v := os.Getenv("FYNIQ_ENV")
	if v == "" && Flavour == FlavourLocal {
		return EnvLocal, nil
	}
	return ParseEnv(v)
}

// Adapter is one implementation of a port linked into this binary.
type Adapter struct {
	Port  string
	Name  string
	Local bool
}

var (
	mu       sync.Mutex
	adapters []Adapter
)

// RegisterAdapter records an adapter linked into the binary. Local adapters call it from init,
// so a release binary that somehow linked one is refused at start-up.
func RegisterAdapter(a Adapter) {
	mu.Lock()
	defer mu.Unlock()
	adapters = append(adapters, a)
}

// Adapters lists the registered adapters, sorted by port and name.
func Adapters() []Adapter {
	mu.Lock()
	defer mu.Unlock()
	out := append([]Adapter(nil), adapters...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Check decides whether a binary of this flavour, with these adapters, may run in env.
func Check(flavour string, env Env, linked []Adapter) error {
	switch flavour {
	case FlavourRelease:
		if env != EnvStaging && env != EnvProduction {
			return &Error{"RUNTIME-FLAVOUR-01", fmt.Sprintf("this is a release build and FYNIQ_ENV=%s: release builds run only in staging or production; use the local build (`make dev`, `make up`) elsewhere", env)}
		}
		for _, a := range linked {
			if a.Local {
				return &Error{"RUNTIME-FLAVOUR-02", fmt.Sprintf("the local %s adapter %q is linked into a release build: rebuild without the `local` tag", a.Port, a.Name)}
			}
		}
	case FlavourLocal:
		if env == EnvStaging || env == EnvProduction {
			return &Error{"RUNTIME-FLAVOUR-03", fmt.Sprintf("this is a local build and FYNIQ_ENV=%s: local adapters never reach staging or production; deploy the release image", env)}
		}
	default:
		return &Error{"RUNTIME-FLAVOUR-04", fmt.Sprintf("unknown build flavour %q", flavour)}
	}
	return nil
}

// CheckThisBinary applies Check to the running binary.
func CheckThisBinary(env Env) error { return Check(Flavour, env, Adapters()) }
