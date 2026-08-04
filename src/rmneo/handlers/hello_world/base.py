# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from app_common import CommonResources

class HelloWorldBase(Construct):
    """Base/infrastructure resources for HelloWorld service - placeholder for future infrastructure"""
    
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Currently no base/infrastructure resources needed for hello world
        # This is a simple example service
        # Future infrastructure resources can be added here:
        # - Demo data storage tables
        # - Example configuration storage
        # - Test buckets or resources
        pass
