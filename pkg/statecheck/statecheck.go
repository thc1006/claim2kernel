// Package statecheck checks durable Claim2Kernel lifecycle invariants over an
// event trace. It is intentionally independent of Kubernetes so traces from
// controllers, schedulers, and launchers can be normalized into one model.
package statecheck

import (
	"fmt"
	"math"
	"strings"

	"github.com/thc1006/claim2kernel/pkg/jsonsafe"
)

type Trace struct {
	Events []Event `json:"events"`
}
type Event struct {
	Type                string  `json:"type"`
	Profile             string  `json:"profile,omitempty"`
	Request             string  `json:"request,omitempty"`
	ContractDigest      string  `json:"contractDigest,omitempty"`
	ArtifactDigest      string  `json:"artifactDigest,omitempty"`
	DeviceFingerprint   string  `json:"deviceFingerprint,omitempty"`
	Precision           string  `json:"precision,omitempty"`
	NumericalUpperBound float64 `json:"numericalUpperBound,omitempty"`
	NumericalBudget     float64 `json:"numericalBudget,omitempty"`
}
type Violation struct {
	Index     int    `json:"index"`
	Invariant string `json:"invariant"`
	Message   string `json:"message"`
}
type Result struct {
	Valid      bool        `json:"valid"`
	Violations []Violation `json:"violations,omitempty"`
}

type profileState struct {
	contract, artifact, device, precision string
	numerical                             float64
	active                                bool
}
type admission struct {
	profile, contract, artifact, device, precision string
	numericalBudget                                float64
}

func DecodeTrace(data []byte) (*Trace, error) {
	var t Trace
	if err := jsonsafe.DecodeStrict(data, &t, 4<<20); err != nil {
		return nil, err
	}
	if len(t.Events) == 0 {
		return nil, fmt.Errorf("trace events must not be empty")
	}
	if len(t.Events) > 100000 {
		return nil, fmt.Errorf("trace exceeds 100000-event safety limit")
	}
	return &t, nil
}
func Check(t *Trace) Result {
	res := Result{Valid: true}
	profiles := map[string]profileState{}
	admissions := map[string]admission{}
	tombstones := map[string]struct{}{}
	violate := func(i int, inv, msg string) {
		res.Valid = false
		res.Violations = append(res.Violations, Violation{Index: i, Invariant: inv, Message: msg})
	}
	if t == nil {
		violate(-1, "TraceWellFormed", "trace is nil")
		return res
	}
	for i, e := range t.Events {
		switch e.Type {
		case "publish":
			if e.Profile == "" || e.ContractDigest == "" || e.ArtifactDigest == "" || e.DeviceFingerprint == "" || e.Precision == "" {
				violate(i, "TraceWellFormed", "publish event is missing required fields")
				continue
			}
			if !finiteNonNegative(e.NumericalUpperBound) {
				violate(i, "TraceWellFormed", "publish numerical upper bound must be finite and non-negative")
				continue
			}
			if existing, exists := profiles[e.Profile]; exists && existing.active {
				violate(i, "NoStaleProfileReuse", "an active profile was republished without revocation")
				continue
			}
			if _, revoked := tombstones[e.ContractDigest]; revoked {
				violate(i, "NoPerformanceResurrection", "a revoked contract digest was republished without a new certificate")
			}
			profiles[e.Profile] = profileState{contract: e.ContractDigest, artifact: e.ArtifactDigest, device: e.DeviceFingerprint, precision: e.Precision, numerical: e.NumericalUpperBound, active: true}
		case "revoke":
			p, ok := profiles[e.Profile]
			if !ok {
				violate(i, "TraceWellFormed", "revoke references unknown profile")
				continue
			}
			p.active = false
			profiles[e.Profile] = p
			tombstones[p.contract] = struct{}{}
		case "device-update":
			p, ok := profiles[e.Profile]
			if !ok {
				violate(i, "TraceWellFormed", "device-update references unknown profile")
				continue
			}
			if e.DeviceFingerprint == "" {
				violate(i, "TraceWellFormed", "device-update has empty fingerprint")
				continue
			}
			p.device = e.DeviceFingerprint
			profiles[e.Profile] = p
		case "admit":
			p, ok := profiles[e.Profile]
			if !ok || !p.active {
				violate(i, "NoUncertifiedDispatch", "admission references an inactive or unknown profile")
				continue
			}
			if e.Request == "" {
				violate(i, "TraceWellFormed", "admit has empty request")
				continue
			}
			if !finiteNonNegative(e.NumericalBudget) {
				violate(i, "TraceWellFormed", "admit numerical budget must be finite and non-negative")
				continue
			}
			if _, exists := admissions[e.Request]; exists {
				violate(i, "TraceWellFormed", "request was admitted more than once without completion")
				continue
			}
			admissions[e.Request] = admission{profile: e.Profile, contract: p.contract, artifact: p.artifact, device: p.device, precision: p.precision, numericalBudget: e.NumericalBudget}
		case "dispatch":
			a, ok := admissions[e.Request]
			if !ok {
				violate(i, "NoUncertifiedDispatch", "dispatch has no prior admission")
				continue
			}
			if !finiteNonNegative(e.NumericalUpperBound) {
				violate(i, "TraceWellFormed", "dispatch numerical upper bound must be finite and non-negative")
				continue
			}
			p, ok := profiles[a.profile]
			if !ok || !p.active {
				violate(i, "NoStaleProfileReuse", "dispatch uses an inactive or missing profile")
				continue
			}
			if p.contract != a.contract || e.ContractDigest != a.contract || p.artifact != a.artifact || e.ArtifactDigest != a.artifact {
				violate(i, "NoStaleProfileReuse", "dispatch identity differs from the admitted contract/artifact")
			}
			if p.device != a.device || e.DeviceFingerprint != a.device {
				violate(i, "AllocationNonReinterpretation", "allocated device semantics changed after admission")
			}
			if p.precision != a.precision || e.Precision != a.precision {
				violate(i, "NoNumericalDowngrade", "dispatch precision differs from admitted precision")
			}
			if p.numerical > a.numericalBudget || e.NumericalUpperBound > a.numericalBudget {
				violate(i, "NoNumericalDowngrade", "numerical upper bound exceeds the admitted budget")
			}
		case "complete":
			if _, ok := admissions[e.Request]; !ok {
				violate(i, "TraceWellFormed", "complete references unknown request")
			} else {
				delete(admissions, e.Request)
			}
		default:
			violate(i, "TraceWellFormed", fmt.Sprintf("unknown event type %q", e.Type))
		}
	}
	return res
}

func finiteNonNegative(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}
func FormatViolations(vs []Violation) string {
	parts := make([]string, 0, len(vs))
	for _, v := range vs {
		parts = append(parts, fmt.Sprintf("event[%d] %s: %s", v.Index, v.Invariant, v.Message))
	}
	return strings.Join(parts, "; ")
}
