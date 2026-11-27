# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Unit tests for the OAuth client seed and the per-API resource grant. Run with `pytest test/unittest/`; no AWS needed."""
import base64
import copy
import json
import os
import re
import sys
import types

import pytest
from botocore.exceptions import ClientError

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "../../cdk/apps"))
import _bootstrap  # noqa: E402,F401  puts the repo root and cdk/utils on the path

NEO = "https://neo.example.com/prod"
NEO_EXECUTE_API = "https://abc123.execute-api.us-east-1.amazonaws.com/prod"
ESPUSER = "https://user.example.com/prod"


def _conditional_failure(op):
    return ClientError({"Error": {"Code": "ConditionalCheckFailedException"}}, op)


class FakeClientsTable:
    """The client registry, applying the expression shapes the two handlers send."""

    def __init__(self, *rows, conflicts=0, before_update=None):
        self.rows = {r["client_id"]: copy.deepcopy(r) for r in rows}
        self.updates = 0
        self.conflicts = conflicts
        self.before_update = before_update

    def get_item(self, Key, ConsistentRead=False):
        row = self.rows.get(Key["client_id"])
        return {"Item": copy.deepcopy(row)} if row else {}

    def put_item(self, Item, ConditionExpression=None):
        if ConditionExpression == "attribute_not_exists(client_id)" and Item["client_id"] in self.rows:
            raise _conditional_failure("PutItem")
        self.rows[Item["client_id"]] = dict(Item)

    def update_item(self, Key, UpdateExpression, ExpressionAttributeNames, ExpressionAttributeValues, ConditionExpression):
        row = self.rows.get(Key["client_id"])
        if self.before_update:
            self.before_update(row)
        if self.conflicts:
            self.conflicts -= 1
            row["allowed_resources"] = list(row.get("allowed_resources") or []) + ["https://raced.example.com"]
            raise _conditional_failure("UpdateItem")
        if row is None or not self._holds(row, ConditionExpression, ExpressionAttributeNames, ExpressionAttributeValues):
            raise _conditional_failure("UpdateItem")
        self.updates += 1
        set_part, _, add_part = UpdateExpression.partition(" ADD ")
        adds = [(ExpressionAttributeNames[m.group(1)], m.group(2)) for m in re.finditer(r"(#\w+) (:\w+)", add_part)]
        # As DynamoDB: ADD of a set onto a list attribute is refused, and nothing is written.
        if any(isinstance(row.get(attr), list) for attr, _ in adds):
            raise ClientError({"Error": {"Code": "ValidationException",
                                         "Message": "An operand in the update expression has an incorrect data type"}}, "UpdateItem")
        for m in re.finditer(r"(#\w+) = (?:if_not_exists\(\1, (:\w+)\)|(:\w+))", set_part):
            attr = ExpressionAttributeNames[m.group(1)]
            if m.group(2) and attr in row:
                continue
            row[attr] = ExpressionAttributeValues[m.group(2) or m.group(3)]
        for attr, value in adds:
            row[attr] = set(row.get(attr) or set()) | set(ExpressionAttributeValues[value])

    @staticmethod
    def _holds(row, condition, names, values):
        for clause in condition.split(" AND "):
            clause = clause.strip()
            exists = re.fullmatch(r"attribute_(not_)?exists\((#?\w+)\)", clause)
            if exists:
                attr = names.get(exists.group(2), exists.group(2))
                if (attr in row) == bool(exists.group(1)):
                    return False
                continue
            equal = re.fullmatch(r"(#\w+) = (:\w+)", clause)
            if equal and row.get(names[equal.group(1)]) != values[equal.group(2)]:
                return False
        return True


class FakeSSM:
    def __init__(self):
        self.params = {}

    def put_parameter(self, Name, Value, Type, Overwrite=False):
        self.params[Name] = (Value, Type)


def _stub_cfnresponse(monkeypatch, sent):
    monkeypatch.setitem(sys.modules, "cfnresponse", types.SimpleNamespace(
        SUCCESS="SUCCESS", FAILED="FAILED",
        send=lambda event, ctx, status, data, physical_id: sent.append((status, data))))


@pytest.fixture
def grant(monkeypatch):
    """Runs the resource-grant handler against a fake registry; returns (status, data)."""
    sent = []
    _stub_cfnresponse(monkeypatch, sent)
    from app_common import _CLIENT_RESOURCES_CODE

    def run(table, grants, request="Create"):
        namespace = {}
        exec(_CLIENT_RESOURCES_CODE, namespace)
        namespace["_table"] = lambda name: table
        event = {"RequestType": request, "ResourceProperties": {"TableName": "espuser-oauth-clients", "Grants": grants}}
        namespace["handler"](event, None)
        return sent[-1]

    return run


