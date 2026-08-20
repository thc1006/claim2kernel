// SPDX-License-Identifier: Apache-2.0
//
// rzf_cuda.cu -- CUDA reference/baseline implementation of the c2k_rzf C ABI.
//
// This is the CROSS-STACK BASELINE required by docs/MOJO_HARDWARE_PROTOCOL.md
// (phase 8) and, equally important, a real-hardware validation of the exact
// GPU algorithm that kernels/mojo/rzf_kernel.mojo implements: one thread block
// per batch element, a regularized Hermitian Gram built and Cholesky-factored
// in shared memory, N independent triangular solves, and a per-batch Frobenius
// normalization. It forms NO explicit matrix inverse.
//
// It deliberately mirrors kernels/rzf/rzf_algorithm.py step for step so that the
// same fail-closed status semantics (non-finite input, non positive-definite
// regularized Gram) hold on device. FP32 complex storage and accumulation.
//
// IMPORTANT HONESTY NOTE: compiling and running this file on a GeForce GTX 1050
// (Pascal, sm_61) is NOT evidence for the Mojo kernel and NOT evidence for the
// declared datacenter targets (NVIDIA sm_80/sm_90, AMD gfx942). It validates the
// algorithm, the C ABI, and the benchmark methodology on real silicon. Any
// datacenter-target claim requires the self-hosted hardware protocol.

#include <cuda_runtime.h>

#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <new>
#include <string>

#include "c2k_rzf.h"

#ifndef C2K_RZF_MAX_USERS
#define C2K_RZF_MAX_USERS 64
#endif

