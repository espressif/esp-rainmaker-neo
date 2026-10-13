# SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from test.itest.conftest import rmng_base_outputs

FIRMWARE_KEYS = ["StackRegion", "CredentialProviderEndpoint", "NodeVideoRoleAliases", "NodeFileRoleAliases"]


def test_server_config_returns_requested_keys_only(associated_device):
    device = associated_device[0]

    reply = device.get_server_config({"rmng-base": {key: None for key in FIRMWARE_KEYS}})

    assert reply == {"rmng-base": {key: rmng_base_outputs[key] for key in FIRMWARE_KEYS}}


def test_server_config_omits_unknown_keys_and_levels(associated_device):
    device = associated_device[0]

    reply = device.get_server_config({
        "rmng-base": {"StackRegion": None, "NoSuchKey": None},
        "no-such-level": {"Foo": None},
    })
    assert reply == {"rmng-base": {"StackRegion": rmng_base_outputs["StackRegion"]}}


def test_server_config_without_request_tree_gets_no_reply(associated_device):
    device = associated_device[0]

    assert device.get_server_config(None, timeout=5) is None
