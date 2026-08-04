# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import Stack
from constructs import Construct
from app_common import CommonResources
from src.claim.handlers.base import ClaimBase


class RMNGClaimBaseStack(Stack):
    """Stateful resources for assisted claiming — the node-ID reservation table
    and the claiming CA KMS key (both RETAIN), plus the CA-key-ARN SSM
    parameter the claim handler reads.

    Owns these now that claiming is a separate stack group; rmng-base no longer
    creates them. The reservation table is a ManagedTable but carries no GSIs
    today, so no shared GSI orchestrator is wired into this stack (see ClaimBase
    for what adding a GSI here would require).
    """

    def __init__(self, scope: Construct, construct_id: str,
                 common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)
        self.common_resources = common_resources
        self.claim_base = ClaimBase(self, "ClaimBase", common_resources)
