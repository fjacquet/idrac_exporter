package collector

import (
	"encoding/json"
	"testing"
)

func TestXstringEmptyArrayDoesNotPanic(t *testing.T) {
	var x xstring
	if err := json.Unmarshal([]byte(`[]`), &x); err != nil {
		t.Fatalf("unmarshal []: %v", err)
	}
	if x.String() != "" {
		t.Fatalf("xstring = %q, want empty", x.String())
	}
}

func TestXstringMemberArray(t *testing.T) {
	var x xstring
	if err := json.Unmarshal([]byte(`[{"Member":"v1"}]`), &x); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if x.String() != "v1" {
		t.Fatalf("xstring = %q, want v1", x.String())
	}
}
