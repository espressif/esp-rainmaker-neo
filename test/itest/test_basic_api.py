# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import requests
from test.itest.conftest import API_GATEWAY_URL

def test_get_hello(test_user1):
    response = test_user1.make_api_request('GET', '/v1/hello')
    assert response.status_code == 200
    assert "Hello" in response.text

def test_get_user_creds(test_user1):
    """Test POST /v1/user/credentials API endpoint"""
    credentials = test_user1.get_aws_credentials()
    assert credentials is not None, "Failed to get user credentials"
    assert 'AccessKeyId' in credentials
    assert 'SecretKey' in credentials
    assert 'SessionToken' in credentials
    assert 'Expiration' in credentials
    assert credentials['Expiration'] > 0

def test_get_user_creds_unauthenticated():
    """Test POST /v1/user/credentials API endpoint without authentication"""
    response = requests.post(f"{API_GATEWAY_URL}/v1/user/credentials")
    assert response.status_code == 401, f"Expected 401, got {response.status_code}. Response: {response.text}"

def test_get_hello_unauthenticated():
    response = requests.get(f"{API_GATEWAY_URL}/v1/hello")
    assert response.status_code == 403
    assert "Missing Authentication Token" in response.text

def test_create_group_unauthenticated():
    response = requests.post(f"{API_GATEWAY_URL}/v1/groups", json={"group_name": "Unauthorized Group"})
    assert response.status_code == 403
    assert "Missing Authentication Token" in response.text

def test_list_groups_unauthenticated():
    response = requests.get(f"{API_GATEWAY_URL}/v1/groups")
    assert response.status_code == 403
    assert "Missing Authentication Token" in response.text
