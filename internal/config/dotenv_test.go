package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvDoesNotOverrideRealEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("IDRAC_2B_ONLY=fromfile\nIDRAC_2B_BOTH=fromfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IDRAC_2B_BOTH", "fromenv") // real env must win
	t.Cleanup(func() { _ = os.Unsetenv("IDRAC_2B_ONLY") })

	LoadDotEnv(filepath.Join(dir, "idrac.yml")) // loads <dir>/.env

	if got := os.Getenv("IDRAC_2B_ONLY"); got != "fromfile" {
		t.Fatalf("IDRAC_2B_ONLY = %q, want fromfile", got)
	}
	if got := os.Getenv("IDRAC_2B_BOTH"); got != "fromenv" {
		t.Fatalf("IDRAC_2B_BOTH = %q, want fromenv (real env must not be overridden)", got)
	}
}
