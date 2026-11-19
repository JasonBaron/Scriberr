package adapters

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestErrorLine(t *testing.T) {
	out := "Traceback (most recent call last):\n  File \"x.py\", line 1\nModuleNotFoundError: No module named 'torch._x'\n"
	if got := ErrorLine(out, nil); got != "ModuleNotFoundError: No module named 'torch._x'" {
		t.Fatal(got)
	}
	if got := ErrorLine("warning: something\nsegfault\n", nil); got != "segfault" {
		t.Fatal(got)
	}
	if got := ErrorLine("", errors.New("exit status 1")); got != "exit status 1" {
		t.Fatal(got)
	}
}

func TestRepairVenv(t *testing.T) {
	dir := t.TempDir()
	venv := filepath.Join(dir, ".venv")
	_ = os.MkdirAll(venv, 0755)

	// Fixed by the reinstall: the env is not moved
	var syncs []string
	healthy := false
	check := func() (bool, string) { return healthy, "ModuleNotFoundError: torch._x" }
	sync := func(extra ...string) error {
		syncs = append(syncs, strings.Join(extra, " "))
		if len(extra) > 0 && extra[0] == "--reinstall" {
			healthy = true
		}
		return nil
	}
	if err := repairVenv("Test", dir, check, sync); err != nil {
		t.Fatal(err)
	}
	if len(syncs) != 1 || syncs[0] != "--reinstall" {
		t.Fatalf("syncs %v", syncs)
	}
	if _, err := os.Stat(venv); err != nil {
		t.Fatal("env should stay in place")
	}

	// Still broken after reinstall: moved aside and rebuilt
	syncs, healthy = nil, false
	sync = func(extra ...string) error {
		syncs = append(syncs, strings.Join(extra, " "))
		if len(extra) > 0 && extra[0] == "--refresh" {
			healthy = true
		}
		return nil
	}
	if err := repairVenv("Test", dir, check, sync); err != nil {
		t.Fatal(err)
	}
	if len(syncs) != 2 || syncs[1] != "--refresh" {
		t.Fatalf("syncs %v", syncs)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".venv.broken-*"))
	if len(matches) != 1 {
		t.Fatalf("old env kept aside: %v", matches)
	}

	// Never fixed: error names the import failure
	healthy = false
	sync = func(extra ...string) error { return nil }
	err := repairVenv("Test", dir, check, sync)
	if err == nil || !strings.Contains(err.Error(), "torch._x") {
		t.Fatalf("err %v", err)
	}
}