@pytest.fixture
def seed(monkeypatch):
    """Runs the client seed handler against a fake registry; returns the CloudFormation status. Seeds what espuser-base declares unless given a stack's own clients."""
    sent = []
    from app_common import _SEED_OAUTH_CLIENTS_CODE
    from src.espuser.stacks.base_res_constants import (
        SEEDED_CLIENT_SECRET_ATTRIBUTES,
        SEEDED_OAUTH_CLIENTS,
    )
    monkeypatch.setenv("TABLE_NAME", "espuser-oauth-clients")

    def run(table, ssm=None, clients=SEEDED_OAUTH_CLIENTS, secret_params=None, secret_attributes=SEEDED_CLIENT_SECRET_ATTRIBUTES):
        monkeypatch.setenv("SEEDED_CLIENTS", json.dumps(clients))
        monkeypatch.setenv("SECRET_PARAMS", json.dumps(secret_params or {}))
        monkeypatch.setenv("SECRET_ATTRIBUTES", json.dumps(secret_attributes))
        namespace = {}
        exec(_SEED_OAUTH_CLIENTS_CODE, namespace)
        namespace["_table"] = lambda: table
        namespace["_ssm"] = lambda: ssm or FakeSSM()
        namespace["send"] = lambda event, ctx, status, data=None: sent.append(status)
        namespace["handler"]({"RequestType": "Update"}, None)
        return sent[-1]

    return run


def _grant(client, resource, default, audiences=None):
    # CloudFormation hands a custom resource every property as a string; audiences=None sends a grant without Audiences.
    grant = {"ClientId": client, "Resource": resource, "Default": "true" if default else "false"}
    if audiences is not None:
        grant["Audiences"] = audiences
    return grant


def test_a_client_without_resources_gets_the_api_and_it_becomes_the_default(grant):
    table = FakeClientsTable({"client_id": "va-client"})
    status, _ = grant(table, [_grant("va-client", NEO, True)])
    assert status == "SUCCESS"
    assert (table.rows["va-client"]["allowed_resources"], table.rows["va-client"]["default_resource"]) == ([NEO], NEO)


def test_resources_and_the_default_an_operator_set_are_kept(grant):
    table = FakeClientsTable({"client_id": "user-pool-client", "allowed_resources": [ESPUSER], "default_resource": ESPUSER})
    grant(table, [_grant("user-pool-client", NEO, True)])
    assert (table.rows["user-pool-client"]["allowed_resources"], table.rows["user-pool-client"]["default_resource"]) == ([ESPUSER, NEO], ESPUSER)


def test_every_name_the_api_accepts_is_granted_and_its_resource_is_the_default(grant):
    """The custom domain's name and the execute-api one both reach the client; what it already holds stays."""
    table = FakeClientsTable({"client_id": "user-pool-client", "allowed_resources": [ESPUSER]})
    status, _ = grant(table, [_grant("user-pool-client", NEO, True, audiences=f"{NEO_EXECUTE_API},{NEO}")])
    assert status == "SUCCESS"
    row = table.rows["user-pool-client"]
    assert (row["allowed_resources"], row["default_resource"]) == ([ESPUSER, NEO, NEO_EXECUTE_API], NEO)


def test_an_api_granted_without_default_leaves_the_default_unset(grant):
    table = FakeClientsTable({"client_id": "user-pool-client"})
    grant(table, [_grant("user-pool-client", ESPUSER, False)])
    assert table.rows["user-pool-client"]["allowed_resources"] == [ESPUSER]
    assert "default_resource" not in table.rows["user-pool-client"]


def test_a_client_already_granted_is_not_written(grant):
    table = FakeClientsTable({"client_id": "va-client", "allowed_resources": [NEO], "default_resource": NEO})
    grant(table, [_grant("va-client", NEO, True)])
    assert table.updates == 0


def test_a_row_changed_by_someone_else_is_reread_and_both_changes_kept(grant):
    table = FakeClientsTable({"client_id": "va-client"}, conflicts=1)
    status, _ = grant(table, [_grant("va-client", NEO, True)])
    assert status == "SUCCESS"
    assert table.rows["va-client"]["allowed_resources"] == ["https://raced.example.com", NEO]


