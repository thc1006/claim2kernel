# Research and publication protocol

## Thesis to test

Device-count admission is insufficient for SLO-critical accelerator workloads;
binding a validated executable contract to the actual DRA allocation prevents
silent numerical, version, architecture, and tail-latency violations.

## Primary hypotheses

- H1: Count-only admission produces false-positive dispatches under heterogeneous
  devices, partitions, version drift, and contention.
- H2: Claim2Kernel reduces false-positive dispatch without unacceptable
  validation overhead.
- H3: A compiler/scheduler contract enables portable kernel variants while
  preserving explicit accuracy and latency budgets.
- H4: Durable lifecycle invariants prevent certificate/capacity resurrection
  after allocation or inventory changes.

## Workload

The first workload is batched complex-valued regularized zero-forcing precoding:

```text
G = H Hᴴ + λI
W = Hᴴ solve(G, I)
```

Use `solve`, not explicit matrix inversion. The complex128 implementation is the
correctness oracle. Candidate implementations must define normalization and
error metrics identically.

## Baselines

- NumPy/BLAS complex128 correctness oracle;
- optimized CPU BLAS;
- CUDA or vendor library and Triton on NVIDIA;
- HIP/rocBLAS/rocWMMA as appropriate on AMD;
- static Mojo with whole device;
- count-only DRA/Kueue admission;
- offline oracle selector as an upper bound;
- full Claim2Kernel.

## Experimental factors

- antennas: 16/32/64/128/256 as hardware permits;
- UEs: 1..64 with `UE <= antenna`;
- batch and precision;
- CPU/full GPU/preconfigured partition;
- idle and controlled co-tenant pressure;
- compiler/driver/container versions;
- cold/warm/restart/reallocation paths;
- in-domain, near-OOD, and explicit out-of-envelope cases.

## Preregistration

Before the frozen test run, commit:

- hypotheses and primary metrics;
- exact case matrix and group split;
- sample-size/rank feasibility calculation;
- warm-up/exclusion rules;
- model features and regularization;
- coverage/content/confidence targets;
- numerical metric and budget;
- success/failure criteria;
- baseline tuning budget;
- handling of timeouts and missing data.

## Required reporting

Report raw sample counts, excluded samples with reasons, hardware/software
fingerprints, all digests, independent coverage, OOD false accepts/rejects,
validation overhead, deadline misses, numerical failures, and negative results.
Synthetic CI data must be visibly separated from hardware evidence.
