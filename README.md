# Claim2Kernel / MojoRAN reference implementation

**Claim2Kernel** is a research reference implementation for binding a compiled
kernel, a Kubernetes DRA allocation, a declared input/interference envelope,
numerical accuracy, and a statistically calibrated latency upper bound into one
fail-closed contract.

The motivating thesis is:

> A logical device count is not an execution guarantee. Dispatch is permitted
> only when the exact artifact, allocated device metadata, input domain,
> software versions, numerical budget, and calibrated tail-latency contract all
> still agree at runtime.

## Current status

- Contract API: `claim2kernel.dev/v1alpha1`
- Implementation version: `0.1.0-research`
- Kubernetes target: v1.36 DRA metadata (`metadata.resource.k8s.io/v1alpha1`)
- Kueue target: v0.19 ResourceClaimTemplate / `ExactCount` path
- Mojo target: **Mojo 1.0 Beta 2 / Modular 26.4**, not a final Mojo 1.0 release
- Local validation: Go/Python/reference workload paths
- Hardware validation: requires a self-hosted NVIDIA or AMD runner

This repository does **not** claim hard real-time behavior, universal OOD
detection, O-RAN conformance, or Mojo GPU performance without retained raw
measurements from the hardware protocol.

## What is implemented

- Strict, size-bounded JSON with duplicate-key rejection.
- `KernelProfile`, `KernelRequest`, `KernelCatalog`, and detached Ed25519
  signature formats.
- Deterministic Go JSON sealing (`claim2kernel-go-json-v1`).
- Input range/category/relation checks and runtime version checks.
- Ridge latency model plus split-conformal or one-sided nonparametric tolerance
  upper residuals.
- Independent test release gate and train/calibration/test leakage controls.
- Hard input/interference envelopes plus regularized Mahalanobis calibrated
  inlier rejection.
- Kubernetes v1.36 DRA device-metadata stream decoder.
- Runtime device assertion validation and exact device-count enforcement.
- Artifact digest/size/mode verification, private staging, no-shell execution,
  output limits, timeout, and Linux descendant process-group termination.
- Kubernetes `ConfigMap`, `ResourceClaimTemplate`, and suspended Kueue `Job`
  renderer.
- Stateful durability checker for five Claim2Kernel invariants.
- Python complex128 RZF correctness oracle and Mojo GPU conformance scaffold.
- Unit, race, fuzz-smoke, statistical, end-to-end, and adversarial test suites.

## Trust model in one diagram

```text
Offline issuer / lab
  measurements -> calibrate.py -> unsealed profile
  source + dataset + artifact digests -> seal -> Ed25519 signature
                                      |
                                      v
Digest-pinned runner image -------- trusted public key baked into image
  c2k launcher                       verified artifact under /opt/c2k
                                      |
Kueue quota admission                |
  -> kube-scheduler DRA allocation   |
  -> kubelet device metadata --------+
                                      |
                                      v
runtime revalidation -> private staged executable -> stdin-json-v1
```

The public key is deliberately **not** embedded beside the contract in its
workload ConfigMap. Doing so would let a party able to replace the ConfigMap
replace both the profile and its trust key. The runtime key must be baked into
the digest-pinned runner image, or supplied by an equivalently protected
cluster trust mechanism.

## Quick start

Prerequisites for the local reference path:

- Go 1.23 or newer (CI uses Go 1.26.x)
- Python 3.11 or newer
- NumPy 2.x
- `jq`, `zip`, and `rsync` for the full release scripts

```bash
python3 -m venv .venv
. .venv/bin/activate
pip install -r tools/calibration/requirements-dev.txt

make verify
make validate-release
make package
```

Run the signed local demo:

```bash
./scripts/run_demo.sh
```

Run all negative cases:

```bash
./scripts/adversarial_suite.sh
cat artifacts/adversarial-results.tsv
```

Calibrate the synthetic CI-only RZF profile:

```bash
./scripts/calibrate_smoke.sh
```

The generated synthetic report is pipeline evidence only. It must not appear
in a paper as hardware performance evidence.

## Core CLI

