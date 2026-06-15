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
