# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import getpass
import json
import os
import sys

# Ensure the repo root is importable (py_sdk/, test/) regardless of CWD — this file lives in cli/.
_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
_REPO_ROOT = os.path.dirname(_SCRIPT_DIR)
if _REPO_ROOT not in sys.path:
    sys.path.insert(0, _REPO_ROOT)

from scripts.rmng_outputs import RmngSettings, TEST_CONFIG_PATH, oidc_endpoints, verify_aws_identity
from py_sdk.test_user import User
from py_sdk.test_device import Device, generate_key_and_cert
from py_sdk.test_group import Group
from py_sdk.test_matter import do_initiate, do_verify_with_nocsr_elements, do_confirm
from test.device_sim import DeviceSim
import requests
import argparse
import random
import secrets
import string
import traceback
from prompt_toolkit import PromptSession
from prompt_toolkit.history import FileHistory
from prompt_toolkit.auto_suggest import AutoSuggestFromHistory
import boto3
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from botocore.config import Config as BotocoreConfig
from botocore.exceptions import ClientError
from cryptography.hazmat.primitives import serialization

# Working state is anchored to the repo, not the caller's CWD, so running from two directories
# does not silently produce two disjoint sets of credentials, history, and simulator caches.
BOT_IAM_CREDENTIALS_FILE = os.path.join(_REPO_ROOT, "bot-iam-user-credentials.json")
USER_HISTORY_FILE = os.path.join(_REPO_ROOT, "cli", ".user.command_history")
DEVICE_HISTORY_FILE = os.path.join(_REPO_ROOT, "cli", ".device.command_history")
ADMINISTRATOR_ACCESS_POLICY_ARN = "arn:aws:iam::aws:policy/AdministratorAccess"

def _ci_bot_user_name(cfg):
    if not cfg:
        return None
    for u in cfg.get("users") or []:
        if u.get("ci_user") is True:
            name = u.get("name")
            return name if name else None
    return None

def read_config():
    try:
        with open(TEST_CONFIG_PATH, 'r') as config_file:
            config = json.load(config_file)
            return config
    except FileNotFoundError:
        print(f"Note: test_config.json not found at {TEST_CONFIG_PATH}. Run 'python3 cli/morpheus.py --setup-test-data' to create it.")
        return None

config = read_config()
BOT_IAM_USER_NAME = _ci_bot_user_name(config)

TEST_CONFIG_DEFAULTS = "test_config.default.json"


def _generate_password(length=16):
    """Generate a random password satisfying Cognito complexity (upper, lower, digit, symbol)."""
    specials = "!@#$%^&*"
    all_chars = string.ascii_letters + string.digits + specials
    while True:
        pw = ''.join(secrets.choice(all_chars) for _ in range(length))
        if (any(c.islower() for c in pw) and any(c.isupper() for c in pw)
                and any(c.isdigit() for c in pw) and any(c in specials for c in pw)):
            return pw


def generate_config():
    """Write test_config.json from test_config.default.json, fully non-interactively.

    Emails come straight from the defaults file; passwords and device certs are auto-generated.
    No prompts, no environment overrides — edit test_config.default.json to change the identities.
    """
    path = TEST_CONFIG_PATH
    defaults_path = os.path.join(_SCRIPT_DIR, TEST_CONFIG_DEFAULTS)

    try:
        with open(defaults_path) as f:
            cfg = json.load(f)
    except FileNotFoundError:
        print(f"Error: defaults {TEST_CONFIG_DEFAULTS} not found next to morpheus.py.")
        return

    for user in cfg.get('users', []):
        user['password'] = _generate_password()

    # Nodes: generate a fresh cert/key per default thing name (RSA if the name says so, else EC).
    for node in cfg.get('nodes', []):
        thing_name = node.get('thing_name', '')
        key_type = 'rsa' if 'rsa' in thing_name.lower() else 'ec'
        key_pem, cert_pem = generate_key_and_cert(thing_name, key_type)
        node['cert'] = cert_pem
        node['key'] = key_pem

    with open(path, 'w') as f:
        json.dump(cfg, f, indent=2)

    print(f"Wrote {path}: {len(cfg.get('users', []))} user(s), {len(cfg.get('nodes', []))} node(s) "
          "(passwords + certs auto-generated). Gitignored — never commit it.")

# Superseded spellings, still accepted so scripts mid-migration keep working. Kept as literals in
# one place: a blanket search-and-replace of the old flag names must not rewrite these, or the
# aliases silently disappear and the notice below starts comparing a name to itself.
OLD_SETUP_FLAG = ('--setup',)
OLD_DESTROY_FLAG = ('--destroy',)
RENAMED_FLAGS = {OLD_SETUP_FLAG[0]: '--setup-test-data', OLD_DESTROY_FLAG[0]: '--destroy-test-data'}


def parse_args():
    parser = argparse.ArgumentParser(description="Test script for IoT device management")
    # Named for the object they act on, matching --setup-bot-user/--destroy-bot-user, and to keep
    # them distinct from deploy.sh's own --setup/--destroy, which act on CloudFormation stacks.
    parser.add_argument('--setup-test-data', *OLD_SETUP_FLAG, dest='setup', action='store_true',
                        help='Seed the test users and devices from test_config.json into the deployment. Not needed to drive an account that already exists: see --user.')
    parser.add_argument('--destroy-test-data', *OLD_DESTROY_FLAG, dest='destroy', action='store_true',
                        help='Remove the test users\' devices, groups and leftover test certificates from the deployment.')
    parser.add_argument('--setup-bot-user', action='store_true',
                        help='Create IAM user bot with AdministratorAccess; write keys to bot-iam-user-credentials.json')
    parser.add_argument('--destroy-bot-user', action='store_true',
                        help='Delete IAM user bot, access keys, and bot-iam-user-credentials.json')
    parser.add_argument('--user', nargs='?', const='0', help='[raw] Single user-side operation (optional ID)')
    parser.add_argument('--device', nargs='?', const='0', help='[raw] Single device-side operation (optional ID)')
    parser.add_argument('--device-sim', nargs='?', const='0', help='[simulator] Run the sequence of raw --device operations a real device performs (Thing Name in test_config.json)')
    parser.add_argument('--app-sim', nargs='?', const='0', help='[simulator] Run the sequence of raw --user operations a real phone app performs (User ID in test_config.json)')
    parser.add_argument('--client-outputs', default=None, help='RMNG outputs source: a local file path or an http(s) URL (e.g. an S3 client-outputs.json). A relative path resolves against the repo root, not the working directory. Default when omitted: rmng-outputs.json at the repo root.')
    parser.add_argument('--password',
                        help='Password for a --user identity that is not in test_config.json. Prompted for securely if omitted; also read from RMNG_PASSWORD. Prefer the prompt or the env var, since a password in argv is visible to other processes and lands in shell history.')
    parser.add_argument('--is-admin', action='store_true',
                        help='Qualifies --user: the given identity is a super admin, so authenticate it against the admin pool. Only needed for identities not in test_config.json, where the super_admin flag already says so.')
    parser.add_argument('--skip-account-check', action='store_true',
                        help='Do not verify that the configured AWS credentials match the account and region in the outputs. For deliberate cross-account use, e.g. reading a published outputs file only to print configuration instructions.')
    parser.add_argument('--gen-device', nargs=2, metavar=('NODE_NAME', 'KEY_TYPE'),
                       help='Generate device certificates. KEY_TYPE must be "rsa" or "ec"')
    return parser.parse_args()


def warn_renamed_flags(argv):
    for old, new in RENAMED_FLAGS.items():
        if old in argv:
            print(f"Note: {old} is now {new}. The old name still works; please update your scripts.")


args = parse_args()
warn_renamed_flags(sys.argv[1:])

# --setup-test-data owns config generation: on a fresh checkout it writes test_config.json from the
# defaults (auto-generated passwords + certs, no prompts) before doing the actual user/device setup.
if args.setup and config is None:
    generate_config()
    config = read_config()

settings = RmngSettings.from_source(args.client_outputs)

# The CLI reaches AWS directly as well as through the API, so the credentials and the outputs must
# describe the same deployment; otherwise a mismatch mutates an account the caller did not name.
verify_aws_identity(settings, skip=args.skip_account_check)

IDENTITY_POOL_ID = settings.identity_pool_id
API_GATEWAY_URL = settings.api_gateway_url
IOT_ENDPOINT = settings.iot_endpoint
REGION = settings.region
ACCOUNT_ID = settings.account_id

ADMIN_USER_POOL_ID = settings.admin_user_pool_id
ADMIN_USER_POOL_CLIENT_ID = settings.admin_client_id
END_USER_POOL_ID = settings.end_user_pool_id
USER_API_GATEWAY_URL = settings.user_api_gateway_url


def handle_get(user, path):
    response = user.make_api_request('GET', f'/{path}')
    print(f"Response: {response.status_code}")
    print(response.text)

def handle_post(user, path, data):
    print(f"Path: {path}, Data: {data}")
    response = user.make_api_request('POST', f'/{path}', data=data)
    print(f"Response: {response.status_code}")
    print(response.text)

def handle_put(user, path, data):
    print(f"Path: {path}, Data: {data}")
    response = user.make_api_request('PUT', f'/{path}', data=data)
    print(f"Response: {response.status_code}")
    print(response.text)

def handle_delete(user, path):
    # skip_cors_check=True: the raw REPL command is a dev convenience —
    # CORS preflight verification is a deploy-config check, not part of
    # the operation. Skipping avoids OPTIONS-route gaps that don't
    # affect the underlying DELETE working.
    response = user.make_api_request('DELETE', f'/{path}', skip_cors_check=True)
    print(f"Response: {response.status_code}")
    print(response.text)

def handle_patch(user, path, data):
    print(f"Path: {path}, Data: {data}")
    # skip_cors_check=True: same rationale as handle_delete above —
    # PATCH OPTIONS routes are commonly missing in API Gateway and the
    # CORS preflight is a deploy-config check, not part of the operation.
    response = user.make_api_request('PATCH', f'/{path}', data=data, skip_cors_check=True)
    print(f"Response: {response.status_code}")
    print(response.text)


def handle_assoc(user, device, group_id):
    error = user.do_user_node_assoc(device, group_id)
    if error:
        print(f"Association failed: {error}")
    else:
        print("Association successful")

def handle_remove_node_from_group(user, device, group_id):
    group_api = Group(user)
    try:
        group_api.remove_node_from_group(group_id, device.node_thing_name)
        print("Dissociation successful")
    except Exception as e:
        print(f"Dissociation failed: {str(e)}")

def handle_user_connect(user):
    credentials = user.assume_role()
    if credentials:
        print("Role assumed successfully")
        if user.mqtt_connect(credentials):
            print("Successfully connected to MQTT")
        else:
            print("Failed to connect to MQTT")
    else:
        print("Failed to assume role")

def handle_user_subscribe(user, thing_name, named_shadows):
    if user.subscribe_to_named_shadows(thing_name, named_shadows):
        print(f"Successfully subscribed to named shadows for thing '{thing_name}'")
        print(f"Subscribed to named shadows: {', '.join(named_shadows)}")
    else:
        print(f"Failed to subscribe to named shadows for thing '{thing_name}'")

def handle_user_publish(user, thing_name, topic_name, data):
    try:
        # Parse the data as JSON
        json_data = json.loads(data)
    except json.JSONDecodeError:
        print("Error: Invalid JSON data. Please provide valid JSON.")
        return

    if user.mqtt_publish_to_topic(thing_name, topic_name, json_data):
        print(f"Successfully published to topic for thing '{thing_name}'")
    else:
        print(f"Failed to publish to topic for thing '{thing_name}'")

def handle_device_subscribe(device, shadow_name=None, topic=None):
    if shadow_name:
        if device.subscribe(shadow_name=shadow_name):
            print(f"Successfully subscribed to named shadow: {shadow_name}")
        else:
            print(f"Failed to subscribe to named shadow: {shadow_name}")
    elif topic:
        if device.subscribe(topic=topic):
            print(f"Successfully subscribed to topic: {topic}")
        else:
            print(f"Failed to subscribe to topic: {topic}")
    else:
        print("Error: Either shadow_name or topic must be provided")

def handle_device_publish(device, data, shadow_name=None):
    if device.update_shadow(data, shadow_name):
        print(f"Successfully published to {'named shadow' if shadow_name else 'device shadow'}")
    else:
        print(f"Failed to publish to {'named shadow' if shadow_name else 'device shadow'}")

