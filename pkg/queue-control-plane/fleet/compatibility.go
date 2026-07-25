package fleet

import "sort"

// ProtocolVersion identifies a worker/control-plane management protocol.
type ProtocolVersion struct {
	Major uint16 `json:"major"`
	Minor uint16 `json:"minor"`
}

// ProtocolRange is the inclusive protocol range supported by the control
// plane during rolling upgrades.
type ProtocolRange struct {
	Minimum ProtocolVersion
	Maximum ProtocolVersion
}

// Capability identifies one negotiated data-plane management operation.
type Capability string

const (
	CapabilityWorkerStatus   Capability = "worker_status"
	CapabilityQueueStatus    Capability = "queue_status"
	CapabilityPause          Capability = "pause"
	CapabilityResume         Capability = "resume"
	CapabilityDrain          Capability = "drain"
	CapabilityTerminate      Capability = "terminate"
	CapabilityFailures       Capability = "failures"
	CapabilityDeadLetters    Capability = "dead_letters"
	CapabilityRetry          Capability = "retry"
	CapabilityBulkRetry      Capability = "bulk_retry"
	CapabilityDelete         Capability = "delete"
	CapabilityPurge          Capability = "purge"
	CapabilityReplay         Capability = "replay"
	CapabilityRetentionCount Capability = "retention_count"
	CapabilityRetentionTime  Capability = "retention_time"
	CapabilityRetentionBytes Capability = "retention_bytes"
)

// CompatibilityState describes a worker relative to the supported protocol
// range without pretending an incompatible worker is controllable.
type CompatibilityState string

const (
	CompatibilityCompatible  CompatibilityState = "compatible"
	CompatibilityWorkerOlder CompatibilityState = "worker_older"
	CompatibilityWorkerNewer CompatibilityState = "worker_newer"
	CompatibilityUnknown     CompatibilityState = "unknown"
)

// Compatibility is the deterministic result of protocol and capability
// negotiation.
type Compatibility struct {
	State            CompatibilityState `json:"state"`
	Enabled          []Capability       `json:"enabled"`
	WorkerOnly       []Capability       `json:"worker_only"`
	ControlPlaneOnly []Capability       `json:"control_plane_only"`
}

// Negotiate safely compares a worker report with the control-plane range and
// enables only capabilities both sides support at a compatible version.
func Negotiate(
	supported ProtocolRange,
	worker ProtocolVersion,
	workerCapabilities []Capability,
	controlPlaneCapabilities []Capability,
) Compatibility {
	state := classifyCompatibility(supported, worker)
	workerSet := capabilitySet(workerCapabilities)
	controlPlaneSet := capabilitySet(controlPlaneCapabilities)

	result := Compatibility{State: state}
	for capability := range workerSet {
		if _, supportedByControlPlane := controlPlaneSet[capability]; supportedByControlPlane {
			if state == CompatibilityCompatible {
				result.Enabled = append(result.Enabled, capability)
			}

			continue
		}

		result.WorkerOnly = append(result.WorkerOnly, capability)
	}
	for capability := range controlPlaneSet {
		if _, supportedByWorker := workerSet[capability]; !supportedByWorker {
			result.ControlPlaneOnly = append(result.ControlPlaneOnly, capability)
		}
	}

	sortCapabilities(result.Enabled)
	sortCapabilities(result.WorkerOnly)
	sortCapabilities(result.ControlPlaneOnly)

	return result
}

func classifyCompatibility(supported ProtocolRange, worker ProtocolVersion) CompatibilityState {
	if compareVersion(supported.Minimum, supported.Maximum) > 0 || worker == (ProtocolVersion{}) {
		return CompatibilityUnknown
	}
	if compareVersion(worker, supported.Minimum) < 0 {
		return CompatibilityWorkerOlder
	}
	if compareVersion(worker, supported.Maximum) > 0 {
		return CompatibilityWorkerNewer
	}

	return CompatibilityCompatible
}

func compareVersion(left, right ProtocolVersion) int {
	if left.Major < right.Major {
		return -1
	}
	if left.Major > right.Major {
		return 1
	}
	if left.Minor < right.Minor {
		return -1
	}
	if left.Minor > right.Minor {
		return 1
	}

	return 0
}

func capabilitySet(capabilities []Capability) map[Capability]struct{} {
	set := make(map[Capability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		set[capability] = struct{}{}
	}

	return set
}

func sortCapabilities(capabilities []Capability) {
	sort.Slice(capabilities, func(i, j int) bool {
		return capabilities[i] < capabilities[j]
	})
}
