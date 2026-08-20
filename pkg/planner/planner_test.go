package planner

import (
	"strings"
	"testing"
	"time"

	"github.com/thc1006/claim2kernel/pkg/contract"
	"github.com/thc1006/claim2kernel/pkg/dra"
)

func profile(t *testing.T) contract.KernelProfile {
	t.Helper()
	min1, max128 := 1.0, 128.0
	min4, max64 := 4.0, 64.0
	p := contract.KernelProfile{APIVersion: contract.APIVersion, Kind: contract.ProfileKind, Metadata: contract.ObjectMeta{Name: "demo-profile", Version: "0.1.0", CreatedAt: "2026-08-20T00:00:00Z", ExpiresAt: "2027-01-01T00:00:00Z"}, Spec: contract.ProfileSpec{
		Artifact:    contract.ArtifactSpec{Path: "artifacts/demo", Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 100, MaxBytes: 1000, Protocol: "stdin-json-v1"},
		Target:      contract.TargetSpec{Backend: "cuda", Vendor: "nvidia", Architecture: "sm_90", DeviceClass: "gpu.example.com"},
		InputDomain: contract.InputDomain{Features: map[string]contract.FeatureSpec{"batch": {Kind: "integer", Required: true, Minimum: &min1, Maximum: &max128, OODFeature: true}, "ues": {Kind: "integer", Required: true, Minimum: &min4, Maximum: &max64, OODFeature: true}}},
		Precision:   contract.PrecisionSpec{Storage: "fp32", Accumulation: "fp32"}, Resources: contract.ResourceSpec{DeviceCount: 1, MinMemoryBytes: 1024},
		Numerical:    contract.NumericalCertificate{Metric: "relative_l2", UpperBound: 0.001, ObservedMax: 0.0005, TestSampleCount: 100},
		Latency:      contract.LatencyCertificate{Method: "split-conformal", Quantile: .95, Confidence: .95, ResidualUpperUS: 20, IOBudgetUS: 5, RuntimeJitterUS: 5, CalibrationSampleCount: 200, TestSampleCount: 100, ObservedCoverage: .97, Model: contract.LinearModel{InterceptUS: 10, Coefficients: map[string]float64{"input.batch": 1, "input.ues": 2, "interference.cpu_pressure": 100}, FeatureOrder: []string{"input.batch", "input.ues", "interference.cpu_pressure"}, RidgeLambda: .001}, CalibratedAt: "2026-08-20T00:00:00Z", MaxAgeSeconds: 365 * 24 * 3600},
		Interference: contract.InterferenceEnvelope{Metrics: map[string]contract.Range{"cpu_pressure": {Minimum: 0, Maximum: .5}}}, Versions: map[string]string{"mojo": ">=1.0.0b2,<1.1.0", "kubernetes": ">=1.36.0,<1.37.0"},
		DeviceAssertions: []contract.DeviceAssertion{{Attribute: "claim2kernel.dev/architecture", Op: "eq", Value: contract.StringValue("sm_90")}, {Attribute: "claim2kernel.dev/healthy", Op: "eq", Value: contract.BoolValue(true)}},
		OOD:              contract.OODCertificate{Method: "mahalanobis-conformal", Required: true, Features: []string{"input.batch", "input.ues", "interference.cpu_pressure"}, Mean: []float64{32, 16, .1}, InverseCovariance: [][]float64{{.01, 0, 0}, {0, .02, 0}, {0, 0, 10}}, Threshold: 20, Coverage: .95, CalibrationSampleCount: 200, ObservedTestInlierRate: .96, Regularization: 1e-6}, Policy: contract.PolicySpec{FailClosed: true}, Provenance: contract.Provenance{Compiler: "mojo 1.0.0b2", SourceDigest: "sha256:" + strings.Repeat("b", 64), DatasetDigest: "sha256:" + strings.Repeat("c", 64)}}}
	if err := contract.SealProfile(&p, time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	return p
}
func request() contract.KernelRequest {
	return contract.KernelRequest{APIVersion: contract.APIVersion, Kind: contract.RequestKind, Metadata: contract.ObjectMeta{Name: "job-1"}, Spec: contract.RequestSpec{DeadlineUS: 200, SafetyMarginUS: 10, MaxNumericalError: .002, Inputs: map[string]contract.Value{"batch": contract.NumberValue(32), "ues": contract.NumberValue(16)}, Interference: map[string]float64{"cpu_pressure": .1}, Versions: map[string]string{"mojo": "1.0.0b2", "kubernetes": "1.36.3"}}}
}
func metadata(t *testing.T, arch string) *dra.DeviceMetadata {
	t.Helper()
	data := `{"apiVersion":"metadata.resource.k8s.io/v1alpha1","kind":"DeviceMetadata","metadata":{"name":"claim","generation":1},"requests":[{"name":"gpu","devices":[{"driver":"gpu.example.com","pool":"node0","name":"gpu0","attributes":{"claim2kernel.dev/architecture":{"string":"` + arch + `"},"claim2kernel.dev/healthy":{"bool":true}}}]}]}`
	m, err := dra.DecodeMetadataStream([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPlanAdmits(t *testing.T) {
	p := profile(t)
	r := request()
	d := Evaluate(&p, &r, PlanPhase, RuntimeEvidence{Now: time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)})
	if !d.Admissible {
		t.Fatalf("rejected: %+v", d.Reasons)
	}
	if d.LatencyMarginUS <= 0 {
		t.Fatal("expected positive margin")
	}
}
func TestOODRejects(t *testing.T) {
	p := profile(t)
	r := request()
	r.Spec.Inputs["batch"] = contract.NumberValue(128)
	r.Spec.Inputs["ues"] = contract.NumberValue(64)
	d := Evaluate(&p, &r, PlanPhase, RuntimeEvidence{Now: time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)})
	if d.Admissible || !hasCode(d, "OOD_REJECTED") {
		t.Fatalf("expected OOD rejection: %+v", d)
	}
}

func TestOODFeatureNeedNotHaveLatencyCoefficient(t *testing.T) {
	p := profile(t)
	delete(p.Spec.Latency.Model.Coefficients, "interference.cpu_pressure")
	p.Spec.Latency.Model.FeatureOrder = []string{"input.batch", "input.ues"}
	p.Seal = nil
	if err := contract.SealProfile(&p, time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	r := request()
	d := Evaluate(&p, &r, PlanPhase, RuntimeEvidence{Now: time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)})
	if !d.Admissible {
		t.Fatalf("OOD-only feature unexpectedly unavailable: %+v", d.Reasons)
	}
}
func TestUnknownInputRejects(t *testing.T) {
	p := profile(t)
	r := request()
	r.Spec.Inputs["surprise"] = contract.NumberValue(1)
	d := Evaluate(&p, &r, PlanPhase, RuntimeEvidence{Now: time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)})
	if d.Admissible || !hasCode(d, "UNKNOWN_INPUT") {
		t.Fatalf("expected unknown-input rejection: %+v", d)
	}
}
func TestRuntimeDRA(t *testing.T) {
	p := profile(t)
	r := request()
	good := Evaluate(&p, &r, RuntimePhase, RuntimeEvidence{Now: time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC), Metadata: metadata(t, "sm_90"), DRARequestName: "gpu"})
	if !good.Admissible {
		t.Fatalf("good metadata rejected: %+v", good.Reasons)
	}
	bad := Evaluate(&p, &r, RuntimePhase, RuntimeEvidence{Now: time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC), Metadata: metadata(t, "gfx942"), DRARequestName: "gpu"})
	if bad.Admissible || !hasCode(bad, "DRA_ATTRIBUTE_MISMATCH") {
		t.Fatalf("bad metadata admitted: %+v", bad)
	}
}
func TestVersionRejects(t *testing.T) {
	p := profile(t)
	r := request()
	r.Spec.Versions["mojo"] = "1.0.0b1"
	d := Evaluate(&p, &r, PlanPhase, RuntimeEvidence{Now: time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)})
	if d.Admissible || !hasCode(d, "UNSUPPORTED_VERSION") {
		t.Fatalf("expected version rejection: %+v", d)
	}
}
func hasCode(d Decision, code string) bool {
	for _, r := range d.Reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}
