# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from app_common import CommonResources

class GVAActionBase(Construct):
    """Base/infrastructure resources for GVA Action - placeholder for future infrastructure"""
    
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Currently no base/infrastructure resources needed for GVA action
        # Uses existing user, group, and node tables for device control
        # Future infrastructure resources can be added here:
        # - GVA-specific user preferences table
        # - Voice assistant configuration storage
        # - GVA command history table
        # - Assistant session state storage
        pass
