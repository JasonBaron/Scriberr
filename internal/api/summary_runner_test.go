package api

import "testing"

func TestSummaryRunnerQueue(t *testing.T) {
	r := newSummaryRunner()
	if n := r.enqueue(summaryTask{JobID: "a", TemplateID: "t1"}, summaryTask{JobID: "b", TemplateID: "t1"}); n != 2 {
		t.Fatalf("added %d, want 2", n)
	}
	if n := r.enqueue(summaryTask{JobID: "a", TemplateID: "t1"}, summaryTask{JobID: "a", TemplateID: "t2"}); n != 1 {
		t.Fatalf("duplicate should be ignored, added %d", n)
	}
	if !r.queuedFor("a") || r.queuedFor("z") {
		t.Error("queuedFor is wrong")
	}

	first := r.next()
	if first.JobID != "a" || first.TemplateID != "t1" {
		t.Fatalf("next = %+v", first)
	}
	if n := r.enqueue(summaryTask{JobID: "a", TemplateID: "t1"}); n != 0 {
		t.Error("the running task should not be queued again")
	}
	st := r.status()
	if !st.Running || st.Queued != 2 || st.Current == nil || st.Current.JobID != "a" {
		t.Errorf("status = %+v", st)
	}
	if idle := r.finish(SummaryRunItem{JobID: "a", Status: "done"}); idle {
		t.Error("queue is not empty yet")
	}
	dropped := r.cancel()
	if len(dropped) != 2 {
		t.Errorf("cancelled %d, want 2", len(dropped))
	}
	st = r.status()
	if st.Running || st.Done != 1 || len(st.Recent) != 1 {
		t.Errorf("status after cancel = %+v", st)
	}
}

func TestMatchesAnyTag(t *testing.T) {
	if !matchesAnyTag([]string{"Therapy "}, []string{"family", "therapy"}) {
		t.Error("should match ignoring case and spaces")
	}
	if matchesAnyTag([]string{"work"}, []string{"family"}) || matchesAnyTag(nil, []string{"x"}) {
		t.Error("should not match")
	}
}
