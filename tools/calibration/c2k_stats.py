"""Statistical primitives for Claim2Kernel certificates.

The functions are deterministic and intentionally avoid SciPy so the reference
artifact can be reproduced with only NumPy and the Python standard library.
"""
from __future__ import annotations

import hashlib
import math
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable

import numpy as np


@dataclass(frozen=True)
class RidgeModel:
    intercept: float
    coefficients: np.ndarray
    feature_mean: np.ndarray
    feature_scale: np.ndarray
    ridge_lambda: float

    def predict(self, x: np.ndarray) -> np.ndarray:
        x = np.asarray(x, dtype=np.float64)
        return self.intercept + x @ self.coefficients


def sha256_file(path: str | Path) -> str:
    h = hashlib.sha256()
    with Path(path).open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return "sha256:" + h.hexdigest()


def fit_ridge(x: np.ndarray, y: np.ndarray, ridge_lambda: float) -> RidgeModel:
    x = np.asarray(x, dtype=np.float64)
    y = np.asarray(y, dtype=np.float64)
    if x.ndim != 2 or y.ndim != 1 or x.shape[0] != y.shape[0] or x.shape[0] < 2:
        raise ValueError("ridge inputs must be finite X[n,d], y[n] with n >= 2")
    if not np.isfinite(x).all() or not np.isfinite(y).all():
        raise ValueError("ridge inputs contain non-finite values")
    if ridge_lambda <= 0 or not math.isfinite(ridge_lambda):
        raise ValueError("ridge_lambda must be finite and > 0")
    mean = x.mean(axis=0)
    scale = x.std(axis=0, ddof=1)
    scale = np.where(scale > 1e-12, scale, 1.0)
    xz = (x - mean) / scale
    yc = y - y.mean()
    gram = xz.T @ xz + ridge_lambda * np.eye(x.shape[1], dtype=np.float64)
    beta_z = np.linalg.solve(gram, xz.T @ yc)
    beta = beta_z / scale
    intercept = float(y.mean() - mean @ beta)
    return RidgeModel(intercept, beta, mean, scale, ridge_lambda)


def conformal_rank(n: int, coverage: float) -> int:
    """Return the 1-indexed split-conformal order-statistic rank.

    ceil((n+1)*coverage) must be <= n; otherwise the requested finite-sample
    coverage is unattainable without returning +infinity.
    """
    _validate_probability(coverage, "coverage")
    if n <= 0:
        raise ValueError("n must be > 0")
    rank = math.ceil((n + 1) * coverage)
    if rank > n:
        raise ValueError(
            f"{n} calibration samples cannot support coverage {coverage}; "
            f"need at least {math.ceil(coverage / (1 - coverage))}"
        )
    return rank


def split_conformal_upper(values: Iterable[float], coverage: float) -> tuple[float, int]:
    a = _finite_vector(values)
    rank = conformal_rank(a.size, coverage)
    return float(np.partition(a, rank - 1)[rank - 1]), rank


def binomial_cdf(k: int, n: int, p: float) -> float:
    """Stable P[Binomial(n,p) <= k] using log-sum-exp."""
    _validate_probability(p, "p")
    if n < 0 or k < -1:
        raise ValueError("invalid binomial arguments")
    if k < 0:
        return 0.0
    if k >= n:
        return 1.0
    logs = []
    for i in range(k + 1):
        log_pmf = (
            math.lgamma(n + 1)
            - math.lgamma(i + 1)
            - math.lgamma(n - i + 1)
            + i * math.log(p)
            + (n - i) * math.log1p(-p)
        )
        logs.append(log_pmf)
    m = max(logs)
    return min(1.0, math.exp(m) * sum(math.exp(v - m) for v in logs))


def tolerance_rank(n: int, content: float, confidence: float) -> int:
    """Smallest 1-indexed rank k with P[F(X_(k)) >= content] >= confidence."""
    _validate_probability(content, "content")
    _validate_probability(confidence, "confidence")
    if n <= 0:
        raise ValueError("n must be > 0")
    for k in range(1, n + 1):
        if binomial_cdf(k - 1, n, content) + 1e-15 >= confidence:
            return k
    raise ValueError(
        f"{n} samples cannot certify content={content} at confidence={confidence}; "
        "increase the calibration split"
    )


def tolerance_upper(
    values: Iterable[float], content: float, confidence: float
) -> tuple[float, int, float]:
    a = _finite_vector(values)
    rank = tolerance_rank(a.size, content, confidence)
    achieved = binomial_cdf(rank - 1, a.size, content)
    return float(np.partition(a, rank - 1)[rank - 1]), rank, achieved


@dataclass(frozen=True)
class MahalanobisModel:
    mean: np.ndarray
    inverse_covariance: np.ndarray
    regularization: float
    condition_number: float

    def score(self, x: np.ndarray) -> np.ndarray:
        x = np.asarray(x, dtype=np.float64)
        if x.ndim == 1:
            x = x.reshape(1, -1)
        delta = x - self.mean
        return np.einsum("ni,ij,nj->n", delta, self.inverse_covariance, delta)


def fit_mahalanobis(x: np.ndarray, regularization: float) -> MahalanobisModel:
    x = np.asarray(x, dtype=np.float64)
    if x.ndim != 2 or x.shape[0] < max(3, x.shape[1] + 1):
        raise ValueError("Mahalanobis fit needs at least max(3, d+1) rows")
    if not np.isfinite(x).all():
        raise ValueError("OOD training data contains non-finite values")
    if regularization <= 0 or not math.isfinite(regularization):
        raise ValueError("regularization must be finite and > 0")
    mean = x.mean(axis=0)
    cov = np.atleast_2d(np.cov(x, rowvar=False, ddof=1))
    diagonal = np.maximum(np.diag(cov), 1e-12)
    regularized = cov + regularization * np.diag(diagonal)
    condition = float(np.linalg.cond(regularized))
    if not math.isfinite(condition) or condition > 1e12:
        raise ValueError(f"regularized covariance remains ill-conditioned: {condition:.3e}")
    inverse = np.linalg.inv(regularized)
    return MahalanobisModel(mean, inverse, regularization, condition)


def empirical_coverage(actual: np.ndarray, upper: np.ndarray) -> float:
    actual = np.asarray(actual, dtype=np.float64)
    upper = np.asarray(upper, dtype=np.float64)
    if actual.shape != upper.shape or actual.size == 0:
        raise ValueError("coverage arrays must have the same non-empty shape")
    return float(np.mean(actual <= upper))


def _finite_vector(values: Iterable[float]) -> np.ndarray:
    a = np.asarray(list(values), dtype=np.float64)
    if a.ndim != 1 or a.size == 0 or not np.isfinite(a).all():
        raise ValueError("expected a non-empty finite vector")
    return a


def _validate_probability(value: float, name: str) -> None:
    if not math.isfinite(value) or not (0.0 < value < 1.0):
        raise ValueError(f"{name} must be strictly between 0 and 1")
