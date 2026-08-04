# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from app_common import CommonResources
from src.alexa.handlers.alexa_cfg.stack import AlexaCfgAPI

class AlexaSkillCore(Construct):
    """Core/compute resources for Alexa Skill - Lambda function and API integration"""
    
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Alexa Skill lambda function is moved to a separate stack

        AlexaCfgAPI(self, "AlexaCfgAPI", common_resources)
