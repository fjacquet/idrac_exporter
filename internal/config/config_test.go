package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPasswordFilePopulatesPassword(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "pw")
	if err := os.WriteFile(secret, []byte("s3cr3t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &AuthConfig{Username: "u", PasswordFile: secret, Scheme: "https"}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if c.Password != "s3cr3t" {
		t.Fatalf("Password = %q, want s3cr3t (trimmed)", c.Password)
	}
}

func TestPasswordFileMissingErrors(t *testing.T) {
	c := &AuthConfig{Username: "u", PasswordFile: "/no/such/file", Scheme: "https"}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for unreadable password_file, got nil")
	}
}

func TestPasswordFileOverridesInlinePassword(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "pw")
	if err := os.WriteFile(secret, []byte("fromfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &AuthConfig{Username: "u", Password: "inline", PasswordFile: secret, Scheme: "https"}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if c.Password != "fromfile" {
		t.Fatalf("Password = %q, want fromfile (password_file takes precedence)", c.Password)
	}
}

func TestConcurrencyFromEnvironment(t *testing.T) {
	t.Setenv("CONFIG_CONCURRENCY", "4")
	c := NewConfig()
	c.FromEnvironment()
	if c.Concurrency != 4 {
		t.Fatalf("Concurrency = %d, want 4", c.Concurrency)
	}
}

func TestConcurrencyDefaultsToUnlimited(t *testing.T) {
	c := NewConfig()
	c.Hosts["default"] = &AuthConfig{Username: "u", Password: "p", Scheme: "http"}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if c.Concurrency != 0 {
		t.Fatalf("Concurrency = %d, want 0 (unlimited default)", c.Concurrency)
	}
}

func TestTargetHosts(t *testing.T) {
	c := NewConfig()
	c.DefaultTarget = "10.0.0.11"
	c.Hosts["10.0.0.11"] = &AuthConfig{Username: "u", Password: "p", Scheme: "https"}
	c.Hosts["10.0.0.10"] = &AuthConfig{Username: "u", Password: "p", Scheme: "http"}
	// "default" is a credential fallback, not a BMC — it must not be reported.
	c.Hosts["default"] = &AuthConfig{Username: "u", Password: "p", Scheme: "https"}

	got := c.TargetHosts()

	want := []HostHealth{
		{Host: "10.0.0.10", Scheme: "http", Default: false},
		{Host: "10.0.0.11", Scheme: "https", Default: true},
	}
	if len(got) != len(want) {
		t.Fatalf("TargetHosts() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("TargetHosts()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestTargetHostsEmptyWhenOnlyDefaultCredential(t *testing.T) {
	c := NewConfig()
	c.Hosts["default"] = &AuthConfig{Username: "u", Password: "p", Scheme: "https"}

	if got := c.TargetHosts(); len(got) != 0 {
		t.Fatalf("TargetHosts() = %+v, want empty", got)
	}
}
