package api

import "testing"

func TestVoiceQueueNewRecordingsFirst(t *testing.T) {
	r := newVoiceRunner()
	r.push(voiceTask{JobID: "lib1"})
	r.push(voiceTask{JobID: "lib2"})
	r.push(voiceTask{JobID: "new1", First: true})
	r.push(voiceTask{JobID: "new2", First: true})
	if r.push(voiceTask{JobID: "lib1"}) {
		t.Fatal("duplicate queued")
	}
	var order []string
	for {
		task, ok := r.pop()
		if !ok {
			break
		}
		order = append(order, task.JobID)
	}
	want := []string{"new1", "new2", "lib1", "lib2"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order %v", order)
		}
	}
}
