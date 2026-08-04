# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from src.rmneo.handlers.group.group.stack import GroupAPI
from app_common import CommonResources

class GroupCore(Construct):
    """Core/compute resources for Group service - Lambda functions and API integrations"""
    
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, *, node_data_reset_function=None, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Create Lambda-based Group API
        GroupAPI(self, "GroupAPI", common_resources, node_data_reset_function=node_data_reset_function)
