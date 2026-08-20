# Claim2Kernel v1alpha1 contract specification

## 1. Mapping to C = (h, a, S, P, M, E, Q, Γ, V)

| Symbol | v1alpha1 fields | Meaning |
|---|---|---|
| `h` | `artifact`, `provenance`, `seal`, detached signature | Exact executable/container/source identity |
| `a` | `target`, `deviceAssertions` | Backend, vendor, architecture, DeviceClass and runtime attributes |
| `S` | `inputDomain.features`, `relations`, `ood` | Hard and calibrated input domain |
| `P` | `precision` | Storage and accumulation precision |
| `M` | `resources` | Device count, minimum memory and declared counters |
| `E` | `numerical` | Error metric and release-gated upper bound |
| `Q` | `latency` | Model, residual order statistic, budgets and freshness |
| `Γ` | `interference` | Declared calibration/load envelope |
| `V` | `versions` | Compiler, driver, Kubernetes and runtime ranges |

## 2. Profile lifecycle

```text
unsealed -> validated -> sealed -> signed -> admitted -> runtime-validated
                                 \-> revoked / expired / stale
```

A change to any typed profile field invalidates the contract digest. A changed
artifact invalidates both the artifact digest and detached signature binding.
Re-sealing a modified profile does not make the old signature valid.

## 3. Required runtime evidence

Runtime phase requires:

- current time;
- all interference metrics used by the profile;
- all software versions constrained by the profile;
- one supported Kubernetes DRA metadata object;
- the exact DRA request name;
- every required device attribute;
- the artifact under the configured root;
- detached signature and the pinned issuer public key when signature enforcement
  is enabled (mandatory in rendered Kubernetes Jobs).

## 4. Input semantics

- Unknown request inputs are rejected.
- Missing required inputs are rejected.
- Numeric values must be finite.
- Integer-valued inputs must have no fractional component.
- JSON integer literals beyond IEEE-754's exact range `[-(2^53-1),2^53-1]` are
  rejected rather than rounded.
- Categories must have appeared in the calibrated category set.
- Relations are numeric only and are checked after type/range validation.

## 5. OOD semantics

The primary OOD boundary is the explicit hard domain and interference envelope.
The Mahalanobis score is a secondary calibrated inlier-region heuristic. A
request is rejected when required features are missing, the score is invalid,
or it exceeds the calibrated threshold.

## 6. Freshness

A profile is invalid before its creation/seal/calibration time (allowing five
minutes of clock skew), at or after `expiresAt`, or when the calibration age
exceeds `latency.maxAgeSeconds`.

## 7. Reserved behavior

`policy.fallbackProfiles` is reserved. Non-empty values are rejected in
v1alpha1 because automatic fallback traversal is not implemented. Silent
acceptance would falsely imply that a numerically and temporally valid alternate
profile will be chosen.

## 8. Stable rejection codes

Examples include:

- `INPUT_OUT_OF_RANGE`, `UNSEEN_CATEGORY`, `RELATION_VIOLATION`
- `INTERFERENCE_OUT_OF_ENVELOPE`, `UNSUPPORTED_VERSION`
- `NUMERICAL_BUDGET_UNSATISFIED`, `LATENCY_SLO_UNSATISFIED`
- `OOD_REJECTED`, `STALE_CERTIFICATE`
- `DRA_DEVICE_COUNT_MISMATCH`, `DRA_ATTRIBUTE_MISSING`,
  `DRA_ATTRIBUTE_MISMATCH`

The decision contains all detected reasons in deterministic code/field order.