def handle_user_auth(user: User) -> bool:
    print(f"Authenticating user: {user.username}")
    success = user.get_aws_credentials()
    if success:
        print(f"Authentication successful for user: {user.username}")
    else:
        print(f"Authentication failed for user: {user.username}")

    # Super admins go into the admin pool; end users into the provider pool they sign in against.
    if user.is_super_admin:
        user.create_super_admin_via_cognito()
    else:
        user.register_user_via_lambda(email=user.username if '@' in user.username else None)
    return success

def handle_device_connect(device):
    if device.connect():
        print("Successfully connected device and subscribed to from_cloud topic")
    else:
        print("Failed to connect device or subscribe to from_cloud topic")

def handle_device_command(sub_command, args, device):
    if sub_command == 'connect':
        handle_device_connect(device)
    elif sub_command == 'subscribe':
        if len(args) == 1:
            entity = args[0]
            if entity.startswith("shadow:"):
                shadow_name = entity[7:]
                handle_device_subscribe(device, shadow_name=shadow_name)
            elif entity.startswith("topic:"):
                topic = entity[6:]
                handle_device_subscribe(device, topic=topic)
            else:
                print("Syntax: subscribe shadow:<named_shadow> OR subscribe topic:<topic_name>")
        else:
            print("Syntax: subscribe shadow:<named_shadow> OR subscribe topic:<topic_name>")
    elif sub_command == 'shadow_connect':
        if len(args) == 1:
            shadow_name = args[0]
            if device.shadow_connect([shadow_name]):
                print(f"Successfully connected to shadow: {shadow_name}")
            else:
                print(f"Failed to connect to shadow: {shadow_name}")
        else:
            print("Syntax: shadow_connect <shadow_name>")
    elif sub_command == 'publish':
        if len(args) >= 2:
            shadow_name = args[0]
            data = ' '.join(args[1:])
            handle_device_publish(device, data, shadow_name)
        else:
            print("Syntax: publish <shadow_name> <json_data>")
    elif sub_command == 'to_cloud':
        if args:
            data = ' '.join(args)
            handle_device_to_cloud(device, data)
        else:
            print("Syntax: to_cloud <json_data or file:path/to/json_file>")
    elif sub_command == 'get_group_info':
        handle_device_get_group_info(device)
    elif sub_command == 'set_node_config':
        if len(args) == 1 and args[0].startswith("file:"):
            file_path = args[0][5:]  # Remove "file:" prefix
            handle_device_set_node_config(device, file_path)
        else:
            print("Syntax: set_node_config file:<path/to/json_file>")
    elif sub_command == 'direct_notify':
        if len(args) >= 1:
            # Join all args for JSON data
            json_data = ' '.join(args)
            handle_device_direct_notify(device, json_data)
        else:
            print("Syntax: direct_notify <json_data or file:path/to/json_file>")
    else:
        print("Unknown device subcommand. Available subcommands:")
        print("  connect")
        print("  shadow_connect <shadow_name>")
        print("  subscribe shadow:<named_shadow> OR subscribe topic:<topic_name>")
        print("  publish <shadow_name> <json_data>")
        print("  to_cloud <json_data or file:path/to/json_file>")
        print("  get_group_info")
        print("  set_node_config file:<path/to/json_file>")
        print("  direct_notify <json_data or file:path/to/json_file>")

def _resolve_password(username):
    """Password for an identity that test_config.json does not carry.

    Ordered so automation has a non-interactive route without pushing real credentials through
    argv, where other processes and shell history can see them.
    """
    if args.password:
        return args.password
    if os.environ.get('RMNG_PASSWORD'):
        return os.environ['RMNG_PASSWORD']
    try:
        return getpass.getpass(f"Password for {username}: ")
    except (EOFError, KeyboardInterrupt):
        print()
        return None


def get_user(user_id):
    """Build a User from test_config.json, or from the credentials given on the command line.

    Any account already provisioned in the deployment can be driven directly, so exercising an
    existing user or admin does not require --setup-test-data or an entry in test_config.json.
    """
    users = (config or {}).get('users', [])
    user_config = None
    try:
        index = int(user_id)
        if 0 <= index < len(users):
            user_config = users[index]
        else:
            # A bare number is always an index; treating an out-of-range one as a username would
            # turn a typo into a password prompt.
            print(f"Error: no user at index {index} in test_config.json ({len(users)} configured)")
            return None
    except ValueError:
        user_config = next((u for u in users if u.get('name') == user_id), None)

    if user_config is None:
        password = _resolve_password(user_id)
        if not password:
            print(f"Error: no password supplied for '{user_id}'")
            return None
        user_config = {'name': user_id, 'password': password, 'super_admin': args.is_admin}

    username = user_config.get('name')
    password = user_config.get('password')
    is_super_admin = user_config.get('super_admin', False)

    if not username:
        print(f"Error: user {user_id} is missing 'name'")
        return None

    if is_super_admin:
        # Admins authenticate against the admin Cognito pool (USER_PASSWORD_AUTH), so a password is required.
        if not password:
            print(f"Error: super admin {username} is missing 'password'")
            return None
        return User(username, password, REGION, IDENTITY_POOL_ID, API_GATEWAY_URL, USER_API_GATEWAY_URL, IOT_ENDPOINT,
                    admin_user_pool_id=ADMIN_USER_POOL_ID, admin_client_id=ADMIN_USER_POOL_CLIENT_ID, is_super_admin=True)

    if '@' not in username:
        print(f"Error: end user {username} must have an email 'name'")
        return None
    if not password:
        print(f"Error: end user {username} is missing 'password'")
        return None
    return User(username, password, REGION, IDENTITY_POOL_ID, API_GATEWAY_URL, USER_API_GATEWAY_URL, IOT_ENDPOINT,
                end_user_pool_id=END_USER_POOL_ID)

# By id or node_id
def get_node(index_or_device_id):
    # Devices need a cert and key, so unlike users they cannot be supplied on the command line.
    nodes = (config or {}).get('nodes', [])
    try:
        index = int(index_or_device_id)
        if 0 <= index < len(nodes):
            node_config = nodes[index]
        else:
            raise ValueError
    except ValueError:
        for node in nodes:
            if node.get('thing_name') == index_or_device_id:
                node_config = node
                break
        else:
            print(f"Error: Device '{index_or_device_id}' not found")
            return None

    thing_name = node_config.get('thing_name')
    cert = node_config.get('cert')
    key = node_config.get('key')

    if not thing_name or not cert or not key:
        print(f"Error: Missing required configuration for device {index_or_device_id}")
        return None

    ca_cert = config.get('ca_cert')
    if not ca_cert:
        print("Error: Missing CA certificate in test_config.json")
        return None

    debug = config.get('debug', False)

    return Device(thing_name, key, cert, ca_cert, IOT_ENDPOINT, REGION, debug)

