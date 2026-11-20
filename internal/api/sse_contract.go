package api

import (
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// eventIDsAreIntegers narrows the id of every event of a streamed response back to an integer. huma 2.40 declares it as
// an integer or a string, since its sender can write a string id; Fyniq's streams only ever send numbers, and a widened
// response type is a breaking change of /v1 (FR-INT-06, oasdiff against the last release).
func eventIDsAreIntegers(api huma.API, path string) {
	item := api.OpenAPI().Paths[path]
	if item == nil || item.Post == nil || item.Post.Responses["200"] == nil {
		return
	}
	mt := item.Post.Responses["200"].Content["text/event-stream"]
	if mt == nil || mt.Schema == nil || mt.Schema.Items == nil {
		return
	}
	events, _ := mt.Schema.Items.Extensions["oneOf"].([]*huma.Schema) // huma lists the events there, not in OneOf
	for _, event := range events {
		if id := event.Properties["id"]; id != nil {
			id.OneOf = nil
			id.Type = huma.TypeInteger
		}
	}
}

// approvalEnumsAreExtensible declares the object types and operations approvals cover as extensible enums (x-extensible-enum), as
// oasdiff reads them: the set grows with every object the platform lets a second person approve, and a widened response enum is a
// breaking change of /v1 (FR-INT-06), so a client is told to handle a value it has not seen. The values are the same.
func approvalEnumsAreExtensible(api huma.API) {
	for _, s := range api.OpenAPI().Components.Schemas.Map() {
		for _, name := range []string{"object_type", "operation"} {
			p := s.Properties[name]
			if p == nil || !slices.ContainsFunc(p.Enum, func(v any) bool {
				return v == "kb_publication" || v == "kb_publication.publish" || v == "organisation_setting"
			}) {
				continue
			}
			if p.Extensions == nil {
				p.Extensions = map[string]any{}
			}
			p.Extensions["x-extensible-enum"] = p.Enum
			p.Enum = nil
		}
	}
}

// messagePurposesAreExtensible declares what an email message is for as an extensible enum (x-extensible-enum): a purpose is added with
// each kind of email the platform sends (an invitation's, a company secretary's reminder), and a widened response enum is a breaking
// change of /v1 (FR-INT-06), so a client is told to handle a purpose it has not seen. The values are the same. The senders' four
// purposes (notifications, invoices, reminders, security) are another, closed list and stay as they are.
func messagePurposesAreExtensible(api huma.API) {
	extensible := func(name string, member any) {
		for schema, s := range api.OpenAPI().Components.Schemas.Map() {
			p := s.Properties[name]
			if strings.HasSuffix(schema, "Draft") { // what a client sends stays a closed list: only what it may receive grows
				continue
			}
			if p == nil || !slices.Contains(p.Enum, member) {
				continue
			}
			if p.Extensions == nil {
				p.Extensions = map[string]any{}
			}
			p.Extensions["x-extensible-enum"] = p.Enum
			p.Enum = nil
		}
	}
	extensible("purpose", any("secretary_reminder"))
	// The kinds of service a connector provides grow with the catalogue (payment links, bank data, e-invoicing): a client is told to handle a kind it has not seen.
	extensible("kind", any("payment-provider"))
	// The bank report formats a source reads grow with the banks' formats (MT942 after CAMT.053): a client is told to handle a format it has not seen.
	extensible("report_format", any("mt942"))
}
