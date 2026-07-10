package runtime

import "testing"

func TestProgressStateValuesAreStable(t *testing.T) {
	if RunCompleted != "completed" || TaskPartial != "partial" || RunCanceled != "canceled" {
		t.Fatalf("unexpected progress state values")
	}
}
