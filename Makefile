SHELL := /usr/bin/env bash
.ONESHELL:
.SHELLFLAGS := -euo pipefail -c

.PHONY: all fmt vet test race py-test demo adversarial calibrate-smoke rzf-smoke verify validate-release clean package verify-package fuzz-smoke schema

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
