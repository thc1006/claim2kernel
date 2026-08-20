package contract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func testProfile() KernelProfile {
	min1, max128 := 1.0, 128.0
	min4, max64 := 4.0, 64.0
	return KernelProfile{
		APIVersion: APIVersion, Kind: ProfileKind,
		Metadata: ObjectMeta{Name: "demo-profile", Version: "0.1.0", CreatedAt: "2026-08-20T00:00:00Z", ExpiresAt: "2027-01-01T00:00:00Z"},
		Spec: ProfileSpec{
			Artifact: ArtifactSpec{Path: "artifacts/demo", Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 100, MaxBytes: 1000, Protocol: "stdin-json-v1"},
			Target:   TargetSpec{Backend: "cpu", Vendor: "generic", Architecture: "x86_64", DeviceClass: "example-device"},
			InputDomain: InputDomain{Features: map[string]FeatureSpec{
				"batch": {Kind: "integer", Required: true, Minimum: &min1, Maximum: &max128, OODFeature: true},
				"ues":   {Kind: "integer", Required: true, Minimum: &min4, Maximum: &max64, OODFeature: true},
			}},
			Precision:        PrecisionSpec{Storage: "fp32", Accumulation: "fp32"},
			Resources:        ResourceSpec{DeviceCount: 1, MinMemoryBytes: 0},
			Numerical:        NumericalCertificate{Metric: "relative_l2", UpperBound: 0.001, ObservedMax: 0.0005, TestSampleCount: 100},
			Latency:          LatencyCertificate{Method: "split-conformal", Quantile: 0.95, Confidence: 0.95, ResidualUpperUS: 20, IOBudgetUS: 5, RuntimeJitterUS: 5, CalibrationSampleCount: 200, TestSampleCount: 100, ObservedCoverage: 0.97, Model: LinearModel{InterceptUS: 10, Coefficients: map[string]float64{"input.batch": 1, "input.ues": 2}, FeatureOrder: []string{"input.batch", "input.ues"}, RidgeLambda: 0.001}, CalibratedAt: "2026-08-20T00:00:00Z", MaxAgeSeconds: 365 * 24 * 3600},
			Interference:     InterferenceEnvelope{Metrics: map[string]Range{"cpu_pressure": {Minimum: 0, Maximum: 0.5}}},
			Versions:         map[string]string{"mojo": ">=1.0.0b2,<1.1.0", "kubernetes": ">=1.36.0,<1.37.0"},
			DeviceAssertions: []DeviceAssertion{{Attribute: "claim2kernel.dev/architecture", Op: "eq", Value: StringValue("x86_64")}},
			OOD:              OODCertificate{Method: "mahalanobis-conformal", Required: true, Features: []string{"input.batch", "input.ues"}, Mean: []float64{32, 16}, InverseCovariance: [][]float64{{0.01, 0}, {0, 0.02}}, Threshold: 20, Coverage: 0.95, CalibrationSampleCount: 200, ObservedTestInlierRate: 0.95, Regularization: 1e-6},
			Policy:           PolicySpec{FailClosed: true},
			Provenance:       Provenance{Compiler: "go1.23.2", SourceDigest: "sha256:" + strings.Repeat("b", 64), DatasetDigest: "sha256:" + strings.Repeat("c", 64)},
		},
	}
}

