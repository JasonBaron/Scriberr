package models

import "testing"

func TestStringListRoundTrip(t *testing.T) {
	in := StringList{"therapy", "planning"}
	v, err := in.Value()
	if err != nil {
		t.Fatal(err)
	}
	var out StringList
	if err := out.Scan(v); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0] != "therapy" || out[1] != "planning" {
		t.Fatalf("got %v", out)
	}
	if v, _ := (StringList{}).Value(); v != nil {
		t.Fatalf("empty list should be NULL, got %v", v)
	}
	if err := out.Scan(nil); err != nil || out != nil {
		t.Fatalf("nil scan: %v %v", out, err)
	}
}
