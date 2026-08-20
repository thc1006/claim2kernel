# OOD and statistical validity

## 1. Valid statistical claim

Claim2Kernel makes only this claim:

> A one-sided latency certificate is statistically calibrated for marginal
> coverage under exchangeability and the explicitly declared workload and
> interference envelope.

It does not claim hard real-time execution, conditional coverage for every
feature vector, validity after distribution shift, or universal OOD detection.

## 2. Mandatory data separation

The calibration CSV requires:

```text
sample_id, group_id, split, <numeric features...>, latency_us, numerical_error
```

Enforced controls:

- `sample_id` is globally unique;
- one `group_id` cannot occur in more than one split;
- split names are exactly `train`, `calibration`, or `test`;
- all features and outcomes are finite;
- latency and numerical error are non-negative;
- columns must exactly match the selected feature set;
- file size and row count are bounded.

A group should represent a correlation unit such as one trace, radio run,
node/driver configuration, site, or time block. Splitting individual samples
from one correlated run across sets is prohibited.

## 3. Latency model

1. Fit a standardized ridge model on `train` only.
2. Compute one-sided residuals `y - y_hat` on `calibration` only.
3. Choose either:
   - split-conformal order statistic `ceil((n+1)q)`, or
   - one-sided nonparametric tolerance order statistic for content `q` and
     confidence `γ`.
4. Clamp a negative residual bound to zero, which is conservative.
5. Evaluate observed coverage once on the frozen independent test split.
6. Refuse release when observed test coverage is below the target.

The test gate is evidence, not a new mathematical guarantee. Repeatedly tuning
and retesting against the same holdout invalidates its role; preregister the
pipeline and rotate the test set after a failed release attempt.

## 4. Numerical accuracy

The v1alpha1 numerical certificate uses the maximum calibration error as the
bound and requires the independent test maximum not to exceed it. It is
conservative but not a universal proof for unseen inputs. Production studies
should also report application-level metrics such as EVM, spectral-efficiency
loss, beamforming gain, solver residual, and failure rates.

## 5. OOD guard

The implementation fits a regularized covariance on training features, checks
conditioning, computes Mahalanobis scores, and sets a split-conformal threshold
on calibration scores. The independent in-distribution test inlier rate must
meet the target.

This verifies coverage of the declared inlier population. It does **not** prove
that every foreign distribution will be detected. Mahalanobis distance can miss
near-OOD, multimodal, manifold, semantic, or adversarial shifts.

Defense order:

1. reject undeclared features and categories;
2. reject hard range and cross-feature violations;
3. reject interference outside the measured envelope;
4. reject version/device mismatches;
5. apply the calibrated Mahalanobis inlier guard;
6. monitor post-deployment drift and revoke/recalibrate profiles.

## 6. OOD evaluation matrix

A publishable evaluation should include:

- range extrapolation and unseen categorical modes;
- new antenna/UE/batch relations;
- contention beyond the calibrated load envelope;
- driver, firmware, compiler, and architecture changes;
- MIG/full-GPU partition changes;
- thermal/power throttling and memory pressure;
- temporal and site shift;
- near-OOD points with low Mahalanobis distance;
- covariance ill-conditioning and singular features;
- malicious NaN/Infinity, duplicate keys, huge dimensions, and integer overflow.

Report false-accept and false-reject rates separately. Do not summarize OOD
quality with only the inlier coverage target.
