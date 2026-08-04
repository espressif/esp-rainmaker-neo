# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    RemovalPolicy,
    aws_dynamodb as dynamodb,
    aws_kms as kms,
)
from constructs import Construct
from app_common import CommonResources, ManagedTable, create_ssm_string_parameter, stable_logical_id
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, SSM_PARAMETERS


class ClaimBase(Construct):
    """Stateful resources for assisted claiming — the node-ID reservation table
    and the CA signing key.

    Instantiated only when a claiming variant is enabled, so a deployment with
    claiming off gets neither (see docs/en/specs/assisted-claiming.md).

    The reservation is the record that makes certificate identity
    server-determined: claim-verify reads the node ID from here rather than
    from anything the caller submits. The CA certificate itself is not minted
    here — it is minted at runtime through the superadmin bootstrap API
    (assisted-claiming §3.9), since it must be signed by the key below, which
    only exists once this stack is deployed.
    """

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # A ManagedTable with no GSIs today. claimant_id is the partition key,
        # so the only query beyond a point read — counting a caller's
        # reservations for quota — is a base-table partition Query needing no
        # index (see src/db/node_id_reservation_db). It is a ManagedTable, not a
        # plain dynamodb.Table, so that a future GSI is added through the
        # serialized orchestrator path (add_global_secondary_index) that every
        # other table in the fleet uses, rather than a one-off CreateTable index.
        #
        # common_resources is deliberately omitted: with no GSIs the orchestrator
        # is never invoked, and this separate stack has no GsiInfraCore to invoke
        # anyway. Adding the first GSI here is therefore NOT free — it needs the
        # rmng-base orchestrator's ARNs (published to SSM as GSI_TRIGGER_LAMBDA_ARN
        # / GSI_STATE_MACHINE_ARN), resolved to a concrete string at synth time
        # rather than a CFN dynamic ref (a ref as the custom-resource ServiceToken
        # trips AWS::EarlyValidation::ResourceExistenceCheck), plus a
        # GsiReadinessGate in this stack. See commit 43b3e645, which removed the
        # earlier ManagedTable for exactly these cross-stack reasons; ManagedTable
        # is reintroduced here only for the no-GSI ergonomics above.
        self.node_id_reservations_table = ManagedTable(
            self,
            "NodeIDReservationsTable",
            table_name=TABLE_NAMES['NODE_ID_RESERVATIONS'],
            partition_key=dynamodb.Attribute(
                name="claimant_id",
                type=dynamodb.AttributeType.STRING
            ),
            sort_key=dynamodb.Attribute(
                name="mac_addr",
                type=dynamodb.AttributeType.STRING
            ),
            # RETAIN, for the same reason as the CA key below.
            #
            # This table is the authoritative {device, claimant} -> node_id
            # mapping. Losing it does not just lose a lookup: every claimed
            # device would be re-assigned a fresh node ID on its next claim,
            # orphaning the IoT Thing, certificate, and shadow it already has.
            # That is unrecoverable, and it would be triggered by nothing more
            # than a deploy with claiming switched off.
            removal_policy=RemovalPolicy.RETAIN,
        )

        # Claiming CA signing key.
        #
        # ECC_NIST_P256 / SIGN_VERIFY because every certificate this service
        # issues is P-256 signed with ECDSA-SHA256. The key material is
        # generated inside KMS and is not exportable, so no code path — and no
        # compromise of a Lambda — can yield an offline signing capability.
        # Each use is a kms:Sign entry in CloudTrail.
        #
        # RETAIN, unlike the reservation table: destroying this key would
        # permanently invalidate the certificate chain of every device ever
        # claimed by this deployment, and the key cannot be regenerated. A
        # stack teardown must leave it behind for an operator to remove
        # deliberately.
        self.claiming_ca_key = kms.Key(
            self,
            "ClaimingCAKey",
            description="RMNG assisted claiming CA signing key (ECDSA P-256)",
            key_spec=kms.KeySpec.ECC_NIST_P256,
            key_usage=kms.KeyUsage.SIGN_VERIFY,
            alias=f"{common_resources.prefix}claiming-ca",
            enable_key_rotation=False,  # unsupported for asymmetric keys
            removal_policy=RemovalPolicy.RETAIN,
        )
        self.claiming_ca_key.node.default_child.override_logical_id(
            stable_logical_id("KMSKey", "claiming-ca"))

        # Published so the claim Lambdas resolve the key at runtime rather than
        # taking a cross-stack CFN reference.
        create_ssm_string_parameter(
            self, "ClaimingCAKeyArnParameter",
            parameter_name=SSM_PARAMETERS['CLAIMING_CA_KEY_ARN'],
            string_value=self.claiming_ca_key.key_arn,
            description="KMS key ARN for the assisted-claiming CA",
        )
