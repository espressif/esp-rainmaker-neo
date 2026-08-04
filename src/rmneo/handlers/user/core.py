# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from app_common import CommonResources
from src.rmneo.handlers.user.assume_role.stack import AssumeRoleAPI
from src.rmneo.handlers.user.user_client.stack import RegisterClient
from src.rmneo.handlers.user.user_creds.stack import UserCredsAPI

class UserCore(Construct):
    """Core/compute resources for User service - Lambda functions and API integrations"""
    
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Create Lambda-based APIs
        AssumeRoleAPI(self, "AssumeRoleAPI", common_resources)
        RegisterClient(self, "RegisterClientAPI", common_resources)
        UserCredsAPI(self, "UserCredsAPI", common_resources)
