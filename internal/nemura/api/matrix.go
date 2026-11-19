package api

// Matrix is the authorisation matrix of the API, one row per operation: who may call it. Every operation has a row, and the test
// TestEveryOperationHasARowInTheAuthorisationMatrix fails the build for one that has none (closed by default). The matrix of
// PRD-002 Appendix B, per role and per field, arrives with the roles slice of p1-nemura-platform; the first rows are the operations that need no
// sign-in and show no organisation's data, then the ones any signed-in person may call.
var Matrix = map[string]string{ // #nosec G101 -- operation names and who may call them, not credentials
	"GET /v1/info":                     "anyone",
	"GET /v1/errors":                   "anyone",
	"GET /v1/errors/{code}":            "anyone",
	"GET /v1/sign-in":                  "anyone",
	"GET /v1/session":                  "signed in (any role)",
	"POST /v1/passkeys/registration":   "signed in (any role), for themselves",
	"POST /v1/passkeys":                "signed in (any role), for themselves",
	"POST /v1/session/step-up":         "signed in (any role), for themselves",
	"POST /v1/session/step-up/confirm": "signed in (any role), for themselves",
	"POST /v1/api-clients":             "Owner, with a fresh passkey confirmation",
	"GET /v1/api-clients":              "Owner",
	"DELETE /v1/api-clients/{id}":      "Owner, with a fresh passkey confirmation",
}
