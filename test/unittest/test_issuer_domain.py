# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Unit tests for discovering an API's custom domain, and with it the ESP User issuer from the API's one custom domain. Run with `pytest test/scripts/`; no AWS needed."""
import os
import sys
import types

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "../../cdk/apps"))
import _bootstrap  # noqa: E402,F401  puts the repo root and cdk/utils on the path

API = "abc123"
OTHER_API = "zzz999"
DEFAULT_URL = "https://abc123.execute-api.ap-south-1.amazonaws.com/prod/"


class _NotFound(Exception):
    pass


class FakeRestApi:
    """API Gateway REST custom domains: {domain: [(base_path, rest_api_id)]}, listed in insertion order."""

    exceptions = types.SimpleNamespace(NotFoundException=_NotFound)

    def __init__(self, domains):
        self.domains = domains

    def get_paginator(self, name):
        assert name == "get_domain_names"
        return types.SimpleNamespace(paginate=lambda: [{"items": [{"domainName": d} for d in self.domains]}])

    def get_base_path_mappings(self, domainName):
        if domainName not in self.domains:
            raise _NotFound(domainName)
        return {"items": [{"basePath": path, "restApiId": api} for path, api in self.domains[domainName]]}


class FakeHttpApi:
    """API Gateway v2 custom domains: {domain: [(mapping_key, api_id)]}."""

    exceptions = types.SimpleNamespace(NotFoundException=_NotFound)

    def __init__(self, domains):
        self.domains = domains

    def get_domain_names(self, **kwargs):
        return {"Items": [{"DomainName": d} for d in self.domains]}

    def get_api_mappings(self, DomainName):
        if DomainName not in self.domains:
            raise _NotFound(DomainName)
        return {"Items": [{"ApiMappingKey": key, "ApiId": api} for key, api in self.domains[DomainName]]}


@pytest.fixture
def discover(monkeypatch):
    """Runs the discovery handler against fake clients; returns (status, data)."""
    sent = []
    monkeypatch.setitem(sys.modules, "cfnresponse", types.SimpleNamespace(
        SUCCESS="SUCCESS", FAILED="FAILED",
        send=lambda event, ctx, status, data, physical_id: sent.append((status, data))))
    from app_common import _API_DOMAIN_DISCOVERY_CODE

    def run(rest=None, http=None, api_type="REST", is_issuer=True):
        clients = {"apigateway": FakeRestApi(rest or {}), "apigatewayv2": FakeHttpApi(http or {})}
        monkeypatch.setitem(sys.modules, "boto3", types.SimpleNamespace(
            client=lambda name, config=None: clients[name]))
        namespace = {}
        exec(_API_DOMAIN_DISCOVERY_CODE, namespace)
        props = {"ApiId": API, "ApiType": api_type, "DefaultUrl": DEFAULT_URL}
        if is_issuer:
            props["IsIssuer"] = "true"
        namespace["handler"]({"RequestType": "Update", "ResourceProperties": props}, None)
        return sent[-1]

    return run


def test_with_no_domain_the_endpoints_and_the_issuer_are_the_api_url(discover):
    status, data = discover(rest={"other.example.com": [("(none)", OTHER_API)]})
    assert status == "SUCCESS"
    assert data["Url"] == DEFAULT_URL.rstrip("/")


@pytest.mark.parametrize("api_type,domains,issuer", [
    ("REST", {"sso.example.com": [("(none)", API)]}, "https://sso.example.com"),
    ("REST", {"sso.example.com": [("auth", API)]}, "https://sso.example.com/auth"),
    ("HTTP", {"sso.example.com": [("", API)]}, "https://sso.example.com"),
    ("HTTP", {"sso.example.com": [("auth", API)]}, "https://sso.example.com/auth"),
])
def test_the_one_mapped_domain_is_the_issuer(discover, api_type, domains, issuer):
    """A domain mapped to another API is not counted."""
    domains = dict(domains, **{"other.example.com": [("(none)" if api_type == "REST" else "", OTHER_API)]})
    status, data = discover(**{api_type.lower(): domains}, api_type=api_type)
    assert status == "SUCCESS"
    assert data["Url"] == issuer


@pytest.mark.parametrize("api_type,domains", [
    ("REST", {"api.example.com": [("(none)", API)], "sso.example.com": [("(none)", API)]}),
    ("REST", {"sso.example.com": [("(none)", API), ("auth", API)]}),
    ("HTTP", {"api.example.com": [("", API)], "sso.example.com": [("", API)]}),
])
def test_more_than_one_mapped_domain_fails_the_deploy(discover, capsys, api_type, domains):
    """Negative: picking one would let AWS's listing order decide every token's issuer."""
    status, data = discover(**{api_type.lower(): domains}, api_type=api_type)
    assert (status, data) == ("FAILED", {})
    logged = capsys.readouterr().out
    for domain in domains:
        assert domain in logged


@pytest.mark.parametrize("api_type", ["REST", "HTTP"])
def test_other_apis_keep_the_first_mapped_domain_and_get_no_issuer(discover, api_type):
    root = "(none)" if api_type == "REST" else ""
    domains = {"api.example.com": [(root, API)], "sso.example.com": [(root, API)]}
    status, data = discover(**{api_type.lower(): domains}, api_type=api_type, is_issuer=False)
    assert status == "SUCCESS"
    assert data["Url"] == "https://api.example.com"


def test_other_apis_without_a_domain_keep_the_api_url(discover):
    status, data = discover(is_issuer=False)
    assert status == "SUCCESS"
    assert data["Url"] == DEFAULT_URL.rstrip("/")
