// Package dra parses Kubernetes v1.36 DRA device metadata streams without
// importing Kubernetes libraries. The wire shape follows
// metadata.resource.k8s.io/v1alpha1.
package dra

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/thc1006/claim2kernel/pkg/contract"
	"github.com/thc1006/claim2kernel/pkg/jsonsafe"
)

const (
	APIVersion             = "metadata.resource.k8s.io/v1alpha1"
	Kind                   = "DeviceMetadata"
	MetadataRoot           = "/var/run/kubernetes.io/dra-device-attributes"
	MaxMetadataBytes       = 4 << 20
	MaxMetadataRequests    = 32
	MaxDevicesPerRequest   = 128
	MaxAttributesPerDevice = 48
	MaxUnknownVersions     = 64
)

type ObjectMeta struct {
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	UID        string `json:"uid,omitempty"`
	Generation int64  `json:"generation,omitempty"`
}

type DeviceMetadata struct {
	APIVersion   string                  `json:"apiVersion"`
	Kind         string                  `json:"kind"`
	Metadata     ObjectMeta              `json:"metadata,omitempty"`
	PodClaimName *string                 `json:"podClaimName,omitempty"`
	Requests     []DeviceMetadataRequest `json:"requests,omitempty"`
}

type DeviceMetadataRequest struct {
	Name    string   `json:"name"`
	Devices []Device `json:"devices,omitempty"`
}

type Device struct {
	Driver      string               `json:"driver"`
	Pool        string               `json:"pool"`
	Name        string               `json:"name"`
	Attributes  map[string]Attribute `json:"attributes,omitempty"`
	NetworkData json.RawMessage      `json:"networkData,omitempty"`
}

type Attribute struct {
	Int      *int64   `json:"int,omitempty"`
	Bool     *bool    `json:"bool,omitempty"`
	String   *string  `json:"string,omitempty"`
	Version  *string  `json:"version,omitempty"`
	Ints     []int64  `json:"ints,omitempty"`
	Bools    []bool   `json:"bools,omitempty"`
	Strings  []string `json:"strings,omitempty"`
	Versions []string `json:"versions,omitempty"`
}

func (a Attribute) validate() error {
	count := 0
	if a.Int != nil {
		count++
	}
	if a.Bool != nil {
		count++
	}
	if a.String != nil {
		count++
	}
	if a.Version != nil {
		count++
	}
	if a.Ints != nil {
		count++
	}
	if a.Bools != nil {
		count++
	}
	if a.Strings != nil {
		count++
	}
	if a.Versions != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("device attribute must contain exactly one union member, got %d", count)
	}
	if a.Ints != nil && len(a.Ints) == 0 || a.Bools != nil && len(a.Bools) == 0 || a.Strings != nil && len(a.Strings) == 0 || a.Versions != nil && len(a.Versions) == 0 {
		return errors.New("list-valued device attribute must not be empty")
	}
	return nil
}

func (a Attribute) Scalar() (contract.Value, bool) {
	switch {
	case a.Int != nil:
		if *a.Int > contract.MaxExactInteger || *a.Int < -contract.MaxExactInteger {
			return contract.Value{}, false
		}
		return contract.NumberValue(float64(*a.Int)), true
	case a.Bool != nil:
		return contract.BoolValue(*a.Bool), true
	case a.String != nil:
		return contract.StringValue(*a.String), true
	case a.Version != nil:
		return contract.StringValue(*a.Version), true
	default:
		return contract.Value{}, false
	}
}

func (a Attribute) Contains(v contract.Value) bool {
	if scalar, ok := a.Scalar(); ok {
		return equalValue(scalar, v)
	}
	switch v.Kind() {
	case "number":
		n, _ := v.Number()
		for _, x := range a.Ints {
			if float64(x) == n {
				return true
			}
		}
	case "bool":
		b, _ := v.Bool()
		for _, x := range a.Bools {
			if x == b {
				return true
			}
		}
	case "string":
		s, _ := v.String()
		for _, x := range a.Strings {
			if x == s {
				return true
			}
		}
		for _, x := range a.Versions {
			if x == s {
				return true
			}
		}
	}
	return false
}

func equalValue(a, b contract.Value) bool {
	if a.Kind() != b.Kind() {
		return false
	}
	switch a.Kind() {
	case "number":
		av, _ := a.Number()
		bv, _ := b.Number()
		return av == bv
	case "string":
		av, _ := a.String()
		bv, _ := b.String()
		return av == bv
	case "bool":
		av, _ := a.Bool()
		bv, _ := b.Bool()
		return av == bv
	default:
		return false
	}
}

