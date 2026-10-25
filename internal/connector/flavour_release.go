//go:build !local

package connector

// LocalFlavour is true in the local image flavour (DEV-07): its substitutes, such as Mailpit without TLS, are
// allowed only there, never in this one.
const LocalFlavour = false
