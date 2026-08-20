# Requirement-to-implementation traceability

| Requirement | Implementation | Primary verification |
|---|---|---|
| Strict JSON / duplicate rejection | `pkg/jsonsafe`, `tools/calibration/jsonsafe.py` | Go fuzz/unit, Python unit, adversarial corpus |
| Contract field and cross-field validity | `pkg/contract/validate.go` | `pkg/contract/*_test.go` |
| Artifact/model/profile binding | `pkg/contract/digest.go`, `pkg/artifact` | seal and artifact tamper cases |
| Detached trust | `pkg/contract/signature.go` | wrong key, old signature, future signature tests |
| No co-located trust anchor | `pkg/k8smanifest` | manifest unit test rejects embedded key pattern |
| Input generalization boundary | `pkg/planner` | range/category/relation/unknown-input cases |
| Statistical upper bound | `tools/calibration/c2k_stats.py` | rank property tests |
| No split leakage | `tools/calibration/calibrate.py` | unique ID/group crossing tests |
| OOD guard | calibration + planner | OOD unit/adversarial cases |
| DRA v1.36 metadata | `pkg/dra` | stream, unknown version, union, fuzz tests |
| Runtime device semantics | `pkg/planner.validateDRA` | wrong arch/missing attr/count cases |
| Safe execution | `pkg/artifact`, `pkg/launcher` | digest/mode/path/output/timeout tests |
| Descendant cleanup | `pkg/launcher/process_linux.go` | Linux process-group test |
| Kueue/DRA manifest | `pkg/k8smanifest` | renderer tests and digest/name checks |
| Durable semantics | `pkg/statecheck` | five negative traces plus valid trace |
| RZF correctness oracle | `kernels/rzf` | self-test and smoke benchmark |
| Mojo API conformance scaffold | `kernels/mojo` | self-hosted hardware workflow only |
