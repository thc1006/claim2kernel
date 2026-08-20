// Package launcher performs runtime revalidation and executes a verified kernel
// artifact without invoking a shell.
package launcher

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/thc1006/claim2kernel/pkg/artifact"
	"github.com/thc1006/claim2kernel/pkg/contract"
	"github.com/thc1006/claim2kernel/pkg/dra"
	"github.com/thc1006/claim2kernel/pkg/planner"
)

type Options struct {
	Root               string
	Profile            *contract.KernelProfile
	Request            *contract.KernelRequest
	Metadata           *dra.DeviceMetadata
	DRARequestName     string
	Signature          *contract.SignatureEnvelope
	SignaturePublicKey ed25519.PublicKey
	RequireSignature   bool
	VerifyOnly         bool
	Timeout            time.Duration
	MaxOutputBytes     int
	Now                time.Time
}

type Result struct {
	Decision       planner.Decision `json:"decision"`
	ArtifactDigest string           `json:"artifactDigest,omitempty"`
	SignatureValid bool             `json:"signatureValid"`
	Executed       bool             `json:"executed"`
	// ExitCode is always emitted. Zero is meaningful evidence of successful
	// execution and must not disappear because of JSON's omitempty behavior.
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

type RejectionError struct{ Decision planner.Decision }

func (e *RejectionError) Error() string {
	return "runtime contract rejected: " + formatReasons(e.Decision.Reasons)
}

func Run(opts Options) (Result, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = 1 << 20
	}
	if opts.MaxOutputBytes > 64<<20 {
		return Result{}, fmt.Errorf("max output exceeds 64 MiB safety ceiling")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	decision := planner.Evaluate(opts.Profile, opts.Request, planner.RuntimePhase, planner.RuntimeEvidence{Now: now, Metadata: opts.Metadata, DRARequestName: opts.DRARequestName})
	result := Result{Decision: decision, ExitCode: -1}
	if !decision.Admissible {
		return result, &RejectionError{Decision: decision}
	}
	if opts.RequireSignature && (opts.Signature == nil || len(opts.SignaturePublicKey) == 0) {
		return result, fmt.Errorf("CONTRACT_SIGNATURE_REQUIRED: detached signature and public key are required")
	}
	if opts.Signature != nil || len(opts.SignaturePublicKey) > 0 {
		if opts.Signature == nil || len(opts.SignaturePublicKey) == 0 {
			return result, fmt.Errorf("CONTRACT_SIGNATURE_INCOMPLETE: provide both signature and public key")
		}
		if err := contract.VerifyProfileSignature(opts.Profile, opts.Signature, opts.SignaturePublicKey, now, 5*time.Minute); err != nil {
			return result, fmt.Errorf("CONTRACT_SIGNATURE_INVALID: %w", err)
		}
		result.SignatureValid = true
	}
	staged, err := artifact.Stage(opts.Root, opts.Profile.Spec.Artifact)
	if err != nil {
		return result, fmt.Errorf("ARTIFACT_VERIFICATION_FAILED: %w", err)
	}
	defer staged.Cleanup()
	result.ArtifactDigest = staged.Digest
	if opts.VerifyOnly {
		return result, nil
	}
	if len(opts.Profile.Spec.Artifact.DefaultArgs) > 64 {
		return result, fmt.Errorf("too many default arguments")
	}
	input, err := json.Marshal(opts.Request)
	if err != nil {
		return result, err
	}
	input = append(input, '\n')
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, staged.Path, opts.Profile.Spec.Artifact.DefaultArgs...)
	configureProcess(cmd)
	cmd.Env = []string{"LANG=C.UTF-8", "LC_ALL=C.UTF-8", "C2K_CONTRACT_DIGEST=" + opts.Profile.Seal.ContractDigest, "C2K_ARTIFACT_DIGEST=" + staged.Digest, "C2K_REQUEST_NAME=" + opts.Request.Metadata.Name}
	cmd.Stdin = bytes.NewReader(input)
	stdout := newLimitedBuffer(opts.MaxOutputBytes)
	stderr := newLimitedBuffer(opts.MaxOutputBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	result.Executed = true
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	if ctx.Err() == context.DeadlineExceeded {
		return result, fmt.Errorf("KERNEL_TIMEOUT: exceeded %s", opts.Timeout)
	}
	if stdout.Exceeded() || stderr.Exceeded() {
		return result, fmt.Errorf("KERNEL_OUTPUT_LIMIT: output exceeded %d bytes", opts.MaxOutputBytes)
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, fmt.Errorf("KERNEL_EXIT_NONZERO: exit code %d", result.ExitCode)
		}
		return result, fmt.Errorf("KERNEL_EXEC_FAILED: %w", err)
	}
	result.ExitCode = 0
	return result, nil
}

func formatReasons(rs []planner.Reason) string {
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		parts = append(parts, r.Code+":"+r.Message)
	}
	return strings.Join(parts, ", ")
}

type limitedBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	limit    int
	exceeded bool
}

func newLimitedBuffer(limit int) *limitedBuffer { return &limitedBuffer{limit: limit} }
func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.exceeded {
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.exceeded = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.exceeded = true
		return len(p), nil
	}
	return b.buf.Write(p)
}
func (b *limitedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buf.String() }
func (b *limitedBuffer) Exceeded() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.exceeded }