def test_a_client_an_operator_deleted_is_skipped_and_the_rest_granted(grant):
    """Negative: a missing row is reported, never re-created without its secret."""
    table = FakeClientsTable({"client_id": "va-client"})
    status, data = grant(table, [_grant("mcp-oauth-client", NEO, True), _grant("va-client", NEO, True)])
    assert status == "SUCCESS"
    assert "mcp-oauth-client" not in table.rows
    assert table.rows["va-client"]["allowed_resources"] == [NEO]
    assert data["Skipped"] == "mcp-oauth-client"


def test_an_unresolved_resource_fails_the_deploy_without_writing(grant):
    """Negative: an empty name would grant a resource no request can ever name."""
    table = FakeClientsTable({"client_id": "va-client"})
    status, _ = grant(table, [_grant("va-client", "", True)])
    assert status == "FAILED"
    assert table.updates == 0


def test_deleting_the_stack_leaves_the_clients_alone(grant):
    table = FakeClientsTable({"client_id": "va-client"})
    status, _ = grant(table, [_grant("va-client", NEO, True)], request="Delete")
    assert status == "SUCCESS"
    assert table.updates == 0


def test_the_seed_leaves_an_existing_user_pool_client_and_va_client_row_as_it_is(seed):
    """A deployment that has these rows keeps them exactly as they are; the seed declares neither."""
    user_pool_client = {"client_id": "user-pool-client", "client_type": "public", "first_party": True,
                        "allowed_resources": [NEO, ESPUSER], "default_resource": NEO}
    va_client = {"client_id": "va-client", "client_type": "confidential", "secret": "s1",
                 "redirect_uris": {"https://layla.amazon.com/api/skill/link/X"}, "allowed_resources": [NEO], "default_resource": NEO}
    table = FakeClientsTable(user_pool_client, va_client, {"client_id": "mcp-oauth-client", "secret": "s2"})
    assert seed(table) == "SUCCESS"
    assert (table.rows["user-pool-client"], table.rows["va-client"]) == (user_pool_client, va_client)


def test_the_seed_fills_a_field_an_existing_client_lacks(seed):
    table = FakeClientsTable(
        {"client_id": "rainmaker-neo-home-app-client", "client_type": "public", "client_name": "RainMaker Home"},
        {"client_id": "mcp-oauth-client", "secret": "s2"})
    assert seed(table) == "SUCCESS"
    assert table.rows["rainmaker-neo-home-app-client"]["first_party"] is True


def test_a_home_app_deployed_before_application_type_becomes_native(seed):
    table = FakeClientsTable(
        {"client_id": "rainmaker-neo-home-app-client", "client_type": "public", "client_name": "RainMaker Home"},
        {"client_id": "mcp-oauth-client", "secret": "s2"})
    assert seed(table) == "SUCCESS"
    assert (table.rows["rainmaker-neo-home-app-client"]["application_type"], table.rows["mcp-oauth-client"]["application_type"]) == ("native", "web")


def test_a_home_app_row_without_a_logo_gets_the_app_icon(seed):
    from src.espuser.stacks.base_res_constants import HOME_APP_LOGO_URI
    table = FakeClientsTable(
        {"client_id": "rainmaker-neo-home-app-client", "client_type": "public", "client_name": "RainMaker Home"},
        {"client_id": "mcp-oauth-client", "secret": "s2"})
    assert seed(table) == "SUCCESS"
    assert table.rows["rainmaker-neo-home-app-client"]["logo_uri"] == HOME_APP_LOGO_URI


def test_a_logo_an_operator_set_on_the_home_app_is_kept(seed):
    own = "data:image/png;base64,iVBORw0KGgo="
    table = FakeClientsTable(
        {"client_id": "rainmaker-neo-home-app-client", "client_type": "public", "client_name": "RainMaker Home", "logo_uri": own},
        {"client_id": "mcp-oauth-client", "secret": "s2"})
    assert seed(table) == "SUCCESS"
    assert table.rows["rainmaker-neo-home-app-client"]["logo_uri"] == own


def test_every_seeded_logo_is_one_the_registry_accepts():
    """clients.ValidateLogoURI: a base64 PNG, JPEG or WebP data: URI of at most 4 KiB decoded."""
    from src.espuser.stacks.base_res_constants import SEEDED_OAUTH_CLIENTS
    logos = [c["logo_uri"] for c in SEEDED_OAUTH_CLIENTS if "logo_uri" in c]
    assert logos
    for uri in logos:
        match = re.fullmatch(r"data:image/(png|jpeg|webp);base64,([A-Za-z0-9+/]+={0,2})", uri)
        assert match and len(base64.b64decode(match.group(2))) <= 4 * 1024


