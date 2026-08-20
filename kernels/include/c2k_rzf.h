/* SPDX-License-Identifier: Apache-2.0
 *
 * c2k_rzf.h -- stable C ABI for the Claim2Kernel batched complex RZF precoder.
 *
 * This header is the single source of truth for the in-process host interface.
 * BOTH the Mojo GPU kernel (kernels/mojo/rzf_kernel.mojo, the deployable target)
 * AND the CUDA reference/baseline (kernels/cuda/rzf_cuda.cu) implement exactly
 * these symbols, so a host (kernels/host/rzf_host.py via ctypes, or any C
 * caller) can load either backend without recompiling.
 *
 * Contract with kernels/rzf/rzf_algorithm.py (the CPU spec) and
 * kernels/rzf/rzf_reference.py (the complex128 oracle):
 *
 *   G   = H H^H                     [B,U,U] Hermitian PSD
 *   A   = G + alpha*I               [B,U,U] Hermitian positive-definite
 *   A X = H                         solve column by column, no explicit inverse
 *   W   = conj(X)^T                 [B,N,U]
 *   W   = W / max(||W||_F, tiny)    per-batch Frobenius normalization
 *
 * Data layout: complex64 stored as interleaved float32 pairs [re, im].
 *   H : length 2*B*U*N floats, row-major index (((b*U)+u)*N + n)*2 + {0:re,1:im}
 *   W : length 2*B*N*U floats, row-major index (((b*N)+n)*U + u)*2 + {0:re,1:im}
 *   status : length B int32, one c2k_rzf_status per batch element.
 *
 * Fail-closed invariant: a batch element with any non-OK status has its W[b]
 * region written as all zeros. A rejected element never returns NaN/Inf or an
 * unnormalized precoder. Callers MUST inspect status before trusting W[b].
 *
 * This is ABI version 2 (contract_exports.mojo declared v1 for the earlier
 * workspace-only scaffold). ABI is append-only; existing symbols keep their
 * signatures across internal kernel specialization.
 */
#ifndef C2K_RZF_H
#define C2K_RZF_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#define C2K_RZF_ABI_VERSION 2

/* Per-batch numerical status. Mirrors kernels/rzf/rzf_algorithm.py. */
typedef enum {
    C2K_RZF_OK = 0,                     /* precoder is valid and normalized      */
    C2K_RZF_NON_FINITE_INPUT = 1,       /* NaN/Inf in H[b]; output zeroed        */
    C2K_RZF_NOT_POSITIVE_DEFINITE = 2,  /* regularized Gram not PD; output zeroed*/
    C2K_RZF_INVALID_SHAPE = 3           /* per-element shape rejection           */
} c2k_rzf_status;

/* Call-level return codes (distinct from per-batch status). 0 == success. */
#define C2K_RZF_CALL_OK              0
#define C2K_RZF_CALL_EINVAL       (-1)  /* null pointer / bad dimensions         */
#define C2K_RZF_CALL_EEXCEED      (-2)  /* dims exceed the handle's max capacity */
#define C2K_RZF_CALL_ENODEV       (-3)  /* no supported accelerator present      */
#define C2K_RZF_CALL_EDEVICE      (-4)  /* device API / launch failure           */
#define C2K_RZF_CALL_ENOMEM       (-5)  /* allocation failure                    */

/* Run flags. */
#define C2K_RZF_FLAG_DETERMINISTIC  (1u << 0)  /* fixed reduction order          */

/* Device-event timing for one c2k_rzf_run call. Kernel/H2D/D2H are measured
 * with backend device events (CUDA events / Mojo DeviceContext timing), NEVER
 * with a host clock. A host clock reading MUST NOT be copied into these fields;
 * end-to-end host wall time is the caller's responsibility. Any phase the
 * backend did not measure is reported as a negative sentinel (-1.0). */
typedef struct {
    double h2d_ms;      /* host-to-device copy of H                         */
    double kernel_ms;   /* kernel execution only                            */
    double d2h_ms;      /* device-to-host copy of W (+ status)              */
    double device_ms;   /* sum of on-device phases, device-event measured   */
    int64_t bytes_h2d;  /* bytes copied host->device                        */
    int64_t bytes_d2h;  /* bytes copied device->host                        */
    uint32_t event_timed; /* 1 if device events populated the *_ms fields    */
    uint32_t reserved;
} c2k_rzf_timing;

/* Returns C2K_RZF_ABI_VERSION for the loaded backend. */
int32_t c2k_rzf_abi_version(void);

/* Human-readable backend identity, e.g. "cuda" or "mojo-gpu". Never NULL. */
const char *c2k_rzf_backend_name(void);

/* Conservative device workspace estimate in bytes, or -1 for invalid dims.
 * Matches contract_exports.mojo's accounting extended for the L factor. */
int64_t c2k_rzf_required_workspace_bytes(int64_t batch, int64_t users, int64_t antennas);

/* Create a persistent context: one DeviceContext/CUDA stream plus host and
 * device buffers pre-allocated for the given maximum shape. The kernel is
 * already compiled into this shared library (AOT); create() performs no JIT.
 * Returns C2K_RZF_CALL_OK and writes *out_handle, or a negative code. */
int32_t c2k_rzf_create(int32_t max_batch, int32_t max_users, int32_t max_antennas,
                       uint32_t flags, void **out_handle);

/* Request hot path. Reuses the buffers allocated by create(); performs no
 * allocation and no JIT. Copies H in, launches the precompiled kernel, copies W
 * and status out, and fills `timing` from device events. `batch/users/antennas`
 * must be <= the create() maxima and satisfy users<=antennas. `alpha` must be
 * > 0 (the regularization weight). `timing` may be NULL. */
int32_t c2k_rzf_run(void *handle,
                    const float *h_interleaved, int32_t batch, int32_t users,
                    int32_t antennas, float alpha, uint32_t flags,
                    float *w_interleaved, int32_t *status,
                    c2k_rzf_timing *timing);

/* Release all context resources. Safe on NULL. */
int32_t c2k_rzf_destroy(void *handle);

/* Last error string for this handle (or a global fallback if handle is NULL).
 * Never NULL; points to backend-owned storage valid until the next call. */
const char *c2k_rzf_last_error(void *handle);

#ifdef __cplusplus
}
#endif

#endif /* C2K_RZF_H */
