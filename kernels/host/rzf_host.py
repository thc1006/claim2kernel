#!/usr/bin/env python3
"""ctypes host for the c2k_rzf C ABI (kernels/include/c2k_rzf.h).

This is the portable, stable host interface: it loads *either* backend shared
library -- the Mojo GPU kernel (`libc2k_rzf.so`, the deployable target) or the
CUDA reference/baseline (`libc2k_rzf_cuda.so`) -- and exposes an identical
Python surface. Correctness tests, the benchmark harness, and the stdin-json-v1
service all call through here, so a single ABI is exercised regardless of
backend.

Backend selection order (first that exists wins), overridable with C2K_RZF_LIB:
  1) $C2K_RZF_LIB (explicit path)
  2) artifacts/rzf/libc2k_rzf.so          (Mojo target backend)
  3) artifacts/rzf/libc2k_rzf_cuda.so     (CUDA reference/baseline)

Fail-closed: a negative call code raises RuntimeError carrying the backend's
last_error; a per-batch non-OK status leaves that W[b] zeroed (never NaN/Inf).
"""
from __future__ import annotations

import ctypes
import os
from pathlib import Path

import numpy as np

# Per-batch status codes -- identical to c2k_rzf.h and rzf_algorithm.py.
C2K_RZF_OK = 0
C2K_RZF_NON_FINITE_INPUT = 1
C2K_RZF_NOT_POSITIVE_DEFINITE = 2
C2K_RZF_INVALID_SHAPE = 3

C2K_RZF_FLAG_DETERMINISTIC = 1 << 0

_REPO_ROOT = Path(__file__).resolve().parents[2]
_DEFAULT_LIBS = (
    _REPO_ROOT / "artifacts" / "rzf" / "libc2k_rzf.so",
    _REPO_ROOT / "artifacts" / "rzf" / "libc2k_rzf_cuda.so",
)


class Timing(ctypes.Structure):
    _fields_ = [
        ("h2d_ms", ctypes.c_double),
        ("kernel_ms", ctypes.c_double),
        ("d2h_ms", ctypes.c_double),
        ("device_ms", ctypes.c_double),
        ("bytes_h2d", ctypes.c_int64),
        ("bytes_d2h", ctypes.c_int64),
        ("event_timed", ctypes.c_uint32),
        ("reserved", ctypes.c_uint32),
    ]

    def as_dict(self) -> dict:
        return {
            "h2dMs": self.h2d_ms,
            "kernelMs": self.kernel_ms,
            "d2hMs": self.d2h_ms,
            "deviceMs": self.device_ms,
            "bytesH2D": int(self.bytes_h2d),
            "bytesD2H": int(self.bytes_d2h),
            "eventTimed": bool(self.event_timed),
        }


def find_library(explicit: str | None = None) -> Path | None:
    if explicit:
        p = Path(explicit)
        return p if p.exists() else None
    env = os.environ.get("C2K_RZF_LIB")
    if env and Path(env).exists():
        return Path(env)
    for cand in _DEFAULT_LIBS:
        if cand.exists():
            return cand
    return None


