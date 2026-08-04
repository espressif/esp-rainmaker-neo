# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from app_common import CommonResources
from test.infra.handlers.test_webhook.stack import WebhookApi

class WebhookCore(Construct):
    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources):
        super().__init__(scope, construct_id)

        WebhookApi(
            self,
            "WehbookApi",
            common_resources
        )