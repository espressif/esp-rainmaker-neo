# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from app_common import CommonResources

class NotificationBase(Construct):
    """Base/infrastructure resources for Notification service - placeholder for future infrastructure"""
    
    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        # Currently no base/infrastructure resources needed for notifications
        # Future infrastructure resources can be added here:
        # - DynamoDB tables for notification history
        # - S3 buckets for notification templates
        pass