def handle_user_commands(user):
    while True:
        session = PromptSession(history=FileHistory(USER_HISTORY_FILE), auto_suggest=AutoSuggestFromHistory(), complete_while_typing=True)
        command = session.prompt("Enter user command (or 'q|quit' to exit user context): ")
        if command.lower() == 'quit' or command.lower() == 'q':
            break

        parts = command.split()
        if not parts:
            continue

        main_command = parts[0].lower()
        args = parts[1:]

        if main_command == 'add_node_to_subgroup':
            if len(args) == 3:
                group_id, subgroup_id, node_id = args
                handle_add_node_to_subgroup(user, group_id, subgroup_id, node_id)
            else:
                print("Syntax: add_node_to_subgroup <group_id> <subgroup_id> <node_id>")
        elif main_command == 'claim_device':
            # claim_device [mac_addr] [capability ...] -- assisted claiming: reserve
            # a node ID for the MAC and exchange a CSR for a signed certificate.
            mac_addr = args[0] if args else None
            capabilities = args[1:] if len(args) > 1 else None
            handle_claim(user, mac_addr, capabilities)
        elif main_command == 'remove_node_from_subgroup':
            if len(args) == 3:
                group_id, subgroup_id, node_id = args
                handle_remove_node_from_subgroup(user, group_id, subgroup_id, node_id)
            else:
                print("Syntax: remove_node_from_subgroup <group_id> <subgroup_id> <node_id>")
        elif main_command == 'update_group':
            if len(args) >= 2:
                group_id = args[0]
                new_group_name = ' '.join(args[1:])
                handle_update_group(user, group_id, new_group_name)
            else:
                print("Syntax: update_group <group_id> <new_group_name>")
        elif main_command == 'update_subgroup':
            if len(args) >= 3:
                group_id = args[0]
                subgroup_id = args[1]
                new_subgroup_name = ' '.join(args[2:])
                handle_update_subgroup(user, group_id, subgroup_id, new_subgroup_name)
            else:
                print("Syntax: update_subgroup <group_id> <subgroup_id> <new_subgroup_name>")
        elif main_command == 'alexa_setup':
            if len(args) == 1:
                config_file = args[0]
                handle_alexa_setup(user, config_file)
            else:
                print("Syntax: alexa_setup <config_file>")
        elif main_command == 'alexa_setup_auto':
            # alexa_setup_auto [config.json] [skill name...] -- both optional. A first
            # arg ending in .json is the config path; the rest is an optional skill name.
            config_file, name_parts = DEFAULT_ALEXA_CONFIG, args
            if args and args[0].endswith('.json'):
                config_file, name_parts = args[0], args[1:]
            handle_alexa_setup_auto(user, config_file,
                                    ' '.join(name_parts) if name_parts else None)
        elif main_command == 'alexa_list_skills':
            handle_alexa_list_skills(args[0] if args else DEFAULT_ALEXA_CONFIG)
        elif main_command == 'alexa_delete_skill':
            if len(args) >= 1:
                config_file = args[1] if len(args) > 1 else DEFAULT_ALEXA_CONFIG
                handle_alexa_delete_skill(config_file, args[0])
            else:
                print("Syntax: alexa_delete_skill <skill_id> [config_file]")
        elif main_command == 'alexa_instruction':
            print("==============")
            print("Please configure the Alexa Skill in the Alexa Developer Console. Then use the 'alexa_setup' command for the initial configuration")
            print_alexa_skill_instructions()
        elif main_command == 'gva_instruction':
            print("==============")
            print("Configure the Google Home project, then use 'gva_setup' to upload the service account JSON")
            print_gva_instructions()
        elif main_command == 'gva_setup':
            if len(args) == 1:
                config_file = args[0]
                handle_gva_setup(user, config_file)
            else:
                print("Syntax: gva_setup <service_account.json>")
        elif main_command == 'st_instruction':
            print("==============")
            print("Register the Schema App in the SmartThings Developer Center, then use 'st_setup' to store the credentials it issues")
            print_smartthings_instructions()
        elif main_command == 'st_setup':
            if len(args) == 1:
                handle_st_setup(user, args[0])
            else:
                print('Syntax: st_setup <config_file>  (JSON: {"client_id": "...", "client_secret": "..."})')
        elif main_command == 'st_get_config':
            handle_st_get_config(user)
        elif main_command == 'st_delete_config':
            handle_st_delete_config(user)
        elif main_command == 'register_client':
            if len(args) == 2:
                platform = args[0]
                mobile_device_token = args[1]
                handle_register_client(user, platform, mobile_device_token)
            else:
                print("Syntax: register_client <platform> <mobile_device_token>")
        elif main_command == 'register_client_android':
            if len(args) == 2:
                android_project_id, device_token = args
                handle_register_client_android(user, android_project_id, device_token)
            else:
                print("Syntax: register_client_android <android-project-id> <device-token>")
        elif main_command == 'register_client_ios':
            if len(args) == 2 or len(args) == 3:
                ios_bundle_id, device_token = args[:2]
                sandbox = len(args) == 3 and args[2].lower() == 'sandbox'
                handle_register_client_ios(user, sandbox, ios_bundle_id, device_token)
            else:
                print("Syntax: register_client_ios <ios-bundle-id> <device-token> [sandbox]")
        elif main_command == 'auth':
            handle_user_auth(user)
        elif main_command == 'connect':
            handle_user_connect(user)
        elif main_command == 'subscribe':
            if len(args) >= 2:
                thing_name = args[0]
                named_shadows = args[1:]
                handle_user_subscribe(user, thing_name, named_shadows)
            else:
                print("Syntax: subscribe <thing_name> <named_shadow1> [named_shadow2 ...]")
        elif main_command == 'publish':
            if len(args) >= 3:
                thing_name = args[0]
                topic_name = args[1]
                data = ' '.join(args[2:])
                handle_user_publish(user, thing_name, topic_name, data)
            else:
                print("Syntax: publish <thing_name> <topic_name> <json_data>")
        elif main_command == 'create_group':
            if args:
                matter = False
                filtered_args = []
                for a in args:
                    if a == '--matter':
                        matter = True
                    else:
                        filtered_args.append(a)
                if filtered_args:
                    group_name = ' '.join(filtered_args)
                    handle_create_group(user, group_name, matter=matter)
                else:
                    print('Syntax: create_group [--matter] <group_name>')
            else:
                print('Syntax: create_group [--matter] <group_name>')
        elif main_command == 'list_groups':
            handle_list_groups(user)
        elif main_command == 'add_capabilities':
            if len(args) == 2:
                group_id = args[0]
                capabilities = [cap.strip() for cap in args[1].split(',') if cap.strip()]
                handle_add_capabilities(user, group_id, capabilities)
            else:
                print('Syntax: add_capabilities <group_id> <capability1[,capability2,...]>')
                print('  Example (convert a group to a Matter fabric): add_capabilities <group_id> matter')
        elif main_command == 'create_subgroup':
            if len(args) == 2:
                group_id, subgroup_name = args
                handle_create_subgroup(user, group_id, subgroup_name)
            else:
                print('Syntax: create_subgroup <group_id> <subgroup_name>')
        elif main_command == 'assoc':
            if len(args) == 2:
                device_id, group_id = args
                device_to_assoc = get_node(device_id)
                if device_to_assoc:
                    handle_assoc(user, device_to_assoc, group_id)
                else:
                    print(f"Device with ID {device_id} not found")
            else:
                print("Syntax: assoc <device_id> <group_id>")
                print("  Note: For Matter nodes, use 'matter_assoc' with optional capabilities")
        elif main_command == 'matter_assoc':
            if len(args) >= 2:
                group_id = args[0]
                sub_cmd = args[1]
                if sub_cmd == 'initiate':
                    handle_matter_assoc_initiate(user, group_id)
                elif sub_cmd == 'verify':
                    if len(args) >= 5:
                        nocsr_elements_hex = args[2]
                        attestation_challenge_hex = args[3]
                        attestation_signature_hex = args[4]
                        request_id = args[5] if len(args) > 5 else None
                        handle_matter_assoc_verify(user, group_id, nocsr_elements_hex, attestation_challenge_hex, attestation_signature_hex, request_id)
                    else:
                        print("Syntax: matter_assoc <group_id> verify <nocsr_elements_hex> <attestation_challenge_hex> <attestation_signature_hex> [request_id]")
                elif sub_cmd == 'confirm':
                    request_id = None
                    capabilities = None
                    if len(args) > 2:
                        request_id = args[2]
                    if len(args) > 3:
                        capabilities = [cap.strip() for cap in args[3].split(',')]
                    handle_matter_assoc_confirm(user, group_id, request_id, capabilities)
                else:
                    print(f"Unknown sub-command: {sub_cmd}. Use initiate, verify, or confirm.")
            else:
                print("Syntax: matter_assoc <group_id> <initiate|verify|confirm> ...")
                print("  Examples:")
                print("    matter_assoc <group_id> initiate")
                print("    matter_assoc <group_id> verify <nocsr_hex> <challenge_hex> <sig_hex> [request_id]")
                print("    matter_assoc <group_id> confirm [request_id] [capabilities]")
        elif main_command == 'matter_get_noc':
            if len(args) == 1:
                handle_matter_get_noc(user, args[0])
            else:
                print("Syntax: matter_get_noc <group_id>")
        elif main_command == 'remove_node_from_group':
            if len(args) == 2:
                device_id, group_id = args
                device_to_dssoc = get_node(device_id)
                if device_to_dssoc:
                    handle_remove_node_from_group(user, device_to_dssoc, group_id)
                else:
                    print(f"Device with ID {device_id} not found")
            else:
                print("Syntax: remove_node_from_group <device_id> <group_id>")
        elif main_command == 'get':
            if len(args) == 1:
                handle_get(user, args[0])
            else:
                print("Syntax: get <path>")
        elif main_command == 'post':
            if len(args) >= 2:
                path = args[0]
                data = ' '.join(args[1:])
                handle_post(user, path, data)
            else:
                print("Syntax: post <path> <data>")
        elif main_command == 'put':
            if len(args) >= 2:
                path = args[0]
                data = ' '.join(args[1:])
                handle_put(user, path, data)
            else:
                print("Syntax: put <path> <data>")
        elif main_command == 'patch':
            if len(args) >= 2:
                path = args[0]
                data = ' '.join(args[1:])
                handle_patch(user, path, data)
            else:
                print("Syntax: patch <path> <data>")
        elif main_command == 'delete':
            if len(args) == 1:
                handle_delete(user, args[0])
            else:
                print("Syntax: delete <path>")
        elif main_command == 'read_shadow':
            if len(args) == 2:
                thing_name, shadow_name = args
                handle_user_read_shadow(user, thing_name, shadow_name)
            else:
                print("Syntax: read_shadow <thing_name> <shadow_name>")
        elif main_command == 'get_sharing_requests':
            handle_get_sharing_requests(user)
        elif main_command in ['accept_sharing_request', 'reject_sharing_request']:
            if len(args) != 1:
                print(f"Syntax: {main_command} <sharing_request_id>")
            else:
                handle_process_sharing_request(user, main_command.split('_')[0], args[0])
        elif main_command == 'register_node':
            if len(args) == 2:
                node_id, admin_group_names_str, tags_str = args
                handle_register_node(user, node_id, admin_group_names_str, tags_str=tags_str)
            else:
                print("Syntax: register_node <node_id> <admin_group_names> <tags>")
        elif main_command == 'upload_file':
            if len(args) == 2:
                file_type, file_path = args
                handle_upload_file(user, file_type, file_path)
            else:
                print("Syntax: upload_file <file_type> <file_path>")
        elif main_command == 'ios_instruction':
            print("==============")
            print("Obtain the APNS credentials from the Apple Developer Console. Then use 'register_ios_platform' to register the integration.")
            print_ios_platform_instructions()
        elif main_command == 'android_instruction':
            print("==============")
            print("Obtain the Firebase service-account JSON. Then use 'register_android_platform' to register the integration.")
            print_android_platform_instructions()
        elif main_command == 'register_ios_platform':
            if len(args) >= 4:
                p8_key_file = args[0]
                key_id = args[1]
                team_id = args[2]
                bundle_id = args[3]
                sandbox = len(args) > 4 and args[4].lower() == 'sandbox'
                handle_register_ios_platform(user, p8_key_file, key_id, team_id, bundle_id, sandbox)
            else:
                print("Syntax: register_ios_platform <p8_key_file> <key_id> <team_id> <bundle_id> [sandbox]")
        elif main_command == 'register_android_platform':
            if len(args) == 1:
                json_file_path = args[0]
                handle_register_android_platform(user, json_file_path)
            else:
                print("Syntax: register_android_platform <json_file_path>")
        elif main_command == 'list_mobile_platforms':
            handle_list_mobile_platforms(user)
        elif main_command == 'update_ios_platform':
            if len(args) >= 4:
                p8_key_file = args[0]
                key_id = args[1]
                team_id = args[2]
                bundle_id = args[3]
                sandbox = len(args) > 4 and args[4].lower() == 'sandbox'
                handle_update_ios_platform(user, p8_key_file, key_id, team_id, bundle_id, sandbox)
            else:
                print("Syntax: update_ios_platform <p8_key_file> <key_id> <team_id> <bundle_id> [sandbox]")
        elif main_command == 'update_android_platform':
            if len(args) == 1:
                json_file_path = args[0]
                handle_update_android_platform(user, json_file_path)
            else:
                print("Syntax: update_android_platform <json_file_path>")
        elif main_command == 'delete_mobile_platform':
            if len(args) == 2:
                platform_name, platform_app_name = args
                handle_delete_mobile_platform(user, platform_name, platform_app_name)
            else:
                print("Syntax: delete_mobile_platform <platform_name> <platform_app_name>")
        elif main_command == 'bulk_register_nodes':
            if len(args) >= 1:
                file_path = args[0]
                admin_group_names_str = None
                tags_str = None
                if len(args) == 2:
                    admin_group_names_str = args[1]
                elif len(args) == 3:
                    admin_group_names_str = args[1]
                    tags_str = args[2]
                handle_bulk_register_nodes(user, file_path, admin_group_names_str, tags_str)
            else:
                print("Syntax: bulk_register_nodes <file_path> [<admin_group_names>] [<tags>]")
        elif main_command == 'get_bulk_register_status':
            if len(args) == 1:
                request_id = args[0]
                handle_get_bulk_register_status(user, request_id)
            else:
                print("Syntax: get_bulk_register_status <request_id>")
        elif main_command == 'get_iot_event_mode':
            handle_get_iot_event_mode(user)
        elif main_command == 'set_iot_event_mode':
            if len(args) == 1:
                handle_set_iot_event_mode(user, args[0].lower())
            else:
                print("Syntax: set_iot_event_mode <direct|sqs>")
        elif main_command == 'setup_ses_sender':
            setup_ses_mailosaur_identity()
        elif main_command == 'request_ses_production':
            request_ses_production_access()
        elif main_command == 'request_sns_production':
            request_sns_production_access()
        elif main_command == 'enable_claim':
            # enable_claim [config_file.json] -- superadmin: mint the claiming CA
            # (after deploying the claim stacks) to turn claiming on, optionally
            # applying a certificate configuration first.
            handle_enable_claim(user, args[0] if args else None)
        else:
            print("Unknown command. Available commands:")
            print("  alexa_setup <config_file>")
            print("  alexa_instruction")
            print("  gva_setup <service_account.json>")
            print("  gva_instruction")
            print("  st_setup <config_file>")
            print("  st_instruction")
            print("  st_get_config")
            print("  st_delete_config")
            print("  register_client <platform> <mobile_device_token>")
            print("  register_client_android <android-project-id> <device-token>")
            print("  register_client_ios <ios-bundle-id> <device-token> [sandbox]")
            print("  auth")
            print("  connect")
            print("  subscribe <thing_name> <named_shadow1> [named_shadow2 ...]")
            print("  publish <thing_name> <topic_name> <json_data>")
            print("  create_group [--matter] <group_name>")
            print("  create_subgroup <group_id> <subgroup_name>")
            print("  add_capabilities <group_id> <capability1[,capability2,...]>")
            print("  list_groups")
            print("  update_group <group_id> <new_group_name>")
            print("  update_subgroup <group_id> <subgroup_id> <new_subgroup_name>")
            print("  assoc <device_id> <group_id>")
            print("  matter_assoc <group_id> initiate")
            print("  matter_assoc <group_id> verify <nocsr_elements_hex> <attestation_challenge_hex> <attestation_signature_hex> [request_id]")
            print("  matter_assoc <group_id> confirm [request_id] [capabilities]")
            print("  matter_get_noc <group_id>")
            print("  remove_node_from_group <device_id> <group_id>")
            print("  claim_device [mac_addr] [capability ...]")
            print("  add_node_to_subgroup <group_id> <subgroup_id> <node_id>")
            print("  remove_node_from_subgroup <group_id> <subgroup_id> <node_id>")
            print("  get <path>")
            print("  post <path> <data>")
            print("  put <path> <data>")
            print("  patch <path> <data>")
            print("  delete <path>")
            print("  read_shadow <thing_name> <shadow_name>")
            print("  get_sharing_requests")
            print("  accept_sharing_request <sharing_request_id>")
            print("  reject_sharing_request <sharing_request_id>")
            print("  register_node <node_id> <tags>")
            print("  upload_file <file_type> <file_path>")
            print("  ios_instruction")
            print("  register_ios_platform <p8_key_file> <key_id> <team_id> <bundle_id> [sandbox]")
            print("  android_instruction")
            print("  register_android_platform <json_file_path>")
            print("  list_mobile_platforms")
            print("  update_ios_platform <p8_key_file> <key_id> <team_id> <bundle_id> [sandbox]")
            print("  update_android_platform <json_file_path>")
            print("  delete_mobile_platform <platform_name> <platform_app_name>")
            print("  bulk_register_nodes <file_path> <admin_group_names> <tags>")
            print("  get_bulk_register_status <request_id>")
            print("  get_iot_event_mode")
            print("  set_iot_event_mode <direct|sqs>")
            print("  setup_ses_sender")
            print("  request_ses_production")
            print("  request_sns_production")
            print("  enable_claim [config_file.json]  (superadmin: mint the claiming CA)")

def handle_device_commands(device):
    while True:
        try:
            session = PromptSession(history=FileHistory(DEVICE_HISTORY_FILE), auto_suggest=AutoSuggestFromHistory())
            command = session.prompt("Enter device command (or 'quit' to exit device context): ")
            if command == "quit":
                break

            parts = command.split()
            if not parts:
                continue

            sub_command = parts[0].lower()
            args = parts[1:]

            handle_device_command(sub_command, args, device)
        except Exception as e:
            print(f"Error executing command: {str(e)}")

