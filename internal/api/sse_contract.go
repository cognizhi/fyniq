package api

import "github.com/danielgtaylor/huma/v2"

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
