package collector

// Tests for ADR 0008 "absent, never zero": an absent/unparseable BMC field must
// yield NO sample, not a misleading 0.  Each test below pairs a zero-value call
// (must emit 0 samples) with a non-zero call (must emit exactly 1 sample).
//
// Tests must NOT run in parallel: config.Config is a singleton.

import (
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// funcCollector adapts a closure into a prometheus.Collector for counting emits.
type funcCollector func(ch chan<- prometheus.Metric)

func (f funcCollector) Describe(chan<- *prometheus.Desc)    {}
func (f funcCollector) Collect(ch chan<- prometheus.Metric) { f(ch) }

// ── Power supply ──────────────────────────────────────────────────────────────

func TestAbsentNotZeroPowerSupplyInputWatts(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.Power = true })
	mc := NewCollector()
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerSupplyInputWatts(ch, 0, "ps-0")
	})); n != 0 {
		t.Fatalf("0-value emitted %d samples, want 0", n)
	}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerSupplyInputWatts(ch, 250.0, "ps-0")
	})); n != 1 {
		t.Fatalf("nonzero emitted %d samples, want 1", n)
	}
}

func TestAbsentNotZeroPowerSupplyInputVoltage(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.Power = true })
	mc := NewCollector()
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerSupplyInputVoltage(ch, 0, "ps-0")
	})); n != 0 {
		t.Fatalf("0-value emitted %d samples, want 0", n)
	}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerSupplyInputVoltage(ch, 220.0, "ps-0")
	})); n != 1 {
		t.Fatalf("nonzero emitted %d samples, want 1", n)
	}
}

func TestAbsentNotZeroPowerSupplyOutputWatts(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.Power = true })
	mc := NewCollector()
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerSupplyOutputWatts(ch, 0, "ps-0")
	})); n != 0 {
		t.Fatalf("0-value emitted %d samples, want 0", n)
	}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerSupplyOutputWatts(ch, 200.0, "ps-0")
	})); n != 1 {
		t.Fatalf("nonzero emitted %d samples, want 1", n)
	}
}

func TestAbsentNotZeroPowerSupplyCapacityWatts(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.Power = true })
	mc := NewCollector()
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerSupplyCapacityWatts(ch, 0, "ps-0")
	})); n != 0 {
		t.Fatalf("0-value emitted %d samples, want 0", n)
	}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerSupplyCapacityWatts(ch, 750.0, "ps-0")
	})); n != 1 {
		t.Fatalf("nonzero emitted %d samples, want 1", n)
	}
}

// ── Power control ─────────────────────────────────────────────────────────────

func TestAbsentNotZeroPowerControlConsumedWatts(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.Power = true })
	mc := NewCollector()
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerControlConsumedWatts(ch, 0, "pc-0", "System Power Control")
	})); n != 0 {
		t.Fatalf("0-value emitted %d samples, want 0", n)
	}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerControlConsumedWatts(ch, 150.0, "pc-0", "System Power Control")
	})); n != 1 {
		t.Fatalf("nonzero emitted %d samples, want 1", n)
	}
}

func TestAbsentNotZeroPowerControlCapacityWatts(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.Power = true })
	mc := NewCollector()
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerControlCapacityWatts(ch, 0, "pc-0", "System Power Control")
	})); n != 0 {
		t.Fatalf("0-value emitted %d samples, want 0", n)
	}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerControlCapacityWatts(ch, 1000.0, "pc-0", "System Power Control")
	})); n != 1 {
		t.Fatalf("nonzero emitted %d samples, want 1", n)
	}
}

func TestAbsentNotZeroPowerControlMinConsumedWatts(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.Power = true })
	mc := NewCollector()
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerControlMinConsumedWatts(ch, 0, "pc-0", "System Power Control")
	})); n != 0 {
		t.Fatalf("0-value emitted %d samples, want 0", n)
	}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerControlMinConsumedWatts(ch, 80.0, "pc-0", "System Power Control")
	})); n != 1 {
		t.Fatalf("nonzero emitted %d samples, want 1", n)
	}
}