def test_the_seed_lambda_environment_stays_under_lambdas_4_kb_limit():
    """The seed hands its Lambda every client in one environment variable, so a larger logo would fail the espuser-base deploy."""
    from aws_cdk import App, Environment, Stack
    from aws_cdk.assertions import Template

    from app_common import seed_oauth_clients
    from src.espuser.stacks.base_res_constants import (
        SEEDED_CLIENT_SECRET_ATTRIBUTES,
        SEEDED_OAUTH_CLIENTS,
    )
    stack = Stack(App(), "Seed", env=Environment(account="123456789012", region="us-east-1"))
    seed_oauth_clients(stack, "SeedOAuthClients", name="espuser-oauth-clients-seed", table_name="espuser-oauth-clients",
                       clients=SEEDED_OAUTH_CLIENTS, secret_attributes=SEEDED_CLIENT_SECRET_ATTRIBUTES)
    (function,) = Template.from_stack(stack).find_resources("AWS::Lambda::Function").values()
    variables = function["Properties"]["Environment"]["Variables"]
    assert sum(len(k) + len(v) for k, v in variables.items()) <= 4 * 1024


def test_only_the_home_app_is_declared_native():
    from src.espuser.stacks.base_res_constants import (
        HOME_APP_CLIENT_ID,
        SEEDED_OAUTH_CLIENTS,
    )
    from src.rmneo.stacks import base_res_constants as rmng
    declared = {c["client_id"]: c["application_type"] for c in SEEDED_OAUTH_CLIENTS + [rmng.ALEXA_OAUTH_CLIENT, rmng.GVA_OAUTH_CLIENT, rmng.SMART_THINGS_OAUTH_CLIENT]}
    assert {cid for cid, kind in declared.items() if kind == "native"} == {HOME_APP_CLIENT_ID}
    assert set(declared.values()) == {"native", "web"}


def test_the_seed_never_overwrites_what_an_operator_set(seed):
    edited = {"client_id": "mcp-oauth-client", "client_name": "MCP only", "client_type": "confidential", "secret": "kept",
              "redirect_uris": {"https://mcp.example.com/oauth2/callback"},
              "allowed_resources": [NEO], "default_resource": NEO}
    table = FakeClientsTable(dict(edited))
    assert seed(table) == "SUCCESS"
    assert {k: table.rows["mcp-oauth-client"][k] for k in edited} == edited


def test_the_seed_never_puts_back_scopes_an_operator_removed_from_a_client_that_declares_none(seed):
    """Negative: the Home app declares no scopes, so its narrowed list and updated_at stay as the operator left them."""
    edited = {"client_id": "rainmaker-neo-home-app-client", "client_type": "public", "client_name": "RainMaker Home", "scopes": ["openid"], "updated_at": 1}
    table = FakeClientsTable(dict(edited), {"client_id": "mcp-oauth-client", "secret": "s2"})
    assert seed(table) == "SUCCESS"
    row = table.rows["rainmaker-neo-home-app-client"]
    assert (row["scopes"], row["updated_at"]) == (["openid"], 1)


MCP_SCOPES = ["openid", "email", "phone", "profile", "offline_access"]


def test_an_existing_client_gains_the_declared_scopes_it_lacks(seed):
    table = FakeClientsTable({"client_id": "mcp-oauth-client", "secret": "s2", "scopes": ["openid", "email", "phone", "profile"], "updated_at": 1})
    assert seed(table) == "SUCCESS"
    row = table.rows["mcp-oauth-client"]
    assert row["scopes"] == MCP_SCOPES
    assert row["updated_at"] > 1


def test_an_operators_extra_scope_is_never_removed(seed):
    """Negative: the seed only adds, keeping the stored order."""
    table = FakeClientsTable({"client_id": "mcp-oauth-client", "secret": "s2", "scopes": ["account:sessions", "openid"], "updated_at": 1})
    assert seed(table) == "SUCCESS"
    assert table.rows["mcp-oauth-client"]["scopes"] == ["account:sessions", "openid", "email", "phone", "profile", "offline_access"]


def test_a_row_without_scopes_gets_the_declared_list(seed):
    table = FakeClientsTable({"client_id": "mcp-oauth-client", "secret": "s2"})
    assert seed(table) == "SUCCESS"
    assert table.rows["mcp-oauth-client"]["scopes"] == MCP_SCOPES


