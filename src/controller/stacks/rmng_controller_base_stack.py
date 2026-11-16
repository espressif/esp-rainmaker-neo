# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""RainMaker Neo Controller base (identity/infra) stack.

Owns the durable, name-addressed resources a controller's certificate needs:
  - rmng-controller-role + its IoT role alias, which turn a device certificate into SigV4
    credentials through the IoT Credential Provider
  - rmng-controller-policy, attached alongside the base node policy on controller certs by the
    node-register hook in src/controller/hooks

This stack references rmng-base purely by name and through SSM, never a CFN cross-stack dependency,
mirroring the bridge split. There is no reverse dependency: rmng-base and rmng-core are unaware of
the controller.
"""

import json
import os

from aws_cdk import (
    CfnOutput,
    Stack,
    aws_iam as iam,
    aws_iot as iot,
    aws_ssm as ssm,
)
from constructs import Construct

from app_common import (
    CommonResources,
    create_iot_role_alias,
    stable_logical_id,
)
from src.controller.stacks.base_res_constants import CONTROLLER_RESOURCES
from src.rmneo.stacks.base_res_constants import SSM_PARAMETERS


def _load_controller_policy_document(region: str, account: str) -> dict:
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), "controller_policy.json")
    with open(path) as policy_file:
        rendered = (policy_file.read()
                    .replace("__REGION__", region)
                    .replace("__ACCOUNT__", account)
                    .replace("__CONTROLLER_ROLE_ALIAS__", CONTROLLER_RESOURCES['CONTROLLER_ROLE_ALIAS']))
    return json.loads(rendered)


class RMNGControllerBaseStack(Stack):
    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        # The API id comes from SSM rather than a CFN import, the same way the claim, GVA and Alexa
        # stacks reach the shared API. An import would make rmng-base undeployable while this stack
        # exists.
        api_gateway_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_ID'])

        controller_role = iam.Role(
            self, "ControllerRole",
            assumed_by=iam.ServicePrincipal("credentials.iot.amazonaws.com"),
            role_name=f"{CONTROLLER_RESOURCES['CONTROLLER_ROLE_NAME']}-{self.region}"
        )
        controller_role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", "controller-role"))

        # Restricting the method to GET is what keeps the controller's REST surface read-only; group scoping is enforced by the RBAC checkpoint, which resolves the device from the certificate the credential provider names the session after.
        controller_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["execute-api:Invoke"],
            resources=[
                f"arn:aws:execute-api:{self.region}:{self.account}:{api_gateway_id}/*/GET/v1/groups/*"
            ],
        ))

        create_iot_role_alias(
            self, "ControllerRoleAlias",
            role_alias=CONTROLLER_RESOURCES['CONTROLLER_ROLE_ALIAS'],
            role_arn=controller_role.role_arn,
        )

        controller_policy = iot.CfnPolicy(
            self, "ControllerPolicy",
            policy_name=CONTROLLER_RESOURCES['CONTROLLER_POLICY_NAME'],
            policy_document=_load_controller_policy_document(self.region, self.account),
        )
        controller_policy.override_logical_id(
            stable_logical_id("IoTPolicy", "controller-policy"))

        CfnOutput(
            self, "ControllerRoleAliases",
            description="IoT role alias names for RainMaker Neo Controller credential-provider access (comma-separated, newest last)",
            value=",".join([CONTROLLER_RESOURCES['CONTROLLER_ROLE_ALIAS']])
        )
        CfnOutput(
            self, "ControllerPolicyName",
            description="IoT policy name attached to RainMaker Neo Controller certificates",
            value=controller_policy.policy_name
        )
