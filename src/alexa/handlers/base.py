# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from app_common import CommonResources

class AlexaSkillBase(Construct):
    """Base/infrastructure resources for Alexa Skill - placeholder for future infrastructure"""
    
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Currently no base/infrastructure resources needed for Alexa skill
        # Uses existing user, group, and node tables for device control
        # Future infrastructure resources can be added here:
        # - Alexa-specific user preferences table
        # - Skill configuration storage
        # - Voice command history table
        # - Alexa session state storage
        pass
