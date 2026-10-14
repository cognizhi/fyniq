// Package ports declares the interfaces the domain depends on. Each port has a production
// adapter and a local adapter (ADR-T8, ADR-T4); cloud SDKs are imported only under
// internal/adapters/<provider>/ (NFR-PORT-03).
package ports

import "time"

// Clock is the only source of the current time (DEV-05). Tests freeze it; non-production
// environments may set a business date.
type Clock interface {
	Now() time.Time
}
