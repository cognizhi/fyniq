package api

// Matrix is the authorisation matrix of the API, one row per operation: who may call it. Every operation has a row, and the test
// TestEveryOperationHasARowInTheAuthorisationMatrix fails the build for one that has none (closed by default). The matrix of
// PRD-002 Appendix B, per role and per field, arrives with the roles slice of p1-nemura-platform; these rows are the operations
// that need no sign-in and show no organisation's data.
var Matrix = map[string]string{
	"GET /v1/info":          "anyone",
	"GET /v1/errors":        "anyone",
	"GET /v1/errors/{code}": "anyone",
}
