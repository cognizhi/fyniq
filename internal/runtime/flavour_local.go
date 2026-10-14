//go:build local

package runtime

// Flavour is "local" in builds tagged `local`: the only builds that contain local adapters
// (DEV-07).
const Flavour = FlavourLocal
