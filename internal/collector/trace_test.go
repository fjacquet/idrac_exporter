package collector

import (
	"bytes"
	"testing"

	"github.com/fjacquet/idrac_exporter/internal/config"
	"github.com/fjacquet/idrac_exporter/internal/log"
)

func TestTraceNeverLeaksToken(t *testing.T) {
	testConfig(t, func(c *config.CollectConfig) {})
	srv := mockRedfish(t, map[string]string{"/redfish/v1/Systems/1": "system.json"})
	defer srv.Close()

	// Info level (not Debug) so the pre-existing debug "Querying" line is
	// suppressed and only the --trace line (logged at Info) can satisfy the
	// path assertion. This isolates the trace feature: the test fails if the
	// trace logging is removed.
	var buf bytes.Buffer
	// Restore the package default logger on exit so the buffer-backed logger
	// does not leak into other tests in this package.
	defer func() { log.SetDefaultLogger(log.NewLogger(log.LevelInfo, true)) }()
	log.SetDefaultLogger(log.NewLoggerWithOutput(log.LevelInfo, &buf))
	config.Trace = true
	defer func() { config.Trace = false }()

	c := testClient(srv)
	c.redfish.session.token = "SUPERSECRET-TOKEN"

	var out struct{ Id string }
	c.redfish.Get("/redfish/v1/Systems/1", &out)

	if !bytes.Contains(buf.Bytes(), []byte("trace: GET /redfish/v1/Systems/1")) {
		t.Fatalf("trace did not emit the GET line: %q", buf.String())
	}
	if bytes.Contains(buf.Bytes(), []byte("SUPERSECRET-TOKEN")) {
		t.Fatalf("trace leaked the auth token: %q", buf.String())
	}
}
