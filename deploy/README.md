# Cluster deployment prerequisites

The renderer intentionally does not invent cluster-specific `DeviceClass`, DRA
driver, Kueue quota, or trust configuration.

Before applying a rendered workload, an administrator must provide:

1. Kubernetes v1.36 with required DRA feature gates.
2. A DRA driver that publishes device metadata and the attributes referenced by
   `profile.spec.deviceAssertions`.
3. A `DeviceClass` matching `profile.spec.target.deviceClass`.
4. Kueue v0.19 with DRA integration, a LocalQueue, ClusterQueue quota, and the
   required DeviceClass mapping for the ResourceClaimTemplate path.
5. A digest-pinned runner image containing:
   - `/c2k`;
   - the exact artifact under `--root`;
   - the trusted issuer public key at `--runtime-public-key-path`.
6. RBAC/admission policy preventing users from substituting unapproved images,
   DeviceClasses, queues, or privileged pod settings.

Kueue v0.19 supports `ExactCount` for this path but does not include every DRA
constraint, configuration, topology, taint, prioritized-list, MPS, or time
slicing semantic in quota decisions. Runtime revalidation protects dispatch
safety; it cannot guarantee that every admitted workload becomes schedulable.
