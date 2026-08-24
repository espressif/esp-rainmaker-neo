# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Single entry point for reading a deployment's merged CDK outputs (rmng-outputs.json).

Every client — cli/morpheus.py, the device/app simulators, alexa_setup — resolves
and parses outputs through here, so output key names, the espuser-base -> rmng-base fallbacks, and
relative-path resolution each exist in exactly one place.

A deployment is identified by two things: this outputs file and the ambient AWS credentials. Both
must describe the same account and region, which verify_aws_identity() enforces.
"""

import json
import os
from dataclasses import dataclass, field

import boto3
import requests
from botocore.exceptions import BotoCoreError, ClientError, NoCredentialsError

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# Generated CDK outputs now live under build/cdk/ (see scripts/merge_cdk_outputs.py).
# The bare filename is kept as a fallback so an outputs file left at the repo root
# by an older checkout still resolves.
DEFAULT_OUTPUTS_FILE = 'rmng-outputs.json'

# test_config.json lives beside cli/morpheus.py, which owns generating it via --setup-test-data.
TEST_CONFIG_PATH = os.path.join(REPO_ROOT, 'cli', 'test_config.json')

# Seeded end-user OIDC client id, used when the outputs predate the EspUserClientId output.
DEFAULT_ESP_USER_CLIENT_ID = 'user-pool-client'

# Alexa smart-home region codes keyed by the AWS region the endpoint lives in. Alexa accepts
# endpoints only in these three regions, wherever RMNG itself is deployed.
AWS_REGION_TO_ALEXA = {'us-east-1': 'NA', 'eu-west-1': 'EU', 'us-west-2': 'FE'}
AWS_REGION_TO_ST = {'us-east-1': 'NA', 'eu-west-1': 'EU', 'ap-northeast-1': 'AP'}

_URL_SCHEMES = ('http://', 'https://')


def resolve_source(source=None):
    """Resolve an outputs source to an absolute path or a URL.

    Relative paths anchor to the repo root, not the caller's CWD, so the tool behaves identically
    from any directory. URLs and absolute paths pass through untouched.
    """
    source = source or DEFAULT_OUTPUTS_FILE
    if source.startswith(_URL_SCHEMES):
        return source
    return source if os.path.isabs(source) else os.path.join(REPO_ROOT, source)


def load(source=None):
    """Load outputs from a local path or an http(s) URL.

    A URL source lets a client point at a published outputs file (e.g. the per-region
    rmng-client-outputs.json in S3) without downloading it first; the fetched JSON has the same
    stack-keyed shape as the local file.
    """
    source = resolve_source(source)
    if source.startswith(_URL_SCHEMES):
        response = requests.get(source, timeout=30)
        response.raise_for_status()
        return response.json()
    with open(source, 'r') as f:
        return json.load(f)


def find_outputs(outputs, key):
    """Collect every CFN output named `key`, at any nesting depth.

    Outputs are keyed by stack, and some stacks publish per-region sub-objects, so one output name
    can appear under several keys. Searching by name keeps callers independent of that layout.
    """
    found, pending = [], [outputs]
    while pending:
        node = pending.pop()
        if isinstance(node, dict):
            if isinstance(node.get(key), str):
                found.append(node[key])
            pending.extend(node.values())
        elif isinstance(node, list):
            pending.extend(node)
    return found


def find_output(outputs, key):
    got = find_outputs(outputs, key)
    return got[0] if got else None


def alexa_region_arns(outputs):
    """Map each AlexaSkillFunctionArn to its Alexa region code (NA/EU/FE).

    The code comes from the region embedded in the ARN, so this reads both the flat and the
    per-region-nested outputs shapes and does not assume the Alexa stacks share RMNG's own region.
    """
    region_arns = {}
    for arn in find_outputs(outputs, 'AlexaSkillFunctionArn'):
        parts = arn.split(':')
        code = AWS_REGION_TO_ALEXA.get(parts[3] if len(parts) > 3 else '')
        if code:
            region_arns[code] = arn
    return region_arns


def default_alexa_arn(region_arns):
    """The endpoint Alexa advertises when no geography-specific one applies; NA is its default."""
    return region_arns.get('NA') or next(iter(region_arns.values()), None)


def st_region_arns(outputs):
    """Map each STSchemaAppFunctionArn to its SmartThings geo code (NA/EU/AP).

    Same shape-agnostic resolution as alexa_region_arns: the geo comes from the
    region embedded in the ARN, covering both flat and per-region-nested outputs.
    """
    region_arns = {}
    for arn in find_outputs(outputs, 'STSchemaAppFunctionArn'):
        parts = arn.split(':')
        code = AWS_REGION_TO_ST.get(parts[3] if len(parts) > 3 else '')
        if code:
            region_arns[code] = arn
    return region_arns


def oidc_endpoints(outputs):
    """Resolve the (authorize, token) OIDC endpoints used for voice-assistant account linking.

    Account linking targets the espuser OIDC broker, not Cognito, and the published endpoints
    already carry the API's custom domain when one is mapped — so they are the source. The stack
    builds them by concatenating onto EspUserApiUrl, which silently loses the separator if that
    value ever lacks its trailing slash, so a result that does not contain the /oauth2/ path is
    treated as unusable: fall through to the deployment's own discovery document, then to a
    correctly-joined API base. Returns (None, None) when the outputs carry no usable source.
    """
    authorize = find_output(outputs, 'EspUserAuthorizeUrl')
    token = find_output(outputs, 'EspUserTokenUrl')
    if authorize and token and '/oauth2/' in authorize and '/oauth2/' in token:
        return authorize, token

    issuer = find_output(outputs, 'EspUserDiscoveryIssuer')
    if issuer:
        try:
            disco = requests.get(f"{issuer.rstrip('/')}/.well-known/openid-configuration", timeout=10).json()
            return disco['authorization_endpoint'], disco['token_endpoint']
        except Exception:
            pass

    api = find_output(outputs, 'EspUserApiUrl')
    if api:
        api = api.rstrip('/')
        return f'{api}/oauth2/authorize', f'{api}/oauth2/token'
    return None, None


@dataclass
class RmngSettings:
    """Everything a client needs to talk to one deployment, extracted from its outputs."""

    source: str
    raw: dict = field(repr=False)

    region: str
    account_id: str
    identity_pool_id: str
    api_gateway_url: str
    iot_endpoint: str
    default_thing_policy: str
    files_bucket_name: str

    # Admins sign in against the admin Cognito pool; end users are provisioned into the provider
    # pool and then sign in through the ESP User OIDC API, so only its pool id is needed here.
    admin_user_pool_id: str
    admin_client_id: str
    end_user_pool_id: str
    user_api_gateway_url: str
    esp_user_client_id: str
    esp_user_discovery_issuer: str

    gva_fulfillment_url: str

    alexa_region_arns: dict
    st_region_arns: dict

    @classmethod
    def from_source(cls, source=None):
        resolved = resolve_source(source)
        return cls.from_outputs(load(resolved), resolved)

    @classmethod
    def from_outputs(cls, outputs, source=DEFAULT_OUTPUTS_FILE):
        esp_user_base = outputs.get('espuser-base', {})
        rmng_base = outputs.get('rmng-base', {})
        rmng_core = outputs.get('rmng-core', {})
        rmng_gva_core = outputs.get('rmng-gva-core', {})

        try:
            required = {
                'region': rmng_base['StackRegion'],
                'account_id': rmng_base['StackAccountId'],
                'identity_pool_id': rmng_base['IdentityPoolId'],
                'api_gateway_url': rmng_base['ApiGatewayUrl'],
                'iot_endpoint': rmng_base['IoTEndpointUrl'],
                'default_thing_policy': rmng_base['DefaultThingPolicyName'],
            }
        except KeyError as e:
            raise SystemExit(f"Error: {source} is missing required rmng-base output {e}")

        return cls(
            source=source,
            raw=outputs,
            files_bucket_name=rmng_base.get('FilesBucketName', ''),
            # espuser-base owns the pools; fall back to rmng-base for deployments predating it.
            admin_user_pool_id=esp_user_base.get('EspAdminUserPoolId') or rmng_base.get('AdminUserPoolId', ''),
            admin_client_id=esp_user_base.get('EspAdminUserPoolClientId') or rmng_base.get('AdminUserPoolClientId', ''),
            end_user_pool_id=esp_user_base.get('EspEndUserPoolId', ''),
            user_api_gateway_url=esp_user_base.get('EspUserApiUrl', ''),
            esp_user_client_id=esp_user_base.get('EspUserClientId') or DEFAULT_ESP_USER_CLIENT_ID,
            esp_user_discovery_issuer=esp_user_base.get('EspUserDiscoveryIssuer', ''),
            # GVA moved to its own stack; fall back to rmng-core for deployments predating it.
            gva_fulfillment_url=rmng_gva_core.get('GVAFulfillmentUrl') or rmng_core.get('GVAFulfillmentUrl', ''),
            alexa_region_arns=alexa_region_arns(outputs),
            st_region_arns=st_region_arns(outputs),
            **required,
        )

    @property
    def default_alexa_arn(self):
        return default_alexa_arn(self.alexa_region_arns)


def verify_aws_identity(settings, skip=False):
    """Abort unless the ambient AWS credentials point at the deployment the outputs describe.

    The tool reaches AWS directly (IoT, Lambda, DynamoDB, Cognito, IAM), so a mismatch here does not
    fail loudly — it silently reads and mutates a different deployment than the one requested.
    """
    if skip:
        print(f"Warning: --skip-account-check set; not verifying AWS credentials against {settings.source}")
        return

    # Region comes from the outputs so this check works even with no ambient region configured.
    try:
        caller = boto3.client('sts', region_name=settings.region).get_caller_identity()
    except NoCredentialsError:
        raise SystemExit(
            "Error: no AWS credentials found. This tool accesses AWS resources directly; configure "
            "credentials (AWS_PROFILE, aws sso login, ~/.aws/credentials, or environment variables) "
            f"for account {settings.account_id} in {settings.region}."
        )
    except (ClientError, BotoCoreError) as e:
        raise SystemExit(f"Error: could not verify AWS identity via STS: {e}")

    if caller['Account'] != settings.account_id:
        raise SystemExit(
            f"Error: AWS account mismatch. {settings.source} describes account {settings.account_id}, "
            f"but the configured credentials are for {caller['Account']} ({caller['Arn']}). Point "
            "--client-outputs at the matching deployment, switch AWS profile, or pass "
            "--skip-account-check if this is deliberate."
        )

    ambient_region = os.environ.get('AWS_REGION') or boto3.session.Session().region_name
    if ambient_region is None:
        print(f"Warning: no AWS region configured; using {settings.region} from {settings.source}.")
    elif ambient_region != settings.region:
        raise SystemExit(
            f"Error: AWS region mismatch. {settings.source} describes region {settings.region}, but "
            f"the environment resolves to {ambient_region}"
            + (f" (profile {os.environ['AWS_PROFILE']})" if os.environ.get('AWS_PROFILE') else "")
            + f". Set AWS_REGION={settings.region} (or AWS_DEFAULT_REGION), or pass "
            "--skip-account-check if this is deliberate."
        )
