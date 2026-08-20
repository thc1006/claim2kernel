package dra

import (
	"strings"
	"testing"

	"github.com/thc1006/claim2kernel/pkg/contract"
)

const valid = `{"apiVersion":"metadata.resource.k8s.io/v1alpha1","kind":"DeviceMetadata","metadata":{"name":"claim","generation":2},"podClaimName":"accelerator","requests":[{"name":"gpu","devices":[{"driver":"gpu.example.com","pool":"node-0","name":"gpu-0","attributes":{"claim2kernel.dev/architecture":{"string":"sm_90"},"claim2kernel.dev/memoryBytes":{"int":85899345920},"claim2kernel.dev/healthy":{"bool":true}}}]}]}`

func TestDecodeMetadata(t *testing.T) {
	m, err := DecodeMetadataStream([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if m.Metadata.Generation != 2 {
		t.Fatalf("generation=%d", m.Metadata.Generation)
	}
	devs, err := DevicesForRequest(m, "gpu")
	if err != nil || len(devs) != 1 {
		t.Fatalf("devices: %v %v", devs, err)
	}
}
func TestUnknownThenKnown(t *testing.T) {
	unknown := `{"apiVersion":"metadata.resource.k8s.io/v99","kind":"DeviceMetadata"}`
	if _, err := DecodeMetadataStream([]byte(unknown + "\n" + valid)); err != nil {
		t.Fatal(err)
	}
}
func TestMalformedKnownIsFatal(t *testing.T) {
	bad := `{"apiVersion":"metadata.resource.k8s.io/v1alpha1","kind":"DeviceMetadata","requests":"bad"}`
	_, err := DecodeMetadataStream([]byte(bad + "\n" + valid))
	if err == nil || !strings.Contains(err.Error(), "decode metadata.resource") {
		t.Fatalf("unexpected err %v", err)
	}
}
func TestDuplicateKey(t *testing.T) {
	bad := strings.Replace(valid, `"kind":"DeviceMetadata"`, `"kind":"DeviceMetadata","kind":"DeviceMetadata"`, 1)
	if _, err := DecodeMetadataStream([]byte(bad)); err == nil {
		t.Fatal("expected duplicate key error")
	}
}
func TestInvalidUnion(t *testing.T) {
	bad := strings.Replace(valid, `{"string":"sm_90"}`, `{"string":"sm_90","bool":true}`, 1)
	if _, err := DecodeMetadataStream([]byte(bad)); err == nil {
		t.Fatal("expected union error")
	}
}
func TestPath(t *testing.T) {
	p, err := MetadataPathForTemplate("accelerator", "gpu", "gpu.example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := "/var/run/kubernetes.io/dra-device-attributes/resourceclaimtemplates/accelerator/gpu/gpu.example.com-metadata.json"
	if p != want {
		t.Fatalf("got %s", p)
	}
	if _, err := MetadataPathForTemplate("../bad", "gpu", "x"); err == nil {
		t.Fatal("expected unsafe path error")
	}
}
func FuzzDecodeMetadataStream(f *testing.F) {
	f.Add([]byte(valid))
	f.Add([]byte(`{"x":1,"x":2}`))
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = DecodeMetadataStream(b) })
}

func TestLargeIntegerAttributeFailsClosed(t *testing.T) {
	tooLarge := int64(contract.MaxExactInteger + 1)
	if _, ok := (Attribute{Int: &tooLarge}).Scalar(); ok {
		t.Fatal("inexact DRA integer must not be converted to float64")
	}
}
