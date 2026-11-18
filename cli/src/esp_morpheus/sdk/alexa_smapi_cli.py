#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""SMAPI operator commands for Alexa skills: --status and --list-skills read; --delete removes a skill.

Creating and configuring a skill is `esp-morpheus admin integrations alexa setup-auto`, which drives the backend endpoint the dashboard also uses.

Auth is the LWA loopback flow: the first run opens a browser for consent and caches the refresh token (or takes SMAPI_REFRESH_TOKEN), so later runs are headless. Reads SMAPI_CLIENT_ID / SMAPI_CLIENT_SECRET, and SMAPI_VENDOR_ID (optional; else the profile's first vendor).
"""
import argparse
import http.server
import json
import os
import threading
import urllib.error
import urllib.parse
import urllib.request
import webbrowser

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
    client_id = required_env("SMAPI_CLIENT_ID")
    client_secret = required_env("SMAPI_CLIENT_SECRET")
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


STAGE = os.environ.get("ALEXA_STAGE", "development")
EVENTS_PERMISSION = "alexa::async_event:write"


def required_env(name):
    val = os.environ.get(name)
    if not val:
        raise SystemExit(f"ERROR: missing config {name}")
    return val


def build_client():
    client_id = required_env("SMAPI_CLIENT_ID")
    client_secret = required_env("SMAPI_CLIENT_SECRET")
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

    print("\n  [3] backend config: not visible to SMAPI; check the dashboard's Alexa tab or "
          "SSM /rmng/alexa/*.")


def main():
    ap = argparse.ArgumentParser(
        description="SMAPI operator commands: inspect, list or delete skills. Creating a skill is `esp-morpheus admin "
                    "integrations alexa setup-auto`, which drives the backend endpoint the "
                    "dashboard also uses.")
    ap.add_argument("--status", action="store_true",
                    help="report which spec steps are applied to --skill-id (read-only)")
    ap.add_argument("--debug", action="store_true", help="with --status, dump raw SMAPI responses")
    ap.add_argument("--list-skills", action="store_true", help="list the vendor's skills (id + name)")
    ap.add_argument("--delete", action="store_true", help="delete the skill (test cleanup)")
    ap.add_argument("--skill-id", help="the skill to inspect or delete")
    args = ap.parse_args()

    skill_id = args.skill_id or os.environ.get("SMAPI_SKILL_ID")

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

    ap.print_help()
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
