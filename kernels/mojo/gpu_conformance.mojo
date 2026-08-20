# SPDX-License-Identifier: Apache-2.0
# Claim2Kernel Mojo 1.0.0b2 GPU conformance probe.
# This follows the official DeviceContext/TileTensor programming pattern.

from std.math import ceildiv
from std.sys import has_accelerator
from max.gpu.host import DeviceContext
from std.gpu import block_dim, block_idx, thread_idx
from layout import TileTensor, row_major

comptime float_dtype = DType.float32
comptime vector_size = 4096
comptime tensor_layout = row_major[vector_size]()
comptime block_size = 256
comptime num_blocks = ceildiv(vector_size, block_size)


def saxpy(
    x: TileTensor[float_dtype, type_of(tensor_layout), MutAnyOrigin],
    y: TileTensor[float_dtype, type_of(tensor_layout), MutAnyOrigin],
    out: TileTensor[float_dtype, type_of(tensor_layout), MutAnyOrigin],
):
    var tid = block_idx.x * block_dim.x + thread_idx.x
    if tid < vector_size:
        out[tid] = 2.0 * x[tid] + y[tid]


def main() raises:
    comptime if not has_accelerator():
        raise "Claim2Kernel GPU conformance requires a supported accelerator"
    else:
        var ctx = DeviceContext()
        var x_host = ctx.enqueue_create_host_buffer[float_dtype](vector_size)
        var y_host = ctx.enqueue_create_host_buffer[float_dtype](vector_size)
        ctx.synchronize()
        for i in range(vector_size):
            x_host[i] = Float32(i)
            y_host[i] = Float32(3)

        var x_dev = ctx.enqueue_create_buffer[float_dtype](vector_size)
        var y_dev = ctx.enqueue_create_buffer[float_dtype](vector_size)
        var out_dev = ctx.enqueue_create_buffer[float_dtype](vector_size)
        ctx.enqueue_copy(dst_buf=x_dev, src_buf=x_host)
        ctx.enqueue_copy(dst_buf=y_dev, src_buf=y_host)

        var x = TileTensor(x_dev, tensor_layout)
        var y = TileTensor(y_dev, tensor_layout)
        var out = TileTensor(out_dev, tensor_layout)
        ctx.enqueue_function[saxpy](
            x,
            y,
            out,
            grid_dim=num_blocks,
            block_dim=block_size,
        )
        var out_host = ctx.enqueue_create_host_buffer[float_dtype](vector_size)
        ctx.enqueue_copy(dst_buf=out_host, src_buf=out_dev)
        ctx.synchronize()

        for i in range(vector_size):
            var expected = Float32(2 * i + 3)
            if out_host[i] != expected:
                raise "GPU conformance result mismatch"
        print('{"claim2kernelMojoGPUConformance":"passed","elements":4096}')