class RzfBackend:
    """Persistent RZF context bound to one backend shared library.

    Mirrors the ABI lifecycle: create() allocates the device context and reusable
    buffers for the maximum shape; run() is the allocation-free, JIT-free hot
    path; close() releases everything.
    """

    def __init__(self, lib_path: str | os.PathLike, max_batch: int, max_users: int,
                 max_antennas: int, deterministic: bool = True):
        self.lib_path = Path(lib_path)
        if not self.lib_path.exists():
            raise FileNotFoundError(f"backend library not found: {self.lib_path}")
        self._lib = ctypes.CDLL(str(self.lib_path))
        self._bind()
        self._handle = ctypes.c_void_p()
        self.max_batch, self.max_users, self.max_antennas = max_batch, max_users, max_antennas
        flags = C2K_RZF_FLAG_DETERMINISTIC if deterministic else 0
        code = self._lib.c2k_rzf_create(
            max_batch, max_users, max_antennas, flags, ctypes.byref(self._handle)
        )
        if code != 0:
            raise RuntimeError(f"c2k_rzf_create failed ({code}): {self._err(None)}")

    def _bind(self) -> None:
        lib = self._lib
        lib.c2k_rzf_abi_version.restype = ctypes.c_int32
        lib.c2k_rzf_backend_name.restype = ctypes.c_char_p
        lib.c2k_rzf_required_workspace_bytes.restype = ctypes.c_int64
        lib.c2k_rzf_required_workspace_bytes.argtypes = [ctypes.c_int64] * 3
        lib.c2k_rzf_create.restype = ctypes.c_int32
        lib.c2k_rzf_create.argtypes = [
            ctypes.c_int32, ctypes.c_int32, ctypes.c_int32, ctypes.c_uint32,
            ctypes.POINTER(ctypes.c_void_p),
        ]
        lib.c2k_rzf_run.restype = ctypes.c_int32
        lib.c2k_rzf_run.argtypes = [
            ctypes.c_void_p, ctypes.POINTER(ctypes.c_float),
            ctypes.c_int32, ctypes.c_int32, ctypes.c_int32,
            ctypes.c_float, ctypes.c_uint32,
            ctypes.POINTER(ctypes.c_float), ctypes.POINTER(ctypes.c_int32),
            ctypes.POINTER(Timing),
        ]
        lib.c2k_rzf_destroy.restype = ctypes.c_int32
        lib.c2k_rzf_destroy.argtypes = [ctypes.c_void_p]
        lib.c2k_rzf_last_error.restype = ctypes.c_char_p
        lib.c2k_rzf_last_error.argtypes = [ctypes.c_void_p]

    def _err(self, handle) -> str:
        msg = self._lib.c2k_rzf_last_error(handle)
        return msg.decode("utf-8", "replace") if msg else ""

    @property
    def abi_version(self) -> int:
        return int(self._lib.c2k_rzf_abi_version())

    @property
    def backend_name(self) -> str:
        return self._lib.c2k_rzf_backend_name().decode("utf-8", "replace")

    def workspace_bytes(self, batch: int, users: int, antennas: int) -> int:
        return int(self._lib.c2k_rzf_required_workspace_bytes(batch, users, antennas))

    def run(self, channel: np.ndarray, alpha: float, deterministic: bool = True):
        """Run the precoder. Returns (W[B,N,U] complex64, status[B] int32, timing dict)."""
        h = np.ascontiguousarray(channel, dtype=np.complex64)
        if h.ndim != 3:
            raise ValueError("channel must be [batch, users, antennas]")
        batch, users, antennas = (int(h.shape[0]), int(h.shape[1]), int(h.shape[2]))
        if users > antennas:
            raise ValueError(f"U>N unsupported at ABI: users={users} > antennas={antennas}")
        if batch > self.max_batch or users > self.max_users or antennas > self.max_antennas:
            raise ValueError("shape exceeds create() maxima")

        h_flat = h.view(np.float32).reshape(-1)
        w_flat = np.zeros(batch * antennas * users * 2, dtype=np.float32)
        status = np.zeros(batch, dtype=np.int32)
        timing = Timing()
        flags = C2K_RZF_FLAG_DETERMINISTIC if deterministic else 0
        code = self._lib.c2k_rzf_run(
            self._handle,
            h_flat.ctypes.data_as(ctypes.POINTER(ctypes.c_float)),
            batch, users, antennas, ctypes.c_float(alpha), flags,
            w_flat.ctypes.data_as(ctypes.POINTER(ctypes.c_float)),
            status.ctypes.data_as(ctypes.POINTER(ctypes.c_int32)),
            ctypes.byref(timing),
        )
        if code != 0:
            raise RuntimeError(f"c2k_rzf_run failed ({code}): {self._err(self._handle)}")
        w = w_flat.view(np.complex64).reshape(batch, antennas, users)
        return w, status, timing.as_dict()

    def close(self) -> None:
        if getattr(self, "_handle", None) is not None and self._handle:
            self._lib.c2k_rzf_destroy(self._handle)
            self._handle = ctypes.c_void_p()

    def __enter__(self) -> "RzfBackend":
        return self

    def __exit__(self, *exc) -> None:
        self.close()

    def __del__(self):
        try:
            self.close()
        except Exception:
            pass


def open_backend(max_batch: int, max_users: int, max_antennas: int, *,
                 lib: str | None = None, deterministic: bool = True) -> RzfBackend | None:
    """Open the first available backend, or None if no library is present.

    Returning None (rather than raising) lets callers cleanly record a NOT RUN
    result when neither the Mojo nor the CUDA shared library has been built."""
    path = find_library(lib)
    if path is None:
        return None
    return RzfBackend(path, max_batch, max_users, max_antennas, deterministic)


if __name__ == "__main__":
    import json

    be = open_backend(4, 16, 64)
    if be is None:
        print(json.dumps({"backend": "NOT RUN: no c2k_rzf shared library built"}))
        raise SystemExit(0)
    with be:
        info = {"backend": be.backend_name, "abiVersion": be.abi_version,
                "library": str(be.lib_path)}
        print(json.dumps(info))
