SHELL := /usr/bin/env bash
.ONESHELL:
.SHELLFLAGS := -euo pipefail -c

.PHONY: all fmt vet test race py-test demo adversarial calibrate-smoke rzf-smoke verify validate-release clean package verify-package fuzz-smoke schema \
	rzf-oracle rzf-cuda rzf-tests rzf-bench rzf-compare rzf-dataset rzf-bind rzf-mojo

# RZF kernel paths (not part of `verify`; need NumPy, and optionally nvcc/mojo).
# PY defaults to python3; override with a NumPy-capable interpreter if needed.
PY ?= python3
RZFPP := PYTHONPATH=kernels/rzf:kernels/host:kernels/baselines
RZFLIB ?= artifacts/rzf/libc2k_rzf_cuda.so

rzf-oracle:
	PYTHONPATH=kernels/rzf $(PY) kernels/rzf/rzf_algorithm.py

rzf-cuda:
	./kernels/cuda/build.sh artifacts/rzf

rzf-tests:
	C2K_RZF_LIB=$(RZFLIB) $(RZFPP) $(PY) -m unittest -v kernels.rzf.tests.test_rzf_correctness

rzf-bench:
	$(PY) kernels/host/bench_rzf.py --smoke --out artifacts/rzf/bench.json --csv artifacts/rzf/bench.csv --backend-lib $(RZFLIB)

rzf-compare:
	$(PY) kernels/host/compare.py --out artifacts/rzf/comparison.json --backend-lib $(RZFLIB)

rzf-dataset:
	PYTHONPATH=kernels/rzf $(PY) kernels/rzf/generate_correctness_dataset.py --out artifacts/rzf/correctness-dataset.json

rzf-bind: rzf-dataset
	PYTHONPATH=tools/calibration $(PY) tools/calibration/bind_rzf_provenance.py \
		--profile examples/profile-rzf-template.json --source kernels/mojo/rzf_kernel.mojo \
		--dataset artifacts/rzf/correctness-dataset.json --out artifacts/rzf/profile-rzf-bound.json

rzf-mojo:
	MOJO_TARGET_ACCELERATOR=$${MOJO_TARGET_ACCELERATOR:-nvidia:sm_90} ./kernels/mojo/build_rzf.sh artifacts/mojo

all: verify

fmt:
	gofmt -w cmd pkg kernels/demo
	python3 -m compileall -q tools kernels/rzf

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...

py-test:
	PYTHONPATH=tools/calibration python3 -m unittest discover -s tools/calibration/tests -v

schema:
	python3 tools/schema_check.py

demo:
	./scripts/run_demo.sh

adversarial:
	./scripts/adversarial_suite.sh

calibrate-smoke:
	./scripts/calibrate_smoke.sh

rzf-smoke:
	python3 kernels/rzf/rzf_reference.py --self-test
	python3 kernels/rzf/benchmark.py --smoke --out artifacts/rzf-smoke.json

fuzz-smoke:
	go test ./pkg/jsonsafe -run '^$$' -fuzz FuzzDecodeStrict -fuzztime 3s
	go test ./pkg/dra -run '^$$' -fuzz FuzzDecodeMetadataStream -fuzztime 3s

verify: fmt vet test py-test schema demo adversarial calibrate-smoke rzf-smoke

validate-release:
	./scripts/validate_release.sh

clean:
	rm -rf dist artifacts/*
	touch artifacts/.gitkeep

package:
	./scripts/package.sh

verify-package: package
	./scripts/verify_package.sh dist/claim2kernel-reference-v$${VERSION:-0.1.0}.zip