def handle_create_subgroup(user, group_id, subgroup_name):
    group_api = Group(user)
    subgroup_id = group_api.create_subgroup(group_id, subgroup_name)
    if subgroup_id:
        print(f"Subgroup '{subgroup_name}' created successfully. Subgroup ID: {subgroup_id}")
    else:
        print(f"Failed to create subgroup '{subgroup_name}' in group {group_id}")

def handle_create_group(user, group_name, matter=False):
    group_api = Group(user)
    if matter:
        try:
            result = group_api.create_matter_group(group_name)
            group_id = result['group_id']
            matter_info = result.get('matter', {})
            print(f"Matter Group '{group_name}' created successfully. Group ID: {group_id}")
            print(f"  Fabric ID: {matter_info.get('fabric_id', 'N/A')}")
            print(f"  IPK: {matter_info.get('ipk', 'N/A')}")
            print(f"  CAT ID Admin: {matter_info.get('group_cat_id_admin', 'N/A')}")
            print(f"  CAT ID Operate: {matter_info.get('group_cat_id_operate', 'N/A')}")
            print(f"  Root CA: {matter_info.get('root_ca', 'N/A')}")
            user.add_group_id(group_id)
            print(f"Group ID added to user {user.username}'s group list")
        except AssertionError as e:
            print(f"Failed to create Matter group '{group_name}': {e}")
    else:
        group_id = group_api.create_group(group_name)
        if group_id:
            print(f"Group '{group_name}' created successfully. Group ID: {group_id}")
            user.add_group_id(group_id)
            print(f"Group ID added to user {user.username}'s group list")
        else:
            print(f"Failed to create group '{group_name}'")

def handle_add_capabilities(user, group_id, capabilities):
    group_api = Group(user)
    resp = group_api.add_group_capabilities(group_id, capabilities)
    if resp.status_code == 200:
        print(f"Capabilities {capabilities} enabled on group {group_id}")
        matter_info = resp.json().get('matter')
        if matter_info:
            print(f"  [Matter Enabled]")
            print(f"  Fabric ID: {matter_info.get('fabric_id', 'N/A')}")
            print(f"  IPK: {matter_info.get('ipk', 'N/A')}")
            print(f"  CAT ID Admin: {matter_info.get('group_cat_id_admin', 'N/A')}")
            print(f"  CAT ID Operate: {matter_info.get('group_cat_id_operate', 'N/A')}")
            print(f"  Root CA: {matter_info.get('root_ca', 'N/A')}")
    else:
        print(f"Failed to enable capabilities {capabilities} on group {group_id}. "
              f"Status code: {resp.status_code}, Response: {resp.text}")

def handle_list_groups(user):
    group_api = Group(user)
    groups_data = group_api.list_groups()
    if "groups" in groups_data:
        for group in groups_data["groups"]:
            print(f"Group ID: {group['group_id']}")
            if "matter" in group:
                matter_info = group["matter"]
                print(f"  [Matter Enabled]")
                print(f"  Fabric ID: {matter_info.get('fabric_id', 'N/A')}")
                print(f"  IPK: {matter_info.get('ipk', 'N/A')}")
                print(f"  CAT ID Admin: {matter_info.get('group_cat_id_admin', 'N/A')}")
                print(f"  CAT ID Operate: {matter_info.get('group_cat_id_operate', 'N/A')}")
                print(f"  Root CA: {matter_info.get('root_ca', 'N/A')}")
            user.add_group_id(group["group_id"])
        print(f"Retrieved and stored {len(user.get_group_ids())} groups for user {user.username}")
    else:
        print("No groups found for the user")

def handle_matter_assoc_initiate(user, group_id):
    request_id, challenge = do_initiate(user, group_id)
    if request_id is None:
        print(f"Initiate failed: {challenge}")
        return
    print(f"Request ID: {request_id}")
    print(f"Challenge: {challenge}")
    if not hasattr(user, '_matter_assoc_state'):
        user._matter_assoc_state = {}
    user._matter_assoc_state = {
        'request_id': request_id,
        'challenge': challenge,
        'group_id': group_id,
    }
    print(f"If you are using interactive Chip Tool (chip-tool interactive start --commissioner-name gamma), you can now take the next step as:")
    print(f"    operationalcredentials csrrequest hex:{challenge} 2 0")

def handle_matter_assoc_verify(user, group_id, nocsr_elements_hex, attestation_challenge_hex, attestation_signature_hex, request_id=None):
    state = getattr(user, '_matter_assoc_state', {})
    if request_id is None:
        request_id = state.get('request_id')
    if not request_id:
        print("Error: No stored request_id. Run 'matter_assoc <group_id> initiate' first, or provide request_id.")
        return

    try:
        nocsr_elements = bytes.fromhex(nocsr_elements_hex)
        attestation_challenge = bytes.fromhex(attestation_challenge_hex)
        attestation_signature = bytes.fromhex(attestation_signature_hex)
    except ValueError as e:
        print(f"Error: Invalid hex input: {e}")
        return

    verify_result, error = do_verify_with_nocsr_elements(
        user, group_id, request_id, nocsr_elements, attestation_challenge, attestation_signature
    )
    if error:
        print(f"Verify failed: {error}")
        return

    print(f"Verify successful!")
    print(f"  NOC: {verify_result.get('noc', 'N/A')}")
    print(f"  Matter Node ID: {verify_result.get('matter_node_id', 'N/A')}")
    if hasattr(user, '_matter_assoc_state'):
        user._matter_assoc_state['verify_result'] = verify_result

    print(f"If you are using interactive Chip Tool (chip-tool interactive start --commissioner-name gamma), you can now take the next step as:")
    print(f"    operationalcredentials add-trusted-root-certificate hex:<rca-encoded as tlv> 2 0")
    print(f"    operationalcredentials add-noc hex:<noc-encoded as tlv> hex:<ipk-encoded as tlv> <user node id> 0x131B 2 0")


def handle_matter_assoc_confirm(user, group_id, request_id=None, capabilities=None):
    state = getattr(user, '_matter_assoc_state', {})
    if request_id is None:
        request_id = state.get('request_id')
    if not request_id:
        print("Error: No stored request_id. Run 'matter_assoc <device_id> <group_id> initiate' first, or provide request_id.")
        return

    result = do_confirm(user, group_id, request_id, capabilities)
    if result is True:
        print("Confirm successful! Association complete.")
        if capabilities:
            print(f"Enabled capabilities: {', '.join(capabilities)}")
        user._matter_assoc_state = {}
    else:
        print(f"Confirm failed: {result}")

def get_hex_from_pem_cert(pem_str):
    if pem_str == 'N/A':
        return 'N/A'
    from cryptography import x509
    return x509.load_pem_x509_certificate(pem_str.encode()).public_bytes(serialization.Encoding.DER).hex()

def get_hex_from_pem_key(pem_str):
    return serialization.load_pem_private_key(pem_str.encode(), password=None).private_bytes(serialization.Encoding.DER, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()).hex()

def handle_matter_get_noc(user, group_id):
    result = user.get_matter_noc(group_id)
    if result:
        print(f"Matter Node ID: {result.get('matter_node_id', 'N/A')}")
        noc_pem = result.get('noc', 'N/A')
        print(f"NOC: {noc_pem}")
        print(f"NOC (hex): {get_hex_from_pem_cert(noc_pem)}")
        key_pem = user.matter_private_key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()).decode()
        print(f"\nUser NOC Private Key: {key_pem}")
        print(f"User NOC Private Key (hex): {get_hex_from_pem_key(key_pem)}")
    else:
        print("Failed to get Matter NOC")

def handle_device_to_cloud(device, args):
    try:
        if args.startswith("file:"):
            # Read JSON data from file
            file_path = args[5:].strip()
            with open(file_path, 'r') as file:
                json_data = json.load(file)
        else:
            # Parse JSON data directly from command line
            json_data = json.loads(args)

        if device.publish_to_cloud(json_data):
            print("Successfully published data to cloud")
        else:
            print("Failed to publish data to cloud")
    except json.JSONDecodeError:
        print("Error: Invalid JSON data")
    except FileNotFoundError:
        print(f"Error: File not found - {args[5:].strip()}")
    except Exception as e:
        print(f"Error: {str(e)}")

def handle_device_direct_notify(device, json_data_str):
    try:
        # Parse JSON data
        if json_data_str.startswith("file:"):
            # Read JSON data from file
            file_path = json_data_str[5:].strip()
            with open(file_path, 'r') as file:
                json_data = json.load(file)
        else:
            # Parse JSON data directly from command line
            json_data = json.loads(json_data_str)

        if device.send_direct_notification(json_data):
            print("Successfully sent direct notification using device's group information")
        else:
            print("Failed to send direct notification")

    except json.JSONDecodeError:
        print("Error: Invalid JSON data")
    except FileNotFoundError:
        print(f"Error: File not found - {json_data_str[5:].strip() if json_data_str.startswith('file:') else 'unknown'}")
    except Exception as e:
        print(f"Error: {e}")
        print("Usage: direct_notify <json_data or file:path/to/json_file>")
        print("Example: direct_notify {\"test\": true}")
        print("Example: direct_notify file:test/direct_notification_example.json")
        print("Note: Device must call get_group_info first to populate group information")

def handle_device_get_group_info(device):
    device.get_group_info()
    if device.group_id:
        print(f"Device's Group ID: {device.group_id}")
    if hasattr(device, 'subgroup_ids') and device.subgroup_ids:
        print(f"Device's Subgroup IDs: {device.subgroup_ids}")
    else:
        print("Failed to get group info or device is not associated with any group")

def handle_device_set_node_config(device, file_path):
    try:
        with open(file_path, 'r') as file:
            config_data = json.load(file)

        if device.set_node_config(config_data):
            print("Node configuration set successfully")
        else:
            print("Failed to set node configuration")
    except FileNotFoundError:
        print(f"Error: File not found - {file_path}")
    except json.JSONDecodeError:
        print(f"Error: Invalid JSON data in file {file_path}")
    except Exception as e:
        print(f"Error: {str(e)}")

def handle_user_read_shadow(user, thing_name, shadow_name):
    if user.read_shadow(thing_name, shadow_name):
        print(f"Successfully requested shadow read for thing '{thing_name}', shadow '{shadow_name}'")
    else:
        print(f"Failed to request shadow read for thing '{thing_name}', shadow '{shadow_name}'")

def handle_register_client(user, platform, mobile_device_token):
    endpt_id = user.register_client(platform, mobile_device_token)

    if endpt_id:
        print(f"Client registered successfully. Endpoint Id: {endpt_id}")
    else:
        print("Failed to register client.")

def handle_register_client_android(user, android_project_id, device_token):
    endpt_id = user.register_client(platform_type="GCM", mobile_device_token=device_token, platform_app_name=android_project_id)
    if endpt_id:
        print(f"Client registered successfully. Endpoint Id: {endpt_id}")
    else:
        print("Failed to register client.")

def handle_register_client_ios(user, sandbox=False, ios_bundle_id='', device_token=''):
    if sandbox:
        platform_type = "APNS_SANDBOX"
    else:
        platform_type = "APNS"
    endpt_id = user.register_client(platform_type=platform_type, mobile_device_token=device_token, platform_app_name=ios_bundle_id)
    if endpt_id:
        print(f"Client registered successfully. Endpoint Id: {endpt_id}")
    else:
        print("Failed to register client.")

def handle_get_sharing_requests(user):
    sharing_requests = user.get_sharing_requests()
    if sharing_requests is not None:
        if sharing_requests:
            print("Current sharing requests:")
            for request_id in sharing_requests:
                print(f"- {request_id}")
        else:
            print("No pending sharing requests.")
    else:
        print("Failed to retrieve sharing requests.")

def handle_process_sharing_request(user, action, sharing_request_id):
    if action not in ['accept', 'reject']:
        print(f"Invalid action: {action}. Must be 'accept' or 'reject'.")
        return

    if action == 'accept':
        success = user.accept_sharing_request(sharing_request_id)
    else:
        success = user.reject_sharing_request(sharing_request_id)

    if success:
        print(f"Successfully {action}ed sharing request: {sharing_request_id}")
    else:
        print(f"Failed to {action} sharing request: {sharing_request_id}")

def _oidc_endpoints():
    """The OIDC authorize/token endpoints for account-linking config, as placeholders when the
    outputs carry none of them so the printed instructions still show what is missing."""
    authorize, token = oidc_endpoints(settings.raw)
    return (authorize or '<EspUserApiUrl>/oauth2/authorize',
            token or '<EspUserApiUrl>/oauth2/token')

