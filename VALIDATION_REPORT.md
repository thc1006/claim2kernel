# Validation report

- Started: 2026-08-20T01:01:51Z
- Completed: 2026-08-20T01:02:12Z
- Go: go version go1.23.2 linux/amd64
- Python: Python 3.13.5
- Unit tests: passed
- Race detector: passed
- Python statistical tests: passed
- End-to-end signed launch: passed
- Adversarial cases: 31 passed
- Stateful invariants: passed for valid trace; all five negative traces rejected
- Fuzz smoke: 3 seconds each for strict JSON and DRA metadata decoders
- RZF complex128/complex64 oracle smoke: passed
- Mojo/GPU: NOT RUN: mojo executable and supported GPU were unavailable in this environment.
- Container build: NOT RUN: docker executable was unavailable in this environment.

This report is evidence about the reference implementation in this environment.
It is **not** evidence of Mojo GPU performance, tail-latency coverage on production
hardware, O-RAN conformance, or hard real-time behavior. Those claims require the
self-hosted hardware protocol in the repository and independently retained raw
measurements.

Full command log: `artifacts/validation/full.log` (generated locally and excluded
from release archives).
