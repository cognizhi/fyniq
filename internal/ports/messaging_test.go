package ports_test

import (
	"reflect"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
)

// A chat connector's port has one method, posting a message: it cannot read a channel, approve, pay or export (FR-NTF-04).
// req: FR-NTF-04
func TestTheMessagingPortHasOneMethod(t *testing.T) {
	typ := reflect.TypeFor[ports.Messaging]()
	if typ.NumMethod() != 1 || typ.Method(0).Name != "Post" {
		t.Fatalf("the messaging port's methods: %d", typ.NumMethod())
	}
}
