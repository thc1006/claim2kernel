# SPDX-License-Identifier: Apache-2.0
# Claim2Kernel Mojo GPU conformance probe (saxpy: out = 2*x + y).
#
# Verified to compile and cross-compile (nvidia:sm_80/sm_90, amdgpu:gfx942) with
# Mojo 1.0.0 / MAX 26.5. The compiler is the source of truth for the import
# paths -- the GPU thread intrinsics live in `std.gpu`, DeviceContext in
# `max.gpu.host`. Mojo 1.0.0 uses `def` (no `fn`) and `comptime` (no `alias`).

from std.sys import has_accelerator
from std.memory import UnsafePointer
from std.gpu import thread_idx, block_idx, block_dim
from max.gpu.host import DeviceContext

comptime vector_size = 4096
comptime block_size = 256


def saxpy(
    x: UnsafePointer[Float32, MutAnyOrigin],
    y: UnsafePointer[Float32, MutAnyOrigin],
    z: UnsafePointer[Float32, MutAnyOrigin],  # `out` is a reserved keyword in Mojo
    n: Int32,
):
    var tid = Int(block_idx.x) * Int(block_dim.x) + Int(thread_idx.x)
    if tid < Int(n):
        z[tid] = 2.0 * x[tid] + y[tid]


def main() raises:
    comptime if not has_accelerator():
        print('{"claim2kernelMojoGPUConformance":"NOT RUN: no supported accelerator"}')
    else:
        var ctx = DeviceContext()
        var x_host = ctx.enqueue_create_host_buffer[DType.float32](vector_size)
        var y_host = ctx.enqueue_create_host_buffer[DType.float32](vector_size)
        var out_host = ctx.enqueue_create_host_buffer[DType.float32](vector_size)
        ctx.synchronize()
        for i in range(vector_size):
            x_host[i] = Float32(i)
            y_host[i] = Float32(3)

        var x_dev = ctx.enqueue_create_buffer[DType.float32](vector_size)
        var y_dev = ctx.enqueue_create_buffer[DType.float32](vector_size)
        var out_dev = ctx.enqueue_create_buffer[DType.float32](vector_size)
        ctx.enqueue_copy(dst_buf=x_dev, src_buf=x_host)
        ctx.enqueue_copy(dst_buf=y_dev, src_buf=y_host)

        # Compile once (AOT), then enqueue: the launch does not JIT.
        var grid = (vector_size + block_size - 1) // block_size
        var kernel = ctx.compile_function[saxpy]()
        ctx.enqueue_function(
            kernel, x_dev, y_dev, out_dev, Int32(vector_size),
            grid_dim=grid, block_dim=block_size,
        )
        ctx.enqueue_copy(dst_buf=out_host, src_buf=out_dev)
        ctx.synchronize()

        for i in range(vector_size):
            var expected = Float32(2 * i + 3)
            if out_host[i] != expected:
                raise Error("GPU conformance result mismatch")
        print('{"claim2kernelMojoGPUConformance":"passed","elements":4096}')