def print_alexa_skill_instructions():
    # Geographies are keyed by Alexa's own region codes, derived from each endpoint ARN, so this
    # reads whichever shape the outputs use and does not assume the Alexa stacks share RMNG's region.
    alexa_regions = settings.alexa_region_arns
    default_arn = settings.default_alexa_arn or '<AlexaSkillFunctionArn>'
    # Account linking runs against the ESP User OIDC IdP (not Cognito). The voice-assistant
    # client is the seeded `va-client` registry row; its secret is not an output — fetch it
    # from the superadmin clients API or SSM (see docs/en/specs/alexa.md).
    authorize_url, token_url = _oidc_endpoints()
    va_client_id = 'va-client'
    print(f"\nYou may now update the Alexa Skill configuration as follows:")
    print(f'1. In the "Smart Home" page of your skill:')
    print(f'   - Edit the "Default endpoint" entry, and add {default_arn}')
    if not alexa_regions:
        print(f'   - No AlexaSkillFunctionArn found in {settings.source}; deploy the rmng-alexa-core stack(s) first')
    else:
        print(f'   - Edit the Appropriate Geography as shown below:')
        for code in ('NA', 'EU', 'FE'):
            if code in alexa_regions:
                print(f'     {code}: {alexa_regions[code]}')
    print(f'2. Goto the Account Linking page:')
    print(f'   - Edit the "Your Web Authorization URL" field to: {authorize_url}')
    print(f'   - Edit the "Access Token URI" field to: {token_url}')
    print(f'   - Edit the "Your Client Id" field to: {va_client_id}')
    print(f'   - Edit the "Your Secret" field to the va-client secret from GET /v1/admin/clients?get_secret=true (or SSM /espuser/base/va-client-secret)')
    print(f'   - Edit the "Scope" field to also add: openid email phone profile')

def print_smartthings_instructions():
    # Geographies are keyed by SmartThings' own geo codes, derived from each endpoint ARN,
    # so this reads whichever shape the outputs use (see st_region_arns).
    st_regions = settings.st_region_arns
    # Account linking runs against the ESP User OIDC IdP (not Cognito). The voice-assistant
    # client is the seeded `va-client` registry row, shared with Alexa and GVA; its secret is
    # not an output — fetch it from the superadmin clients API or SSM.
    authorize_url, token_url = _oidc_endpoints()
    va_client_id = 'va-client'
    print(f"\nSmartThings configuration (https://developer.smartthings.com/):")
    print(f'1. Device Integrations > create a Product > add a Cloud Connector > ST Schema')
    print(f'   - The Product is the container; adding the Schema App links the two automatically')
    print(f'   - App icon: use the logo at assets/smartthings_logo.png')
    print(f'2. Set the Target ARN for each geography:')
    if not st_regions:
        print(f'   - No STSchemaAppFunctionArn found in {settings.source}; deploy the rmng-st-core stack(s) first')
    else:
        for code, label in (('NA', 'North America'), ('EU', 'Europe'), ('AP', 'Asia-Pacific')):
            if code in st_regions:
                print(f'     {label}: {st_regions[code]}')
    print(f'3. Device Cloud Credentials (your cloud, given to SmartThings):')
    print(f'   - Client ID: {va_client_id}')
    print(f'   - Client Secret: the va-client secret from GET /v1/admin/clients?get_secret=true (or SSM /espuser/base/va-client-secret)')
    print(f'   - OAuth URL: {authorize_url}')
    print(f'   - Token URL: {token_url}')
    print(f'   - OAuth Scope: openid email phone profile')
    print(f'4. Save. SmartThings then issues its OWN Client ID and Secret — note the direction,')
    print(f'   these are different from the credentials in step 3.')
    print(f'5. Store them: st_setup <config_file>, where the file holds')
    print(f'   {{"client_id": "<SmartThings client id>", "client_secret": "<SmartThings client secret>"}}')
    print(f'6. Link your account in the SmartThings app and the devices appear. Discovery names a')
    print(f'   pre-made c2c-* handler type, so nothing per-device-type is created in the Workspace:')
    print(f'   the Product from step 1 is only the container the Schema App lives in.')


def handle_st_setup(user, config_file):
    """Store the credentials SmartThings issued for the Schema App.

    Read from a file rather than argv: the client secret is 512 characters and a secret
    in argv is visible to other processes and lands in shell history.
    """
    try:
        with open(config_file, 'r') as f:
            config = json.load(f)

        for field in ('client_id', 'client_secret'):
            if not config.get(field):
                print(f"Error: '{field}' missing or empty in {config_file}")
                return

        response = user.st_post_configuration(config['client_id'], config['client_secret'])
        print(f"Response: {response.status_code}")
        print(response.text)

    except FileNotFoundError:
        print(f"Error: Config file not found: {config_file}")
    except json.JSONDecodeError:
        print(f"Error: Invalid JSON in config file: {config_file}")
    except Exception as e:
        print(f"Error: {str(e)}")


def handle_st_get_config(user):
    try:
        response = user.st_get_configuration()
        print(f"Response: {response.status_code}")
        print(response.text)
    except Exception as e:
        print(f"Error: {str(e)}")


def handle_st_delete_config(user):
    try:
        response = user.st_delete_configuration()
        print(f"Response: {response.status_code}")
        print(response.text)
    except Exception as e:
        print(f"Error: {str(e)}")


def print_gva_instructions():
    # Account linking runs against the ESP User OIDC IdP (not Cognito). The voice-assistant
    # client is the seeded `va-client` registry row; its secret is not an output — fetch it
    # from the superadmin clients API or SSM (see docs/en/specs/gva.md).
    authorize_url, token_url = _oidc_endpoints()
    va_client_id = 'va-client'
    fulfillment_url = settings.gva_fulfillment_url or '<GVAFulfillmentUrl>'
    print(f"\nGoogle Home configuration (https://console.home.google.com/projects):")
    print(f'1. Create/open a Google Home project linked to your Google Cloud project')
    print(f'2. Add a Cloud-to-cloud integration and configure under Develop > Setup:')
    print(f'   - OAuth Client ID: {va_client_id}')
    print(f'   - OAuth Client Secret: the va-client secret from GET /v1/admin/clients?get_secret=true (or SSM /espuser/base/va-client-secret)')
    print(f'   - Authorization URL: {authorize_url}')
    print(f'   - Token URL: {token_url}')
    print(f'   - Fulfillment URL: {fulfillment_url}')
    print(f'   - Scopes: openid, email, phone, profile')
    print(f'   - App icon: Use the logo at assets/gva_logo.png')
    print(f'3. Enable the HomeGraph API:')
    print(f'   - Goto https://console.cloud.google.com/apis/library/homegraph.googleapis.com')
    print(f'   - Select your project and click Enable')
    print(f'4. Create a Service Account for Report State:')
    print(f'   - Goto "IAM & Admin" > "Service Accounts" > "Create Service Account"')
    print(f'   - Name it (e.g. "homegraph-agent")')
    print(f'   - Role: Service Accounts > Service Account OpenID Connect Identity Token Creator')
    print(f'   - Click the created account > Keys > Add Key > Create New Key > JSON')
    print(f'   - Download the JSON key file')
    print(f'5. Run: gva_setup <downloaded_service_account.json>')


def print_ios_platform_instructions():
    print(f"\nApple Push Notifications setup (https://developer.apple.com/account/resources):")
    print(f'1. Create an App ID of type App:')
    print(f'   - Add the bundle ID (used as <bundle_id>)')
    print(f'   - Enable the Push Notifications capability')
    print(f'   - Note the Team ID (used as <team_id>)')
    print(f'2. Create a Key:')
    print(f'   - Type: Apple Push Notifications service (APNs)')
    print(f'   - Point it at the App ID created above')
    print(f'   - Download the .p8 key file and note the Key ID (used as <key_id>)')
    print(f'3. Run: register_ios_platform <p8_key_file> <key_id> <team_id> <bundle_id> [sandbox]')


def print_android_platform_instructions():
    print(f"\nFirebase Cloud Messaging setup (https://console.firebase.google.com/):")
    print(f'1. Create or choose a project')
    print(f'2. Goto Settings > Service accounts')
    print(f"3. On the 'Firebase Admin SDK' tab, click 'Generate new private key' and download the JSON")
    print(f'4. Run: register_android_platform <downloaded_service_account.json>')



def handle_alexa_setup(user, config_file):
    try:
        with open(config_file, 'r') as f:
            config = json.load(f)

        response = user.alexa_post_configuration(
            redirect_uris=config['redirect_urls'],
            client_id=config['alexa_client_id'],
            client_secret=config['alexa_client_secret'],
            skill_id=config['skill_id']
        )
        print(f"Response: {response.status_code}")
        print(response.text)

        if response.status_code == 200:
            print_alexa_skill_instructions()

    except FileNotFoundError:
        print(f"Error: Config file not found: {config_file}")
    except json.JSONDecodeError:
        print(f"Error: Invalid JSON in config file: {config_file}")
    except Exception as e:
        print(f"Error: {str(e)}")

DEFAULT_ALEXA_CONFIG = 'alexa_skills_config.json'


def _alexa_config_to_env(config_file):
    """Load the Alexa skills config JSON and populate the env the SMAPI helper reads,
    so operators maintain a single config file instead of remembering env vars.

    Example alexa_skills_config.json:
        {
          "smapi_client_id": "amzn1.application-oa2-client.xxxx",   # LWA security profile
          "smapi_client_secret": "xxxx",
          "skill_name": "ESP RainMaker",     # optional
          "skill_id": "amzn1.ask.skill.xxxx" # optional; omit to create a new skill
        }
    va-client creds, cognito domain and endpoint ARNs are read from rmng-outputs.json.
    Returns the parsed config dict."""
    with open(config_file, 'r') as f:
        config = json.load(f)
    env_map = {
        'SMAPI_CLIENT_ID': 'smapi_client_id',
        'SMAPI_CLIENT_SECRET': 'smapi_client_secret',
        'SMAPI_VENDOR_ID': 'vendor_id',
        'SMAPI_REFRESH_TOKEN': 'smapi_refresh_token',
        'ALEXA_REDIRECT_URLS': 'redirect_urls',
    }
    for env_key, cfg_key in env_map.items():
        val = config.get(cfg_key)
        if val:
            os.environ[env_key] = ','.join(val) if isinstance(val, list) else str(val)
    # alexa_setup loads outputs itself, keyed off RMNG_OUTPUTS; point it at the source the CLI
    # resolved so --client-outputs applies there too rather than falling back to a CWD lookup.
    os.environ['RMNG_OUTPUTS'] = settings.source
    return config


def _import_alexa_setup():
    sys.path.insert(0, os.path.join(_REPO_ROOT, 'tools'))
    import alexa_setup
    return alexa_setup


def handle_alexa_setup_auto(user, config_file, skill_name=None):
    """Create/update + fully configure an Alexa skill via SMAPI, then POST the backend
    config as this super-admin user (no AWS creds needed). Inputs come from config_file;
    skill_name, if given, overrides the config's skill_name."""
    try:
        config = _alexa_config_to_env(config_file)
        alexa_setup = _import_alexa_setup()

        # Config-API POST goes through this super-admin user's session, not AWS SigV4.
        def post_config_fn(skill_id, client_id, client_secret, redirect_uris):
            response = user.alexa_post_configuration(
                redirect_uris=redirect_uris, client_id=client_id,
                client_secret=client_secret, skill_id=skill_id)
            print(f"Config API response: {response.status_code}")
            print(response.text)
            if response.status_code != 200:
                raise SystemExit(f"config API POST failed: {response.status_code}")

        alexa_setup.setup(post_config_fn,
                          skill_id=config.get('skill_id'),
                          skill_name=skill_name or config.get('skill_name'))

    except FileNotFoundError:
        print(f"Error: Config file not found: {config_file}")
    except json.JSONDecodeError:
        print(f"Error: Invalid JSON in config file: {config_file}")
    except Exception as e:
        print(f"Error: {str(e)}")


def handle_alexa_list_skills(config_file):
    """List all Alexa skills under the vendor (SMAPI creds from config_file)."""
    try:
        _alexa_config_to_env(config_file)
        _import_alexa_setup().list_all()
    except FileNotFoundError:
        print(f"Error: Config file not found: {config_file}")
    except json.JSONDecodeError:
        print(f"Error: Invalid JSON in config file: {config_file}")
    except Exception as e:
        print(f"Error: {str(e)}")


def handle_alexa_delete_skill(config_file, skill_id):
    """Delete an Alexa skill by id (SMAPI creds from config_file)."""
    try:
        _alexa_config_to_env(config_file)
        _import_alexa_setup().delete(skill_id)
    except FileNotFoundError:
        print(f"Error: Config file not found: {config_file}")
    except json.JSONDecodeError:
        print(f"Error: Invalid JSON in config file: {config_file}")
    except Exception as e:
        print(f"Error: {str(e)}")

