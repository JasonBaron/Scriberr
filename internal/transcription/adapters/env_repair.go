package adapters

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"scriberr/pkg/logger"
)

// CheckEnvironment runs importStatement in the uv project at envPath and
// returns whether it worked and, when it did not, the error line Python
// printed (for example "ModuleNotFoundError: No module named 'torch._x'").
// Unlike CheckEnvironmentReady it is never cached.
func CheckEnvironment(envPath, importStatement string) (bool, string) {
	cmd := exec.Command("uv", "run", UVTLSFlag, "--project", envPath, "python", "-c", importStatement)
	out, err := cmd.CombinedOutput()
	if err == nil {
		setEnvCache(envPath, importStatement, true)
		return true, ""
	}
	setEnvCache(envPath, importStatement, false)
	return false, ErrorLine(string(out), err)
}

// ErrorLine picks the line that names a Python failure from its output:
// the last line that looks like "SomethingError: ..." or "...Exception...",
// else the last non-empty line, else err.
func ErrorLine(out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if pythonErrorLine(l) {
			return truncate(l, 300)
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return truncate(l, 300)
		}
	}
	if err != nil {
		return err.Error()
	}
	return "unknown error"
}

func pythonErrorLine(l string) bool {
	head, _, ok := strings.Cut(l, ":")
	if !ok || strings.ContainsAny(head, " \t\"'/") {
		return false
	}
	return strings.HasSuffix(head, "Error") || strings.HasSuffix(head, "Exception") ||
		strings.HasSuffix(head, "Interrupt") || strings.HasSuffix(head, "Exit")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func setEnvCache(envPath, importStatement string, ready bool) {
	envCacheMutex.Lock()
	envCache[fmt.Sprintf("%s:%s", envPath, importStatement)] = ready
	envCacheMutex.Unlock()
}

// InvalidateEnvironmentCache forgets every cached readiness check, so the
// next PrepareEnvironment imports for real.
func InvalidateEnvironmentCache() {
	envCacheMutex.Lock()
	envCache = make(map[string]bool)
	envCacheMutex.Unlock()
}

// repairVenv brings a broken uv environment back after a normal sync did not:
//  1. reinstall every package in place (fast: files come from the uv cache);
//  2. move .venv aside and install from scratch, refreshing the cache.
//
// sync runs "uv sync" with the extra flags given. It returns nil once check
// passes, else the last import error.
func repairVenv(name, projectDir string, check func() (bool, string), sync func(extra ...string) error) error {
	ok, reason := check()
	if ok {
		return nil
	}
	logger.Warn(name+" environment is broken; reinstalling its packages", "error", reason)
	if err := sync("--reinstall"); err != nil {
		logger.Warn(name+" reinstall failed", "error", err)
	}
	if ok, reason = check(); ok {
		logger.Info(name + " environment repaired (packages reinstalled)")
		return nil
	}

	venv := filepath.Join(projectDir, ".venv")
	if _, err := os.Stat(venv); err == nil {
		aside := venv + ".broken-" + time.Now().Format("20060102-150405")
		logger.Warn(name+" still broken after reinstall; rebuilding it from scratch", "error", reason, "old_env", aside)
		if err := os.Rename(venv, aside); err != nil {
			return fmt.Errorf("%s environment is broken (%s) and could not be moved aside: %w", name, reason, err)
		}
	}
	if err := sync("--refresh"); err != nil {
		return fmt.Errorf("%s rebuild failed: %w", name, err)
	}
	if ok, reason = check(); ok {
		logger.Info(name+" environment rebuilt from scratch", "note", "the old environment was kept as .venv.broken-*; delete it when you no longer need it")
		return nil
	}
	return fmt.Errorf("%s environment still fails to import after a rebuild: %s", name, reason)
}
