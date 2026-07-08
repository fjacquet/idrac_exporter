package config

import "testing"

func TestHasTargetHosts(t *testing.T) {
	c := NewConfig()
	c.Hosts["default"] = &AuthConfig{Username: "u", Password: "p"}
	if c.HasTargetHosts() {
		t.Fatal("only 'default' configured, want false")
	}
	c.Hosts["10.0.0.1"] = &AuthConfig{Username: "u", Password: "p"}
	if !c.HasTargetHosts() {
		t.Fatal("a real host is configured, want true")
	}
}

func TestValidateAcceptsDeprecatedDefaultTarget(t *testing.T) {
	c := NewConfig()
	c.Hosts["default"] = &AuthConfig{Username: "u", Password: "p", Scheme: "http"}
	c.DefaultTarget = "192.168.1.1"
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate with default_target set returned error: %v", err)
	}
}