def handle_gva_setup(user, config_file):
    try:
        with open(config_file, 'r') as f:
            config = json.load(f)

        response = user.gva_post_configuration(config)
        print(f"Response: {response.status_code}")
        print(response.text)

    except FileNotFoundError:
        print(f"Error: Config file not found: {config_file}")
    except json.JSONDecodeError:
        print(f"Error: Invalid JSON in config file: {config_file}")
    except Exception as e:
        print(f"Error: {str(e)}")

def handle_add_node_to_subgroup(user, group_id, subgroup_id, node_id):
    group_api = Group(user)
    try:
        group_api.add_node_to_subgroup(group_id, subgroup_id, node_id)
        print(f"Successfully added node {node_id} to subgroup {subgroup_id} in group {group_id}")
    except Exception as e:
        print(f"Failed to add node to subgroup: {str(e)}")

def handle_remove_node_from_subgroup(user, group_id, subgroup_id, node_id):
    group_api = Group(user)
    try:
        group_api.remove_node_from_subgroup(group_id, subgroup_id, node_id)
        print(f"Successfully removed node {node_id} from subgroup {subgroup_id} in group {group_id}")
    except Exception as e:
        print(f"Failed to remove node from subgroup: {str(e)}")

def handle_update_group(user, group_id, new_group_name):
    group_api = Group(user)
    try:
        group_api.update_group(group_id, new_group_name)
        print(f"Successfully updated group {group_id} to '{new_group_name}'")
    except Exception as e:
        print(f"Failed to update group: {str(e)}")

def handle_update_subgroup(user, group_id, subgroup_id, new_subgroup_name):
    group_api = Group(user)
    try:
        group_api.update_subgroup(group_id, subgroup_id, new_subgroup_name)
        print(f"Successfully updated subgroup {subgroup_id} in group {group_id} to '{new_subgroup_name}'")
    except Exception as e:
        print(f"Failed to update subgroup: {str(e)}")

def delete_user_groups(user):
    try:
        group_api = Group(user)
        groups = group_api.list_groups()
        for group in groups.get('groups', []):
            try:
                print("Deleting group", group['group_id'])
                group_api.delete_group(group['group_id'])
            except Exception as e:
                print(f"Failed to delete group {group.get('group_id', 'unknown')}: {str(e)}")
                continue
    except Exception as e:
        print(f"Failed to list/delete user groups for user {user.username if user else 'unknown'}: {str(e)}")

def setup_ses_mailosaur_identity():
    """Verify a Mailosaur address as an SES email identity so sandbox SES can
    deliver OTP emails to the test inbox, then select it as the active OTP sender.
    Reads the SES verification link out of the Mailosaur inbox and follows it — no
    real mailbox or manual click. The verified address is recorded in test_config.json
    as `otp_ses_sender` for tests to read against, and written as the selected sender
    in the espuser-admin-config table (config_name=email-sender, subtype=global) — which is
    where the OTP dispatcher reads the sender from (no env var)."""
    from test.itest.email_utils import generate_mailosaur_email_specific, verify_ses_identity_via_mailosaur

    print("Setting up SES sender identity via Mailosaur...")
    # Local-part is scoped by account + region so concurrent accounts/regions each
    # verify and read their own inbox on the shared Mailosaur server (SES
    # identities are per account+region). Prefix configurable via `otp_ses_sender_prefix`.
    prefix = config.get('otp_ses_sender_prefix', 'ses-sender')
    email = generate_mailosaur_email_specific(f"{prefix}-{ACCOUNT_ID}-{REGION}")
    if not email:
        print("  Skipped: Mailosaur not configured (mailosaur_server_id/api_key).")
        return

    if not verify_ses_identity_via_mailosaur(email, REGION):
        print(f"  Could not verify SES identity {email} via Mailosaur.")
        return

    config['otp_ses_sender'] = email
    try:
        # TEST_CONFIG_PATH, not a bare name: a relative path resolves against the caller's
        # CWD, which writes a second config file that read_config never looks at.
        with open(TEST_CONFIG_PATH, 'w') as f:
            json.dump(config, f, indent=2)
        print(f"  Verified SES identity {email}; saved as otp_ses_sender in {TEST_CONFIG_PATH}.")
    except Exception as e:
        print(f"  Verified {email} but failed to persist to {TEST_CONFIG_PATH}: {e}")

    # Written straight to espuser-admin-configs: there is no admin API for senders, and OTP
    # dispatch reads the active sender from this row (it falls back to the sole verified
    # identity only when exactly one exists, which is never true on a shared test account).
    try:
        boto3.client('dynamodb', region_name=REGION).put_item(
            TableName='espuser-admin-configs',
            Item={
                'config_name': {'S': 'email-sender'},
                'subtype': {'S': 'global'},
                'value': {'S': email},
                'updated_at': {'N': str(int(time.time()))},
            },
        )
        print(f"  Marked {email} as the active global sender.")
    except Exception as e:
        print(f"  Verified {email} but could not mark it active: {e}")


def request_ses_production_access():
    """Request SES production access (moves the account out of the sandbox) for
    this region, so SES can deliver to unverified recipients. This files an AWS
    support case; approval is asynchronous. Idempotent — skips if the account is
    already in production. Request details are configurable in test_config.json
    (`ses_website_url`, `ses_use_case_description`)."""
    import boto3

    print("Requesting SES production access...")
    ses = boto3.client('sesv2', region_name=REGION)
    try:
        if ses.get_account().get('ProductionAccessEnabled'):
            print("  Already in production; nothing to do.")
            return
    except Exception as e:
        print(f"  Could not read SES account status: {e}")
        return

    try:
        ses.put_account_details(
            MailType='TRANSACTIONAL',
            WebsiteURL=config.get('ses_website_url', 'https://rainmaker.espressif.com'),
            UseCaseDescription=config.get(
                'ses_use_case_description',
                'Transactional passwordless login one-time codes (OTP) for ESP RainMaker users.'),
            ProductionAccessEnabled=True,
        )
        print(f"  Filed SES production-access request for {REGION}; approval is asynchronous.")
    except ses.exceptions.ConflictException:
        print("  A production-access request is already pending.")
    except Exception as e:
        print(f"  Failed to request SES production access: {e}")


def request_sns_production_access():
    """Move the account out of the SNS SMS sandbox for this region so SMS OTPs can
    go to unverified numbers. Idempotent — skips if already out of the sandbox.

    Unlike SES, AWS exposes no API to file the SMS sandbox-exit case (no
    put-account-details equivalent, no Service Quotas code for the SMS spend
    limit, and the Support create-case API needs a paid support plan). The spend
    limit is also capped at $1 while in the sandbox, so it can only be raised
    AFTER exit. This step therefore prints the exact console link to open the
    exit case, and raises the spend limit only once already out of the sandbox
    (configurable via `sns_sms_spend_limit_usd`)."""
    import boto3

    print("Requesting SNS SMS production access...")
    sns = boto3.client('sns', region_name=REGION)
    try:
        in_sandbox = sns.get_sms_sandbox_account_status().get('IsInSandbox', True)
    except Exception as e:
        print(f"  Could not read SNS SMS sandbox status: {e}")
        return

    if in_sandbox:
        print("  In the SMS sandbox. Exit has no API — open the case once in the console:")
        print(f"    https://{REGION}.console.aws.amazon.com/sns/v3/home?region={REGION}#/mobile/text-messaging")
        print("    (Text messaging → Account information → Exit SMS sandbox.)")
        print("    Re-run this command after exit to raise the monthly spend limit.")
        return

    # Out of the sandbox: now the spend limit can actually be raised above $1.
    spend_limit = str(config.get('sns_sms_spend_limit_usd', 100))
    try:
        sns.set_sms_attributes(attributes={'MonthlySpendLimit': spend_limit})
        print(f"  Out of the SMS sandbox; set monthly SMS spend limit to ${spend_limit}.")
    except Exception as e:
        print(f"  Out of the sandbox, but could not set spend limit to ${spend_limit}: {e}")


def setup_users():
    users = [get_user(i) for i in range(len(config.get('users', [])))]
    user_map = {}

    print("Setting up users...")
    for user in users:
        if user:
            # Super admins go into the admin pool; end users into the provider pool they sign in against.
            if user.is_super_admin:
                user.create_super_admin_via_cognito()
                print(f"Super admin {user.username} provisioned in Cognito")
            else:
                user.register_user_via_lambda(email=user.username if '@' in user.username else None)
                print(f"User {user.username} provisioned in the provider pool")
            user_map[user.username] = user

            # Check if user has any groups
            group_api = Group(user)
            groups = group_api.list_groups()
            if not groups.get('groups'):
                # Create a default "Home" group if no groups exist
                group_id = group_api.create_group("Home")
                if group_id:
                    user.add_group_id(group_id)
                    print(f"Created default 'Home' group for user {user.username} with group id {group_id}")
                else:
                    print(f"Failed to create default 'Home' group for user {user.username}")
            else:
                print(f"User {user.username} already has groups {groups}, skipping group creation")
                i = 0
                for group in groups.get('groups', []):
                    user.add_group_id(group['group_id'])
                    if i==0:
                        for node in group.get('node_ids', []):
                            user.add_device(node)
                    i += 1
        else:
            print("Failed to create a user")

    return user_map

# Thing-name prefix the integration tests generate (test/itest/conftest.py builds
# "test-<key_type>-device-<uuid>", "test-kvs-<hex>", and similar).
TEST_THING_PREFIX = 'test-'


def _is_test_thing(thing_name):
    """Whether a thing was created by this repo's test tooling.

    Test nodes share DefaultThingPolicy with every other node in the deployment, so name is the only
    thing that identifies them. Anything outside this repo's test prefix and the local config is
    left alone -- other tooling owns its own things.
    """
    if thing_name.startswith(TEST_THING_PREFIX):
        return True
    return any(n.get('thing_name') == thing_name for n in (config or {}).get('nodes', []))


# IoT's control-plane list/delete APIs throttle at a low rate. Adaptive retry adds client-side rate
# limiting so bursts back off instead of failing, and a modest pool keeps the sweep inside the
# service limit; higher concurrency just converts into ThrottlingExceptions.
CERT_SWEEP_WORKERS = 8
_SWEEP_RETRY_CONFIG = BotocoreConfig(retries={'max_attempts': 10, 'mode': 'adaptive'})

_sweep_local = threading.local()


def _sweep_iot_client():
    """One IoT client per worker thread, so the sweep is not serialised on a shared connection pool."""
    if not hasattr(_sweep_local, 'iot'):
        _sweep_local.iot = boto3.client('iot', region_name=REGION, config=_SWEEP_RETRY_CONFIG)
    return _sweep_local.iot


def _find_test_things():
    """Names of things this repo's test tooling created, cheapest way the account allows.

    Fleet indexing answers the "test-*" prefix directly, one query instead of a walk. Without it,
    enumerate the registry and filter client-side: one page per 250 things, still far cheaper than
    inspecting every certificate on the policy.
    """
    iot_client = boto3.client('iot', region_name=REGION, config=_SWEEP_RETRY_CONFIG)
    names, token = set(), None
    try:
        while True:
            kwargs = {'queryString': f'thingName:{TEST_THING_PREFIX}*', 'maxResults': 250}
            if token:
                kwargs['nextToken'] = token
            response = iot_client.search_index(**kwargs)
            names.update(t['thingName'] for t in response.get('things', []))
            token = response.get('nextToken')
            if not token:
                break
    except (iot_client.exceptions.IndexNotReadyException,
            iot_client.exceptions.InvalidRequestException) as e:
        print(f"Fleet indexing unavailable ({type(e).__name__}); listing the registry instead.")
        names.clear()
        for page in iot_client.get_paginator('list_things').paginate():
            names.update(t['thingName'] for t in page.get('things', [])
                         if t['thingName'].startswith(TEST_THING_PREFIX))

    # test_config.json names do not carry the prefix, so add them explicitly; a name that no longer
    # exists is skipped when its principals are looked up.
    names.update(n['thing_name'] for n in (config or {}).get('nodes', []) if n.get('thing_name'))
    return sorted(names)