func TestAbsentNotZeroPowerControlMaxConsumedWatts(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.Power = true })
	mc := NewCollector()
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerControlMaxConsumedWatts(ch, 0, "pc-0", "System Power Control")
	})); n != 0 {
		t.Fatalf("0-value emitted %d samples, want 0", n)
	}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerControlMaxConsumedWatts(ch, 300.0, "pc-0", "System Power Control")
	})); n != 1 {
		t.Fatalf("nonzero emitted %d samples, want 1", n)
	}
}

func TestAbsentNotZeroPowerControlAvgConsumedWatts(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.Power = true })
	mc := NewCollector()
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerControlAvgConsumedWatts(ch, 0, "pc-0", "System Power Control")
	})); n != 0 {
		t.Fatalf("0-value emitted %d samples, want 0", n)
	}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerControlAvgConsumedWatts(ch, 175.0, "pc-0", "System Power Control")
	})); n != 1 {
		t.Fatalf("nonzero emitted %d samples, want 1", n)
	}
}

func TestAbsentNotZeroPowerControlInterval(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.Power = true })
	mc := NewCollector()
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerControlInterval(ch, 0, "pc-0", "System Power Control")
	})); n != 0 {
		t.Fatalf("0-value emitted %d samples, want 0", n)
	}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewPowerControlInterval(ch, 60.0, "pc-0", "System Power Control")
	})); n != 1 {
		t.Fatalf("nonzero emitted %d samples, want 1", n)
	}
}

// ── Storage capacity ──────────────────────────────────────────────────────────

func TestAbsentNotZeroStorageDriveCapacity(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.Storage = true })
	mc := NewCollector()
	// zero CapacityBytes → no sample
	driveZero := &StorageDrive{Id: "drive-0", CapacityBytes: 0}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewStorageDriveCapacity(ch, "RAID.Slot.1", driveZero)
	})); n != 0 {
		t.Fatalf("0-value emitted %d samples, want 0", n)
	}
	// non-zero CapacityBytes → 1 sample
	driveNonzero := &StorageDrive{Id: "drive-0", CapacityBytes: 1099511627776}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewStorageDriveCapacity(ch, "RAID.Slot.1", driveNonzero)
	})); n != 1 {
		t.Fatalf("nonzero emitted %d samples, want 1", n)
	}
}

func TestAbsentNotZeroStorageVolumeCapacity(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.Storage = true })
	mc := NewCollector()
	// zero CapacityBytes → no sample
	volZero := &StorageVolume{Id: "vol-0", CapacityBytes: 0}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewStorageVolumeCapacity(ch, "RAID.Slot.1", volZero)
	})); n != 0 {
		t.Fatalf("0-value emitted %d samples, want 0", n)
	}
	// non-zero CapacityBytes → 1 sample
	volNonzero := &StorageVolume{Id: "vol-0", CapacityBytes: 2199023255552}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewStorageVolumeCapacity(ch, "RAID.Slot.1", volNonzero)
	})); n != 1 {
		t.Fatalf("nonzero emitted %d samples, want 1", n)
	}
}

// ── Network current speed ─────────────────────────────────────────────────────

func TestAbsentNotZeroNetworkPortCurrentSpeed(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) { c.Network = true })
	mc := NewCollector()
	// all speed fields zero → no sample
	portZero := &NetworkPort{Id: "NIC.1.1"}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewNetworkPortCurrentSpeed(ch, "NIC.1", portZero)
	})); n != 0 {
		t.Fatalf("0-value emitted %d samples, want 0", n)
	}
	// CurrentLinkSpeedMbps non-zero → 1 sample
	portNonzero := &NetworkPort{Id: "NIC.1.1", CurrentLinkSpeedMbps: 10000}
	if n := testutil.CollectAndCount(funcCollector(func(ch chan<- prometheus.Metric) {
		mc.NewNetworkPortCurrentSpeed(ch, "NIC.1", portNonzero)
	})); n != 1 {
		t.Fatalf("nonzero emitted %d samples, want 1", n)
	}
}
