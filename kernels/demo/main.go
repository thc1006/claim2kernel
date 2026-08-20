// Command demo-kernel is a deterministic protocol conformance artifact. It is
// not performance evidence and must never be used to claim Mojo/GPU results.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/thc1006/claim2kernel/pkg/contract"
)

func main() {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		fail(err)
	}
	req, err := contract.LoadRequest(data)
	if err != nil {
		fail(err)
	}
	keys := make([]string, 0, len(req.Spec.Inputs))
	for k := range req.Spec.Inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	work := 1.0
	for _, k := range keys {
		if n, ok := req.Spec.Inputs[k].Number(); ok {
			work *= n
		}
	}
	sum := sha256.Sum256(data)
	out := map[string]any{"protocol": "stdin-json-v1", "request": req.Metadata.Name, "workUnits": work, "requestSHA256": hex.EncodeToString(sum[:]), "contractDigest": os.Getenv("C2K_CONTRACT_DIGEST"), "artifactDigest": os.Getenv("C2K_ARTIFACT_DIGEST")}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, "demo-kernel:", err); os.Exit(2) }
