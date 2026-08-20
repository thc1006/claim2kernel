package contract

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"time"
)

const signatureDomain = "claim2kernel.dev/profile-signature/v1\x00"

type signaturePayload struct {
	Domain         string `json:"domain"`
	KeyID          string `json:"keyID"`
	ContractDigest string `json:"contractDigest"`
	ArtifactDigest string `json:"artifactDigest"`
	SignedAt       string `json:"signedAt"`
}

func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

func KeyID(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "ed25519:" + hex.EncodeToString(sum[:16])
}

func MarshalPrivateKeyPEM(key ed25519.PrivateKey) ([]byte, error) {
	b, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}), nil
}
func MarshalPublicKeyPEM(key ed25519.PublicKey) ([]byte, error) {
	b, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: b}), nil
}
func ParsePrivateKeyPEM(data []byte) (ed25519.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || len(rest) != 0 {
		return nil, fmt.Errorf("expected exactly one PEM private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ed, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not Ed25519")
	}
	return ed, nil
}
func ParsePublicKeyPEM(data []byte) (ed25519.PublicKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || len(rest) != 0 {
		return nil, fmt.Errorf("expected exactly one PEM public key")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ed, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not Ed25519")
	}
	return ed, nil
}

func SignProfile(p *KernelProfile, privateKey ed25519.PrivateKey, at time.Time) (*SignatureEnvelope, error) {
	if err := ValidateProfile(p, true); err != nil {
		return nil, err
	}
	sealedAt, _ := ParseTime(p.Seal.SealedAt)
	expiresAt, _ := ParseTime(p.Metadata.ExpiresAt)
	if at.Before(sealedAt.Add(-5 * time.Minute)) {
		return nil, fmt.Errorf("signature time predates the profile seal")
	}
	if !at.Before(expiresAt) {
		return nil, fmt.Errorf("cannot sign an expired profile")
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	env := &SignatureEnvelope{
		APIVersion: APIVersion, Kind: SignatureKind, Algorithm: "Ed25519",
		KeyID: KeyID(publicKey), ContractDigest: p.Seal.ContractDigest,
		ArtifactDigest: p.Spec.Artifact.Digest, SignedAt: at.UTC().Format(time.RFC3339Nano),
	}
	payload, err := signatureMessage(env)
	if err != nil {
		return nil, err
	}
	env.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	return env, nil
}

func VerifyProfileSignature(p *KernelProfile, env *SignatureEnvelope, publicKey ed25519.PublicKey, now time.Time, maxFutureSkew time.Duration) error {
	if err := ValidateProfile(p, true); err != nil {
		return err
	}
	if err := ValidateSignatureEnvelope(env); err != nil {
		return err
	}
	if env.KeyID != KeyID(publicKey) {
		return fmt.Errorf("signature keyID does not match public key")
	}
	if env.ContractDigest != p.Seal.ContractDigest || env.ArtifactDigest != p.Spec.Artifact.Digest {
		return fmt.Errorf("signature is bound to a different profile or artifact")
	}
	signedAt, _ := ParseTime(env.SignedAt)
	createdAt, _ := ParseTime(p.Metadata.CreatedAt)
	expiresAt, _ := ParseTime(p.Metadata.ExpiresAt)
	if signedAt.Before(createdAt.Add(-maxFutureSkew)) || !signedAt.Before(expiresAt) {
		return fmt.Errorf("signature timestamp is outside the profile validity interval")
	}
	if signedAt.After(now.Add(maxFutureSkew)) {
		return fmt.Errorf("signature timestamp is in the future beyond allowed skew")
	}
	if sealedAt, err := ParseTime(p.Seal.SealedAt); err == nil && signedAt.Before(sealedAt.Add(-maxFutureSkew)) {
		return fmt.Errorf("signature predates the profile seal")
	}
	sig, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	payload, err := signatureMessage(env)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, payload, sig) {
		return fmt.Errorf("Ed25519 signature verification failed")
	}
	return nil
}

func ValidateSignatureEnvelope(env *SignatureEnvelope) error {
	if env == nil {
		return fmt.Errorf("signature envelope is nil")
	}
	issues := []string{}
	if env.APIVersion != APIVersion {
		issues = append(issues, "apiVersion must be "+APIVersion)
	}
	if env.Kind != SignatureKind {
		issues = append(issues, "kind must be "+SignatureKind)
	}
	if env.Algorithm != "Ed25519" {
		issues = append(issues, "algorithm must be Ed25519")
	}
	if env.KeyID == "" {
		issues = append(issues, "keyID is required")
	}
	if !digestRE.MatchString(env.ContractDigest) || !digestRE.MatchString(env.ArtifactDigest) {
		issues = append(issues, "contract and artifact digests must be sha256")
	}
	if _, err := ParseTime(env.SignedAt); err != nil {
		issues = append(issues, "signedAt must be RFC3339")
	}
	decoded, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil || len(decoded) != ed25519.SignatureSize {
		issues = append(issues, "signature must be a base64 Ed25519 signature")
	}
	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}

func signatureMessage(env *SignatureEnvelope) ([]byte, error) {
	payload := signaturePayload{Domain: signatureDomain, KeyID: env.KeyID, ContractDigest: env.ContractDigest, ArtifactDigest: env.ArtifactDigest, SignedAt: env.SignedAt}
	return canonicalJSON(payload)
}
