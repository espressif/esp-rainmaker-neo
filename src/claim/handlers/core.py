# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_iam as iam,
    aws_ssm as ssm,
    Stack,
)
from constructs import Construct
from app_common import (
    CommonResources,
    create_lambda_function,
    create_base_lambda_role,
    create_cfn_api_method,
    get_or_create_api_resource,
    add_cors_options,
)
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, SSM_PARAMETERS, IOT_RESOURCES
from src.espuser.stacks.base_res_constants import USER_TABLE_NAMES
from arn_utils import get_table_arn, get_ssm_parameter_arn


class ClaimCore(Construct):
    """Compute and API resources for assisted claiming.

    One Lambda backs both claim routes (POST /v1/claim/initiate and
    POST /v1/claim/verify), dispatching on the resource path — the same
    one-handler-per-subsystem shape the rest of the codebase uses. This keeps a
    single function, role, log group and permission set in CloudFormation
    instead of duplicating them per route.

    Nothing here is created unless a claiming variant is enabled, so a
    deployment with claiming off has no claim Lambda and no claim routes
    (see docs/en/specs/assisted-claiming.md Req 16).
    """

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources,
                 node_register_policy: iam.Policy = None, v1_resource_id: str = None,
                 **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "claim_handler"
        lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # Reservation table. initiate does a point read plus a conditional
        # create; verify reads the claim key and stamps the issuing CA. Query is
        # the quota count — a base-table partition query on claimant_id, no GSI,
        # so the grant is the table ARN, not an index ARN. No DeleteItem — no
        # path here removes a reservation.
        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:UpdateItem", "dynamodb:Query"],
            resources=[get_table_arn(TABLE_NAMES['NODE_ID_RESERVATIONS'], region)]
        ))

        # Resolving the caller's user context reads the user-details row.
        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['USER_DETAILS'], region)]
        ))

        # Certificate binding reuses the shared node register/update helpers,
        # so the role needs the same IoT and node-table grants registration
        # uses (create thing, register/attach/detach certs, attach policies).
        if node_register_policy is not None:
            lambda_role.attach_inline_policy(node_register_policy)

        # The CA material: the key ARN and the CA certificate, both published
        # to SSM by the base stack and the CA bootstrap. The certificate is
        # public; the private key is never in SSM.
        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["ssm:GetParameter"],
            resources=[
                get_ssm_parameter_arn(SSM_PARAMETERS['CLAIMING_CA_KEY_ARN'], region),
                get_ssm_parameter_arn(SSM_PARAMETERS['CLAIMING_CA_CERT_PEM'], region),
                # Leaf subject/validity are read from the certificate config (§3.9).
                get_ssm_parameter_arn(SSM_PARAMETERS['CLAIMING_CONFIG'], region),
            ]
        ))

        # The signing grant, scoped to the one CA key.
        #
        # The CA key is created in the base stack; its ARN comes across through
        # SSM rather than a cross-stack export, keeping the two stacks
        # independently deployable — same pattern as the API Gateway wiring.
        #
        # Only Sign and GetPublicKey: no kms:Decrypt and no key-policy
        # administration, so nothing here can export the key or widen its use.
        # The ability to mint device identities therefore lives in exactly one
        # Lambda role and nowhere else (Req 11.5). The trade-off of folding
        # initiate into this same Lambda is that the initiate path now runs
        # under a role that *can* sign even though it never does; the reduction
        # to a single function/role is the reason it is acceptable.
        ca_key_arn = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['CLAIMING_CA_KEY_ARN']
        )
        if ca_key_arn:
            lambda_role.add_to_policy(iam.PolicyStatement(
                actions=["kms:Sign", "kms:GetPublicKey"],
                resources=[ca_key_arn],
            ))

        # The claiming variant and per-claimant quota are runtime configuration,
        # read from the claiming-config SSM document set through the admin API —
        # not deploy-time env vars — so the stack carries no claiming inputs. The
        # CA-material SSM parameter paths are likewise ca_bootstrap Go constants
        # (matching the gva/alexa admin config handlers). CLAIMING_CA_ID is a
        # value, not a parameter path, so it stays an env var.
        environment = {
            "CLAIMING_CA_ID": "claiming-ca-1",
            "DEFAULT_THING_POLICY_NAME": IOT_RESOURCES['DEFAULT_THING_POLICY_NAME'],
            "DEVICE_FILE_POLICY_NAME": IOT_RESOURCES['DEVICE_FILE_POLICY_NAME'],
            "DEVICE_VIDEO_POLICY_NAME": IOT_RESOURCES['DEVICE_VIDEO_POLICY_NAME'],
        }

        self.claim_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=lambda_role,
            environment=environment,
        )

        # /v1/claim, then a resource per route, both wired to the one function.
        # The shared API's /v1 is owned by rmng-core; when a parent id is
        # supplied (the separate claim stack passes it from SSM) attach /claim
        # under it rather than recreating /v1, which would collide on the API.
        v1_parent_id = v1_resource_id or get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1"
        )
        claim_resource_id = get_or_create_api_resource(
            self, "ClaimResource", common_resources,
            v1_parent_id, "claim"
        )

        initiate_resource_id = get_or_create_api_resource(
            self, "ClaimInitiateResource", common_resources,
            claim_resource_id, "initiate"
        )
        create_cfn_api_method(
            self, "ClaimInitiatePostMethod", common_resources,
            initiate_resource_id, "POST", self.claim_function
        )
        add_cors_options(
            self, "ClaimInitiateOptionsMethod", common_resources,
            initiate_resource_id, allowed_methods=["POST"]
        )

        verify_resource_id = get_or_create_api_resource(
            self, "ClaimVerifyResource", common_resources,
            claim_resource_id, "verify"
        )
        create_cfn_api_method(
            self, "ClaimVerifyPostMethod", common_resources,
            verify_resource_id, "POST", self.claim_function
        )
        add_cors_options(
            self, "ClaimVerifyOptionsMethod", common_resources,
            verify_resource_id, allowed_methods=["POST"]
        )

        self._build_admin_api(common_resources, region, ca_key_arn)

    def _build_admin_api(self, common_resources, region, ca_key_arn):
        """Superadmin CA configuration and bootstrap API (assisted-claiming §3.9).

        A Lambda separate from the claim handler: it is superadmin-gated and
        holds the CA-minting grants — signing and writes to the config and CA
        certificate parameters — that the user-facing claim handler must never
        have. Certificate identity/validity is set here at runtime, so it never
        passes through rmng-inputs.json or deploy inputs.
        """
        function_name = "claim_admin"
        admin_role = create_base_lambda_role(self, function_name, common_resources)

        # Read the key ARN, the certificate config and the CA cert; write the
        # config and the CA cert. Scoped to exactly the three claiming params.
        admin_role.add_to_policy(iam.PolicyStatement(
            actions=["ssm:GetParameter"],
            resources=[
                get_ssm_parameter_arn(SSM_PARAMETERS['CLAIMING_CA_KEY_ARN'], region),
                get_ssm_parameter_arn(SSM_PARAMETERS['CLAIMING_CA_CERT_PEM'], region),
                get_ssm_parameter_arn(SSM_PARAMETERS['CLAIMING_CONFIG'], region),
            ]
        ))
        admin_role.add_to_policy(iam.PolicyStatement(
            actions=["ssm:PutParameter"],
            resources=[
                get_ssm_parameter_arn(SSM_PARAMETERS['CLAIMING_CA_CERT_PEM'], region),
                get_ssm_parameter_arn(SSM_PARAMETERS['CLAIMING_CONFIG'], region),
            ]
        ))
        # Resolving the caller's superadmin status reads the user-details row.
        admin_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['USER_DETAILS'], region)]
        ))
        # Minting signs the CA with the one key; GetPublicKey builds the cert.
        if ca_key_arn:
            admin_role.add_to_policy(iam.PolicyStatement(
                actions=["kms:Sign", "kms:GetPublicKey"],
                resources=[ca_key_arn],
            ))

        # No custom env: the CA-material parameter paths are ca_bootstrap Go
        # constants, and create_lambda_function supplies the user-pool/JWKS env
        # the superadmin check needs.
        self.claim_admin_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=admin_role,
        )

        # /v1/admin/claiming/{config,ca}, both wired to the one admin function.
        # /v1/admin is the shared admin resource (created by rmng-base, its id
        # published to SSM); attach /claiming under it rather than recreating it.
        claiming_parent_id = get_or_create_api_resource(
            self, "AdminClaimingResource", common_resources,
            common_resources.admin_api_resource_id, "claiming"
        )
        for name, path in (("Config", "config"), ("CA", "ca")):
            resource_id = get_or_create_api_resource(
                self, f"AdminClaiming{name}Resource", common_resources,
                claiming_parent_id, path
            )
            for method in ("POST", "GET"):
                create_cfn_api_method(
                    self, f"AdminClaiming{name}{method}Method", common_resources,
                    resource_id, method, self.claim_admin_function
                )
            add_cors_options(
                self, f"AdminClaiming{name}OptionsMethod", common_resources,
                resource_id, allowed_methods=["GET", "POST"]
            )
