package gpu

import (
	"context"
	"testing"
	"time"
)

func TestLockIsExclusive(t *testing.T) {
	release, err := Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := TryAcquire(); ok {
		t.Fatal("second acquire succeeded while lock held")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx); err == nil {
		t.Fatal("Acquire should time out while lock held")
	}
	release()
	release() // releasing twice must be safe
	r2, ok := TryAcquire()
	if !ok {
		t.Fatal("lock not free after release")
	}
	r2()
}

func TestWaitForMemory(t *testing.T) {
	orig := freeMiB
	defer func() { freeMiB = orig }()

	freeMiB = func() (int, bool) { return 8000, true }
	if r := WaitForMemory(context.Background(), 6000, time.Minute, nil); !r.Measured || r.TimedOut || r.FreeMiB != 8000 {
		t.Errorf("enough memory: %+v", r)
	}

	freeMiB = func() (int, bool) { return 0, false }
	if r := WaitForMemory(context.Background(), 6000, time.Minute, nil); r.Measured || r.TimedOut {
		t.Errorf("no nvidia-smi should skip waiting: %+v", r)
	}

	freeMiB = func() (int, bool) { return 2000, true }
	reports := 0
	r := WaitForMemory(context.Background(), 6000, 0, func(int) { reports++ })
	if !r.TimedOut || reports != 0 {
		t.Errorf("maxWait 0 should give up at once: %+v reports=%d", r, reports)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = WaitForMemory(ctx, 6000, time.Hour, func(int) { reports++ })
	if !r.TimedOut || reports != 1 {
		t.Errorf("cancelled ctx should stop waiting: %+v reports=%d", r, reports)
	}
}

func TestSettingsFromEnv(t *testing.T) {
	t.Setenv(EnvMinFreeMB, "")
	t.Setenv(EnvWaitMax, "")
	if MinFreeMiB() != defaultMinFreeMB || WaitMax() != defaultWaitMax {
		t.Error("defaults not applied")
	}
	t.Setenv(EnvMinFreeMB, "4500")
	t.Setenv(EnvWaitMax, "90s")
	if MinFreeMiB() != 4500 || WaitMax() != 90*time.Second {
		t.Error("env overrides not applied")
	}
	t.Setenv(EnvWaitMax, "0")
	if WaitMax() != 0 {
		t.Error("0 should disable waiting")
	}
}

func TestHooksRunInOrder(t *testing.T) {
	var got []int
	OnBeforeTranscription(func(context.Context) string { got = append(got, 1); return "first" })
	OnBeforeTranscription(func(context.Context) string { got = append(got, 2); return "" })
	notes := RunBeforeTranscription(context.Background())
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("hooks = %v", got)
	}
	if len(notes) != 1 || notes[0] != "first" {
		t.Errorf("notes = %v", notes)
	}
}