func TestSealAndVerify(t *testing.T) {
	p := testProfile()
	if err := SealProfile(&p, time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProfile(&p, true); err != nil {
		t.Fatal(err)
	}
	original := p.Seal.ContractDigest
	p.Spec.Latency.IOBudgetUS++
	if err := VerifySeal(&p); err == nil {
		t.Fatal("expected seal mismatch")
	}
	if p.Seal.ContractDigest != original {
		t.Fatal("seal unexpectedly mutated")
	}
}

func TestStrictProfileDuplicateKey(t *testing.T) {
	p := testProfile()
	b, _ := json.Marshal(p)
	bad := strings.Replace(string(b), `"kind":"KernelProfile"`, `"kind":"KernelProfile","kind":"KernelProfile"`, 1)
	if _, err := LoadProfile([]byte(bad), false); err == nil {
		t.Fatal("expected duplicate-key rejection")
	}
}

func TestSignAndVerify(t *testing.T) {
	p := testProfile()
	now := time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)
	if err := SealProfile(&p, now); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	env, err := SignProfile(&p, priv, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyProfileSignature(&p, env, pub, now.Add(2*time.Second), 30*time.Second); err != nil {
		t.Fatal(err)
	}
	p.Spec.Artifact.Digest = "sha256:" + strings.Repeat("d", 64)
	if err := VerifyProfileSignature(&p, env, pub, now.Add(2*time.Second), 30*time.Second); err == nil {
		t.Fatal("expected tamper rejection")
	}
}

func TestPEMRoundTrip(t *testing.T) {
	pub, priv, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privPEM, _ := MarshalPrivateKeyPEM(priv)
	pubPEM, _ := MarshalPublicKeyPEM(pub)
	priv2, err := ParsePrivateKeyPEM(privPEM)
	if err != nil {
		t.Fatal(err)
	}
	pub2, err := ParsePublicKeyPEM(pubPEM)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(priv, priv2) {
		t.Fatal("private key mismatch")
	}
	if KeyID(pub) != KeyID(pub2) {
		t.Fatal("public key mismatch")
	}
}

func TestProfileRejectsTraversalArtifactPath(t *testing.T) {
	p := testProfile()
	p.Spec.Artifact.Path = "../escape"
	if err := ValidateProfile(&p, false); err == nil || !strings.Contains(err.Error(), "artifact.path") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
}

func TestProfileRejectsNonNumericRelation(t *testing.T) {
	p := testProfile()
	p.Spec.InputDomain.Features["mode"] = FeatureSpec{Kind: "category", Required: true, Categories: []string{"a", "b"}}
	p.Spec.InputDomain.Relations = []Relation{{Left: "mode", Op: "=", Right: "batch"}}
	if err := ValidateProfile(&p, false); err == nil || !strings.Contains(err.Error(), "must be numeric") {
		t.Fatalf("expected non-numeric relation rejection, got %v", err)
	}
}

func TestProfileRejectsUnboundedOODDimension(t *testing.T) {
	p := testProfile()
	p.Spec.OOD.Features = make([]string, MaxOODFeatures+1)
	p.Spec.OOD.Mean = make([]float64, MaxOODFeatures+1)
	p.Spec.OOD.InverseCovariance = make([][]float64, MaxOODFeatures+1)
	for i := range p.Spec.OOD.Features {
		p.Spec.OOD.Features[i] = "input.batch"
		p.Spec.OOD.InverseCovariance[i] = make([]float64, MaxOODFeatures+1)
		p.Spec.OOD.InverseCovariance[i][i] = 1
	}
	if err := ValidateProfile(&p, false); err == nil || !strings.Contains(err.Error(), "OOD features exceeds") {
		t.Fatalf("expected OOD dimension safety rejection, got %v", err)
	}
}

func TestProfileRequiresCoherentTimestamps(t *testing.T) {
	p := testProfile()
	p.Metadata.CreatedAt = "2027-02-01T00:00:00Z"
	if err := ValidateProfile(&p, false); err == nil || !strings.Contains(err.Error(), "createdAt must be before") {
		t.Fatalf("expected timestamp coherence rejection, got %v", err)
	}
}

func TestProfileRejectsReservedFallbackProfiles(t *testing.T) {
	p := testProfile()
	p.Spec.Policy.FallbackProfiles = []string{"alternate"}
	if err := ValidateProfile(&p, false); err == nil || !strings.Contains(err.Error(), "reserved in v1alpha1") {
		t.Fatalf("expected reserved fallback rejection, got %v", err)
	}
}

func TestValueRejectsInexactIntegerLiteral(t *testing.T) {
	var v Value
	if err := json.Unmarshal([]byte("9007199254740992"), &v); err == nil || !strings.Contains(err.Error(), "exact v1alpha1 range") {
		t.Fatalf("expected exact-integer rejection, got %v", err)
	}
}

func TestSealRejectsTimestampOutsideValidity(t *testing.T) {
	p := testProfile()
	if err := SealProfile(&p, time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired seal rejection, got %v", err)
	}
}

func TestCertificateRejectsFutureCreation(t *testing.T) {
	p := testProfile()
	p.Metadata.CreatedAt = "2026-09-01T00:00:00Z"
	p.Metadata.ExpiresAt = "2027-01-01T00:00:00Z"
	p.Spec.Latency.CalibratedAt = "2026-09-01T00:00:00Z"
	if err := SealProfile(&p, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := CertificateFresh(&p, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)); err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("expected future profile rejection, got %v", err)
	}
}
