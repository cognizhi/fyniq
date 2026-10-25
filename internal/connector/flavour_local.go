//go:build local

package connector

// LocalFlavour is true in the local image flavour (DEV-07): its substitutes, such as Mailpit without TLS, are
// allowed only here.
const LocalFlavour = true
