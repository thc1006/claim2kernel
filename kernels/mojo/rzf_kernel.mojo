# SPDX-License-Identifier: Apache-2.0
#
# rzf_kernel.mojo -- batched complex regularized zero-forcing precoder GPU kernel
# for Claim2Kernel / MojoRAN.
#
# ============================ STATUS =========================================
# COMPILES with Mojo 1.0.0 / MAX 26.5 (in the pinned >=1.0.0b2,<1.1.0 window;
# see scripts/check_mojo_version.py). Build logs are retained under artifacts/.
# GPU EXECUTION on a declared target (NVIDIA sm_80/sm_90, AMD gfx942) is
# *NOT RUN*: the only local GPU is a GTX 1050 (sm_61), which Mojo's GPU backend
# does not target. The algorithm is validated on CPU against the complex128
# oracle (kernels/rzf/rzf_algorithm.py, ~1e-16) and on real silicon via the
# byte-identical CUDA mirror (kernels/cuda/rzf_cuda.cu) run on that GTX 1050 as
# an sm_61 baseline. No target-architecture Mojo performance is claimed anywhere.
#
# ============================ API BASIS ======================================
# Import paths verified against the installed toolchain (the compiler is ground
# truth; the online docs lag the std./max. split):
#   std.gpu           -> thread_idx, block_idx, block_dim
#   max.gpu           -> barrier          (block synchronization)
#   max.gpu.host      -> DeviceContext
#   std.memory        -> UnsafePointer, stack_allocation, AddressSpace
#   std.math          -> sqrt, isfinite ; std.sys -> has_accelerator
# Mojo 1.0.0 uses `def` (no `fn`) and `comptime` (no `alias`/`@parameter`).
#
# ============================ MATH (must equal the oracle) ===================
#   G = H H^H ; A = G + alpha I ; A X = H (Cholesky, no inverse) ;
#   W = conj(X)^T ; W = W / max(||W||_F, tiny).  FP32 storage + accumulation.
# H is [B,U,N], W is [B,N,U], both interleaved float32 pairs [re, im]; status is
# int32[B]. One thread block per batch element. Fail-closed: a batch with
# non-finite input or a non positive-definite regularized Gram is zeroed and
# flagged (never NaN/Inf, never an unnormalized precoder).

from std.sys import has_accelerator
from std.math import sqrt, isfinite
from std.memory import UnsafePointer, stack_allocation, AddressSpace
from std.gpu import thread_idx, block_idx, block_dim
from max.gpu import barrier
from max.gpu.host import DeviceContext

comptime MAX_USERS = 64        # compile-time cap; shared Cholesky tile is MAX_USERS^2
comptime BLOCK_DIM = 128       # threads per block (power of two for the reduction)
comptime TINY_F32 = Float32(1.1754943508222875e-38)  # np.finfo(float32).tiny

comptime STATUS_OK = Int32(0)
comptime STATUS_NON_FINITE_INPUT = Int32(1)
comptime STATUS_NOT_POSITIVE_DEFINITE = Int32(2)


