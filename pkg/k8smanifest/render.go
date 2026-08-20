// Package k8smanifest renders a signed Claim2Kernel workload as Kubernetes
// resource.k8s.io/v1 objects without a Kubernetes client dependency.
package k8smanifest

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/thc1006/claim2kernel/pkg/contract"
	"github.com/thc1006/claim2kernel/pkg/dra"
)

type Options struct {
	Namespace            string
	JobName              string
	Image                string
	QueueName            string
	ArtifactRoot         string
	DRARequestName       string
	DRADriverName        string
	Profile              *contract.KernelProfile
	Request              *contract.KernelRequest
	Signature            *contract.SignatureEnvelope
	PublicKeyPEM         []byte
	RuntimePublicKeyPath string
	Now                  time.Time
}

var labelRE = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
var subdomainRE = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9.]*[a-z0-9])?$`)

func Render(opts Options) ([]byte, error) {
	if opts.Profile == nil || opts.Request == nil || opts.Signature == nil || len(opts.PublicKeyPEM) == 0 {
		return nil, fmt.Errorf("profile, request, detached signature, and public key are required")
	}
	if err := contract.ValidateProfile(opts.Profile, true); err != nil {
		return nil, err
	}
	if err := contract.ValidateRequest(opts.Request); err != nil {
		return nil, err
	}
	if err := contract.ValidateSignatureEnvelope(opts.Signature); err != nil {
		return nil, err
	}
	publicKey, err := contract.ParsePublicKeyPEM(opts.PublicKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := contract.VerifyProfileSignature(opts.Profile, opts.Signature, publicKey, now, 5*time.Minute); err != nil {
		return nil, fmt.Errorf("verify detached profile signature: %w", err)
	}
	if opts.Namespace == "" {
		opts.Namespace = "default"
	}
	if opts.QueueName == "" {
		opts.QueueName = "default"
	}
	if opts.ArtifactRoot == "" {
		opts.ArtifactRoot = "/opt/c2k"
	}
	if opts.DRARequestName == "" {
		opts.DRARequestName = "gpu"
	}
	if opts.RuntimePublicKeyPath == "" {
		opts.RuntimePublicKeyPath = "/etc/claim2kernel-trust/public.pem"
	}
	if !strings.HasPrefix(opts.RuntimePublicKeyPath, "/") || strings.ContainsRune(opts.RuntimePublicKeyPath, '\x00') || strings.Contains(opts.RuntimePublicKeyPath, "..") {
		return nil, fmt.Errorf("runtime public-key path must be an absolute, non-traversing path baked into the trusted runner image")
	}
	for name, v := range map[string]string{"namespace": opts.Namespace, "jobName": opts.JobName, "queueName": opts.QueueName, "draRequestName": opts.DRARequestName} {
		if !validName(v) {
			return nil, fmt.Errorf("%s %q is not a DNS-1123 label", name, v)
		}
	}
	if !strings.Contains(opts.Image, "@sha256:") {
		return nil, fmt.Errorf("image must be pinned by digest")
	}
	if opts.Profile.Spec.Artifact.ContainerDigest == "" {
		return nil, fmt.Errorf("profile artifact.containerDigest is required for Kubernetes rendering")
	}
	if opts.Image != opts.Profile.Spec.Artifact.ContainerDigest {
		return nil, fmt.Errorf("image %q is not the container digest bound into the profile", opts.Image)
	}
	if !validSubdomain(opts.DRADriverName) {
		return nil, fmt.Errorf("DRA driver name %q must be a DNS subdomain", opts.DRADriverName)
	}
	profileJSON, _ := json.MarshalIndent(opts.Profile, "", "  ")
	requestJSON, _ := json.MarshalIndent(opts.Request, "", "  ")
	signatureJSON, _ := json.MarshalIndent(opts.Signature, "", "  ")
	if len(profileJSON)+len(requestJSON)+len(signatureJSON) > 900*1024 {
		return nil, fmt.Errorf("contract ConfigMap payload exceeds 900 KiB")
	}
	suffix := strings.TrimPrefix(opts.Profile.Seal.ContractDigest, "sha256:")[:12]
	configName := nameWithSuffix(opts.JobName, "contract-"+suffix)
	claimName := nameWithSuffix(opts.JobName, "device-"+suffix)
	metadataPath, err := dra.MetadataPathForTemplate("accelerator", opts.DRARequestName, opts.DRADriverName)
	if err != nil {
		return nil, err
	}
	immutable := true
	config := map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": configName, "namespace": opts.Namespace, "labels": commonLabels(opts.Profile)}, "immutable": immutable, "data": map[string]string{"profile.json": string(profileJSON) + "\n", "request.json": string(requestJSON) + "\n", "signature.json": string(signatureJSON) + "\n"}}
	claim := map[string]any{"apiVersion": "resource.k8s.io/v1", "kind": "ResourceClaimTemplate", "metadata": map[string]any{"name": claimName, "namespace": opts.Namespace, "labels": commonLabels(opts.Profile)}, "spec": map[string]any{"metadata": map[string]any{"labels": commonLabels(opts.Profile)}, "spec": map[string]any{"devices": map[string]any{"requests": []any{map[string]any{"name": opts.DRARequestName, "exactly": map[string]any{"deviceClassName": opts.Profile.Spec.Target.DeviceClass, "allocationMode": "ExactCount", "count": opts.Profile.Spec.Resources.DeviceCount}}}}}}}
	labels := commonLabels(opts.Profile)
	labels["kueue.x-k8s.io/queue-name"] = opts.QueueName
	args := []string{"launch", "--root", opts.ArtifactRoot, "--profile", "/etc/c2k/profile.json", "--request", "/etc/c2k/request.json", "--metadata", metadataPath, "--dra-request", opts.DRARequestName, "--signature", "/etc/c2k/signature.json", "--public-key", opts.RuntimePublicKeyPath, "--require-signature"}
	job := map[string]any{"apiVersion": "batch/v1", "kind": "Job", "metadata": map[string]any{"name": opts.JobName, "namespace": opts.Namespace, "labels": labels}, "spec": map[string]any{"suspend": true, "backoffLimit": 0, "podReplacementPolicy": "Failed", "template": map[string]any{"metadata": map[string]any{"labels": commonLabels(opts.Profile)}, "spec": map[string]any{"automountServiceAccountToken": false, "restartPolicy": "Never", "securityContext": map[string]any{"runAsNonRoot": true, "seccompProfile": map[string]any{"type": "RuntimeDefault"}}, "resourceClaims": []any{map[string]any{"name": "accelerator", "resourceClaimTemplateName": claimName}}, "containers": []any{map[string]any{"name": "kernel", "image": opts.Image, "imagePullPolicy": "IfNotPresent", "command": []string{"/c2k"}, "args": args, "securityContext": map[string]any{"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "capabilities": map[string]any{"drop": []string{"ALL"}}}, "resources": map[string]any{"claims": []any{map[string]any{"name": "accelerator", "request": opts.DRARequestName}}}, "volumeMounts": []any{map[string]any{"name": "contract", "mountPath": "/etc/c2k", "readOnly": true}, map[string]any{"name": "tmp", "mountPath": "/tmp"}}}}, "volumes": []any{map[string]any{"name": "contract", "configMap": map[string]any{"name": configName, "defaultMode": 292}}, map[string]any{"name": "tmp", "emptyDir": map[string]any{"sizeLimit": "512Mi"}}}}}}}
	list := map[string]any{"apiVersion": "v1", "kind": "List", "items": []any{config, claim, job}}
	return json.MarshalIndent(list, "", "  ")
}
func commonLabels(p *contract.KernelProfile) map[string]string {
	return map[string]string{"app.kubernetes.io/name": "claim2kernel", "app.kubernetes.io/component": "kernel-runner", "claim2kernel.dev/profile": p.Metadata.Name, "claim2kernel.dev/contract": strings.TrimPrefix(p.Seal.ContractDigest, "sha256:")[:16]}
}
func validName(s string) bool { return len(s) > 0 && len(s) <= 63 && labelRE.MatchString(s) }
func validSubdomain(s string) bool {
	if len(s) == 0 || len(s) > 253 || !subdomainRE.MatchString(s) || strings.Contains(s, "..") {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		if !validName(part) {
			return false
		}
	}
	return true
}
func nameWithSuffix(base, suffix string) string {
	suffix = strings.Trim(suffix, "-")
	reserve := len(suffix) + 1
	if reserve >= 63 {
		return suffix[:63]
	}
	maxBase := 63 - reserve
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	base = strings.Trim(base, "-")
	return base + "-" + suffix
}
