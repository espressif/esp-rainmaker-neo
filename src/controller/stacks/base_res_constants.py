# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

# Physical resource names owned by the RainMaker Neo Controller. CONTROLLER_POLICY_NAME must match the fallback
# in src/controller/hooks/hooks.go, which attaches it during node registration.

CONTROLLER_RESOURCES = {
    # IoT
    "CONTROLLER_POLICY_NAME": "rmng-controller-policy",
    "CONTROLLER_ROLE_NAME": "rmng-controller-role",
    "CONTROLLER_ROLE_ALIAS": "rmng-controller-role-v1",
}
