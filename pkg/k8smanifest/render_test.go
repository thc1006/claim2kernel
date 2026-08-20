package k8smanifest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/thc1006/claim2kernel/pkg/contract"
)

func fixture(t *testing.T) (*contract.KernelProfile, *contract.KernelRequest, *contract.SignatureEnvelope, []byte) {
	t.Helper()
	min, max := 1.0, 2.0
	image := "example/c2k@sha256:" + strings.Repeat("d", 64)
	p := &contract.KernelProfile{APIVersion: contract.APIVersion, Kind: contract.ProfileKind, Metadata: contract.ObjectMeta{Name: "p", Version: "1", CreatedAt: "2026-08-20T00:00:00Z", ExpiresAt: "2027-01-01T00:00:00Z"}, Spec: contract.ProfileSpec{Artifact: contract.ArtifactSpec{Path: "artifacts/k", Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 1, MaxBytes: 2, Protocol: "stdin-json-v1", ContainerDigest: image}, Target: contract.TargetSpec{Backend: "cuda", Vendor: "nvidia", Architecture: "sm-90", DeviceClass: "gpu.example.com"}, InputDomain: contract.InputDomain{Features: map[string]contract.FeatureSpec{"x": {Kind: "number", Required: true, Minimum: &min, Maximum: &max}}}, Precision: contract.PrecisionSpec{Storage: "fp32", Accumulation: "fp32"}, Resources: contract.ResourceSpec{DeviceCount: 1}, Numerical: contract.NumericalCertificate{Metric: "e", UpperBound: .1, ObservedMax: .05, TestSampleCount: 1}, Latency: contract.LatencyCertificate{Method: "split-conformal", Quantile: .9, Confidence: .9, ResidualUpperUS: 1, CalibrationSampleCount: 10, TestSampleCount: 10, ObservedCoverage: 1, Model: contract.LinearModel{InterceptUS: 1, Coefficients: map[string]float64{"input.x": 1}, FeatureOrder: []string{"input.x"}, RidgeLambda: .1}, CalibratedAt: "2026-08-20T00:00:00Z", MaxAgeSeconds: 9999999}, Interference: contract.InterferenceEnvelope{Metrics: map[string]contract.Range{}}, Versions: map[string]string{"mojo": ">=1.0.0b2,<1.1.0"}, OOD: contract.OODCertificate{Required: false}, Policy: contract.PolicySpec{FailClosed: true}, Provenance: contract.Provenance{Compiler: "mojo", SourceDigest: "sha256:" + strings.Repeat("b", 64), DatasetDigest: "sha256:" + strings.Repeat("c", 64)}}}
	if err := contract.SealProfile(p, time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	r := &contract.KernelRequest{APIVersion: contract.APIVersion, Kind: contract.RequestKind, Metadata: contract.ObjectMeta{Name: "r"}, Spec: contract.RequestSpec{DeadlineUS: 20, SafetyMarginUS: 1, MaxNumericalError: .2, Inputs: map[string]contract.Value{"x": contract.NumberValue(1)}, Interference: map[string]float64{}, Versions: map[string]string{"mojo": "1.0.0b2"}}}
	pub, priv, _ := contract.GenerateKey()
	sig, err := contract.SignProfile(p, priv, time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	pem, _ := contract.MarshalPublicKeyPEM(pub)
	return p, r, sig, pem
}
func TestRender(t *testing.T) {
	p, r, sig, pem := fixture(t)
	b, err := Render(Options{Namespace: "default", JobName: "job", Image: p.Spec.Artifact.ContainerDigest, QueueName: "research", DRADriverName: "gpu.example.com", Profile: p, Request: r, Signature: sig, PublicKeyPEM: pem, Now: time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 3 {
		t.Fatalf("items=%d", len(list.Items))
	}
	s := string(b)
	for _, needle := range []string{"resource.k8s.io/v1", "ExactCount", "kueue.x-k8s.io/queue-name", "dra-device-attributes/resourceclaimtemplates/accelerator/gpu/gpu.example.com-metadata.json", "--require-signature", "/etc/claim2kernel-trust/public.pem"} {
		if !strings.Contains(s, needle) {
			t.Fatalf("missing %s", needle)
		}
	}
	if strings.Contains(s, "public.pem\": ") || strings.Contains(s, "PUBLIC KEY") {
		t.Fatal("trust anchor must not be embedded in the workload ConfigMap")
	}
}
func TestRejectUnpinnedImage(t *testing.T) {
	p, r, sig, pem := fixture(t)
	_, err := Render(Options{JobName: "job", Image: "example/c2k:latest", DRADriverName: "gpu.example.com", Profile: p, Request: r, Signature: sig, PublicKeyPEM: pem, Now: time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)})
	if err == nil {
		t.Fatal("expected image digest rejection")
	}
}

func TestRejectImageNotBoundIntoProfile(t *testing.T) {
	p, r, sig, pem := fixture(t)
	_, err := Render(Options{JobName: "job", Image: "other/c2k@sha256:" + strings.Repeat("e", 64), DRADriverName: "gpu.example.com", Profile: p, Request: r, Signature: sig, PublicKeyPEM: pem, Now: time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)})
	if err == nil {
		t.Fatal("expected image/profile binding rejection")
	}
}

func TestGeneratedNamesPreserveDigestSuffix(t *testing.T) {
	p, r, sig, pem := fixture(t)
	b, err := Render(Options{JobName: strings.Repeat("a", 63), Image: p.Spec.Artifact.ContainerDigest, DRADriverName: "gpu.example.com", Profile: p, Request: r, Signature: sig, PublicKeyPEM: pem, Now: time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	suffix := strings.TrimPrefix(p.Seal.ContractDigest, "sha256:")[:12]
	if !strings.Contains(string(b), suffix) {
		t.Fatalf("generated resource names lost digest suffix %q", suffix)
	}
}

func TestRejectMismatchedSignatureKey(t *testing.T) {
	p, r, sig, _ := fixture(t)
	other, _, err := contract.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pem, _ := contract.MarshalPublicKeyPEM(other)
	_, err = Render(Options{JobName: "job", Image: p.Spec.Artifact.ContainerDigest, DRADriverName: "gpu.example.com", Profile: p, Request: r, Signature: sig, PublicKeyPEM: pem, Now: time.Date(2026, 8, 20, 3, 0, 0, 0, time.UTC)})
	if err == nil || !strings.Contains(err.Error(), "keyID") {
		t.Fatalf("expected signature/key rejection, got %v", err)
	}
}
