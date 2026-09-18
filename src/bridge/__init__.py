# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Bridge support: bridged children behind a bridge node.

Moved in from the enterprise add-on module. The stacks under stacks/ are built
directly by cdk/apps/rmng.py, so there is no register() entrypoint here any
more — that hook exists only for modules living outside this repo.
"""
