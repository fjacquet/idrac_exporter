package config

import "testing"

func TestOTLPConfigDefaults(t *testing.T) {
	c := NewConfig()
	c.Hosts["default"] = &AuthConfig{Username: "u", Password: "p"}
	if err := c.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if c.OTLP.IdentityLabel != "system" {
		t.Errorf("identity_label default = %q, want system", c.OTLP.IdentityLabel)
	}
	if c.OTLP.Protocol != "grpc" {
		t.Errorf("protocol default = %q, want grpc", c.OTLP.Protocol)
	}
	if c.OTLP.Endpoint != "localhost:4317" {
		t.Errorf("endpoint default = %q, want localhost:4317", c.OTLP.Endpoint)
	}
	if c.OTLP.Insecure {
		t.Errorf("insecure default = true, want false (secure by default)")
	}
	if c.Collection.IntervalSeconds != 60 {
		t.Errorf("collection interval = %v, want 60", c.Collection.IntervalSeconds)
	}
	if c.OTLP.IntervalSeconds != 60 {
		t.Errorf("otlp interval default = %v, want 60 (= collection interval)", c.OTLP.IntervalSeconds)
	}
}

func TestOTLPConfigInvalidProtocol(t *testing.T) {
	c := NewConfig()
	c.Hosts["default"] = &AuthConfig{Username: "u", Password: "p"}
	c.OTLP.Protocol = "carrier-pigeon"
	if err := c.Validate(); err == nil {
		t.Fatalf("expected error for invalid protocol")
	}
}
