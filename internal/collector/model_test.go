package collector

import (
	"encoding/json"
	"testing"
)

func TestTemperatureSensorNumberId(t *testing.T) {
	// Spec property is "SensorNumber"; GetId must use it as the fallback when
	// MemberId is absent (the tag was json:"Number", which never parsed).
	var temp Temperature
	if err := json.Unmarshal([]byte(`{"SensorNumber":7,"ReadingCelsius":40}`), &temp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := temp.GetId(99); got != "7" {
		t.Fatalf("GetId = %q, want \"7\" (SensorNumber, not array fallback)", got)
	}
}
