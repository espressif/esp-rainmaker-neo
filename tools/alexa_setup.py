#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
Automate the Alexa Smart Home skill setup via SMAPI, replacing the manual
Alexa Developer Console procedure in docs/en/specs/alexa.md ("Alexa Developer
Console Setup", steps 1-4).

Runs the whole procedure in one command:
  1. create skill (or reuse SMAPI_SKILL_ID)            -> Step 1 (create Smart Home skill)
  2. set the alexa::async_event:write permission        -> events (enables AcceptGrant creds)
  3. set account linking to the Cognito va-client       -> Step 1 (account linking)
  4. POST the backend config API (SigV4)                -> Step 2+3 (AcceptGrant creds -> SSM,
     Cognito callbacks, and the Lambda alexa-connectedhome invoke permission)
  5. set the smart-home endpoint ARN(s) in the manifest -> Step 4 (default endpoint)

It does NOT submit for certification (Amazon's async review). Pass --dry-run to build
payloads without writes. Diagnostics: --status (per-step report), --status --debug (raw).

Auth: SMAPI uses a loopback browser flow (first run only, then cached); the
config-API POST is SigV4-signed with your ambient AWS credentials (aws sso login / a
profile). So the run needs BOTH SMAPI_CLIENT_ID/SECRET and AWS credentials.

--- Inputs ---
CLI args (preferred):
  --skill-id      operate on an existing skill instead of creating one (idempotent)
  --skill-name    skill name (default "ESP RainMaker")
Env / auto-loaded from rmng-outputs.json:
  SMAPI_CLIENT_ID / SMAPI_CLIENT_SECRET   LWA security profile
  SMAPI_VENDOR_ID                         optional; else taken from get_vendor_list
  RMNG_OUTPUTS                            path to rmng-outputs.json (default ./rmng-outputs.json)
  EspVaClientId / EspVaClientSecret       va-client creds (from outputs)
  ESP_AUTH_URL + ESP_TOKEN_URL            override the OIDC authorize/token URLs
                                          (default: EspUserAuthorizeUrl/EspUserTokenUrl outputs)
  ALEXA_ENDPOINT_ARN[_NA/EU/FE]           smart-home Lambda ARN(s) (from outputs: AlexaSkillFunctionArn)
  PRIVACY_POLICY_URL                      optional; defaults to the RainMaker privacy URL
"""
import argparse
import http.server
import json
import os
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import webbrowser

# Runs both as a module imported by cli/morpheus.py and as a standalone script, so make the repo root
# importable either way before reaching for the shared outputs helpers.
_REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
if _REPO_ROOT not in sys.path:
    sys.path.insert(0, _REPO_ROOT)

from scripts.rmng_outputs import (
    alexa_region_arns,
    default_alexa_arn,
    find_output,
    oidc_endpoints,
    resolve_source,
)
from scripts.rmng_outputs import load as rmng_outputs_load

SMAPI_BASE = os.environ.get("SMAPI_BASE", "https://api.amazonalexa.com")

# --- LWA (Login with Amazon) auth: loopback browser flow for SMAPI tokens ---
# One-time bootstrap: create an LWA Security Profile (client id/secret) and add
# http://127.0.0.1:9090/cb as an Allowed Return URL. First run opens the browser;
# the refresh token is cached so later runs are headless.
SCOPES = "alexa::ask:skills:readwrite alexa::ask:skills:test"
LWA_AUTHORIZE_URL = "https://www.amazon.com/ap/oa"
LWA_TOKEN_URL = "https://api.amazon.com/auth/o2/token"
CACHE_PATH = os.environ.get("SMAPI_TOKEN_CACHE",
                            os.path.expanduser("~/.config/rmng/smapi_token.json"))


class _CallbackHandler(http.server.BaseHTTPRequestHandler):
    """Captures the ?code=... (or ?error=...) from the LWA redirect, once."""

    def do_GET(self):  # noqa: N802 (http.server API)
        params = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
        self.server.auth_code = params.get("code", [None])[0]
        self.server.auth_error = params.get("error_description", params.get("error", [None]))[0]
        self.server.auth_state = params.get("state", [None])[0]
        self.send_response(200)
        self.send_header("Content-Type", "text/html")
        self.end_headers()
        ok = self.server.auth_code is not None
        body = "Authentication complete. You can close this tab." if ok else \
               f"Authentication failed: {self.server.auth_error}"
        self.wfile.write(f"<html><body><h3>{body}</h3></body></html>".encode())

    def log_message(self, *args):  # silence default stderr logging
        pass


def _exchange(data):
    """POST to the LWA token endpoint and return the parsed JSON."""
    body = urllib.parse.urlencode(data).encode()
    req = urllib.request.Request(LWA_TOKEN_URL, data=body,
                                 headers={"Content-Type": "application/x-www-form-urlencoded"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.loads(resp.read().decode())


def _browser_login(client_id, client_secret, port):
    """Loopback OAuth: open browser, capture the code, return a refresh token."""
    redirect_uri = f"http://127.0.0.1:{port}/cb"
    state = os.urandom(8).hex()
    query = urllib.parse.urlencode({
        "client_id": client_id, "scope": SCOPES, "response_type": "code",
        "redirect_uri": redirect_uri, "state": state,
    })
    server = http.server.HTTPServer(("127.0.0.1", port), _CallbackHandler)
    server.auth_code = server.auth_error = server.auth_state = None
    print(f"Opening browser for LWA consent (redirect {redirect_uri}) ...")
    print("If it doesn't open, visit:\n  " + LWA_AUTHORIZE_URL + "?" + query)
    webbrowser.open(LWA_AUTHORIZE_URL + "?" + query)
    t = threading.Thread(target=server.handle_request)  # serve exactly one request
    t.start()
    t.join(timeout=300)
    server.server_close()
    if server.auth_error:
        raise RuntimeError(f"LWA denied: {server.auth_error}")
    if not server.auth_code:
        raise RuntimeError("no authorization code received (timed out or return URL mismatch)")
    if server.auth_state != state:
        raise RuntimeError("state mismatch -- possible CSRF, aborting")
    tokens = _exchange({
        "grant_type": "authorization_code", "code": server.auth_code,
        "client_id": client_id, "client_secret": client_secret, "redirect_uri": redirect_uri,
    })
    if "refresh_token" not in tokens:
        raise RuntimeError(f"token exchange returned no refresh_token: {tokens}")
    return tokens["refresh_token"]


def get_refresh_token(client_id, client_secret, port):
    """env -> cache -> browser. Persist so the browser is a first-run-only cost."""
    if os.environ.get("SMAPI_REFRESH_TOKEN"):
        return os.environ["SMAPI_REFRESH_TOKEN"]
    if os.path.exists(CACHE_PATH):
        try:
            return json.load(open(CACHE_PATH))["refresh_token"]
        except (KeyError, ValueError):
            pass  # corrupt cache -> re-auth
    token = _browser_login(client_id, client_secret, port)
    os.makedirs(os.path.dirname(CACHE_PATH), exist_ok=True)
    with open(os.open(CACHE_PATH, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "w") as f:
        json.dump({"refresh_token": token}, f)
    print(f"Cached refresh token at {CACHE_PATH} (chmod 600). Future runs skip the browser.")
    return token


def get_access_token():
    """LWA refresh -> access token (same creds/flow the SDK uses), for raw REST calls."""
    client_id = cfg("SMAPI_CLIENT_ID", {}, required=True)
    client_secret = cfg("SMAPI_CLIENT_SECRET", {}, required=True)
    refresh_token = get_refresh_token(client_id, client_secret,
                                      int(os.environ.get("SMAPI_REDIRECT_PORT", "9090")))
    return _exchange({"grant_type": "refresh_token", "refresh_token": refresh_token,
                      "client_id": client_id, "client_secret": client_secret})["access_token"]


def fetch_account_linking(skill_id, access_token):
    """Read account linking via raw REST and UNWRAP the accountLinkingResponse envelope
    (the SDK model doesn't, so get_account_linking_info_v1 returns all-null). Returns the
    inner dict, or {} if none configured."""
    url = f"{SMAPI_BASE}/v1/skills/{skill_id}/stages/{STAGE}/accountLinkingClient"
    req = urllib.request.Request(url, headers={"Authorization": access_token})
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return json.loads(r.read().decode()).get("accountLinkingResponse", {})
    except urllib.error.HTTPError as e:
        if e.code == 404:
            return {}
        raise


# The Alexa account-linking redirect URLs are not returned by the accountLinkingClient
# API; they are vendor-scoped and identical across a vendor's skills. Verify against the
# console's "Alexa Redirect URLs" once; override with ALEXA_REDIRECT_URLS (comma-sep) if
# your vendor's differ.
def alexa_redirect_urls(vendor_id):
    override = os.environ.get("ALEXA_REDIRECT_URLS")
    if override:
        return [u.strip() for u in override.split(",") if u.strip()]
    hosts = ["pitangui.amazon.com", "layla.amazon.com", "alexa.amazon.co.jp"]
    return [f"https://{h}/api/skill/link/{vendor_id}" for h in hosts]


def sigv4_post(url, body, region):
    """POST a JSON body to an AWS_IAM-authorized API Gateway endpoint, SigV4-signed
    with the ambient AWS credentials (the same ones used to deploy the stack)."""
    import boto3
    from botocore.auth import SigV4Auth
    from botocore.awsrequest import AWSRequest
    creds = boto3.Session().get_credentials()
    if creds is None:
        raise SystemExit("no AWS credentials found (configure AWS_PROFILE / env creds)")
    data = json.dumps(body)
    aws_req = AWSRequest(method="POST", url=url, data=data,
                         headers={"Content-Type": "application/json"})
    SigV4Auth(creds.get_frozen_credentials(), "execute-api", region).add_auth(aws_req)
    req = urllib.request.Request(url, data=data.encode(), method="POST",
                                 headers=dict(aws_req.headers))
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            print(f"config API HTTP {r.status}: {r.read().decode()}")
    except urllib.error.HTTPError as e:
        raise SystemExit(f"config API HTTP {e.code}: {e.read().decode()}")


def sigv4_config_poster(api_url, region, manufacturer_name=None):
    """Config-POST for the standalone CLI: SigV4-sign with ambient AWS credentials.
    Returns a post_config_fn(skill_id, client_id, client_secret, redirect_uris).

    manufacturer_name is the brand advertised in Alexa discovery. Left as None it is omitted
    from the body, so the deployment keeps whatever brand is already stored."""
    def poster(skill_id, client_id, client_secret, redirect_uris):
        url = api_url.rstrip("/") + "/v1/admin/integrations/alexa/configuration"
        body = {"skill_id": skill_id, "client_id": client_id,
                "client_secret": client_secret, "redirect_uris": redirect_uris}
        if manufacturer_name is not None:
            body["manufacturer_name"] = manufacturer_name
        sigv4_post(url, body, region)
    return poster

STAGE = os.environ.get("ALEXA_STAGE", "development")
# Scopes the seeded va-client is granted in the OIDC client registry (see SEEDED_OAUTH_CLIENTS
# and the "scopes" default in esp_user_base_stack.py); /oauth2/authorize rejects anything outside them.
LINKING_SCOPES = ["openid", "email", "profile", "phone"]
EVENTS_PERMISSION = "alexa::async_event:write"


# --------------------------------------------------------------------------- config

def load_outputs():
    """RMNG_OUTPUTS is how cli/morpheus.py hands over the source it already resolved from
    --client-outputs; a bare relative path falls back to the repo root, not the CWD."""
    path = resolve_source(os.environ.get("RMNG_OUTPUTS"))
    if not path.startswith(("http://", "https://")) and not os.path.exists(path):
        return {}
    return rmng_outputs_load(path)


def map_region_arns(outputs):
    """Map every AlexaSkillFunctionArn to its Alexa region code (NA/EU/FE) by the
    region embedded in the ARN. Returns (region_arns, default_arn)."""
    region_arns = alexa_region_arns(outputs)
    return region_arns, default_alexa_arn(region_arns)


def cfg(name, outputs, output_key=None, required=True, default=None):
    val = os.environ.get(name) or (find_output(outputs, output_key) if output_key else None) or default
    if required and not val:
        raise SystemExit(f"ERROR: missing config {name}" + (f" (or output {output_key})" if output_key else ""))
    return val


# --------------------------------------------------------------------------- manifest

def build_manifest(cfgd, include_endpoint=False, include_events=False):
    """The endpoint is set only AFTER the backend config API has granted the Lambda(s)
    the alexa-connectedhome.amazon.com invoke permission -- otherwise Alexa rejects the
    endpoint ARN at manifest-build time."""
    from ask_smapi_model.v1.skill.manifest.skill_manifest import SkillManifest
    from ask_smapi_model.v1.skill.manifest.skill_manifest_apis import SkillManifestApis
    from ask_smapi_model.v1.skill.manifest.smart_home_apis import SmartHomeApis
    from ask_smapi_model.v1.skill.manifest.skill_manifest_endpoint import SkillManifestEndpoint
    from ask_smapi_model.v1.skill.manifest.region import Region
    from ask_smapi_model.v1.skill.manifest.skill_manifest_publishing_information import (
        SkillManifestPublishingInformation)
    from ask_smapi_model.v1.skill.manifest.skill_manifest_localized_publishing_information import (
        SkillManifestLocalizedPublishingInformation)
    from ask_smapi_model.v1.skill.manifest.skill_manifest_privacy_and_compliance import (
        SkillManifestPrivacyAndCompliance)
    from ask_smapi_model.v1.skill.manifest.skill_manifest_localized_privacy_and_compliance import (
        SkillManifestLocalizedPrivacyAndCompliance)
    from ask_smapi_model.v1.skill.manifest.permission_items import PermissionItems
    from ask_smapi_model.v1.skill.manifest.distribution_mode import DistributionMode

    publishing = SkillManifestPublishingInformation(
        locales={"en-US": SkillManifestLocalizedPublishingInformation(
            name=cfgd["skill_name"],
            summary="Control your ESP RainMaker devices with Alexa.",
            description="Control your ESP RainMaker devices with Alexa.",
            example_phrases=["Alexa, turn on the light"],
            keywords=["smart home", "iot"],
        )},
        category="SMART_HOME",
        # Private Smart Home skills relied on Alexa-for-Business (retired), so the
        # console flags PRIVATE as "Action required". Default PUBLIC; override with
        # DISTRIBUTION_MODE=PRIVATE if you really want it.
        distribution_mode=os.environ.get("DISTRIBUTION_MODE", DistributionMode.PUBLIC.value),
        is_available_worldwide=True,
    )
    privacy = SkillManifestPrivacyAndCompliance(
        locales={"en-US": SkillManifestLocalizedPrivacyAndCompliance(
            privacy_policy_url=cfgd["privacy_url"])},
        uses_personal_info=True,
        is_child_directed=False,
        is_export_compliant=True,
        contains_ads=False,
    )

    smart_home = SmartHomeApis()
    if include_endpoint:
        regions = {code: Region(endpoint=SkillManifestEndpoint(uri=arn))
                   for code, arn in cfgd["region_arns"].items()}
        smart_home = SmartHomeApis(
            endpoint=SkillManifestEndpoint(uri=cfgd["default_arn"]),
            regions=regions or None,
            protocol_version="3",
        )

    return SkillManifest(
        manifest_version="1.0",
        publishing_information=publishing,
        privacy_and_compliance=privacy,
        permissions=[PermissionItems(name=EVENTS_PERMISSION)] if include_events else None,
        apis=SkillManifestApis(smart_home=smart_home),
    )


def build_account_linking(cfgd):
    from ask_smapi_model.v1.skill.account_linking.account_linking_request import AccountLinkingRequest
    from ask_smapi_model.v1.skill.account_linking.account_linking_request_payload import (
        AccountLinkingRequestPayload)
    from ask_smapi_model.v1.skill.account_linking.account_linking_type import AccountLinkingType
    return AccountLinkingRequest(account_linking_request=AccountLinkingRequestPayload(
        object_type=AccountLinkingType.AUTH_CODE.value,
        authorization_url=cfgd["auth_url"],
        access_token_url=cfgd["token_url"],
        client_id=cfgd["va_client_id"],
        client_secret=cfgd["va_client_secret"],
        scopes=LINKING_SCOPES,
        access_token_scheme="HTTP_BASIC",
    ))


# --------------------------------------------------------------------------- SMAPI ops

def wait_for_build(client, skill_id, timeout=300):
    """Poll manifest build status until it leaves IN_PROGRESS."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        status = client.get_skill_status_v1(skill_id)
        m = getattr(status, "manifest", None)
        req = getattr(m, "last_update_request", None) if m else None
        state = getattr(req, "status", None)
        state = getattr(state, "value", state)  # SDK returns a Status enum, not a str
        print(f"  build status: {state}")
        if state and state != "IN_PROGRESS":
            if state != "SUCCEEDED":
                raise SystemExit(f"manifest build failed: {state} errors={getattr(req,'errors',None)}")
            return
        time.sleep(5)
    raise SystemExit("timed out waiting for manifest build")


def build_client():
    client_id = cfg("SMAPI_CLIENT_ID", {}, required=True)
    client_secret = cfg("SMAPI_CLIENT_SECRET", {}, required=True)
    port = int(os.environ.get("SMAPI_REDIRECT_PORT", "9090"))
    refresh_token = get_refresh_token(client_id, client_secret, port)
    from ask_smapi_sdk import StandardSmapiClientBuilder
    return StandardSmapiClientBuilder(
        client_id=client_id, client_secret=client_secret, refresh_token=refresh_token).client()


def resolve_vendor(client, vendor_id=None):
    if vendor_id:
        return vendor_id
    vendors = client.get_vendor_list_v1().vendors or []
    if not vendors:
        raise SystemExit("no vendor for this LWA profile")
    return vendors[0].id


def resolve_cfgd(outputs, skill_name):
    """Build the config dict (auth URLs, va-client creds, endpoint ARNs, privacy)
    from rmng-outputs.json with env-var overrides. Shared by the CLI and morpheus.py."""
    region = os.environ.get("RMNG_REGION") or find_output(outputs, "StackRegion") or "us-east-1"
    # Account linking targets the espuser OIDC broker, not Cognito directly: the
    # EspUserAuthorizeUrl/EspUserTokenUrl outputs already carry the API's custom
    # domain when one is mapped, so no URL construction is needed here. Shared with
    # cli/morpheus.py's instruction printers so the two cannot report different endpoints.
    outputs_auth_url, outputs_token_url = oidc_endpoints(outputs)
    auth_url = os.environ.get("ESP_AUTH_URL") or outputs_auth_url
    token_url = os.environ.get("ESP_TOKEN_URL") or outputs_token_url

    region_arns, default_arn = map_region_arns(outputs)
    default_arn = os.environ.get("ALEXA_ENDPOINT_ARN") or default_arn
    region_arns.update({c: os.environ[f"ALEXA_ENDPOINT_ARN_{c}"]
                        for c in ("NA", "EU", "FE") if os.environ.get(f"ALEXA_ENDPOINT_ARN_{c}")})
    if not default_arn:
        raise SystemExit("ERROR: no ALEXA_ENDPOINT_ARN and no AlexaSkillFunctionArn in outputs")
    if not (auth_url and token_url):
        raise SystemExit("ERROR: no OIDC endpoints in outputs (EspUserAuthorizeUrl/EspUserTokenUrl, "
                         "EspUserDiscoveryIssuer, or EspUserApiUrl); set ESP_AUTH_URL/ESP_TOKEN_URL")

    cfgd = {
        "skill_name": skill_name or "ESP RainMaker",
        "privacy_url": cfg("PRIVACY_POLICY_URL", outputs, required=False,
                           default="https://rainmaker.espressif.com/privacy"),
        "va_client_id": cfg("EspVaClientId", outputs, "EspVaClientId"),
        "va_client_secret": cfg("EspVaClientSecret", outputs, "EspVaClientSecret"),
        "auth_url": auth_url, "token_url": token_url,
        "default_arn": default_arn, "region_arns": region_arns,
    }
    return cfgd, region


def run_setup(client, cfgd, vendor_id, skill_id, post_config_fn):
    """Full SMAPI setup pipeline. post_config_fn(skill_id, client_id, client_secret,
    redirect_uris) performs the backend config-API POST -- SigV4 for the CLI, the
    super-admin user's session for morpheus.py. Returns the skill_id."""
    from ask_smapi_model.v1.skill.create_skill_request import CreateSkillRequest
    from ask_smapi_model.v1.skill.manifest.skill_manifest_envelope import SkillManifestEnvelope

    if skill_id:
        print(f"1. reusing existing skill {skill_id}")
    else:
        print("1. creating skill ...")
        skill_id = client.create_skill_for_vendor_v1(CreateSkillRequest(
            vendor_id=vendor_id, manifest=build_manifest(cfgd))).skill_id
        print(f"   created skill_id: {skill_id}")
        wait_for_build(client, skill_id)

    # events permission before account linking (a manifest build resets linking)
    print("2. setting events permission ...")
    client.update_skill_manifest_v1(skill_id, STAGE, SkillManifestEnvelope(
        manifest=build_manifest(cfgd, include_events=True)))
    wait_for_build(client, skill_id)

    print("3. setting account linking ...")
    client.update_account_linking_info_v1(skill_id, STAGE, build_account_linking(cfgd))
    al = fetch_account_linking(skill_id, get_access_token())
    if not al.get("clientId"):
        raise SystemExit("account linking did not persist")
    print(f"   linked client_id: {al['clientId']}")

    msg = client.get_skill_credentials_v1(skill_id).skill_messaging_credentials
    redirect_uris = alexa_redirect_urls(vendor_id)
    print("4. posting backend config ...")
    print("   payload:", json.dumps({"skill_id": skill_id, "client_id": msg.client_id,
          "client_secret": f"<len={len(msg.client_secret)}>", "redirect_uris": redirect_uris}))
    post_config_fn(skill_id, msg.client_id, msg.client_secret, redirect_uris)

    # endpoint only after the config POST has granted the Lambda invoke permission;
    # brief wait so it propagates before Alexa validates it.
    time.sleep(5)
    print("5. setting smart-home endpoint ...")
    client.update_skill_manifest_v1(skill_id, STAGE, SkillManifestEnvelope(
        manifest=build_manifest(cfgd, include_endpoint=True, include_events=True)))
    wait_for_build(client, skill_id)

    print("6. enabling skill for this account ...")
    try:
        client.set_skill_enablement_v1(skill_id, STAGE)
        print("   enabled.")
    except Exception as e:
        print(f"   not enable-able via SMAPI ({type(e).__name__}). Enable + link in the "
              "Alexa app: More -> Skills & Games -> search the skill -> Enable To Use.")

    print(f"\nSkill setup complete: {skill_id}\n")
    show_status(client, skill_id)
    return skill_id


def setup(post_config_fn, skill_id=None, skill_name=None):
    """Programmatic entry (used by morpheus.py). Reads config from env + rmng-outputs.json
    and runs the pipeline, delegating the config-API POST to post_config_fn."""
    outputs = load_outputs()
    cfgd, _ = resolve_cfgd(outputs, skill_name or os.environ.get("SKILL_NAME"))
    client = build_client()
    vendor_id = resolve_vendor(client, os.environ.get("SMAPI_VENDOR_ID"))
    print(f"vendor_id: {vendor_id}")
    return run_setup(client, cfgd, vendor_id, skill_id, post_config_fn)


# --- reusable entrypoints shared by the CLI and morpheus.py ---

def list_all():
    """List the vendor's skills (id + name)."""
    client = build_client()
    list_skills(client, resolve_vendor(client, os.environ.get("SMAPI_VENDOR_ID")))


def delete(skill_id):
    build_client().delete_skill_v1(skill_id)
    print(f"deleted {skill_id}")


def status(skill_id, debug=False):
    show_status(build_client(), skill_id, debug=debug)


def list_skills(client, vendor_id):
    """List the vendor's skills (id + name) so test clutter is easy to spot/prune."""
    skills = client.list_skills_for_vendor_v1(vendor_id).skills or []
    print(f"{len(skills)} skill(s) for vendor {vendor_id}:")
    for s in skills:
        names = s.name_by_locale or {}
        name = names.get("en-US") or next(iter(names.values()), "?")
        print(f"  {s.skill_id}  {name!r}")


def _raw(obj):
    try:
        return obj.to_dict()
    except Exception:
        return obj


def show_status(client, skill_id, debug=False):
    """Report which of alexa.md's 4 console steps are actually applied to the skill."""
    print(f"Status for {skill_id} (stage={STAGE}):\n")

    # Step 1a: skill exists
    print("  [1] create skill ......... DONE (skill exists)")

    # Step 1b: account linking configured (raw REST -- the SDK getter returns all-null
    # because the response is wrapped in an "accountLinkingResponse" envelope).
    try:
        al = fetch_account_linking(skill_id, get_access_token())
        linked = bool(al.get("clientId") and al.get("accessTokenUrl"))
        print(f"  [1] account linking ...... {'DONE' if linked else 'NOT SET'}"
              + (f"  (client={al.get('clientId')})" if linked else ""))
        if debug:
            print("      RAW account linking:", json.dumps(al, indent=2))
    except Exception as e:
        print(f"  [1] account linking ...... UNKNOWN ({type(e).__name__}: {e})")

    # Step 4: endpoint ARN(s) in the manifest + events permission
    try:
        env = client.get_skill_manifest_v1(skill_id, STAGE)
        sh = env.manifest.apis.smart_home if env.manifest and env.manifest.apis else None
        ep = getattr(getattr(sh, "endpoint", None), "uri", None) if sh else None
        regions = getattr(sh, "regions", None) if sh else None
        perms = [p.get("name") for p in (_raw(env.manifest).get("permissions") or [])]
        print(f"  [4] default endpoint ..... {'DONE ('+ep+')' if ep else 'NOT SET'}")
        print(f"      region endpoints ..... {list(regions.keys()) if regions else '(none)'}")
        print(f"      events permission .... {'DONE' if EVENTS_PERMISSION in perms else 'NOT SET'} (have: {perms})")
        if debug:
            print("      RAW apis:", json.dumps(_raw(env.manifest.apis), default=str, indent=2))
            print("      RAW permissions:", json.dumps(_raw(env.manifest).get("permissions"), default=str))
    except Exception as e:
        print(f"  [4] manifest ............. UNKNOWN ({type(e).__name__}: {e})")

    # Step 2: AcceptGrant creds available
    try:
        msg = client.get_skill_credentials_v1(skill_id).skill_messaging_credentials
        print(f"  [2] AcceptGrant creds .... AVAILABLE (client={msg.client_id})")
    except Exception as e:
        print(f"  [2] AcceptGrant creds .... UNKNOWN ({type(e).__name__}: {e})")

    # Enablement: does the skill show in the Alexa app for this developer account?
    try:
        client.get_skill_enablement_status_v1(skill_id, STAGE)
        print("  [+] skill enabled ........ DONE (visible in the Alexa app)")
    except Exception:
        print("  [+] skill enabled ........ NOT ENABLED (won't appear in the Alexa app)")

    print("\n  redirect URLs (for Cognito va-client callbacks) are vendor-derived, not from "
          "this API -- see alexa_redirect_urls().")
    print("\n  [3] backend config API POST: done during a full run; --status cannot verify it "
          "(check SSM /rmng/alexa/* or the Cognito va-client callbacks).")


def main():
    ap = argparse.ArgumentParser(description="Automate Alexa Smart Home skill setup via SMAPI.")
    ap.add_argument("--dry-run", action="store_true", help="build payloads, print, make no writes")
    ap.add_argument("--status", action="store_true",
                    help="report which spec steps are applied to SMAPI_SKILL_ID (read-only)")
    ap.add_argument("--debug", action="store_true", help="with --status, dump raw SMAPI responses")
    ap.add_argument("--list-skills", action="store_true", help="list the vendor's skills (id + name)")
    ap.add_argument("--delete", action="store_true", help="delete the skill (test cleanup)")
    ap.add_argument("--skill-id", help="operate on an existing skill instead of creating one")
    ap.add_argument("--skill-name", help="skill name (default 'ESP RainMaker')")
    ap.add_argument("--manufacturer",
                    help="brand advertised in Alexa discovery (set your own for a rebranded/OEM "
                         "deployment); omit to keep the stored brand, pass '' to reset to Espressif")
    args = ap.parse_args()

    # CLI args take precedence over env (env kept as a fallback).
    skill_id = args.skill_id or os.environ.get("SMAPI_SKILL_ID")
    skill_name = args.skill_name or os.environ.get("SKILL_NAME", "ESP RainMaker")
    # Distinguish "not supplied" from an empty value, which resets the brand to the default.
    manufacturer_name = args.manufacturer if args.manufacturer is not None else os.environ.get("ALEXA_MANUFACTURER_NAME")

    if args.list_skills:
        list_all()
        return 0

    if args.delete:
        if not skill_id:
            raise SystemExit("ERROR: pass --skill-id to delete")
        delete(skill_id)
        return 0

    if args.status:
        if not skill_id:
            raise SystemExit("ERROR: pass --skill-id to inspect")
        status(skill_id, debug=args.debug)
        return 0

    outputs = load_outputs()
    cfgd, region = resolve_cfgd(outputs, skill_name)

    if args.dry_run:
        print("== DRY RUN ==")
        print("account linking:", cfgd["auth_url"], "|", cfgd["token_url"], "| client", cfgd["va_client_id"])
        print("default endpoint:", cfgd["default_arn"], "| region overrides:", cfgd["region_arns"] or "(none)")
        print("privacy url:", cfgd["privacy_url"])
        print("manufacturer:", manufacturer_name if manufacturer_name is not None else "(keep stored brand)")
        build_manifest(cfgd, include_endpoint=True, include_events=True)  # validate model shape
        build_account_linking(cfgd)
        print("payloads build OK. No writes performed.")
        return 0

    api_url = os.environ.get("ALEXA_CONFIG_API_URL") or find_output(outputs, "ApiGatewayUrl")
    if not api_url:
        raise SystemExit("ERROR: no ALEXA_CONFIG_API_URL and no ApiGatewayUrl in outputs")

    client = build_client()
    vendor_id = resolve_vendor(client, os.environ.get("SMAPI_VENDOR_ID"))
    print(f"vendor_id: {vendor_id}")
    run_setup(client, cfgd, vendor_id, skill_id, sigv4_config_poster(api_url, region, manufacturer_name))
    return 0


if __name__ == "__main__":
    sys.exit(main())
