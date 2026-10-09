package voiceid

import (
	"math"
	"testing"
)

func vec(xs ...float64) []float64 { return xs }

func TestCosineAndCentroid(t *testing.T) {
	if s := Cosine(vec(1, 0), vec(1, 0)); math.Abs(s-1) > 1e-9 {
		t.Fatal(s)
	}
	if s := Cosine(vec(1, 0), vec(0, 1)); math.Abs(s) > 1e-9 {
		t.Fatal(s)
	}
	if Cosine(vec(1), vec(1, 2)) != 0 || Cosine(nil, nil) != 0 {
		t.Fatal("mismatched lengths should score 0")
	}
	c := Centroid([]Sample{{vec(2, 0), 10}, {vec(0, 5), 10}, {vec(1, 2, 3), 10}})
	if math.Abs(c[0]-c[1]) > 1e-9 || math.Abs(c[0]*c[0]+c[1]*c[1]-1) > 1e-9 {
		t.Fatalf("centroid %v", c)
	}
	// Long samples are capped so they do not drown out short ones
	c = Centroid([]Sample{{vec(1, 0), 5000}, {vec(0, 1), 600}})
	if math.Abs(c[0]-c[1]) > 1e-9 {
		t.Fatalf("capped weights %v", c)
	}
}

func TestAssign(t *testing.T) {
	profiles := []Profile{{"p1", "Jason", vec(1, 0, 0)}, {"p2", "Dana", vec(0, 1, 0)}}
	prints := []Print{
		{Speaker: "S0", Vector: vec(0.9, 0.1, 0), Seconds: 60},
		{Speaker: "S1", Vector: vec(0.1, 0.9, 0.1), Seconds: 60},
		{Speaker: "S2", Vector: vec(0, 0, 1), Seconds: 60},   // nobody
		{Speaker: "S3", Vector: vec(0.95, 0, 0), Seconds: 3}, // too short
	}
	m := Assign(prints, profiles, 0.6, nil, nil)
	if len(m) != 2 || m[0].Speaker != "S0" || m[0].Name != "Jason" || m[1].Speaker != "S1" || m[1].Name != "Dana" {
		t.Fatalf("%+v", m)
	}
	// Named by hand: skipped, and that name is not given to anyone else
	m = Assign(prints, profiles, 0.6, map[string]bool{"S0": true}, map[string]bool{"dana": true})
	if len(m) != 0 {
		t.Fatalf("%+v", m)
	}
	// One profile per recording: the closer speaker wins
	two := []Print{{Speaker: "A", Vector: vec(0.8, 0.2, 0), Seconds: 30}, {Speaker: "B", Vector: vec(1, 0.05, 0), Seconds: 30}}
	m = Assign(two, profiles[:1], 0.6, nil, nil)
	if len(m) != 1 || m[0].Speaker != "B" {
		t.Fatalf("%+v", m)
	}
	// Too close between two profiles: no name
	amb := []Print{{Speaker: "A", Vector: vec(1, 1, 0), Seconds: 30}}
	if m = Assign(amb, profiles, 0.6, nil, nil); len(m) != 0 {
		t.Fatalf("ambiguous %+v", m)
	}
}

func TestAssess(t *testing.T) {
	good := Print{Speaker: "S0", Vector: vec(1, 0), Seconds: 120, Chunks: []Chunk{{0, 8, 0.8}, {8, 16, 0.7}, {16, 24, 0.75}, {24, 30, 0.6}}}
	mixed := Print{Speaker: "S1", Vector: vec(0, 1), Seconds: 120, Chunks: []Chunk{{0, 8, 0.8}, {8, 16, 0.1}, {16, 24, 0.75}, {24, 32, 0.2}}}
	if p := Assess([]Print{good}); len(p) != 0 {
		t.Fatalf("%+v", p)
	}
	p := Assess([]Print{good, mixed})
	if len(p) != 1 || p[0].Kind != "mixed" || p[0].Speakers[0] != "S1" {
		t.Fatalf("%+v", p)
	}
	twin := Print{Speaker: "S2", Vector: vec(0.98, 0.1), Seconds: 60}
	p = Assess([]Print{good, twin})
	if len(p) != 1 || p[0].Kind != "same" {
		t.Fatalf("%+v", p)
	}
	short := Print{Speaker: "S3", Vector: vec(1, 0), Seconds: 5}
	if p = Assess([]Print{good, short}); len(p) != 0 {
		t.Fatalf("short speakers are not judged: %+v", p)
	}
}

func TestEnrollable(t *testing.T) {
	for _, n := range []string{"", "Speaker 1", "SPEAKER_00", "speaker_01", "Unknown", "?", "Guest 2", "  "} {
		if Enrollable(n) {
			t.Errorf("%q should not enroll", n)
		}
	}
	for _, n := range []string{"Jason", "Dr. Smith", "Mom", "Speaker Pelosi"} {
		if !Enrollable(n) {
			t.Errorf("%q should enroll", n)
		}
	}
}
