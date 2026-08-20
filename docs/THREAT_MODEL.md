# Threat model

## Protected properties

- An unsigned or differently signed profile cannot be dispatched.
- Modified artifact bytes cannot be executed under an old certificate.
- A stale or expired certificate cannot be reused.
- A device allocation cannot be silently reinterpreted after admission.
- Precision/error budgets cannot be silently downgraded.
- Out-of-envelope requests fail closed.
- A timed-out kernel cannot intentionally leave ordinary child processes alive
  on the Linux target.

## Adversary capabilities considered

- Supplies malformed, duplicate-key, deeply nested, oversized, or future-version
  JSON.
- Changes the profile after sealing, or re-seals it while retaining an old
  signature.
- Swaps or edits an artifact between verification and execution.
- Supplies symlink/traversal paths and writable executables.
- Supplies wrong DRA device count, missing attributes, wrong architecture, or
  union-confused metadata.
- Tries unknown inputs/categories, version drift, non-finite values, OOD points,
  or a deadline too small for the certificate.
- Produces unlimited output, hangs, or forks descendants.
- Replays lifecycle state to resurrect a revoked certificate.

## Mitigations

- Strict size-bounded decoding and unknown-field rejection.
- SHA-256 contract/model/artifact/source/dataset binding.
- Detached Ed25519 signature with domain separation and timestamp checks.
- Public trust key outside the workload ConfigMap and inside a digest-pinned
  runner image.
- Resolved-root containment, regular/executable/non-writable mode checks, hashing
  the same opened file descriptor, and private staged execution.
- No shell interpolation; bounded arguments, output, timeout, and process group.
- Runtime DRA metadata revalidation and exact assertion semantics.
- Stateful invariant checker and tombstones for revoked contract digests.

## Residual risks

- A compromised issuer or stolen signing key can issue malicious contracts.
- A privileged node, kubelet, DRA driver, kernel, or container runtime can forge
  metadata or execution evidence; there is no TPM/GPU attestation in v0.1.
- A namespace administrator can alter the request/Job. Admission policy and RBAC
  remain necessary.
- The source path can still be manipulated by a fully privileged local attacker;
  staging closes ordinary verify/execute TOCTOU but is not a substitute for an
  immutable image, dm-verity, IMA, or `openat2`-based confinement.
- Non-Linux development builds do not kill descendant process groups.
- General-purpose Linux, drivers, and GPU queues do not provide hard real-time
  guarantees.
- Go JSON canonicalization is implementation-specific, not RFC 8785.
- OOD and latency guarantees fail under violated exchangeability or undeclared
  shift.
- Kueue admission and kube-scheduler allocation remain separate decisions;
  runtime rejection can preserve safety but may reduce liveness.
