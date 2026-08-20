package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/thc1006/claim2kernel/pkg/contract"
	"github.com/thc1006/claim2kernel/pkg/dra"
	"github.com/thc1006/claim2kernel/pkg/k8smanifest"
	"github.com/thc1006/claim2kernel/pkg/launcher"
	"github.com/thc1006/claim2kernel/pkg/planner"
	"github.com/thc1006/claim2kernel/pkg/statecheck"
)

const version = "0.1.0-research"

// inputReadError is used internally so command handlers can keep their call
// sites compact without exposing a raw panic to users. run() converts only
// this sentinel panic back into an ordinary CLI error; unexpected programmer
// panics still propagate and remain visible during development.
type inputReadError struct{ err error }

func (e inputReadError) Error() string { return e.err.Error() }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "c2k:", err)
		os.Exit(1)
	}
}

func run() (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if readErr, ok := recovered.(inputReadError); ok {
				err = readErr
				return
			}
			panic(recovered)
		}
	}()
	if len(os.Args) < 2 {
		usage()
		return errors.New("missing command")
	}
	switch os.Args[1] {
	case "version":
		return writeJSON(os.Stdout, map[string]string{"version": version, "contractAPI": contract.APIVersion})
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "seal":
		err = cmdSeal(os.Args[2:])
	case "keygen":
		err = cmdKeygen(os.Args[2:])
	case "sign":
		err = cmdSign(os.Args[2:])
	case "verify-signature":
		err = cmdVerifySignature(os.Args[2:])
	case "select":
		err = cmdSelect(os.Args[2:])
	case "render-k8s":
		err = cmdRender(os.Args[2:])
	case "launch":
		err = cmdLaunch(os.Args[2:])
	case "inspect-metadata":
		err = cmdInspectMetadata(os.Args[2:])
	case "statecheck":
		err = cmdStatecheck(os.Args[2:])
	default:
		usage()
		return fmt.Errorf("unknown command %q", os.Args[1])
	}
	return err
}
func usage() {
	fmt.Fprintln(os.Stderr, "usage: c2k <version|validate|seal|keygen|sign|verify-signature|select|render-k8s|launch|inspect-metadata|statecheck> [flags]")
}

func cmdValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	profile := fs.String("profile", "", "profile JSON")
	catalog := fs.String("catalog", "", "catalog JSON")
	request := fs.String("request", "", "request JSON")
	signature := fs.String("signature", "", "signature JSON")
	sealed := fs.Bool("sealed", false, "require a valid seal")
	if err := fs.Parse(args); err != nil {
		return err
	}
	count := 0
	for _, v := range []string{*profile, *catalog, *request, *signature} {
		if v != "" {
			count++
		}
	}
	if count != 1 {
		return errors.New("exactly one of --profile, --catalog, --request, or --signature is required")
	}
	var err error
	switch {
	case *profile != "":
		_, err = contract.LoadProfile(mustRead(*profile), *sealed)
	case *catalog != "":
		_, err = contract.LoadCatalog(mustRead(*catalog), *sealed)
	case *request != "":
		_, err = contract.LoadRequest(mustRead(*request))
	case *signature != "":
		_, err = contract.LoadSignature(mustRead(*signature))
	}
	if err != nil {
		return err
	}
	return writeJSON(os.Stdout, map[string]any{"valid": true, "sealed": *sealed})
}
func cmdSeal(args []string) error {
	fs := flag.NewFlagSet("seal", flag.ContinueOnError)
	in := fs.String("profile", "", "unsealed profile")
	out := fs.String("out", "", "output profile")
	at := fs.String("at", "", "RFC3339 time (default now)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" || *out == "" {
		return errors.New("--profile and --out are required")
	}
	p, err := contract.LoadProfile(mustRead(*in), false)
	if err != nil {
		return err
	}
	p.Seal = nil
	now, err := parseOptionalTime(*at)
	if err != nil {
		return err
	}
	if err := contract.SealProfile(p, now); err != nil {
		return err
	}
	return atomicWriteJSON(*out, p, 0o644, true)
}
func cmdKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	privPath := fs.String("private", "", "private PEM output")
	pubPath := fs.String("public", "", "public PEM output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *privPath == "" || *pubPath == "" {
		return errors.New("--private and --public are required")
	}
	pub, priv, err := contract.GenerateKey()
	if err != nil {
		return err
	}
	privPEM, _ := contract.MarshalPrivateKeyPEM(priv)
	pubPEM, _ := contract.MarshalPublicKeyPEM(pub)
	if err := writeExclusive(*privPath, privPEM, 0o600); err != nil {
		return err
	}
	if err := writeExclusive(*pubPath, pubPEM, 0o644); err != nil {
		_ = os.Remove(*privPath)
		return err
	}
	return writeJSON(os.Stdout, map[string]string{"keyID": contract.KeyID(pub), "private": *privPath, "public": *pubPath})
}
func cmdSign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	profile := fs.String("profile", "", "sealed profile")
	key := fs.String("private-key", "", "private PEM")
	out := fs.String("out", "", "signature output")
	at := fs.String("at", "", "RFC3339 time")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *profile == "" || *key == "" || *out == "" {
		return errors.New("--profile, --private-key, and --out are required")
	}
	p, err := contract.LoadProfile(mustRead(*profile), true)
	if err != nil {
		return err
	}
	priv, err := contract.ParsePrivateKeyPEM(mustRead(*key))
	if err != nil {
		return err
	}
	now, err := parseOptionalTime(*at)
	if err != nil {
		return err
	}
	sig, err := contract.SignProfile(p, priv, now)
	if err != nil {
		return err
	}
	return atomicWriteJSON(*out, sig, 0o644, true)
}
func cmdVerifySignature(args []string) error {
	fs := flag.NewFlagSet("verify-signature", flag.ContinueOnError)
	profile := fs.String("profile", "", "sealed profile")
	sigPath := fs.String("signature", "", "signature JSON")
	pubPath := fs.String("public-key", "", "public PEM")
	at := fs.String("at", "", "verification time")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := contract.LoadProfile(mustRead(*profile), true)
	if err != nil {
		return err
	}
	sig, err := contract.LoadSignature(mustRead(*sigPath))
	if err != nil {
		return err
	}
	pub, err := contract.ParsePublicKeyPEM(mustRead(*pubPath))
	if err != nil {
		return err
	}
	now, err := parseOptionalTime(*at)
	if err != nil {
		return err
	}
	if err := contract.VerifyProfileSignature(p, sig, pub, now, 5*time.Minute); err != nil {
		return err
	}
	return writeJSON(os.Stdout, map[string]any{"valid": true, "keyID": sig.KeyID, "contractDigest": p.Seal.ContractDigest})
}
func cmdSelect(args []string) error {
	fs := flag.NewFlagSet("select", flag.ContinueOnError)
	catalogPath := fs.String("catalog", "", "sealed catalog")
	requestPath := fs.String("request", "", "request")
	phaseStr := fs.String("phase", "plan", "plan or runtime")
	metadataPath := fs.String("metadata", "", "DRA metadata for runtime")
	draReq := fs.String("dra-request", "gpu", "DRA request name")
	at := fs.String("at", "", "evaluation time")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := contract.LoadCatalog(mustRead(*catalogPath), true)
	if err != nil {
		return err
	}
	r, err := contract.LoadRequest(mustRead(*requestPath))
	if err != nil {
		return err
	}
	now, err := parseOptionalTime(*at)
	if err != nil {
		return err
	}
	ev := planner.RuntimeEvidence{Now: now, DRARequestName: *draReq}
	if *metadataPath != "" {
		ev.Metadata, err = dra.ReadMetadata(*metadataPath)
		if err != nil {
			return err
		}
	}
	p, d := planner.Select(c, r, planner.Phase(*phaseStr), ev)
	out := map[string]any{"decision": d}
	if p != nil {
		out["selectedProfile"] = p.Metadata.Name
	}
	if err := writeJSON(os.Stdout, out); err != nil {
		return err
	}
	if !d.Admissible {
		return &launcher.RejectionError{Decision: d}
	}
	return nil
}
func cmdRender(args []string) error {
	fs := flag.NewFlagSet("render-k8s", flag.ContinueOnError)
	profilePath := fs.String("profile", "", "sealed profile")
	requestPath := fs.String("request", "", "request")
	sigPath := fs.String("signature", "", "signature")
	pubPath := fs.String("public-key", "", "public key")
	runtimePubPath := fs.String("runtime-public-key-path", "/etc/claim2kernel-trust/public.pem", "trusted public-key path baked into the digest-pinned runner image")
	namespace := fs.String("namespace", "default", "namespace")
	job := fs.String("job", "", "job name")
	image := fs.String("image", "", "digest-pinned runner image")
	queue := fs.String("queue", "default", "Kueue LocalQueue")
	root := fs.String("root", "/opt/c2k", "artifact root in image")
	draReq := fs.String("dra-request", "gpu", "DRA request name")
	driver := fs.String("driver", "", "DRA driver name")
	out := fs.String("out", "", "output JSON; stdout if empty")
	at := fs.String("at", "", "RFC3339 verification time (default now)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := contract.LoadProfile(mustRead(*profilePath), true)
	if err != nil {
		return err
	}
	r, err := contract.LoadRequest(mustRead(*requestPath))
	if err != nil {
		return err
	}
	sig, err := contract.LoadSignature(mustRead(*sigPath))
	if err != nil {
		return err
	}
	now, err := parseOptionalTime(*at)
	if err != nil {
		return err
	}
	b, err := k8smanifest.Render(k8smanifest.Options{Namespace: *namespace, JobName: *job, Image: *image, QueueName: *queue, ArtifactRoot: *root, DRARequestName: *draReq, DRADriverName: *driver, Profile: p, Request: r, Signature: sig, PublicKeyPEM: mustRead(*pubPath), RuntimePublicKeyPath: *runtimePubPath, Now: now})
	if err != nil {
		return err
	}
	if *out == "" {
		_, err = os.Stdout.Write(append(b, '\n'))
		return err
	}
	return atomicWrite(*out, append(b, '\n'), 0o644, true)
}
func cmdLaunch(args []string) error {
	fs := flag.NewFlagSet("launch", flag.ContinueOnError)
	root := fs.String("root", ".", "artifact root")
	profilePath := fs.String("profile", "", "sealed profile")
	requestPath := fs.String("request", "", "request")
	metadataPath := fs.String("metadata", "", "DRA metadata")
	draReq := fs.String("dra-request", "gpu", "DRA request name")
	sigPath := fs.String("signature", "", "signature")
	pubPath := fs.String("public-key", "", "public key")
	requireSig := fs.Bool("require-signature", false, "require a valid signature")
	verifyOnly := fs.Bool("verify-only", false, "verify without executing")
	timeout := fs.Duration("timeout", 30*time.Second, "execution timeout")
	maxOutput := fs.Int("max-output", 1<<20, "stdout/stderr limit each")
	at := fs.String("at", "", "evaluation time")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := contract.LoadProfile(mustRead(*profilePath), true)
	if err != nil {
		return err
	}
	r, err := contract.LoadRequest(mustRead(*requestPath))
	if err != nil {
		return err
	}
	m, err := dra.ReadMetadata(*metadataPath)
	if err != nil {
		return err
	}
	opts := launcher.Options{Root: *root, Profile: p, Request: r, Metadata: m, DRARequestName: *draReq, RequireSignature: *requireSig, VerifyOnly: *verifyOnly, Timeout: *timeout, MaxOutputBytes: *maxOutput}
	opts.Now, err = parseOptionalTime(*at)
	if err != nil {
		return err
	}
	if *sigPath != "" {
		opts.Signature, err = contract.LoadSignature(mustRead(*sigPath))
		if err != nil {
			return err
		}
	}
	if *pubPath != "" {
		opts.SignaturePublicKey, err = contract.ParsePublicKeyPEM(mustRead(*pubPath))
		if err != nil {
			return err
		}
	}
	res, runErr := launcher.Run(opts)
	if err := writeJSON(os.Stdout, res); err != nil {
		return err
	}
	return runErr
}
func cmdInspectMetadata(args []string) error {
	fs := flag.NewFlagSet("inspect-metadata", flag.ContinueOnError)
	path := fs.String("file", "", "metadata file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	m, err := dra.ReadMetadata(*path)
	if err != nil {
		return err
	}
	return writeJSON(os.Stdout, m)
}
func cmdStatecheck(args []string) error {
	fs := flag.NewFlagSet("statecheck", flag.ContinueOnError)
	path := fs.String("trace", "", "trace JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	t, err := statecheck.DecodeTrace(mustRead(*path))
	if err != nil {
		return err
	}
	res := statecheck.Check(t)
	if err := writeJSON(os.Stdout, res); err != nil {
		return err
	}
	if !res.Valid {
		return fmt.Errorf("stateful invariants violated: %s", statecheck.FormatViolations(res.Violations))
	}
	return nil
}

func parseOptionalTime(s string) (time.Time, error) {
	if s == "" {
		return time.Now().UTC(), nil
	}
	return time.Parse(time.RFC3339Nano, s)
}
func mustRead(path string) []byte {
	if path == "" {
		panic(inputReadError{err: errors.New("required file path is empty")})
	}
	b, err := os.ReadFile(path)
	if err != nil {
		panic(inputReadError{err: fmt.Errorf("read %s: %w", path, err)})
	}
	return b
}
func writeJSON(w *os.File, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
func writeExclusive(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
func atomicWriteJSON(path string, v any, mode os.FileMode, overwrite bool) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(b, '\n'), mode, overwrite)
}
func atomicWrite(path string, data []byte, mode os.FileMode, overwrite bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("refusing to overwrite %s", path)
		}
	}
	f, err := os.CreateTemp(dir, ".c2k-tmp-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