# One block per batch element. Shared A holds the U*U Gram, Cholesky-factored in
# place into its lower triangle L. Complex arithmetic is inlined on re/im pairs.
def rzf_kernel(
    H: UnsafePointer[Float32, MutAnyOrigin],
    W: UnsafePointer[Float32, MutAnyOrigin],
    status: UnsafePointer[Int32, MutAnyOrigin],
    users: Int32,      # fixed-width: kernel args must be DevicePassable (not Int/UInt)
    antennas: Int32,
    alpha: Float32,
    tiny: Float32,
):
    var U = Int(users)
    var N = Int(antennas)
    var b = Int(block_idx.x)
    var t = Int(thread_idx.x)
    var nthreads = Int(block_dim.x)

    var A = stack_allocation[
        2 * MAX_USERS * MAX_USERS, DType.float32, address_space = AddressSpace.SHARED
    ]()
    var red = stack_allocation[BLOCK_DIM, DType.float32, address_space = AddressSpace.SHARED]()
    var ctrl = stack_allocation[2, DType.float32, address_space = AddressSpace.SHARED]()

    var Hb = H + b * U * N * 2
    var Wb = W + b * N * U * 2

    # ---- Pass 0: cooperative non-finite scan (no atomics; OR via shared array).
    var local_bad = Float32(0)
    var idx = t
    while idx < U * N:
        if not isfinite(Hb[2 * idx]) or not isfinite(Hb[2 * idx + 1]):
            local_bad = Float32(1)
        idx += nthreads
    red[t] = local_bad
    barrier()
    if t == 0:
        var bad = Float32(0)
        for k in range(nthreads):
            if red[k] != Float32(0):
                bad = Float32(1)
        ctrl[0] = bad
    barrier()
    if ctrl[0] != Float32(0):
        var j = t
        while j < N * U:
            Wb[2 * j] = Float32(0)
            Wb[2 * j + 1] = Float32(0)
            j += nthreads
        if t == 0:
            status[b] = STATUS_NON_FINITE_INPUT
        return

    # ---- Pass 1: Gram A = H H^H + alpha*I, entry (i,j)=sum_n H[i,n] conj(H[j,n]).
    var e = t
    while e < U * U:
        var i = e // U
        var jj = e % U
        var accr = Float32(0)
        var acci = Float32(0)
        for n in range(N):
            var hr = Hb[2 * (i * N + n)]
            var hi = Hb[2 * (i * N + n) + 1]
            var gr = Hb[2 * (jj * N + n)]
            var gi = Hb[2 * (jj * N + n) + 1]
            # H[i,n] * conj(H[j,n])
            accr += hr * gr + hi * gi
            acci += hi * gr - hr * gi
        if i == jj:
            accr += alpha
        A[2 * e] = accr
        A[2 * e + 1] = acci
        e += nthreads
    barrier()

    # ---- Pass 2: in-place lower Cholesky by thread 0 (serial, deterministic).
    if t == 0:
        var pd = Float32(1)
        for jc in range(U):
            var d = A[2 * (jc * U + jc)]
            for k in range(jc):
                var lr = A[2 * (jc * U + k)]
                var li = A[2 * (jc * U + k) + 1]
                d -= lr * lr + li * li
            if not isfinite(d) or d <= Float32(0):
                pd = Float32(0)
                break
            var ljj = sqrt(d)
            A[2 * (jc * U + jc)] = ljj
            A[2 * (jc * U + jc) + 1] = Float32(0)
            var inv = Float32(1) / ljj
            for i in range(jc + 1, U):
                var vr = A[2 * (i * U + jc)]
                var vi = A[2 * (i * U + jc) + 1]
                for k in range(jc):
                    var ar = A[2 * (i * U + k)]
                    var ai = A[2 * (i * U + k) + 1]
                    var br = A[2 * (jc * U + k)]
                    var bi = A[2 * (jc * U + k) + 1]
                    # L[i,k] * conj(L[j,k])
                    vr -= ar * br + ai * bi
                    vi -= ai * br - ar * bi
                A[2 * (i * U + jc)] = vr * inv
                A[2 * (i * U + jc) + 1] = vi * inv
        ctrl[0] = pd
    barrier()
    if ctrl[0] == Float32(0):
        var j2 = t
        while j2 < N * U:
            Wb[2 * j2] = Float32(0)
            Wb[2 * j2 + 1] = Float32(0)
            j2 += nthreads
        if t == 0:
            status[b] = STATUS_NOT_POSITIVE_DEFINITE
        return

    # ---- Pass 3: each thread solves a strided subset of the N columns.
    var yv = stack_allocation[2 * MAX_USERS, DType.float32]()
    var xv = stack_allocation[2 * MAX_USERS, DType.float32]()
    var local_sq = Float32(0)
    var n = t
    while n < N:
        for i in range(U):                        # forward: L y = H[:,n]
            var accr = Hb[2 * (i * N + n)]
            var acci = Hb[2 * (i * N + n) + 1]
            for k in range(i):
                var lr = A[2 * (i * U + k)]
                var li = A[2 * (i * U + k) + 1]
                var yr = yv[2 * k]
                var yi = yv[2 * k + 1]
                accr -= lr * yr - li * yi
                acci -= lr * yi + li * yr
            var d = A[2 * (i * U + i)]             # L[i,i] real
            yv[2 * i] = accr / d
            yv[2 * i + 1] = acci / d
        var i2 = U - 1
        while i2 >= 0:                             # backward: L^H x = y
            var accr = yv[2 * i2]
            var acci = yv[2 * i2 + 1]
            for k in range(i2 + 1, U):
                # conj(L[k,i2]) * x[k]
                var lr = A[2 * (k * U + i2)]
                var li = -A[2 * (k * U + i2) + 1]
                var xr = xv[2 * k]
                var xi = xv[2 * k + 1]
                accr -= lr * xr - li * xi
                acci -= lr * xi + li * xr
            var d = A[2 * (i2 * U + i2)]
            xv[2 * i2] = accr / d
            xv[2 * i2 + 1] = acci / d
            i2 -= 1
        for u in range(U):                         # W[b,n,u] = conj(x[u]) (unnormalized)
            local_sq += xv[2 * u] * xv[2 * u] + xv[2 * u + 1] * xv[2 * u + 1]
            Wb[2 * (n * U + u)] = xv[2 * u]
            Wb[2 * (n * U + u) + 1] = -xv[2 * u + 1]
        n += nthreads

    # ---- Block reduction of the Frobenius sum-of-squares (deterministic tree).
    red[t] = local_sq
    barrier()
    var s = nthreads // 2
    while s > 0:
        if t < s:
            red[t] += red[t + s]
        barrier()
        s = s // 2
    if t == 0:
        var norm = sqrt(red[0])
        var denom = norm if norm > tiny else tiny
        ctrl[1] = Float32(1) / denom
        status[b] = STATUS_OK
    barrier()

    # ---- Pass 4: rescale each thread's own columns by the shared block norm.
    var scale = ctrl[1]
    var n2 = t
    while n2 < N:
        for u in range(U):
            Wb[2 * (n2 * U + u)] *= scale
            Wb[2 * (n2 * U + u) + 1] *= scale
        n2 += nthreads