// DecodeMetadataStream returns the first supported object. Unknown versions are
// skipped. A malformed object claiming a supported version is fatal, matching
// Kubernetes' own decoder behavior.
func DecodeMetadataStream(data []byte) (*DeviceMetadata, error) {
	if len(data) == 0 {
		return nil, errors.New("no metadata objects found in stream")
	}
	if len(data) > MaxMetadataBytes {
		return nil, fmt.Errorf("metadata stream exceeds %d bytes", MaxMetadataBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	unknown := []string{}
	for {
		var raw json.RawMessage
		err := dec.Decode(&raw)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read metadata object from stream: %w", err)
		}
		if err := jsonsafe.RejectDuplicateKeys(raw, 64); err != nil {
			return nil, fmt.Errorf("decode metadata object: %w", err)
		}
		var tm struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
		}
		if err := json.Unmarshal(raw, &tm); err != nil {
			return nil, fmt.Errorf("decode metadata type meta: %w", err)
		}
		if tm.APIVersion == "" || tm.Kind == "" {
			return nil, fmt.Errorf("decode metadata object: apiVersion and kind are required")
		}
		if tm.APIVersion != APIVersion {
			if len(unknown) >= MaxUnknownVersions {
				return nil, fmt.Errorf("metadata stream exceeds %d unknown-version objects", MaxUnknownVersions)
			}
			unknown = append(unknown, tm.APIVersion)
			continue
		}
		if tm.Kind != Kind {
			return nil, fmt.Errorf("decode %s: kind must be %s", APIVersion, Kind)
		}
		var out DeviceMetadata
		if err := jsonsafe.DecodeStrict(raw, &out, MaxMetadataBytes); err != nil {
			return nil, fmt.Errorf("decode %s: %w", APIVersion, err)
		}
		if err := Validate(&out); err != nil {
			return nil, fmt.Errorf("decode %s: %w", APIVersion, err)
		}
		return &out, nil
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("no compatible metadata version found in stream (unknown versions: %s)", strings.Join(unknown, ", "))
	}
	return nil, errors.New("no metadata objects found in stream")
}

func ReadMetadata(path string) (*DeviceMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxMetadataBytes+1))
	if err != nil {
		return nil, err
	}
	return DecodeMetadataStream(data)
}

func Validate(m *DeviceMetadata) error {
	if m == nil {
		return errors.New("metadata is nil")
	}
	if m.APIVersion != APIVersion || m.Kind != Kind {
		return fmt.Errorf("unexpected type %s %s", m.APIVersion, m.Kind)
	}
	if len(m.Requests) == 0 {
		return errors.New("requests must not be empty")
	}
	if len(m.Requests) > MaxMetadataRequests {
		return fmt.Errorf("requests exceeds %d-entry safety limit", MaxMetadataRequests)
	}
	seenRequests := map[string]struct{}{}
	for i, r := range m.Requests {
		if r.Name == "" {
			return fmt.Errorf("requests[%d].name is empty", i)
		}
		if _, ok := seenRequests[r.Name]; ok {
			return fmt.Errorf("duplicate request name %q", r.Name)
		}
		seenRequests[r.Name] = struct{}{}
		if len(r.Devices) == 0 {
			return fmt.Errorf("requests[%d].devices is empty", i)
		}
		if len(r.Devices) > MaxDevicesPerRequest {
			return fmt.Errorf("requests[%d].devices exceeds %d-entry safety limit", i, MaxDevicesPerRequest)
		}
		seenDevices := map[string]struct{}{}
		for j, d := range r.Devices {
			if d.Driver == "" || d.Pool == "" || d.Name == "" {
				return fmt.Errorf("requests[%d].devices[%d] requires driver, pool, and name", i, j)
			}
			id := d.Driver + "/" + d.Pool + "/" + d.Name
			if _, ok := seenDevices[id]; ok {
				return fmt.Errorf("duplicate device %q", id)
			}
			seenDevices[id] = struct{}{}
			if len(d.Attributes) > MaxAttributesPerDevice {
				return fmt.Errorf("device %s attributes exceeds %d-entry safety limit", id, MaxAttributesPerDevice)
			}
			for name, a := range d.Attributes {
				if name == "" {
					return fmt.Errorf("empty attribute name")
				}
				if err := a.validate(); err != nil {
					return fmt.Errorf("device %s attribute %s: %w", id, name, err)
				}
			}
		}
	}
	return nil
}

func DevicesForRequest(m *DeviceMetadata, requestName string) ([]Device, error) {
	for _, r := range m.Requests {
		if r.Name == requestName {
			return r.Devices, nil
		}
	}
	return nil, fmt.Errorf("DRA metadata does not contain request %q", requestName)
}

func MetadataPathForTemplate(podClaimName, requestName, driverName string) (string, error) {
	for label, s := range map[string]string{"podClaimName": podClaimName, "requestName": requestName, "driverName": driverName} {
		if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\\\x00") {
			return "", fmt.Errorf("unsafe %s %q", label, s)
		}
	}
	return filepath.Join(MetadataRoot, "resourceclaimtemplates", podClaimName, requestName, driverName+"-metadata.json"), nil
}
