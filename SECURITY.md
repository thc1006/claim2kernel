# Security policy

Claim2Kernel is a research reference implementation, not a security boundary by itself.
Report vulnerabilities privately to the repository maintainers. Do not attach live signing
keys, proprietary kernel artifacts, production DRA metadata, or customer traces to public issues.

The launcher is fail-closed: malformed contracts, stale or unsigned profiles (when signatures
are required), OOD requests, mismatched DRA metadata, digest mismatches, and expired certificates
are rejected. The threat model and residual risks are documented in `docs/THREAT_MODEL.md`.
