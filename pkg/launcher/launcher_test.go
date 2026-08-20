package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thc1006/claim2kernel/pkg/artifact"
	"github.com/thc1006/claim2kernel/pkg/contract"
	"github.com/thc1006/claim2kernel/pkg/dra"
)

func fixture(t *testing.T) (string, *contract.KernelProfile, *contract.KernelRequest, *dra.DeviceMetadata) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "kernel")
	body := "#!/bin/sh\ncat >/dev/null\nprintf '{\"ok\":true}\\n'\n"
	if err := os.WriteFile(path, []byte(body), 0o500); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o500); err != nil {
		t.Fatal(err)
	}
	digest, size, err := artifact.DigestFile(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	min, max := 1.0, 10.0
	p := &contract.KernelProfile{APIVersion: contract.APIVersion, Kind: contract.ProfileKind, Metadata: contract.ObjectMeta{Name: "launch-profile", Version: "0.1.0", CreatedAt: "2026-08-20T00:00:00Z", ExpiresAt: "2027-01-01T00:00:00Z"}, Spec: contract.ProfileSpec{Artifact: contract.ArtifactSpec{Path: "kernel", Digest: digest, SizeBytes: size, MaxBytes: 1 << 20, Protocol: "stdin-json-v1"}, Target: contract.TargetSpec{Backend: "cpu", Vendor: "generic", Architecture: "x86-64", DeviceClass: "cpu.example.com"}, InputDomain: contract.InputDomain{Features: map[string]contract.FeatureSpec{"work": {Kind: "integer", Required: true, Minimum: &min, Maximum: &max}}}, Precision: contract.PrecisionSpec{Storage: "fp32", Accumulation: "fp32"}, Resources: contract.ResourceSpec{DeviceCount: 1}, Numerical: contract.NumericalCertificate{Metric: "relative_l2", UpperBound: .01, ObservedMax: .005, TestSampleCount: 10}, Latency: contract.LatencyCertificate{Method: "split-conformal", Quantile: .9, Confidence: .9, ResidualUpperUS: 1, IOBudgetUS: 1, RuntimeJitterUS: 1, CalibrationSampleCount: 20, TestSampleCount: 10, ObservedCoverage: 1, Model: contract.LinearModel{InterceptUS: 1, Coefficients: map[string]float64{"input.work": 1}, FeatureOrder: []string{"input.work"}, RidgeLambda: .001}, CalibratedAt: "2026-08-20T00:00:00Z", MaxAgeSeconds: 365 * 24 * 3600}, Interference: contract.InterferenceEnvelope{Metrics: map[string]contract.Range{}}, Versions: map[string]string{"runtime": ">=1.0.0,<2.0.0"}, DeviceAssertions: []contract.DeviceAssertion{{Attribute: "claim2kernel.dev/architecture", Op: "eq", Value: contract.StringValue("x86-64")}}, OOD: contract.OODCertificate{Required: false}, Policy: contract.PolicySpec{FailClosed: true}, Provenance: contract.Provenance{Compiler: "go", SourceDigest: "sha256:" + strings.Repeat("b", 64), DatasetDigest: "sha256:" + strings.Repeat("c", 64)}}}
	if err := contract.SealProfile(p, time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	r := &contract.KernelRequest{APIVersion: contract.APIVersion, Kind: contract.RequestKind, Metadata: contract.ObjectMeta{Name: "launch-job"}, Spec: contract.RequestSpec{DeadlineUS: 100, SafetyMarginUS: 5, MaxNumericalError: .02, Inputs: map[string]contract.Value{"work": contract.NumberValue(2)}, Interference: map[string]float64{}, Versions: map[string]string{"runtime": "1.1.0"}}}
	m, err := dra.DecodeMetadataStream([]byte(`{"apiVersion":"metadata.resource.k8s.io/v1alpha1","kind":"DeviceMetadata","metadata":{"name":"claim","generation":1},"requests":[{"name":"cpu","devices":[{"driver":"cpu.example.com","pool":"node0","name":"cpu0","attributes":{"claim2kernel.dev/architecture":{"string":"x86-64"}}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return root, p, r, m
}
func TestRunSigned(t *testing.T) {
	root, p, r, m := fixture(t)
	pub, priv, _ := contract.GenerateKey()
	now := time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)
	sig, err := contract.SignProfile(p, priv, now)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(Options{Root: root, Profile: p, Request: r, Metadata: m, DRARequestName: "cpu", Signature: sig, SignaturePublicKey: pub, RequireSignature: true, Timeout: 5 * time.Second, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Executed || res.ExitCode != 0 || !strings.Contains(res.Stdout, "ok") {
		t.Fatalf("bad result %+v", res)
	}
}
func TestRequireSignature(t *testing.T) {
	root, p, r, m := fixture(t)
	_, err := Run(Options{Root: root, Profile: p, Request: r, Metadata: m, DRARequestName: "cpu", RequireSignature: true, VerifyOnly: true, Now: time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC)})
	if err == nil || !strings.Contains(err.Error(), "SIGNATURE_REQUIRED") {
		t.Fatalf("unexpected error %v", err)
	}
}
func TestOutputLimit(t *testing.T) {
	b := newLimitedBuffer(3)
	n, err := b.Write([]byte("abcdef"))
	if err != nil || n != 6 || b.String() != "abc" || !b.Exceeded() {
		t.Fatalf("limited buffer failed")
	}
}
