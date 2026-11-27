# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Unit tests for the API custom-domain discovery handler. Run with `pytest test/unittest/`; no AWS needed."""
import os
import sys
import types

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "../../cdk/apps"))
import _bootstrap  # noqa: E402,F401  puts cdk/utils and the gsi_infra submodule on the path

EXECUTE_API = "https://abc123.execute-api.us-east-1.amazonaws.com/prod/"


@pytest.fixture
def discover(monkeypatch):
    """Runs the inline handler with API Gateway stubbed to report one mapping; returns the attributes it sends to CloudFormation."""
    sent = []
    stubs = {
        "boto3": types.ModuleType("boto3"),
        "cfnresponse": types.SimpleNamespace(SUCCESS="SUCCESS", FAILED="FAILED", send=lambda event, ctx, status, data, physical_id: sent.append((status, data))),
        "botocore": types.ModuleType("botocore"),
        "botocore.config": types.SimpleNamespace(Config=lambda **kwargs: None),
    }
    for name, module in stubs.items():
        monkeypatch.setitem(sys.modules, name, module)
    from app_common import _API_DOMAIN_DISCOVERY_CODE

    def run(domain, base_path, **props):
        namespace = {}
        exec(_API_DOMAIN_DISCOVERY_CODE, namespace)
        namespace["_rest_mappings"] = lambda api_id: iter([(domain, base_path)] if domain else [])
        event = {"RequestType": "Create", "ResourceProperties": {"ApiId": "abc123", "ApiType": "REST", "DefaultUrl": EXECUTE_API, **props}}
        namespace["handler"](event, None)
        status, data = sent[-1]
        assert status == "SUCCESS"
        return data

    return run


def test_no_domain_uses_the_execute_api_url(discover):
    assert discover("", "")["Url"] == "https://abc123.execute-api.us-east-1.amazonaws.com/prod"


def test_a_root_mapping_is_the_bare_domain(discover):
    assert discover("api.example.com", "")["Url"] == "https://api.example.com"


def test_a_base_path_mapping_keeps_its_path(discover):
    assert discover("iot.example.com", "neo")["Url"] == "https://iot.example.com/neo"


def test_without_a_domain_the_execute_api_url_is_the_only_audience(discover):
    data = discover("", "")
    assert (data["Resource"], data["Audiences"]) == ("https://abc123.execute-api.us-east-1.amazonaws.com/prod", "https://abc123.execute-api.us-east-1.amazonaws.com/prod")


def test_a_mapped_domain_is_advertised_and_the_execute_api_url_stays_accepted(discover):
    assert discover("iot.example.com", "neo", ResourcePath="/v1/mcp") == {
        "Url": "https://iot.example.com/neo",
        "Resource": "https://iot.example.com/neo/v1/mcp",
        "Audiences": "https://abc123.execute-api.us-east-1.amazonaws.com/prod/v1/mcp,https://iot.example.com/neo/v1/mcp",
    }