def _sweep_one_thing(thing_name, dry_run=False):
    """Delete one test thing and any certificate left holding nothing else.

    Returns (things_deleted, certs_deleted, certs_skipped, failures). A certificate shared with an
    out-of-scope thing is left intact, since detaching it would break a node this tool does not own.
    Failures are counted rather than swallowed, so a throttled sweep cannot look complete.
    """
    iot_client = _sweep_iot_client()
    try:
        certs = []
        for page in iot_client.get_paginator('list_thing_principals').paginate(thingName=thing_name):
            certs.extend(page.get('principals', []))

        deletable, skipped = [], 0
        for cert_arn in certs:
            siblings = []
            for page in iot_client.get_paginator('list_principal_things').paginate(principal=cert_arn):
                siblings.extend(page.get('things', []))
            if all(_is_test_thing(t) for t in siblings):
                deletable.append(cert_arn)
            else:
                skipped += 1

        if dry_run:
            return 1, len(deletable), skipped, 0

        for cert_arn in certs:
            iot_client.detach_thing_principal(thingName=thing_name, principal=cert_arn)
        iot_client.delete_thing(thingName=thing_name)

        deleted_certs = 0
        for cert_arn in deletable:
            try:
                # Whatever policies the cert carries must come off before it can be deleted; do not
                # assume it is only on DefaultThingPolicy.
                for page in iot_client.get_paginator('list_attached_policies').paginate(target=cert_arn):
                    for policy in page.get('policies', []):
                        iot_client.detach_policy(policyName=policy['policyName'], target=cert_arn)
                certificate_id = cert_arn.split('/')[-1]
                iot_client.update_certificate(certificateId=certificate_id, newStatus='INACTIVE')
                iot_client.delete_certificate(certificateId=certificate_id)
                deleted_certs += 1
            except Exception as e:
                print(f"Failed to delete certificate {cert_arn}: {str(e)}")
        return 1, deleted_certs, skipped, 0
    except iot_client.exceptions.ResourceNotFoundException:
        return 0, 0, 0, 0
    except Exception as e:
        print(f"Failed to process thing {thing_name}: {str(e)}")
        return 0, 0, 0, 1


def sweep_test_certs(dry_run=False):
    """Delete leftover test things and their certificates.

    Driven from the things rather than from DefaultThingPolicy's targets: that policy is attached to
    every node in the deployment, so scanning it costs one API call per node in the account to find
    a handful of test leftovers. Cost now scales with what the tests left behind, not with the size
    of the deployment.
    """
    try:
        things = _find_test_things()
    except Exception as e:
        print(f"Failed to find test things: {str(e)}")
        return

    if not things:
        print("Cert sweep: no test things found.")
        return

    print(f"Sweeping {len(things)} candidate test thing(s)"
          f"{' (dry run)' if dry_run else ''}...", flush=True)
    deleted_things = deleted_certs = skipped_certs = failures = 0
    with ThreadPoolExecutor(max_workers=CERT_SWEEP_WORKERS) as pool:
        for t, c, s, f in pool.map(lambda n: _sweep_one_thing(n, dry_run), things):
            deleted_things += t
            deleted_certs += c
            skipped_certs += s
            failures += f

    verb = 'would delete' if dry_run else 'deleted'
    print(f"Cert sweep: {verb} {deleted_things} thing(s) and {deleted_certs} certificate(s); "
          f"left {skipped_certs} certificate(s) shared with non-test things untouched.")
    if failures:
        print(f"WARNING: {failures} thing(s) could not be processed — the sweep is "
              f"incomplete. Re-run --destroy-test-data to retry them.")


def setup_nodes(user_map: dict[str, User]):
    devices = [get_node(i) for i in range(len(config.get('nodes', [])))]
    nodes = config.get('nodes', [])
    admin_group_name = config.get('admin_group_name', None)

    # Register through the admin API as the super admin rather than invoking the registration
    # Lambda directly: API Gateway supplies the caller identity the handler authorizes against, so
    # nothing here needs the Lambda's physical name or a synthesised request context. Re-registering
    # an existing cert is idempotent server-side, which keeps repeated --setup-test-data runs safe.
    superadmin = next((u for u in user_map.values() if u.is_super_admin), None)
    if not superadmin:
        print("No super admin configured; node registration would be unauthorized.")
        return

    print("Setting up devices...")
    for i, node_config in enumerate(nodes):
        device = devices[i] if i < len(devices) else None
        if device:
            if superadmin.register_node(device, tags=["created_by:test"],
                                        admin_group_names=[admin_group_name]):
                print(f"Node {device.node_thing_name} registered successfully")

                # Check if this node should be associated with a user
                associate_to = node_config.get('associate_to')
                if associate_to:
                    user = user_map.get(associate_to)
                    if user:
                        print(f"Associating node {device.node_thing_name} with user {user.get_group_ids()}")
                        group_id = user.get_group_ids()[0]
                        if device.node_thing_name in user.get_devices():
                            print(f"User {associate_to} already has device {device.node_thing_name}, skipping association")
                            continue
                        error = user.do_user_node_assoc(device, group_id)
                        if error:
                            print(f"Failed to associate node {device.node_thing_name} with user {associate_to}: {error}")
                        else:
                            print(f"Successfully associated node {device.node_thing_name} with user {associate_to} in group {group_id}")
                    else:
                        print(f"User {associate_to} not found, skipping association")
            else:
                print(f"Failed to register node {device.node_thing_name}")
        else:
            print("Failed to create a device")

    return devices


def _iam_user_exists(iam, user_name: str) -> bool:
    try:
        iam.get_user(UserName=user_name)
        return True
    except ClientError as e:
        if e.response.get("Error", {}).get("Code") == "NoSuchEntity":
            return False
        raise


def setup_bot_user():
    if not BOT_IAM_USER_NAME:
        print(
            'Error: test_config.json must include a user with "ci_user": true and a "name" '
            "for the CI IAM bot user."
        )
        sys.exit(1)
    iam = boto3.client("iam")
    if _iam_user_exists(iam, BOT_IAM_USER_NAME):
        print(
            f"Error: IAM user {BOT_IAM_USER_NAME!r} already exists. "
            "Run --destroy-bot-user first."
        )
        sys.exit(1)

    iam.create_user(
        UserName=BOT_IAM_USER_NAME,
        Tags=[
            {"Key": "Created-For", "Value": "Jenkins+CI"},
            {"Key": "Created-By", "Value": "Repository"},
        ],
    )
    iam.attach_user_policy(
        UserName=BOT_IAM_USER_NAME,
        PolicyArn=ADMINISTRATOR_ACCESS_POLICY_ARN,
    )
    resp = iam.create_access_key(UserName=BOT_IAM_USER_NAME)
    key = resp["AccessKey"]
    payload = {
        "user_name": BOT_IAM_USER_NAME,
        "access_key_id": key["AccessKeyId"],
        "secret_access_key": key["SecretAccessKey"],
    }
    with open(BOT_IAM_CREDENTIALS_FILE, "w", encoding="utf-8") as f:
        json.dump(payload, f, indent=2)
    try:
        os.chmod(BOT_IAM_CREDENTIALS_FILE, 0o600)
    except OSError:
        pass
    print(
        f"Created IAM user {BOT_IAM_USER_NAME} with AdministratorAccess.\n"
        f"Credentials: {BOT_IAM_CREDENTIALS_FILE}"
    )


def destroy_bot_user():
    if not BOT_IAM_USER_NAME:
        print(
            'Error: test_config.json must include a user with "ci_user": true and a "name" '
            "for the CI IAM bot user."
        )
        sys.exit(1)
    iam = boto3.client("iam")
    if not _iam_user_exists(iam, BOT_IAM_USER_NAME):
        print(f"IAM user {BOT_IAM_USER_NAME!r} does not exist; nothing to destroy.")
        if os.path.isfile(BOT_IAM_CREDENTIALS_FILE):
            os.remove(BOT_IAM_CREDENTIALS_FILE)
            print(f"Removed {BOT_IAM_CREDENTIALS_FILE}")
        return

    paginator = iam.get_paginator("list_access_keys")
    for page in paginator.paginate(UserName=BOT_IAM_USER_NAME):
        for meta in page.get("AccessKeyMetadata", []):
            iam.delete_access_key(
                UserName=BOT_IAM_USER_NAME,
                AccessKeyId=meta["AccessKeyId"],
            )

    try:
        iam.detach_user_policy(
            UserName=BOT_IAM_USER_NAME,
            PolicyArn=ADMINISTRATOR_ACCESS_POLICY_ARN,
        )
    except ClientError as e:
        if e.response.get("Error", {}).get("Code") != "NoSuchEntity":
            raise

    iam.delete_user(UserName=BOT_IAM_USER_NAME)

    if os.path.isfile(BOT_IAM_CREDENTIALS_FILE):
        os.remove(BOT_IAM_CREDENTIALS_FILE)

    print(
        f"Deleted IAM user {BOT_IAM_USER_NAME} and removed "
        f"{BOT_IAM_CREDENTIALS_FILE} (if present)."
    )


def main():
    if args.gen_device:
        node_name, key_type = args.gen_device
        if key_type not in ['rsa', 'ec']:
            print('Error: key_type must be either "rsa" or "ec"')
            sys.exit(1)
        key_pem, cert_pem = generate_key_and_cert(node_name, key_type)
        result = {
            "thing_name": node_name,
            "cert": cert_pem,
            "key": key_pem
        }
        print(json.dumps(result))
        return

    if args.setup_bot_user and args.destroy_bot_user:
        print("Error: use only one of --setup-bot-user or --destroy-bot-user")
        sys.exit(1)

    if args.setup_bot_user:
        setup_bot_user()
        return

    if args.destroy_bot_user:
        destroy_bot_user()
        return

    if args.setup or args.destroy:
        if args.setup:
            print("Starting setup process...")
            user_map = setup_users()
            setup_nodes(user_map)
            print("Setup completed.")

        elif args.destroy:
            print("Destroying devices...")
            devices = [get_node(i) for i in range(len(config.get('nodes', [])))]
            for device in devices:
                try:
                    if device:
                        if device.destroy_test_node():
                            print(f"Node {device.node_thing_name} destroyed successfully")
                        else:
                            print(f"Failed to destroy node {device.node_thing_name}")
                    else:
                        print("Failed to get a device for destruction")
                except Exception as e:
                    print(f"Error destroying device {device.node_thing_name if device else 'unknown'}: {str(e)}")
                    continue

            # Note: We don't have a destroy operation for users in the current implementation
            users = [get_user(i) for i in range(len(config.get('users', [])))]
            for user in users:
                try:
                    delete_user_groups(user)
                except Exception as e:
                    print(f"Failed to delete groups for user {user.username if user else 'unknown'}: {str(e)}")
                    continue

            sweep_test_certs()

            print("Note: User accounts are not destroyed as part of this operation.")

        return

    if args.user is None and args.device is None and args.device_sim is None and args.app_sim is None:
        print("Error: Either --user or --device or --device-sim or --app-sim must be specified")
        sys.exit(1)

    if args.user:
        user = get_user(args.user)
        if user is None:
            sys.exit(1)
        print(f"Entering user context for: {user.username}")
        handle_user_commands(user)

    if args.device:
        device = get_node(args.device)
        if device is None:
            sys.exit(1)
        print(f"Entering device context for: {device.node_thing_name}")
        handle_device_commands(device)

    if args.device_sim:
        if args.device_sim == '0':
            print("Error: --device-sim requires a device id from test_config.json")
            sys.exit(1)

        try:
            # Create and start the device simulator. Pass the CLI-resolved paths:
            # test_config.json lives beside morpheus.py (where --setup-test-data writes it), not in
            # the caller's CWD, and outputs come from --client-outputs.
            simulator = DeviceSim(args.device_sim, config_path=TEST_CONFIG_PATH,
                                  rmng_outputs_path=settings.source)
            if simulator.start():
                simulator.run_interactive()
            else:
                print("Failed to start simulator")
                sys.exit(1)
        except Exception as e:
            print(f"Error running simulator: {str(e)}")
            print(traceback.format_exc())
            sys.exit(1)

    if args.app_sim:
        if args.app_sim == '0':
            print("Error: --app-sim requires a user id from test_config.json")
            sys.exit(1)

        try:
            # Create and start the app simulator. Pass the paths the CLI already
            # resolved: test_config.json lives beside morpheus.py (where --setup-test-data writes
            # it), not in the caller's CWD, and outputs come from --client-outputs.
            from test.app_sim import AppSim
            simulator = AppSim(args.app_sim, config_path=TEST_CONFIG_PATH,
                               rmng_outputs_path=settings.source)
            if simulator.start():
                simulator.run_interactive()
            else:
                print("Failed to start simulator")
                sys.exit(1)
        except Exception as e:
            print(f"Error running simulator: {str(e)}")
            print(traceback.format_exc())
            sys.exit(1)

def get_list_from_str(str):
    if str:
        return [s.strip() for s in str.split(',')]
    return None

def parse_tags_str(tags_str):
    tags = []
    if tags_str:
        for tag in tags_str.split(','):
            tag = tag.strip()
            if ':' in tag:
                tags.append(tag)
    return tags

