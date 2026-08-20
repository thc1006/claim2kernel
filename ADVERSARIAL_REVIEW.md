# Max-rigor adversarial review

## Review disposition

The v0.1 reference implementation is suitable as a research prototype and
artifact foundation. It is not yet a production operator or validated GPU
system.

## High-severity findings fixed during review

1. **Trust-anchor co-location**: public key was initially placed with the
   profile/signature ConfigMap. Fixed by local cryptographic verification plus a
   runtime key path baked into the digest-pinned image.
2. **Reseal bypass misconception**: a profile could be changed and re-sealed.
   Detached Ed25519 binding now makes the old signature invalid.
3. **Verify/execute race**: execution originally risked using mutable path bytes.
   Fixed by hashing and copying the same opened descriptor into private staging.
4. **Child-process escape on timeout**: Linux cancellation now kills the process
   group and is covered by a regression test.
5. **OOD matrix ambiguity**: square-only validation could admit indefinite
   matrices and negative scores. Fixed with finite symmetry and Cholesky
   positive-definiteness checks.
6. **Data leakage**: split labels alone did not prevent duplicate/correlated runs
   crossing sets. Fixed with unique `sample_id` and split-disjoint `group_id`.
7. **Image/profile mismatch**: renderer now requires exact equality between the
   requested digest-pinned image and the container digest sealed in the profile.
8. **Generated-name digest loss**: long Kubernetes names could truncate the
   identifying digest suffix. Fixed by reserving suffix length before truncation.
9. **Silent fallback semantics**: non-empty `fallbackProfiles` were accepted but
   not used. They are now rejected in v1alpha1.
10. **Integer precision confusion**: JSON/DRA integers beyond binary64 exact range
    are now rejected/fail closed rather than rounded.
11. **Certificate time incoherence**: creation, calibration, seal, signature,
    expiration, and current time are cross-checked with bounded skew.
12. **Unbounded metadata/profile dimensions**: explicit caps were added before
    expensive matrix and collection operations.

## Medium-severity limitations retained and documented

- Go canonical JSON is not RFC 8785.
- DRA metadata is authenticated only by the cluster trust boundary, not remote
  attestation.
- Mahalanobis is not a universal OOD detector.
- Independent-test gating can be invalidated by repeated post-failure tuning.
- Request intent is not signed in v0.1.
- Non-Linux builds lack descendant process-group termination.
- Kueue quota admission cannot guarantee the later device allocation.
- No controller persists decisions or automatically revokes live profiles.
- No optimized RZF Mojo kernel or real GPU measurements are claimed.

## Release gates

A release archive is generated only after:

- `gofmt`, `go vet`, unit tests and race detector pass;
- Python statistical tests pass;
- signed end-to-end demo passes;
- all adversarial cases fail for the expected reason;
- calibration and RZF smoke paths pass;
- strict JSON and DRA decoders complete fuzz smoke;
- schema examples validate;
- the validation report explicitly records Mojo/GPU and Docker as passed or not
  run, never silently assumed.
