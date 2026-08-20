import csv
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import numpy as np

class CalibrateCLITest(unittest.TestCase):
    def test_end_to_end_without_test_leakage(self):
        with tempfile.TemporaryDirectory() as td:
            td = Path(td)
            template = td / "template.json"
            data = td / "data.csv"
            out = td / "profile.json"
            report = td / "report.json"
            template.write_text(json.dumps(_template()))
            rng = np.random.default_rng(42)
            rows = []
            counts = {"train": 240, "calibration": 240, "test": 120}
            sample_index = 0
            for split, count in counts.items():
                for local_index in range(count):
                    # Test rows are intentionally narrower; calibration rows
                    # are intentionally noisier. This test verifies plumbing,
                    # while exact rank properties are tested separately.
                    batch = int(rng.integers(8, 65 if split != "test" else 49))
                    ues = int(rng.integers(4, 25 if split != "test" else 20))
                    pressure = float(rng.uniform(0.02, 0.35 if split != "test" else 0.25))
                    noise_scale = 7.0 if split == "calibration" else 3.0
                    latency = 20 + 0.4 * batch + 1.1 * ues + 90 * pressure + abs(rng.normal(0, noise_scale))
                    numerical = float(rng.uniform(0, 9e-4 if split == "calibration" else 4e-4))
                    rows.append({
                        "sample_id": f"{split}-{sample_index}",
                        "group_id": f"{split}-run-{local_index // 20}",
                        "split": split,
                        "input.batch": batch,
                        "input.ues": ues,
                        "interference.cpu_pressure": pressure,
                        "latency_us": latency,
                        "numerical_error": numerical,
                    })
                    sample_index += 1
            with data.open("w", newline="") as f:
                writer = csv.DictWriter(f, fieldnames=list(rows[0]))
                writer.writeheader(); writer.writerows(rows)
            script = Path(__file__).parents[1] / "calibrate.py"
            env = os.environ.copy(); env["PYTHONPATH"] = str(script.parent)
            proc = subprocess.run([
                sys.executable, str(script), "--template", str(template), "--csv", str(data),
                "--out", str(out), "--report", str(report), "--coverage", "0.95",
                "--confidence", "0.95", "--compiler", "mojo 1.0.0b2",
                "--source-digest", "sha256:" + "b" * 64,
                "--calibrated-at", "2026-08-20T00:00:00Z",
                "--io-budget-us", "5", "--runtime-jitter-us", "10",
            ], env=env, text=True, capture_output=True)
            self.assertEqual(proc.returncode, 0, proc.stderr)
            profile = json.loads(out.read_text()); rep = json.loads(report.read_text())
            self.assertNotIn("seal", profile)
            self.assertGreaterEqual(profile["spec"]["latency"]["observedCoverage"], 0.95)
            self.assertGreaterEqual(profile["spec"]["ood"]["observedTestInlierRate"], 0.95)
            self.assertFalse(rep["claims"]["testUsedForFitting"])
            self.assertTrue(rep["dataHygiene"]["sampleIDsUniqueAcrossAllSplits"])
            self.assertTrue(rep["dataHygiene"]["groupsDisjointAcrossSplits"])
            self.assertEqual(profile["metadata"]["createdAt"], "2026-08-20T00:00:00.000000Z")
            self.assertEqual(rep["splitCounts"], counts)

    def test_rejects_group_crossing_splits(self):
        with tempfile.TemporaryDirectory() as td:
            td = Path(td)
            template = td / "template.json"
            data = td / "data.csv"
            out = td / "profile.json"
            report = td / "report.json"
            template.write_text(json.dumps(_template()))
            fields = ["sample_id", "group_id", "split", "input.batch", "input.ues", "interference.cpu_pressure", "latency_us", "numerical_error"]
            rows = [
                {"sample_id": "a", "group_id": "same-run", "split": "train", "input.batch": 8, "input.ues": 4, "interference.cpu_pressure": 0.1, "latency_us": 10, "numerical_error": 0.0},
                {"sample_id": "b", "group_id": "same-run", "split": "test", "input.batch": 8, "input.ues": 4, "interference.cpu_pressure": 0.1, "latency_us": 10, "numerical_error": 0.0},
            ]
            with data.open("w", newline="") as f:
                writer = csv.DictWriter(f, fieldnames=fields); writer.writeheader(); writer.writerows(rows)
            script = Path(__file__).parents[1] / "calibrate.py"
            env = os.environ.copy(); env["PYTHONPATH"] = str(script.parent)
            proc = subprocess.run([
                sys.executable, str(script), "--template", str(template), "--csv", str(data),
                "--out", str(out), "--report", str(report), "--compiler", "mojo 1.0.0b2",
                "--source-digest", "sha256:" + "b" * 64,
            ], env=env, text=True, capture_output=True)
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("crosses splits", proc.stderr)

def _template():
    return {
        "apiVersion":"claim2kernel.dev/v1alpha1","kind":"KernelProfile",
        "metadata":{"name":"rzf-smoke","version":"0.1.0","createdAt":"2026-08-20T00:00:00Z","expiresAt":"2027-01-01T00:00:00Z"},
        "spec":{
            "artifact":{"path":"artifacts/rzf","digest":"sha256:"+"a"*64,"sizeBytes":1,"maxBytes":1048576,"protocol":"stdin-json-v1"},
            "target":{"backend":"cuda","vendor":"nvidia","architecture":"sm_90","deviceClass":"gpu.example.com"},
            "inputDomain":{"features":{"batch":{"kind":"integer","required":True,"minimum":1,"maximum":128,"oodFeature":True},"ues":{"kind":"integer","required":True,"minimum":1,"maximum":64,"oodFeature":True}}},
            "precision":{"storage":"fp32","accumulation":"fp32"},"resources":{"deviceCount":1,"minMemoryBytes":0},
            "numerical":{"metric":"relative_l2","upperBound":1,"observedMax":0,"testSampleCount":1},
            "latency":{"method":"split-conformal","quantile":0.9,"confidence":0.9,"residualUpperUS":0,"ioBudgetUS":0,"runtimeJitterUS":0,"calibrationSampleCount":1,"testSampleCount":1,"observedCoverage":1,"model":{"interceptUS":0,"coefficients":{"input.batch":0,"input.ues":0,"interference.cpu_pressure":0},"featureOrder":["input.batch","input.ues","interference.cpu_pressure"],"ridgeLambda":1e-6},"calibratedAt":"2026-08-20T00:00:00Z","maxAgeSeconds":1000},
            "interference":{"metrics":{"cpu_pressure":{"minimum":0,"maximum":0.5}}},
            "versions":{"mojo":">=1.0.0b2,<1.1.0"},"ood":{"required":False},
            "policy":{"failClosed":True},"provenance":{"compiler":"unset","sourceDigest":"sha256:"+"b"*64,"datasetDigest":"sha256:"+"c"*64}
        }
    }

if __name__ == "__main__": unittest.main()
