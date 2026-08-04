# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from app_common import CommonResources

class IntegrationBase(Construct):
    """Base/infrastructure resources for Integrations - placeholder for future infrastructure"""
    
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Currently no base/infrastructure resources needed for integrations
        # Uses existing user_details table and creates SNS platform applications dynamically
        # Future infrastructure resources can be added here:
        # - Mobile device registration table
        # - Push notification preferences table
        # - Mobile app configuration storage
        # - Device token management table
        pass
