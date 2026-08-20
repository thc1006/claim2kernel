# Architecture

## 1. Research question

Can a cloud-native accelerator workload be admitted and dispatched based on an
execution contract rather than a device count, while rejecting stale,
out-of-envelope, numerically unsafe, or reinterpreted allocations?

Claim2Kernel separates three time scales:

1. **Offline build and calibration**: compile an artifact and establish a
   statistically calibrated certificate.
2. **Cluster admission**: choose a sealed profile, account quota with Kueue, and
   request a DRA device.
3. **Node-local dispatch**: re-read allocated-device metadata, verify the trust
   chain and artifact, then execute without placing Kubernetes in the kernel's
   per-invocation control loop.

## 2. Components

### Contract issuer

The issuer binds artifact digest, source/dataset provenance, target
architecture, input domain, precision, resources, numerical certificate,
latency certificate, interference envelope, versions, DRA assertions, OOD
model, and policy. The unsealed profile is validated, sealed, and signed.

### Planner

The planner evaluates immutable profile/request inputs. Plan phase checks the
contract without DRA metadata. Runtime phase additionally requires and validates
Kubernetes device metadata. Any missing observation or unknown input is a
rejection.

### Kueue and DRA

Kueue handles quota and workload admission. The kube-scheduler chooses actual
DRA devices. Claim2Kernel deliberately does not assume that quota admission
implies a particular device or SLO. Runtime validation closes that semantic gap.

### Launcher

The launcher verifies certificate freshness and signature, checks the exact
artifact through an opened file descriptor, copies it into a mode-0500 private
directory, and executes that copy without a shell. Linux timeout cancellation
kills the complete process group.

### Stateful checker

Normalized lifecycle traces are checked for durable invariants independent of a
specific controller implementation. This lets later controller, scheduler, and
runtime events be replayed through one checker.

## 3. Contract decision

For a request `x`:

```text
predicted_compute(x)
+ calibrated_residual_upper
+ io_budget
+ runtime_jitter
<= deadline - safety_margin
```

and:

```text
profile.numerical.upperBound <= request.maxNumericalError
```

Admission also requires all hard ranges, categories, relations, interference
ranges, version ranges, DRA assertions, freshness, seal, signature, and OOD
checks to pass.

## 4. Trust boundaries

Trusted inputs:

- issuer public key pinned in the digest-pinned runner image;
- container image digest selected by deployment policy;
- Kubernetes control plane, kubelet, and DRA driver within the cluster threat
  model;
- issuer's measurement and key-management process.

Untrusted or hostile inputs:

- profile, request, signature envelope, and device-metadata JSON;
- artifact path and bytes before verification;
- kernel stdout/stderr and exit behavior;
- catalog ordering and unsupported future metadata objects.

The detached signature authenticates the sealed profile against the pinned
trust key. It is not hardware attestation and does not protect against a fully
privileged node or compromised issuer.

## 5. Versioning

`v1alpha1` is intentionally strict. Unknown JSON fields are rejected. Future
schema versions must use a new `apiVersion`; DRA metadata streams may contain
unknown versions, which are skipped until a supported object is found.

The v1alpha1 canonicalization identifier is `claim2kernel-go-json-v1`:
Go `encoding/json` over the typed profile with `seal` removed. This is stable
inside this implementation but is not RFC 8785 JCS. Cross-language signing must
reproduce those bytes exactly or wait for a versioned canonicalization
migration.