namespace {

// ---- device complex helpers (float2 = {re, im}) ---------------------------
__device__ __forceinline__ float2 cadd(float2 a, float2 b) { return make_float2(a.x + b.x, a.y + b.y); }
__device__ __forceinline__ float2 csub(float2 a, float2 b) { return make_float2(a.x - b.x, a.y - b.y); }
__device__ __forceinline__ float2 cmul(float2 a, float2 b) {
    return make_float2(a.x * b.x - a.y * b.y, a.x * b.y + a.y * b.x);
}
__device__ __forceinline__ float2 cconj(float2 a) { return make_float2(a.x, -a.y); }
__device__ __forceinline__ float2 cscale(float2 a, float s) { return make_float2(a.x * s, a.y * s); }
__device__ __forceinline__ float cabs2(float2 a) { return a.x * a.x + a.y * a.y; }

// One block per batch element. Dynamic shared memory holds the U*U Gram, which
// is Cholesky-factored in place into its lower triangle L.
//   H : [B,U,N] interleaved, W : [B,N,U] interleaved, status : [B].
__global__ void rzf_kernel(const float2 *__restrict__ H, float2 *__restrict__ W,
                           int32_t *__restrict__ status, int users, int antennas,
                           float alpha, float tiny) {
    extern __shared__ float2 A[];  // U*U
    __shared__ int flags[2];       // [0]=non_finite, [1]=positive_definite
    __shared__ float redsum[256];
    __shared__ float scale_sh;

    const int b = blockIdx.x;
    const int t = threadIdx.x;
    const int nthreads = blockDim.x;
    const int U = users, N = antennas;
    const float2 *Hb = H + (size_t)b * U * N;
    float2 *Wb = W + (size_t)b * N * U;

    if (t == 0) { flags[0] = 0; flags[1] = 1; }
    __syncthreads();

    // Pass 0: non-finite scan over H[b] (U*N complex elements).
    int local_bad = 0;
    for (int idx = t; idx < U * N; idx += nthreads) {
        float2 v = Hb[idx];
        if (!isfinite(v.x) || !isfinite(v.y)) local_bad = 1;
    }
    if (local_bad) atomicOr(&flags[0], 1);
    __syncthreads();
    if (flags[0]) {
        for (int idx = t; idx < N * U; idx += nthreads) Wb[idx] = make_float2(0.f, 0.f);
        if (t == 0) status[b] = C2K_RZF_NON_FINITE_INPUT;
        return;
    }

    // Pass 1: Gram A = H H^H + alpha*I, entry (i,j) = sum_n H[i,n] conj(H[j,n]).
    // Fixed left-to-right summation order over n (deterministic).
    for (int e = t; e < U * U; e += nthreads) {
        int i = e / U, j = e % U;
        float2 acc = make_float2(0.f, 0.f);
        for (int n = 0; n < N; ++n) acc = cadd(acc, cmul(Hb[i * N + n], cconj(Hb[j * N + n])));
        if (i == j) acc.x += alpha;  // Hermitian diagonal is real
        A[e] = acc;
    }
    __syncthreads();

    // Pass 2: in-place lower Cholesky by thread 0 (serial, deterministic).
    if (t == 0) {
        for (int j = 0; j < U && flags[1]; ++j) {
            float d = A[j * U + j].x;
            for (int k = 0; k < j; ++k) d -= cabs2(A[j * U + k]);
            if (!isfinite(d) || d <= 0.0f) { flags[1] = 0; break; }
            float ljj = sqrtf(d);
            A[j * U + j] = make_float2(ljj, 0.f);
            float inv = 1.0f / ljj;
            for (int i = j + 1; i < U; ++i) {
                float2 v = A[i * U + j];
                for (int k = 0; k < j; ++k) v = csub(v, cmul(A[i * U + k], cconj(A[j * U + k])));
                A[i * U + j] = cscale(v, inv);
            }
        }
    }
    __syncthreads();
    if (!flags[1]) {
        for (int idx = t; idx < N * U; idx += nthreads) Wb[idx] = make_float2(0.f, 0.f);
        if (t == 0) status[b] = C2K_RZF_NOT_POSITIVE_DEFINITE;
        return;
    }

    // Pass 3: each thread solves a strided subset of the N columns and writes the
    // UNNORMALIZED precoder W[b][n][:] = conj(x), accumulating sum |x|^2.
    float local_sq = 0.0f;
    float2 y[C2K_RZF_MAX_USERS];
    float2 x[C2K_RZF_MAX_USERS];
    for (int n = t; n < N; n += nthreads) {
        for (int i = 0; i < U; ++i) {                     // forward: L y = H[:,n]
            float2 acc = Hb[i * N + n];
            for (int k = 0; k < i; ++k) acc = csub(acc, cmul(A[i * U + k], y[k]));
            y[i] = cscale(acc, 1.0f / A[i * U + i].x);
        }
        for (int i = U - 1; i >= 0; --i) {                // backward: L^H x = y
            float2 acc = y[i];
            for (int k = i + 1; k < U; ++k) acc = csub(acc, cmul(cconj(A[k * U + i]), x[k]));
            x[i] = cscale(acc, 1.0f / A[i * U + i].x);
        }
        for (int u = 0; u < U; ++u) {
            local_sq += cabs2(x[u]);
            Wb[n * U + u] = cconj(x[u]);
        }
    }

    // Block reduction of the Frobenius sum-of-squares (deterministic tree).
    redsum[t] = local_sq;
    __syncthreads();
    for (int s = nthreads / 2; s > 0; s >>= 1) {
        if (t < s) redsum[t] += redsum[t + s];
        __syncthreads();
    }
    if (t == 0) {
        float norm = sqrtf(redsum[0]);
        scale_sh = 1.0f / (norm > tiny ? norm : tiny);
        status[b] = C2K_RZF_OK;
    }
    __syncthreads();

    // Pass 4: rescale each thread's own columns by the shared block norm.
    float scale = scale_sh;
    for (int n = t; n < N; n += nthreads) {
        for (int u = 0; u < U; ++u) Wb[n * U + u] = cscale(Wb[n * U + u], scale);
    }
}

// ---- host context ---------------------------------------------------------
struct Context {
    int max_batch = 0, max_users = 0, max_antennas = 0;
    uint32_t flags = 0;
    cudaStream_t stream = nullptr;
    float *d_H = nullptr, *d_W = nullptr;
    int32_t *d_status = nullptr;
    float *p_H = nullptr, *p_W = nullptr;   // pinned host staging
    int32_t *p_status = nullptr;
    cudaEvent_t ev_start = nullptr, ev_h2d = nullptr, ev_kernel = nullptr, ev_d2h = nullptr;
    std::string last_error;
    int block_dim = 128;
};

thread_local std::string g_global_error;

void set_err(Context *c, const std::string &m) {
    if (c) c->last_error = m; else g_global_error = m;
}

const float kTinyF32 = 1.1754943508222875e-38f;  // FLT_MIN (matches np.finfo(float32).tiny)

}  // namespace