def test_a_row_with_an_empty_scope_list_gets_the_declared_list(seed):
    table = FakeClientsTable({"client_id": "mcp-oauth-client", "secret": "s2", "scopes": [], "updated_at": 1})
    assert seed(table) == "SUCCESS"
    assert table.rows["mcp-oauth-client"]["scopes"] == MCP_SCOPES


def test_a_row_holding_every_declared_scope_is_not_rewritten(seed):
    table = FakeClientsTable({"client_id": "mcp-oauth-client", "secret": "s2", "scopes": MCP_SCOPES + ["account:sessions"], "updated_at": 1})
    assert seed(table) == "SUCCESS"
    assert (table.rows["mcp-oauth-client"]["scopes"], table.rows["mcp-oauth-client"]["updated_at"]) == (MCP_SCOPES + ["account:sessions"], 1)


def test_scopes_changed_while_the_seed_runs_fail_the_deploy_rather_than_dropping_the_change(seed):
    """Negative: the scopes are rewritten only if no registry write landed since the seed read them."""
    def edit(row):
        row["scopes"].append("account:sessions")
        row["updated_at"] += 1
    table = FakeClientsTable({"client_id": "mcp-oauth-client", "secret": "s2", "scopes": ["openid"], "updated_at": 1}, before_update=edit)
    assert seed(table) == "FAILED"
    assert table.rows["mcp-oauth-client"]["scopes"] == ["openid", "account:sessions"]


def test_only_the_assistants_and_mcp_are_declared_with_offline_access():
    """Negative: the Home app signs in through a browser session, so its grants end with that session."""
    from src.espuser.stacks.base_res_constants import (
        HOME_APP_CLIENT_ID,
        SEEDED_OAUTH_CLIENTS,
    )
    from src.rmneo.stacks import base_res_constants as rmng
    declared = {c["client_id"]: c.get("scopes", []) for c in SEEDED_OAUTH_CLIENTS + [rmng.ALEXA_OAUTH_CLIENT, rmng.GVA_OAUTH_CLIENT, rmng.SMART_THINGS_OAUTH_CLIENT]}
    assert {cid for cid, scopes in declared.items() if "offline_access" in scopes} == set(declared) - {HOME_APP_CLIENT_ID}


def test_the_seed_fails_on_a_confidential_client_with_no_secret(seed):
    """Negative: a stale row would otherwise surface an empty secret to the MCP proxy."""
    table = FakeClientsTable({"client_id": "mcp-oauth-client", "client_type": "confidential"})
    assert seed(table) == "FAILED"


def test_the_seed_creates_the_home_app_as_a_public_first_party_client(seed):
    table, ssm = FakeClientsTable(), FakeSSM()
    assert seed(table, ssm) == "SUCCESS"
    home = table.rows["rainmaker-neo-home-app-client"]
    assert (home["client_type"], home["application_type"], home["first_party"], home["require_pkce"]) == ("public", "native", True, True)
    assert home["redirect_uris"] == {"rainmaker://com.espressif.novahome/success"}
    assert "post_logout_redirect_uris" not in home, "a native app signs out with /oauth2/revoke, so it registers no post-logout redirect"
    assert "secret" not in home
    assert ssm.params == {}


def test_espuser_seeds_no_voice_assistant_client():
    """Negative: each assistant's stack creates its own client, so a deployment without that assistant carries none."""
    from src.espuser.stacks.base_res_constants import SEEDED_OAUTH_CLIENTS
    from src.rmneo.stacks.base_res_constants import VOICE_ASSISTANT_CLIENT_SECRET_PARAMS
    assert not {c["client_id"] for c in SEEDED_OAUTH_CLIENTS} & set(VOICE_ASSISTANT_CLIENT_SECRET_PARAMS)


@pytest.mark.parametrize("client_name", ["ALEXA_OAUTH_CLIENT", "GVA_OAUTH_CLIENT", "SMART_THINGS_OAUTH_CLIENT"])
def test_an_assistant_stack_creates_its_own_confidential_client_and_hands_over_only_its_secret(seed, client_name):
    from src.rmneo.stacks import base_res_constants as rmng
    client = getattr(rmng, client_name)
    client_id, param = client["client_id"], rmng.VOICE_ASSISTANT_CLIENT_SECRET_PARAMS[client["client_id"]]
    table, ssm = FakeClientsTable(), FakeSSM()
    assert seed(table, ssm, clients=[client], secret_params={client_id: param}, secret_attributes={}) == "SUCCESS"
    assert set(table.rows) == {client_id}
    row = table.rows[client_id]
    assert (row["client_type"], row["application_type"], bool(row["secret"]), row.get("first_party")) == ("confidential", "web", True, None)
    assert ssm.params == {param: (row["secret"], "SecureString")}


