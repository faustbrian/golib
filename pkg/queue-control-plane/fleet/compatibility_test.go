package fleet

import "testing"

func TestNegotiateClassifiesRollingProtocolVersions(t *testing.T) {
	t.Parallel()

	supported := ProtocolRange{
		Minimum: ProtocolVersion{Major: 1, Minor: 1},
		Maximum: ProtocolVersion{Major: 1, Minor: 3},
	}

	tests := map[string]struct {
		worker ProtocolVersion
		want   CompatibilityState
	}{
		"missing":     {worker: ProtocolVersion{}, want: CompatibilityUnknown},
		"older major": {worker: ProtocolVersion{Minor: 1}, want: CompatibilityWorkerOlder},
		"older minor": {worker: ProtocolVersion{Major: 1}, want: CompatibilityWorkerOlder},
		"minimum":     {worker: ProtocolVersion{Major: 1, Minor: 1}, want: CompatibilityCompatible},
		"maximum":     {worker: ProtocolVersion{Major: 1, Minor: 3}, want: CompatibilityCompatible},
		"newer":       {worker: ProtocolVersion{Major: 2}, want: CompatibilityWorkerNewer},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := Negotiate(supported, tt.worker, nil, nil)
			if got.State != tt.want {
				t.Fatalf("Negotiate().State = %q, want %q", got.State, tt.want)
			}
		})
	}
}

func TestNegotiateEnablesOnlySharedCapabilities(t *testing.T) {
	t.Parallel()

	got := Negotiate(
		ProtocolRange{
			Minimum: ProtocolVersion{Major: 1},
			Maximum: ProtocolVersion{Major: 1, Minor: 2},
		},
		ProtocolVersion{Major: 1, Minor: 1},
		[]Capability{CapabilityDrain, CapabilityPause, CapabilityPause, Capability("future")},
		[]Capability{CapabilityRetry, CapabilityPause, CapabilityPause},
	)

	assertCapabilities(t, "Enabled", got.Enabled, []Capability{CapabilityPause})
	assertCapabilities(
		t,
		"WorkerOnly",
		got.WorkerOnly,
		[]Capability{CapabilityDrain, Capability("future")},
	)
	assertCapabilities(t, "ControlPlaneOnly", got.ControlPlaneOnly, []Capability{CapabilityRetry})
}

func TestNegotiateDisablesCapabilitiesForIncompatibleVersions(t *testing.T) {
	t.Parallel()

	got := Negotiate(
		ProtocolRange{
			Minimum: ProtocolVersion{Major: 1},
			Maximum: ProtocolVersion{Major: 1, Minor: 2},
		},
		ProtocolVersion{Major: 2},
		[]Capability{CapabilityPause},
		[]Capability{CapabilityPause},
	)
	if len(got.Enabled) != 0 {
		t.Fatalf("Negotiate().Enabled = %v, want no enabled capabilities", got.Enabled)
	}
}

func TestNegotiateRejectsInvalidSupportedRange(t *testing.T) {
	t.Parallel()

	got := Negotiate(
		ProtocolRange{
			Minimum: ProtocolVersion{Major: 1, Minor: 2},
			Maximum: ProtocolVersion{Major: 1, Minor: 1},
		},
		ProtocolVersion{Major: 1},
		nil,
		nil,
	)
	if got.State != CompatibilityUnknown {
		t.Fatalf("Negotiate().State = %q, want %q", got.State, CompatibilityUnknown)
	}
}

func assertCapabilities(t *testing.T, name string, got, want []Capability) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
}
