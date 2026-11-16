# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""RainMaker Neo Controller: a node that reads and controls the other nodes in its single group.

The stacks under stacks/ are built by cdk/apps/controller.py, its own app because the deploy engine
maps one app per stack group. There is no register() entrypoint here; that hook exists only for
modules living outside this repo.

Core holds none of this. What core does hold is generic: the device principal
(src/rmneo/principal), which resolves any IoT credential-provider caller from its certificate at the
single RBAC checkpoint every data Lambda runs. See docs/en/specs/controller.md §7.1.
"""
