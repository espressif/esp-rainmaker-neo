# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

# Physical resource names owned by rmng-bridge. Keep BRIDGE_CHILDREN_TABLE and the event names in sync with the Go side (bridge_children_db.go, add_remove_child/handler.go).

# cascade_delete MUST deploy under core's node-left-group hook name, or core's async invoke hits a non-existent function and the cascade silently no-ops. Imported from core so there's one source of truth.
from src.rmneo.stacks.base_res_constants import NODE_LIFECYCLE_HOOKS

BRIDGE_RESOURCES = {
    # DynamoDB
    "BRIDGE_CHILDREN_TABLE": "rmng-bridge-children",
    # IoT
    "BRIDGE_POLICY_NAME": "rmng-bridge-policy",
    # IoT topic rule names — AWS requires ^[a-zA-Z0-9_]+$ (snake_case).
    "RULE_FROM_CLOUD_REWRITE": "bridge_from_cloud_rewrite_rule",
    "RULE_UNICAST_REWRITE": "bridge_unicast_rewrite_rule",
    # Bridge control-plane rule (spec §3.4 Rule D). Named generically so
    # future bridge-only ops can land on the same rule without a
    # firmware-facing topic rename — the Lambda dispatches on event name.
    "RULE_TO_CLOUD": "bridge_to_cloud_rule",
    # Function names. add_remove_child is IoT-rule-invoked. cascade_delete is the
    # node-left-group hook target core invokes (EventBridge migration deferred).
    # Node registration and presence carry no entry here: both run in-process in
    # the core binaries that already hold those events (src/bridge/hooks).
    "ADD_REMOVE_CHILD_FUNCTION_NAME": "rmng-bridge-add-remove-child",
    "CASCADE_DELETE_FUNCTION_NAME": NODE_LIFECYCLE_HOOKS["NODE_LEFT_GROUP"],
}

# Functions outside the bridge package that bridge code needs to invoke
# by name. Kept here to avoid hardcoding strings in env-var wiring.
EXTERNAL_FUNCTION_NAMES = {
    "NODE_DATA_RESET": "rmng-node-reset",
}