def test_the_home_app_redirect_is_added_to_a_hand_made_row_without_dropping_its_own(seed):
    """An existing row keeps every redirect an operator registered; the seed only adds the app's."""
    table = FakeClientsTable({"client_id": "rainmaker-neo-home-app-client", "client_type": "public", "require_pkce": True,
                              "redirect_uris": {"com.espressif.rainmaker://oauth2redirect"}})
    assert seed(table) == "SUCCESS"
    row = table.rows["rainmaker-neo-home-app-client"]
    assert row["redirect_uris"] == {"com.espressif.rainmaker://oauth2redirect", "rainmaker://com.espressif.novahome/success"}
    assert row["first_party"] is True


HAND_MADE_HOME_APP = {
    "client_id": "rainmaker-neo-home-app-client", "client_type": "public", "require_pkce": True,
    "redirect_uris": ["com.espressif.rainmaker://oauth2redirect", "http://127.0.0.1/callback"],
    "post_logout_redirect_uris": ["http://127.0.0.1"], "updated_at": 1,
}


def test_a_hand_made_row_holding_its_redirects_as_a_list_gets_them_back_as_a_set_with_the_app_redirect(seed):
    """A row written outside the registry API can hold redirects as a DynamoDB list, which ADD cannot extend."""
    table = FakeClientsTable(dict(HAND_MADE_HOME_APP))
    assert seed(table) == "SUCCESS"
    row = table.rows["rainmaker-neo-home-app-client"]
    assert row["redirect_uris"] == {"com.espressif.rainmaker://oauth2redirect", "http://127.0.0.1/callback",
                                    "rainmaker://com.espressif.novahome/success"}
    assert row["post_logout_redirect_uris"] == ["http://127.0.0.1"], "the seed declares none, so an operator's post-logout redirect is left as it is"
    assert row["first_party"] is True
    assert row["updated_at"] > HAND_MADE_HOME_APP["updated_at"]


def _registry_edit(added):
    """An admin API write landing between the seed's read and its write: it changes the row and moves updated_at."""
    def edit(row):
        if added not in row["redirect_uris"]:
            row["redirect_uris"].append(added)
            row["updated_at"] += 1
    return edit


def test_a_list_changed_while_the_seed_runs_fails_the_deploy_rather_than_dropping_the_change(seed):
    """Negative: the list is rewritten only if no registry write landed since the seed read it."""
    added = "com.example.other://cb"
    table = FakeClientsTable(dict(HAND_MADE_HOME_APP), before_update=_registry_edit(added))
    assert seed(table) == "FAILED"
    assert added in table.rows["rainmaker-neo-home-app-client"]["redirect_uris"]


def test_each_api_a_seeded_client_uses_is_declared_once_with_the_default_first():
    from src.espuser.stacks.base_res_constants import (
        SEEDED_CLIENT_APIS,
        SEEDED_OAUTH_CLIENTS,
    )
    seeded = {c["client_id"] for c in SEEDED_OAUTH_CLIENTS}
    assert set(SEEDED_CLIENT_APIS) == seeded
    assert SEEDED_CLIENT_APIS["rainmaker-neo-home-app-client"] == ["neo", "espuser"]


def test_espuser_seeds_neither_user_pool_client_nor_va_client():
    """Negative: the password API issues to the Home app and each assistant has its own client, so neither is declared, granted or handed a secret."""
    from src.espuser.stacks.base_res_constants import (
        SEEDED_CLIENT_APIS,
        SEEDED_CLIENT_SECRET_ATTRIBUTES,
        SEEDED_OAUTH_CLIENTS,
    )
    declared = {c["client_id"] for c in SEEDED_OAUTH_CLIENTS} | set(SEEDED_CLIENT_APIS) | set(SEEDED_CLIENT_SECRET_ATTRIBUTES)
    assert not declared & {"user-pool-client", "va-client"}


def test_the_first_party_list_is_the_published_esp_user_client_alone():
    """The identity pool and /v1/user/credentials trust the one client espuser-base publishes, with no duplicate entry."""
    from src.espuser.stacks.base_res_constants import (
        HOME_APP_CLIENT_ID,
        first_party_client_ids,
    )
    assert first_party_client_ids(HOME_APP_CLIENT_ID) == [HOME_APP_CLIENT_ID]