```text
c2k validate             strict structural and semantic validation
c2k seal                 bind the complete profile to digests
c2k keygen               create an Ed25519 signing key pair
c2k sign                 sign a sealed profile
c2k verify-signature     verify detached signature and timestamps
c2k select               evaluate a catalog in plan/runtime phase
c2k render-k8s           render v1.36 DRA + suspended Kueue Job JSON
c2k launch               runtime revalidation, artifact staging, execution
c2k inspect-metadata     decode Kubernetes DRA device metadata
c2k statecheck           check lifecycle traces against durable invariants
```

## Statistical contract

For request features `x`, Claim2Kernel evaluates:

```text
U(x) + IO_budget + runtime_jitter <= deadline - safety_margin
numerical_upper_bound <= request_error_budget
```

`U(x)` is a fitted latency prediction plus a held-out calibration residual
order statistic. The valid claim is deliberately limited to:

> statistically calibrated marginal SLO coverage under exchangeability and the
> declared workload/interference envelope.

The independent test split is not used to fit the model or choose the bound. It
is a release gate. Exact `sample_id` uniqueness and `group_id` split isolation
are enforced to prevent obvious measurement leakage.

See [docs/OOD_AND_STATISTICS.md](docs/OOD_AND_STATISTICS.md).

## Kubernetes rendering

After producing a sealed and signed profile:

```bash
c2k render-k8s \
  --profile profile-sealed.json \
  --request request.json \
  --signature profile-signature.json \
  --public-key issuer-public.pem \
  --runtime-public-key-path /etc/claim2kernel-trust/public.pem \
  --image ghcr.io/ORG/RUNNER@sha256:... \
  --driver gpu.example.com \
  --job rzf-a100 \
  --queue research \
  --out workload.json
```

`--public-key` is used locally to cryptographically validate the manifest
inputs. `--runtime-public-key-path` points to the trust anchor already present
inside the digest-pinned image. Cluster-specific `DeviceClass`, driver, Kueue
mapping, quota, feature gates, and trust-key provisioning remain administrator
responsibilities; see [deploy/README.md](deploy/README.md).

## Mojo hardware path

```bash
MOJO_TARGET_ACCELERATOR=nvidia:sm_90 ./kernels/mojo/build.sh
MOJO_TARGET_ACCELERATOR=amdgpu:gfx942 ./kernels/mojo/build.sh
```

The included Mojo code is a conformance probe and C ABI scaffold. The optimized
RZF Mojo kernel is intentionally left as the first measured research milestone;
no fabricated GPU result or uncompiled source is labelled complete. Follow
[docs/MOJO_HARDWARE_PROTOCOL.md](docs/MOJO_HARDWARE_PROTOCOL.md).

## Repository layout

```text
cmd/c2k/                  reference CLI
pkg/contract/             v1alpha1 types, validation, seals, signatures
pkg/planner/              deterministic admission logic
pkg/dra/                  Kubernetes device-metadata decoder
pkg/artifact/             artifact verification and private staging
pkg/launcher/             runtime execution boundary
pkg/k8smanifest/          DRA/Kueue workload renderer
pkg/statecheck/           lifecycle invariant checker
tools/calibration/        statistical calibration and independent test gate
kernels/rzf/              complex128 oracle and CPU smoke benchmark
kernels/mojo/             Mojo 1.0 Beta 2 conformance/C ABI scaffold
schemas/                  Draft 2020-12 structural schema
examples/                 positive, OOD, metadata, and state-trace corpus
docs/                     specification, threat model, research protocol
```

## Explicit non-goals of v0.1

- A long-running Kubernetes operator or admission webhook.
- Automatic traversal of `fallbackProfiles`; the field is reserved and any
  non-empty value is rejected.
- Hardware attestation of GPU identity or firmware.
- Dynamic MIG reconfiguration in the request path.
- Conditional coverage guarantees for every individual input.
- A universal semantic OOD detector.
- Hard real-time guarantees from a general-purpose Linux/GPU/Kubernetes stack.

## License

Apache License 2.0. See [LICENSE](LICENSE).