# --------------------------- C ABI scalar exports ---------------------------
# Names/semantics match kernels/include/c2k_rzf.h. `ABI="C"` matches the repo's
# contract_exports.mojo; the toolchain warns it is deprecated in favor of
# abi("C"), so the RZF build below does not use --Werror.

@export("c2k_rzf_abi_version", ABI="C")
def c2k_rzf_abi_version() -> Int32:
    return 2


@export("c2k_rzf_required_workspace_bytes", ABI="C")
def c2k_rzf_required_workspace_bytes(batch: Int64, users: Int64, antennas: Int64) -> Int64:
    if batch <= 0 or users <= 0 or antennas < users:
        return -1
    return 8 * batch * (users * antennas + users * users + users * users + antennas * users)


# --------------------------- conformance / host harness ----------------------
# Demonstrates the required host pattern on a compilable, runnable path:
#   * persistent DeviceContext created once,
#   * host and device buffers allocated once and REUSED across launches,
#   * the kernel compiled once (compile_function_checked = AOT) and only
#     enqueued in the loop -> the request hot path performs no JIT,
#   * per-batch Frobenius-norm check of the output (a valid precoder has
#     ||W[b]||_F == 1 unless its status flags a rejection).
# On a CPU-only or unsupported-GPU host it prints a NOT RUN line and exits 0.
def main() raises:
    comptime if not has_accelerator():
        print('{"claim2kernelRzfMojo":"NOT RUN: no supported accelerator"}')
    else:
        var B = 4
        var U = 8
        var N = 32
        var alpha = Float32(0.1)
        var hn = B * U * N * 2
        var wn = B * N * U * 2

        var ctx = DeviceContext()
        var h_host = ctx.enqueue_create_host_buffer[DType.float32](hn)
        var w_host = ctx.enqueue_create_host_buffer[DType.float32](wn)
        var s_host = ctx.enqueue_create_host_buffer[DType.int32](B)
        var h_dev = ctx.enqueue_create_buffer[DType.float32](hn)
        var w_dev = ctx.enqueue_create_buffer[DType.float32](wn)
        var s_dev = ctx.enqueue_create_buffer[DType.int32](B)
        ctx.synchronize()

        # Deterministic channel (no RNG dependency): a fixed reproducible pattern.
        for i in range(hn):
            var x = Float32((i * 2654435761) % 1000) / Float32(500) - Float32(1)
            h_host[i] = x
        ctx.enqueue_copy(dst_buf=h_dev, src_buf=h_host)

        # Compile the kernel ONCE (AOT); reuse the handle for every launch.
        var kernel = ctx.compile_function[rzf_kernel]()

        # Hot path: reuse buffers + compiled kernel; no allocation, no JIT.
        for _iter in range(3):
            ctx.enqueue_function(
                kernel,
                h_dev,
                w_dev,
                s_dev,
                Int32(U),
                Int32(N),
                alpha,
                TINY_F32,
                grid_dim=B,
                block_dim=BLOCK_DIM,
            )
        ctx.enqueue_copy(dst_buf=w_host, src_buf=w_dev)
        ctx.enqueue_copy(dst_buf=s_host, src_buf=s_dev)
        ctx.synchronize()

        var ok = True
        for bb in range(B):
            if s_host[bb] != STATUS_OK:
                ok = False
            var sumsq = Float32(0)
            for k in range(N * U * 2):
                var v = w_host[bb * N * U * 2 + k]
                sumsq += v * v
            var nrm = sqrt(sumsq)
            if nrm < Float32(0.99) or nrm > Float32(1.01):
                ok = False
        if ok:
            print('{"claim2kernelRzfMojo":"conformance passed","batch":4}')
        else:
            print('{"claim2kernelRzfMojo":"conformance FAILED"}')
            raise Error("RZF Mojo conformance failed")
