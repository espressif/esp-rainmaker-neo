# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Unit tests must not build a real AWS client; mark allow_aws to opt out."""
import boto3
import pytest


@pytest.fixture(autouse=True)
def no_aws(request, monkeypatch):
    if request.node.get_closest_marker("allow_aws"):
        return

    def forbidden(*args, **kwargs):
        service = (args[0] if args else kwargs.get("service_name", "?"))
        raise AssertionError(
            f"this test tried to build a real AWS client ({service}). Unit tests must not reach AWS: "
            "inject a fake, or mark the test allow_aws if it genuinely needs one.")

    for name in ("client", "resource", "Session"):
        monkeypatch.setattr(boto3, name, forbidden)


def pytest_configure(config):
    config.addinivalue_line(
        "markers", "allow_aws: this test may construct a real AWS client")
