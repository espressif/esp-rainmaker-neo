# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

TABLE_NAMES = {
    'TEST_WEBHOOK_TABLE' : 'rmng-test-webhook-table'
}

SSM_PARAMETERS = {
    'API_GATEWAY_ID': '/rmng/test/base/api-gateway-id',
    'API_GATEWAY_ROOT_RESOURCE_ID': '/rmng/test/base/api-gateway-root-resource-id',
    # Read by testmock.PARAM_NAME (src/rmneo/notification/testmock): where an itest-marked endpoint is delivered.
    'WEBHOOK_MOCK': '/rmng/test/webhook-mock',
}

# Made up, so real LWA refuses it. The backend sends LWA and SMAPI calls made with it to the webhook mock.
ITEST_LWA_CLIENT_ID = 'amzn1.application-oa2-client.itest-placeholder'