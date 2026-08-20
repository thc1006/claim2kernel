package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

func canonicalJSON(v any) ([]byte, error) {
	// encoding/json sorts string map keys. Struct field order is fixed by the
	// schema. Non-finite floats are rejected by json.Marshal.
	return json.Marshal(v)
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func ContractDigest(p *KernelProfile) (string, error) {
	if p == nil {
		return "", fmt.Errorf("nil profile")
	}
	copy := *p
	copy.Seal = nil
	data, err := canonicalJSON(copy)
	if err != nil {
		return "", fmt.Errorf("canonicalize profile: %w", err)
	}
	return digestBytes(data), nil
}

func ModelDigest(p *KernelProfile) (string, error) {
	if p == nil {
		return "", fmt.Errorf("nil profile")
	}
	payload := struct {
		InputDomain  InputDomain          `json:"inputDomain"`
		Numerical    NumericalCertificate `json:"numerical"`
		Latency      LatencyCertificate   `json:"latency"`
		Interference InterferenceEnvelope `json:"interference"`
		OOD          OODCertificate       `json:"ood"`
		Versions     map[string]string    `json:"versions"`
	}{p.Spec.InputDomain, p.Spec.Numerical, p.Spec.Latency, p.Spec.Interference, p.Spec.OOD, p.Spec.Versions}
	data, err := canonicalJSON(payload)
	if err != nil {
		return "", fmt.Errorf("canonicalize model payload: %w", err)
	}
	return digestBytes(data), nil
}

func SealProfile(p *KernelProfile, now time.Time) error {
	if p == nil {
		return fmt.Errorf("nil profile")
	}
	p.Seal = nil
	if issues := validateProfileBase(p); len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	createdAt, _ := ParseTime(p.Metadata.CreatedAt)
	expiresAt, _ := ParseTime(p.Metadata.ExpiresAt)
	if now.Before(createdAt.Add(-5 * time.Minute)) {
		return fmt.Errorf("seal time predates profile creation beyond allowed skew")
	}
	if !now.Before(expiresAt) {
		return fmt.Errorf("cannot seal an expired profile")
	}
	contractDigest, err := ContractDigest(p)
	if err != nil {
		return err
	}
	modelDigest, err := ModelDigest(p)
	if err != nil {
		return err
	}
	p.Seal = &Seal{ContractDigest: contractDigest, ModelDigest: modelDigest, SealedAt: now.UTC().Format(time.RFC3339Nano)}
	return nil
}

func VerifySeal(p *KernelProfile) error {
	if p == nil || p.Seal == nil {
		return fmt.Errorf("seal is missing")
	}
	if !digestRE.MatchString(p.Seal.ContractDigest) || !digestRE.MatchString(p.Seal.ModelDigest) {
		return fmt.Errorf("seal digests are malformed")
	}
	sealedAt, err := ParseTime(p.Seal.SealedAt)
	if err != nil {
		return fmt.Errorf("seal.sealedAt must be RFC3339")
	}
	createdAt, err := ParseTime(p.Metadata.CreatedAt)
	if err != nil {
		return fmt.Errorf("metadata.createdAt must be RFC3339")
	}
	expiresAt, err := ParseTime(p.Metadata.ExpiresAt)
	if err != nil {
		return fmt.Errorf("metadata.expiresAt must be RFC3339")
	}
	if sealedAt.Before(createdAt.Add(-5*time.Minute)) || !sealedAt.Before(expiresAt) {
		return fmt.Errorf("seal.sealedAt must be within the profile validity interval")
	}
	wantContract, err := ContractDigest(p)
	if err != nil {
		return err
	}
	if p.Seal.ContractDigest != wantContract {
		return fmt.Errorf("contract digest mismatch: sealed %s computed %s", p.Seal.ContractDigest, wantContract)
	}
	wantModel, err := ModelDigest(p)
	if err != nil {
		return err
	}
	if p.Seal.ModelDigest != wantModel {
		return fmt.Errorf("model digest mismatch: sealed %s computed %s", p.Seal.ModelDigest, wantModel)
	}
	return nil
}
