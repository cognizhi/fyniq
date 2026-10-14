//go:build !local

package runtime

// Flavour is "release" in every build without the `local` tag: the images that run in staging
// and production (DEV-07).
const Flavour = FlavourRelease
