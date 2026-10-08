package adapters

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestWhisperXRefOverride(t *testing.T) {
	t.Setenv(EnvWhisperXRef, "")
	if got := whisperXRef(); got != DefaultWhisperXRef {
		t.Errorf("default ref = %q, want %q", got, DefaultWhisperXRef)
	}
	t.Setenv(EnvWhisperXRef, " v3.8.6 ")
	if got := whisperXRef(); got != "v3.8.6" {
		t.Errorf("override ref = %q, want v3.8.6", got)
	}
}

func TestLockEnvSerialisesSamePath(t *testing.T) {
	var inside, maxInside int32
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			unlock := lockEnv("/tmp/scriberr-test-env/../scriberr-test-env") // cleaned to one key
			n := atomic.AddInt32(&inside, 1)
			for {
				m := atomic.LoadInt32(&maxInside)
				if n <= m || atomic.CompareAndSwapInt32(&maxInside, m, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&inside, -1)
			unlock()
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	if maxInside != 1 {
		t.Errorf("setup ran %d at once in the same env, want 1", maxInside)
	}
}
