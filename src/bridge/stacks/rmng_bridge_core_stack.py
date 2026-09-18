# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""RMNG bridge core (compute) stack.

Owns the bridge's compute plane:
  - rmng-bridge-add-remove-child Lambda + IAM role + IoT invoke permission
  - cascade_delete Lambda (deployed under NODE_LEFT_GROUP hook name)
  - Three IoT rules:
      bridge_from_cloud_rewrite_rule   (Rule A — children's from_cloud rewrite)
      bridge_unicast_rewrite_rule      (Rule B — children's unicast rewrite, topic(5) fix)
      bridge_to_cloud_rule             (Rule D per spec §3.4 — sole rule on
                                        rainmaker/bridges/+/to_cloud, gated by
                                        clientid() = topic(3); dispatches on
                                        event name so future bridge-only ops
                                        share the rule)

Storage (bridge_children table + bridge IoT policy) lives in
RMNGBridgeBaseStack. This stack references the bridge_children table BY NAME via
get_table_arn(...) — no CFN cross-stack dep on the base construct — mirroring the
rmng-base / rmng-core split. Base must therefore deploy first so the table
exists at runtime; the module entrypoint enforces that ordering via
add_dependency.

Cross-stack dependencies:
  - rmng-base: GroupDeviceMappingTable name + ARN (referenced by ARN, no CFN dep)
  - rmng-bridge-base: bridge_children table name + ARN (referenced by ARN, no CFN dep)
  - No reverse dependency — rmng-base / rmng-core are unaware of rmng-bridge.
"""

from aws_cdk import (
    CfnOutput,
    Stack,
    aws_iam as iam,
    aws_iot as iot,
)
from constructs import Construct

from app_common import (
    CommonResources,
    create_base_lambda_role,
    create_iot_rule_role,
    create_iot_topic_rule,
    create_lambda_function,
)
from arn_utils import get_index_arn, get_table_arn, get_topic_arn
from src.rmneo.stacks.base_res_constants import TABLE_NAMES
from src.bridge.stacks.base_res_constants import BRIDGE_RESOURCES, EXTERNAL_FUNCTION_NAMES


class RMNGBridgeCoreStack(Stack):
    """Compute stack: bridge Lambdas + IoT rules."""

    def __init__(
        self,
        scope: Construct,
        construct_id: str,
        common_resources: CommonResources,
        **kwargs,
    ) -> None:
        super().__init__(scope, construct_id, **kwargs)

        region = self.region
        account = self.account

        # ---------------------------------------------------------------
        # add_remove_child Lambda
        # ---------------------------------------------------------------
        function_name = "add_remove_child"
        lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # Bridge_children table — full CRUD
        lambda_role.add_to_policy(
            iam.PolicyStatement(
                actions=[
                    "dynamodb:PutItem",
                    "dynamodb:GetItem",
                    "dynamodb:Query",
                    "dynamodb:DeleteItem",
                ],
                resources=[
                    get_table_arn(BRIDGE_RESOURCES["BRIDGE_CHILDREN_TABLE"], region),
                ],
            )
        )

        # group_device_mapping table — required to read the bridge's
        # current group and add/remove the child as a member. The
        # index ARN is needed too because group.GetNodesGroup queries
        # by node_id via the GSI rmng-group-node-assoc-by-node-id;
        # AWS treats index queries as a separate resource for IAM.
        lambda_role.add_to_policy(
            iam.PolicyStatement(
                actions=[
                    "dynamodb:PutItem",
                    "dynamodb:GetItem",
                    "dynamodb:Query",
                    "dynamodb:DeleteItem",
                ],
                resources=[
                    get_table_arn(TABLE_NAMES["GROUP_DEVICE_MAPPING"], region),
                    get_index_arn("GROUP_DEVICE_MAPPING_NODE_ID", region),
                ],
            )
        )

        # IoT control plane — CreateThing / DeleteThing / DescribeThing
        # (no account-resource scoping needed; iot:* control APIs are
        # account-wide in IAM ARN shape).
        lambda_role.add_to_policy(
            iam.PolicyStatement(
                actions=[
                    "iot:CreateThing",
                    "iot:DeleteThing",
                    "iot:DescribeThing",
                    "iot:UpdateThing",
                ],
                resources=[f"arn:aws:iot:{region}:{account}:thing/*"],
            )
        )

        # IoT data plane — publish bridgeAck on the bridge's from_cloud.
        # (publish_input_event_handler's role uses the same wildcard
        # scope.)
        lambda_role.add_to_policy(
            iam.PolicyStatement(
                actions=["iot:Publish"],
                resources=[get_topic_arn("rainmaker/nodes/*/from_cloud", region)],
            )
        )

        # removeChild invokes node_data_reset to wipe the child's
        # triggers/schedules/timeseries/automations.
        # node_data_reset Lambda lives in rmng-core; we reference it by
        # name (Python constant — no CFN cross-stack dep).
        node_data_reset_fn_arn = (
            f"arn:aws:lambda:{region}:{account}:function:"
            f"{EXTERNAL_FUNCTION_NAMES['NODE_DATA_RESET']}"
        )
        lambda_role.add_to_policy(
            iam.PolicyStatement(
                actions=["lambda:InvokeFunction"],
                resources=[node_data_reset_fn_arn],
            )
        )

        add_remove_child_fn = create_lambda_function(
            self,
            function_name,
            common_resources,
            lambda_role=lambda_role,
            aws_function_name=BRIDGE_RESOURCES["ADD_REMOVE_CHILD_FUNCTION_NAME"],
            environment={
                "NODE_DATA_RESET_FUNCTION_NAME":
                    EXTERNAL_FUNCTION_NAMES["NODE_DATA_RESET"],
            },
        )

        # ---------------------------------------------------------------
        # IoT Rules
        # ---------------------------------------------------------------

        # Rule A — from_cloud rewrite (pure SQL).
        # `topic(3)` is the source thing name; if it contains '--', the
        # rule extracts the parent and republishes to the bridges
        # namespace so the bridge can pick it up on its existing
        # subscription.
        rule_a_role = create_iot_rule_role(
            self,
            "RuleAFromCloudRewriteRole",
            role_name="bridge-rule-a-role",
            common_resources=common_resources,
            description="IoT rule role for bridge from_cloud rewrite",
        )
        rule_a_role.add_to_policy(
            iam.PolicyStatement(
                actions=["iot:Publish"],
                resources=[
                    get_topic_arn(
                        "rainmaker/bridges/*/children/*/from_cloud", region
                    ),
                ],
            )
        )
        rule_a_destination = (
            "rainmaker/bridges/"
            "${substring(topic(3), 0, indexof(topic(3), '--'))}"
            "/children/${topic(3)}/from_cloud"
        )
        create_iot_topic_rule(
            self,
            "RuleAFromCloudRewrite",
            rule_name=BRIDGE_RESOURCES["RULE_FROM_CLOUD_REWRITE"],
            topic_rule_payload=iot.CfnTopicRule.TopicRulePayloadProperty(
                sql=(
                    "SELECT * FROM 'rainmaker/nodes/+/from_cloud' "
                    "WHERE indexof(topic(3), '--') >= 0"
                ),
                aws_iot_sql_version="2016-03-23",
                actions=[
                    iot.CfnTopicRule.ActionProperty(
                        republish=iot.CfnTopicRule.RepublishActionProperty(
                            role_arn=rule_a_role.role_arn,
                            topic=rule_a_destination,
                            qos=1,
                        ),
                    ),
                ],
            ),
        )

        # Rule B — unicast params rewrite.
        # `topic(5)` is the shadow-name segment of
        # `rainmaker/nodes/<thing>/user/<shadow>/params` (IoT SQL is
        # 1-indexed over all '/' segments including 'user').
        rule_b_role = create_iot_rule_role(
            self,
            "RuleBUnicastRewriteRole",
            role_name="bridge-rule-b-role",
            common_resources=common_resources,
            description="IoT rule role for bridge unicast params rewrite",
        )
        rule_b_role.add_to_policy(
            iam.PolicyStatement(
                actions=["iot:Publish"],
                resources=[
                    get_topic_arn(
                        "rainmaker/bridges/*/children/*/user/*/params", region
                    ),
                ],
            )
        )
        rule_b_destination = (
            "rainmaker/bridges/"
            "${substring(topic(3), 0, indexof(topic(3), '--'))}"
            "/children/${topic(3)}/user/${topic(5)}/params"
        )
        create_iot_topic_rule(
            self,
            "RuleBUnicastRewrite",
            rule_name=BRIDGE_RESOURCES["RULE_UNICAST_REWRITE"],
            topic_rule_payload=iot.CfnTopicRule.TopicRulePayloadProperty(
                sql=(
                    "SELECT * FROM 'rainmaker/nodes/+/user/+/params' "
                    "WHERE indexof(topic(3), '--') >= 0"
                ),
                aws_iot_sql_version="2016-03-23",
                actions=[
                    iot.CfnTopicRule.ActionProperty(
                        republish=iot.CfnTopicRule.RepublishActionProperty(
                            role_arn=rule_b_role.role_arn,
                            topic=rule_b_destination,
                            qos=1,
                        ),
                    ),
                ],
            ),
        )

        # Rule D — bridge to_cloud control plane (spec §3.4).
        # Sole rule on rainmaker/bridges/+/to_cloud.
        #
        # The WHERE clause is a defence-in-depth gate beyond the bridge
        # IoT policy substitution. For MQTT-connected bridges the
        # policy's ${iot:Connection.Thing.ThingName} already pins the
        # topic. But Thing-substitution doesn't apply to IAM-
        # authenticated publishers (Lambda roles, dev IAM,
        # `aws iot-data publish`), so without this WHERE any principal
        # granted iot:Publish on rainmaker/bridges/*/to_cloud could
        # trigger add_child / remove_child for any bridge. clientid()
        # returns empty for non-MQTT publishers, so the equality check
        # no-ops them.
        #
        # `topic(3) as thing_name` is preserved to keep the
        # PublishInputEvent contract (event.ThingName = parent) shared
        # with publish_input_event_handler.
        bridge_event_rule = create_iot_topic_rule(
            self,
            "BridgeToCloudRule",
            rule_name=BRIDGE_RESOURCES["RULE_TO_CLOUD"],
            topic_rule_payload=iot.CfnTopicRule.TopicRulePayloadProperty(
                sql=(
                    "SELECT topic(3) as thing_name, * as data "
                    "FROM 'rainmaker/bridges/+/to_cloud' "
                    "WHERE clientid() = topic(3)"
                ),
                aws_iot_sql_version="2016-03-23",
                actions=[
                    iot.CfnTopicRule.ActionProperty(
                        lambda_=iot.CfnTopicRule.LambdaActionProperty(
                            function_arn=add_remove_child_fn.function_arn,
                        ),
                    ),
                ],
            ),
        )

        # Allow IoT to invoke the Lambda.
        add_remove_child_fn.add_permission(
            "BridgeToCloudInvokePermission",
            principal=iam.ServicePrincipal("iot.amazonaws.com"),
            action="lambda:InvokeFunction",
            source_arn=bridge_event_rule.attr_arn,
        )

        # ---------------------------------------------------------------
        # cascade_delete Lambda.
        # Async-invoked by node.shadow_node when a bridge is removed
        # from its group or moved to a new group. Spec §5.8.
        # ---------------------------------------------------------------
        cascade_fn_name = "cascade_delete"
        cascade_role = create_base_lambda_role(self, cascade_fn_name, common_resources)

        cascade_role.add_to_policy(
            iam.PolicyStatement(
                actions=[
                    "dynamodb:PutItem",
                    "dynamodb:GetItem",
                    "dynamodb:Query",
                    "dynamodb:DeleteItem",
                ],
                resources=[
                    get_table_arn(BRIDGE_RESOURCES["BRIDGE_CHILDREN_TABLE"], region),
                ],
            )
        )
        cascade_role.add_to_policy(
            iam.PolicyStatement(
                actions=[
                    "dynamodb:PutItem",
                    "dynamodb:GetItem",
                    "dynamodb:Query",
                    "dynamodb:DeleteItem",
                ],
                resources=[
                    get_table_arn(TABLE_NAMES["GROUP_DEVICE_MAPPING"], region),
                    get_index_arn("GROUP_DEVICE_MAPPING_NODE_ID", region),
                ],
            )
        )
        cascade_role.add_to_policy(
            iam.PolicyStatement(
                actions=["iot:DeleteThing", "iot:DescribeThing"],
                resources=[f"arn:aws:iot:{region}:{account}:thing/*"],
            )
        )
        # node_data_reset invoke (cross-stack reference by name; same
        # pattern as add_remove_child above).
        cascade_role.add_to_policy(
            iam.PolicyStatement(
                actions=["lambda:InvokeFunction"],
                resources=[node_data_reset_fn_arn],
            )
        )

        # Deploy this Lambda under the node-left-group hook name core invokes
        # (CASCADE_DELETE_FUNCTION_NAME resolves to NODE_LIFECYCLE_HOOKS
        # ["NODE_LEFT_GROUP"]). Core async-invokes that exact name when a node
        # leaves a group; if the deployed name differs, the cascade silently
        # no-ops. Pinned explicitly rather than relying on CDK's name derivation.
        cascade_delete_fn = create_lambda_function(
            self,
            cascade_fn_name,
            common_resources,
            lambda_role=cascade_role,
            aws_function_name=BRIDGE_RESOURCES["CASCADE_DELETE_FUNCTION_NAME"],
            environment={
                "NODE_DATA_RESET_FUNCTION_NAME":
                    EXTERNAL_FUNCTION_NAMES["NODE_DATA_RESET"],
            },
        )

        # The node-register hook and the presence cascade run in-process, inside
        # the callers that already hold the registration and disconnect events
        # (src/bridge/hooks), so this stack deploys no Lambda for either.

        # ---------------------------------------------------------------
        # CfnOutputs — consumed by test/bridge_register.py and tests.
        # ---------------------------------------------------------------
        CfnOutput(self, "AddRemoveChildLambdaName", value=add_remove_child_fn.function_name)
        CfnOutput(self, "CascadeDeleteLambdaName", value=cascade_delete_fn.function_name)
        CfnOutput(self, "RuleAName", value=BRIDGE_RESOURCES["RULE_FROM_CLOUD_REWRITE"])
        CfnOutput(self, "RuleBName", value=BRIDGE_RESOURCES["RULE_UNICAST_REWRITE"])
        CfnOutput(
            self, "BridgeToCloudRuleName",
            value=BRIDGE_RESOURCES["RULE_TO_CLOUD"],
        )
