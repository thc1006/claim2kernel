# SPDX-License-Identifier: Apache-2.0
# Minimal shared-library ABI probe. The production RZF kernel should keep this
# ABI version stable while its internal specialization evolves.

@export("c2k_contract_abi_version", ABI="C")
def contract_abi_version() -> Int32:
    return 1


@export("c2k_required_workspace_bytes", ABI="C")
def required_workspace_bytes(batch: Int64, users: Int64, antennas: Int64) -> Int64:
    if batch <= 0 or users <= 0 or antennas < users:
        return -1
    # Conservative fp32 complex workspace estimate for H, Gram, and output.
    return 8 * batch * (users * antennas + users * users + antennas * users)