extern "C" {

int32_t c2k_rzf_abi_version(void) { return C2K_RZF_ABI_VERSION; }

const char *c2k_rzf_backend_name(void) { return "cuda"; }

int64_t c2k_rzf_required_workspace_bytes(int64_t batch, int64_t users, int64_t antennas) {
    if (batch <= 0 || users <= 0 || antennas < users) return -1;
    // fp32-complex (8B) for H, Gram, L, and W, per batch element.
    return 8 * batch * (users * antennas + users * users + users * users + antennas * users);
}

int32_t c2k_rzf_create(int32_t max_batch, int32_t max_users, int32_t max_antennas,
                       uint32_t flags, void **out_handle) {
    if (!out_handle || max_batch <= 0 || max_users <= 0 || max_antennas < max_users)
        return C2K_RZF_CALL_EINVAL;
    if (max_users > C2K_RZF_MAX_USERS) { set_err(nullptr, "max_users exceeds C2K_RZF_MAX_USERS"); return C2K_RZF_CALL_EINVAL; }

    int device_count = 0;
    if (cudaGetDeviceCount(&device_count) != cudaSuccess || device_count == 0) {
        set_err(nullptr, "no CUDA device present");
        return C2K_RZF_CALL_ENODEV;
    }
    if (cudaSetDevice(0) != cudaSuccess) { set_err(nullptr, "cudaSetDevice failed"); return C2K_RZF_CALL_EDEVICE; }

    Context *c = new (std::nothrow) Context();
    if (!c) return C2K_RZF_CALL_ENOMEM;
    c->max_batch = max_batch; c->max_users = max_users; c->max_antennas = max_antennas; c->flags = flags;

    size_t h_elems = (size_t)max_batch * max_users * max_antennas;
    size_t w_elems = (size_t)max_batch * max_antennas * max_users;
    // Record into the global slot: the context is destroyed on failure, so a
    // per-handle error would be freed before the caller could read it.
    auto fail = [&](const char *m, int code) { set_err(nullptr, m); c2k_rzf_destroy(c); return code; };

    if (cudaMalloc(&c->d_H, h_elems * 2 * sizeof(float)) != cudaSuccess) return fail("cudaMalloc d_H", C2K_RZF_CALL_ENOMEM);
    if (cudaMalloc(&c->d_W, w_elems * 2 * sizeof(float)) != cudaSuccess) return fail("cudaMalloc d_W", C2K_RZF_CALL_ENOMEM);
    if (cudaMalloc(&c->d_status, (size_t)max_batch * sizeof(int32_t)) != cudaSuccess) return fail("cudaMalloc d_status", C2K_RZF_CALL_ENOMEM);
    if (cudaHostAlloc(&c->p_H, h_elems * 2 * sizeof(float), cudaHostAllocDefault) != cudaSuccess) return fail("cudaHostAlloc p_H", C2K_RZF_CALL_ENOMEM);
    if (cudaHostAlloc(&c->p_W, w_elems * 2 * sizeof(float), cudaHostAllocDefault) != cudaSuccess) return fail("cudaHostAlloc p_W", C2K_RZF_CALL_ENOMEM);
    if (cudaHostAlloc(&c->p_status, (size_t)max_batch * sizeof(int32_t), cudaHostAllocDefault) != cudaSuccess) return fail("cudaHostAlloc p_status", C2K_RZF_CALL_ENOMEM);
    if (cudaStreamCreate(&c->stream) != cudaSuccess) return fail("cudaStreamCreate", C2K_RZF_CALL_EDEVICE);
    if (cudaEventCreate(&c->ev_start) != cudaSuccess) return fail("cudaEventCreate", C2K_RZF_CALL_EDEVICE);
    if (cudaEventCreate(&c->ev_h2d) != cudaSuccess) return fail("cudaEventCreate", C2K_RZF_CALL_EDEVICE);
    if (cudaEventCreate(&c->ev_kernel) != cudaSuccess) return fail("cudaEventCreate", C2K_RZF_CALL_EDEVICE);
    if (cudaEventCreate(&c->ev_d2h) != cudaSuccess) return fail("cudaEventCreate", C2K_RZF_CALL_EDEVICE);

    *out_handle = c;
    return C2K_RZF_CALL_OK;
}

int32_t c2k_rzf_run(void *handle, const float *h_interleaved, int32_t batch, int32_t users,
                    int32_t antennas, float alpha, uint32_t flags, float *w_interleaved,
                    int32_t *status, c2k_rzf_timing *timing) {
    Context *c = static_cast<Context *>(handle);
    if (!c || !h_interleaved || !w_interleaved || !status) return C2K_RZF_CALL_EINVAL;
    if (batch <= 0 || users <= 0 || antennas < users) { set_err(c, "invalid dims (need users<=antennas)"); return C2K_RZF_CALL_EINVAL; }
    if (!(alpha > 0.0f)) { set_err(c, "alpha must be > 0"); return C2K_RZF_CALL_EINVAL; }
    if (batch > c->max_batch || users > c->max_users || antennas > c->max_antennas) { set_err(c, "dims exceed create() maxima"); return C2K_RZF_CALL_EEXCEED; }
    (void)flags;

    size_t h_elems = (size_t)batch * users * antennas;
    size_t w_elems = (size_t)batch * antennas * users;
    size_t h_bytes = h_elems * 2 * sizeof(float);
    size_t w_bytes = w_elems * 2 * sizeof(float);
    size_t shmem = (size_t)users * users * sizeof(float2);

    int shared_optin = 0;
    cudaDeviceGetAttribute(&shared_optin, cudaDevAttrMaxSharedMemoryPerBlockOptin, 0);
    if ((int)shmem > shared_optin && shared_optin > 0) {
        set_err(c, "U*U Cholesky tile exceeds device shared memory; reduce users");
        return C2K_RZF_CALL_EDEVICE;
    }

    std::memcpy(c->p_H, h_interleaved, h_bytes);  // caller -> pinned (host, untimed)

    cudaEventRecord(c->ev_start, c->stream);
    cudaMemcpyAsync(c->d_H, c->p_H, h_bytes, cudaMemcpyHostToDevice, c->stream);
    cudaEventRecord(c->ev_h2d, c->stream);

    rzf_kernel<<<batch, c->block_dim, shmem, c->stream>>>(
        reinterpret_cast<const float2 *>(c->d_H), reinterpret_cast<float2 *>(c->d_W),
        c->d_status, users, antennas, alpha, kTinyF32);
    cudaEventRecord(c->ev_kernel, c->stream);

    cudaMemcpyAsync(c->p_W, c->d_W, w_bytes, cudaMemcpyDeviceToHost, c->stream);
    cudaMemcpyAsync(c->p_status, c->d_status, (size_t)batch * sizeof(int32_t), cudaMemcpyDeviceToHost, c->stream);
    cudaEventRecord(c->ev_d2h, c->stream);

    cudaError_t err = cudaStreamSynchronize(c->stream);
    if (err != cudaSuccess) { set_err(c, cudaGetErrorString(err)); return C2K_RZF_CALL_EDEVICE; }

    std::memcpy(w_interleaved, c->p_W, w_bytes);  // pinned -> caller (host, untimed)
    std::memcpy(status, c->p_status, (size_t)batch * sizeof(int32_t));

    if (timing) {
        float h2d = 0, kern = 0, d2h = 0;
        cudaEventElapsedTime(&h2d, c->ev_start, c->ev_h2d);
        cudaEventElapsedTime(&kern, c->ev_h2d, c->ev_kernel);
        cudaEventElapsedTime(&d2h, c->ev_kernel, c->ev_d2h);
        timing->h2d_ms = h2d; timing->kernel_ms = kern; timing->d2h_ms = d2h;
        timing->device_ms = (double)h2d + kern + d2h;
        timing->bytes_h2d = (int64_t)h_bytes; timing->bytes_d2h = (int64_t)(w_bytes + batch * sizeof(int32_t));
        timing->event_timed = 1; timing->reserved = 0;
    }
    return C2K_RZF_CALL_OK;
}

int32_t c2k_rzf_destroy(void *handle) {
    Context *c = static_cast<Context *>(handle);
    if (!c) return C2K_RZF_CALL_OK;
    if (c->d_H) cudaFree(c->d_H);
    if (c->d_W) cudaFree(c->d_W);
    if (c->d_status) cudaFree(c->d_status);
    if (c->p_H) cudaFreeHost(c->p_H);
    if (c->p_W) cudaFreeHost(c->p_W);
    if (c->p_status) cudaFreeHost(c->p_status);
    if (c->ev_start) cudaEventDestroy(c->ev_start);
    if (c->ev_h2d) cudaEventDestroy(c->ev_h2d);
    if (c->ev_kernel) cudaEventDestroy(c->ev_kernel);
    if (c->ev_d2h) cudaEventDestroy(c->ev_d2h);
    if (c->stream) cudaStreamDestroy(c->stream);
    delete c;
    return C2K_RZF_CALL_OK;
}

const char *c2k_rzf_last_error(void *handle) {
    Context *c = static_cast<Context *>(handle);
    const std::string &s = c ? c->last_error : g_global_error;
    return s.empty() ? "" : s.c_str();
}

}  // extern "C"