def handle_claim(user, mac_addr=None, capabilities=None):
    """Run a full assisted claim (initiate + verify) for a device MAC.

    The claim flow lives in the SDK (User.claim); this just drives it and saves
    the issued material. Writes <node_id>.crt, <node_id>.key, and
    <node_id>-ca.crt to the working directory so a device can use them. A MAC is
    generated when none is given.
    """
    try:
        result = user.claim(mac_addr, capabilities=capabilities)
    except RuntimeError as e:
        print(f"Claim failed: {e}")
        return

    node_id = result["node_id"]
    paths = {
        f"{node_id}.crt": result["certificate"],
        f"{node_id}.key": result["private_key"],
        f"{node_id}-ca.crt": result["ca_certificate"],
    }
    for path, contents in paths.items():
        with open(path, "w") as f:
            f.write(contents)

    print(f"Claimed node {node_id} (mac {result['mac_addr']})")
    print(f"  certificate -> {node_id}.crt")
    print(f"  private key -> {node_id}.key")
    print(f"  CA cert     -> {node_id}-ca.crt")


def handle_register_node(user, node_id, admin_group_names_str=None, tags_str=None):
    """Handle node registration from test_config.json with custom tags.

    Args:
        user (User): User object to use for registration
        node_id (str): Node ID/index from test_config.json
        admin_group_names_str (str, optional): Comma-separated list of admin group names
        tags_str (str, optional): Comma-separated key:value pairs for tags
    """
    # Parse tags string into list
    tags = parse_tags_str(tags_str)
    admin_group_names = get_list_from_str(admin_group_names_str)

    # Get node using the existing get_node function
    device = get_node(node_id)
    if not device:
        return

    admin_group_names.append(config.get('admin_group_name', None))

    # Register the node
    if user.register_node(device, tags, admin_group_names):
        print(f"Node {device.node_thing_name} registered successfully")
    else:
        print("Node registration failed")

def handle_upload_file(user, file_type, file_path):
    """Handle file upload using the file upload API.

    Args:
        user (User): User object to use for upload
        file_type (str): Type of file (e.g., 'node_cert')
        file_path (str): Path to the file to upload
    """
    if not os.path.exists(file_path):
        print(f"Error: File not found - {file_path}")
        return

    print(f"Uploading file {file_path} as type {file_type}...")
    success, result = user.upload_file(file_path, file_type)

    if success:
        print(f"File uploaded successfully!")
        print(f"Local file: {file_path}")
        print(f"S3 location: {result}")
    else:
        print(f"Upload failed: {result}")

def handle_register_ios_platform(user, p8_key_file, key_id, team_id, bundle_id, sandbox=False):
    """Handle iOS platform registration.

    Args:
        user (User): Super admin user object
        p8_key_file (str): Path to P8 key file
        key_id (str): Key ID
        team_id (str): Team ID
        bundle_id (str): Bundle ID
        sandbox (bool): True for sandbox environment
    """
    if not os.path.exists(p8_key_file):
        print(f"Error: P8 key file not found - {p8_key_file}")
        return

    try:
        with open(p8_key_file, 'r') as f:
            p8_key = f.read()

        result = user.register_ios_platform(p8_key, key_id, team_id, bundle_id, sandbox)
        if result:
            print(f"iOS platform registered successfully")
            print(f"Environment: {'Sandbox' if sandbox else 'Production'}")
        else:
            print(f"Failed to register iOS platform")
    except Exception as e:
        print(f"Error reading P8 key file: {str(e)}")

def handle_register_android_platform(user, json_file_path):
    """Handle Android platform registration.

    Args:
        user (User): Super admin user object
        json_file_path (str): Path to JSON file containing GCM service account key
    """
    if not os.path.exists(json_file_path):
        print(f"Error: JSON file not found - {json_file_path}")
        return

    try:
        with open(json_file_path, 'r') as f:
            json_content = f.read()

        # Validate that the file contains valid JSON
        try:
            json.loads(json_content)
        except json.JSONDecodeError as e:
            print(f"Error: Invalid JSON in file {json_file_path}: {str(e)}")
            return

        result = user.register_android_platform(json_content)
        if result:
            print(f"Android platform registered successfully")
        else:
            print(f"Failed to register Android platform")
    except Exception as e:
        print(f"Error reading JSON file: {str(e)}")

def handle_list_mobile_platforms(user):
    """Handle listing mobile platforms.

    Args:
        user (User): Super admin user object
    """
    user.list_mobile_platforms()

def handle_get_iot_event_mode(user):
    """Print the current IoT-rule action mode (direct|sqs) for both
    node_offline_rule and device_to_cloud_rule.

    Args:
        user (User): Super admin user object
    """
    result = user.admin_get_iot_event_mode()
    if isinstance(result, dict):
        print(json.dumps(result, indent=2))
    else:
        print(f"Failed to get iot-event-mode (status={getattr(result, 'status_code', 'N/A')})")
        if hasattr(result, 'text'):
            print(result.text)

def handle_set_iot_event_mode(user, mode):
    """Flip the IoT-rule action mode for both node_offline_rule and
    device_to_cloud_rule. Both rules switch together.

    Args:
        user (User): Super admin user object
        mode (str): "direct" or "sqs"
    """
    if mode not in ("direct", "sqs"):
        print(f'Invalid mode "{mode}". Expected "direct" or "sqs".')
        return
    result = user.admin_put_iot_event_mode(mode)
    if isinstance(result, dict):
        print(json.dumps(result, indent=2))
    else:
        print(f"Failed to set iot-event-mode (status={getattr(result, 'status_code', 'N/A')})")
        if hasattr(result, 'text'):
            print(result.text)


def handle_enable_claim(user, config_file=None):
    """Enable assisted claiming (superadmin only).

    The claim stack group stands up the CA key and API but leaves claiming off:
    it is on only once a mode is configured AND the CA is minted, both through
    the admin API. This command does both — it stores the claiming configuration
    (defaulting mode to "user_authenticated" if the config omits it) and then
    mints the CA. Idempotent: re-running reports the existing CA unchanged and
    re-applies the configuration.

    Args:
        user (User): super admin user object
        config_file (str): optional path to a JSON claiming-config file

    Config file format (every field optional; omit one and its default applies):

        {
          "mode": "user_authenticated",     # claiming variant; defaults to
                                            #   user_authenticated when omitted
          "max_nodes_per_claimant": 20,     # per-caller lifetime node quota
                                            #   (user_authenticated); 0 => default
          "subject": {                      # shared by the CA and every leaf;
            "country": "IN",                #   the leaf CN is always the node id
            "state": "Maharashtra",         #   and is never taken from here
            "locality": "Pune",
            "organization": "Espressif Systems",
            "organizational_unit": "RainMaker",
            "email": "rainmaker@espressif.com"
          },
          "ca_common_name": "Espressif RainMaker Claiming CA",  # default: derived
                                            #   from the key's account/region
          "ca_validity_years": 30,          # default: leaf + 20 (~120)
          "leaf_validity_years": 10         # default: 100
        }

    Validation (else the config call returns 400): mode, if present, must be an
    implemented variant (user_authenticated); country, if present, must be a
    two-letter ISO code; validity years and the quota must be non-negative; and
    the effective leaf validity must not exceed the effective CA validity (0 =>
    the default on each side).
    """
    config = {}
    if config_file:
        try:
            with open(config_file) as f:
                config = json.load(f)
        except (OSError, json.JSONDecodeError) as e:
            print(f"Failed to read config file {config_file}: {e}")
            return

    # Enabling means the runtime must have a mode; default it so `enable_claim`
    # with no config still turns claiming fully on.
    config.setdefault("mode", "user_authenticated")

    resp = user.claim_admin_set_config(config)
    if resp.status_code != 200:
        print(f"Failed to set claiming configuration (status={resp.status_code}): {resp.text}")
        return
    print(f"Claiming configuration stored (mode={config['mode']}).")

    resp = user.claim_admin_mint_ca()
    if resp.status_code not in (200, 201):
        print(f"Failed to mint the claiming CA (status={resp.status_code}): {resp.text}")
        return

    body = resp.json()
    if resp.status_code == 201:
        print(f"Claiming CA minted (CN={body.get('common_name')}). Claiming is now enabled.")
    else:
        print("Claiming CA already present — claiming is already enabled.")
    if body.get("ca_certificate"):
        print(body["ca_certificate"])

def handle_update_ios_platform(user, p8_key_file, key_id, team_id, bundle_id, sandbox=False):
    """Handle iOS platform update.

    Args:
        user (User): Super admin user object
        p8_key_file (str): Path to P8 key file
        key_id (str): Key ID
        team_id (str): Team ID
        bundle_id (str): Bundle ID
        sandbox (bool): True for sandbox environment
    """
    if not os.path.exists(p8_key_file):
        print(f"Error: P8 key file not found - {p8_key_file}")
        return

    try:
        with open(p8_key_file, 'r') as f:
            p8_key = f.read()

        result = user.update_mobile_platform(
            platform="APNS",
            authentication_key=p8_key,
            key_id=key_id,
            team_id=team_id,
            bundle_id=bundle_id,
            apns_sandbox=sandbox
        )
        if result:
            print(f"iOS platform updated successfully")
            print(f"Environment: {'Sandbox' if sandbox else 'Production'}")
        else:
            print(f"Failed to update iOS platform")
    except Exception as e:
        print(f"Error reading P8 key file: {str(e)}")

def handle_update_android_platform(user, json_file_path):
    """Handle Android platform update.

    Args:
        user (User): Super admin user object
        json_file_path (str): Path to JSON file containing GCM service account key
    """
    if not os.path.exists(json_file_path):
        print(f"Error: JSON file not found - {json_file_path}")
        return

    try:
        with open(json_file_path, 'r') as f:
            json_content = f.read()

        # Validate that the file contains valid JSON
        try:
            json.loads(json_content)
        except json.JSONDecodeError as e:
            print(f"Error: Invalid JSON in file {json_file_path}: {str(e)}")
            return

        result = user.update_mobile_platform(
            platform="GCM",
            api_key=json_content
        )
        if result:
            print(f"Android platform updated successfully")
        else:
            print(f"Failed to update Android platform")
    except Exception as e:
        print(f"Error reading JSON file: {str(e)}")

def handle_delete_mobile_platform(user, platform_name, platform_app_name):
    """Handle mobile platform deletion.

    Args:
        platform_name (str): Platform name
        platform_app_name (str): Platform application name
    """
    try:
        result = user.delete_mobile_platform(platform_name, platform_app_name)
        if result:
            print(f"Mobile platform deleted successfully")
        else:
            print(f"Failed to delete mobile platform")
    except Exception as e:
        print(f"Error deleting mobile platform: {str(e)}")

def handle_bulk_register_nodes(user, file_path, admin_group_names_str=None, tags_str=None):
    """Handle bulk node registration from a CSV file.

    Args:
        user (User): User object to use for registration
        file_path (str): Path to the CSV file to upload
    """
    if not os.path.exists(file_path):
        print(f"Error: File not found - {file_path}")
        return

    tags = parse_tags_str(tags_str)
    admin_group_names = get_list_from_str(admin_group_names_str)

    print(f"Uploading file {file_path} as type node_cert...")
    success, s3_path = user.upload_file(file_path, 'node_cert')
    if success:
        print(f"File uploaded successfully!")
        print(f"Local file: {file_path}")
        print(f"S3 location: {s3_path}")
    else:
        print(f"Upload failed: {s3_path}")

    user.bulk_register_nodes(s3_path, admin_group_names, tags)

def handle_get_bulk_register_status(user, request_id):
    """Handle getting the status of a bulk node registration request.

    Args:
        user (User): User object to use for getting the status
        request_id (str): Request ID of the bulk node registration request
    """
    user.get_bulk_register_status(request_id)

if __name__ == "__main__":
    main()

# Example runs
# python3 morpheus.py --setup-test-data
# python3 morpheus.py --destroy-test-data
# python3 morpheus.py --setup-bot-user
# python3 morpheus.py --destroy-bot-user
#
# There are two layers here. --user and --device are the lower-level raw operations: one user-side or one device-side call each. --app-sim and --device-sim sit on top of them and run the sequence of those raw operations that we recommend a real phone app or device performs.
#
# Raw operations (lower level):
# python3 morpheus.py --user user@example.com
# python3 morpheus.py --device node_rsa
#
# Simulators (recommended sequence of the raw operations):
# python3 morpheus.py --app-sim user@example.com
# python3 morpheus.py --device-sim node_multi
# python3 morpheus.py --client-outputs https://rmng-public-assets-123456789012.s3.us-east-1.amazonaws.com/ap-south-1/rmng-client-outputs.json --user user@example.com
