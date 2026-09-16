#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0
"""
Automate the Google Cloud side of the GVA (Google Smart Home) setup via REST,
replacing the manual procedure in docs/en/specs/gva.md ("Google Console Setup",
steps 3-4) and the key-file download/upload dance of `gva_setup`.

Runs the whole procedure in one command:
  1. enable the HomeGraph API on the project           -> Step 3 (Enable HomeGraph API)
  2. create the report-state service account           -> Step 4 (Create Service Account)
  3. grant it the Service Account OpenID Connect
     Identity Token Creator role on the project        -> Step 4 (role assignment)
  4. create a JSON key, held in memory                 -> Step 4 (Generate a JSON key)
  5. POST the key to the backend config API            -> Store Configuration (persists to
     SSM /rmng/gva/service_account_json and updates the Cognito va-client redirect URIs)

It does NOT touch the Google Home Developer Console: creating the Home project
and the cloud-to-cloud integration (OAuth client, auth/token/fulfillment URLs,
scopes, icon) has no public API and stays manual -- see `gva_instruction`
(steps 1-2). The smart home Test Suite likewise can only be run from the console.

--- Inputs ---
  project_id            GCP project linked to the Google Home project (required)
  service_account_name  account id to create or reuse (default "homegraph-agent")

Auth: needs a Google user credential allowed to enable services, create service
accounts, set the project IAM policy and create keys (project Owner, or Service
Usage Admin + Service Account Admin + Project IAM Admin). Taken from
GOOGLE_ACCESS_TOKEN if set, else from `gcloud auth print-access-token`.

Safety: steps 1-3 are idempotent (existing enablement, account and binding are
detected and reused). A NEW key is minted on every run and old keys are never
deleted (Google caps user-managed keys at 10 per account; the script warns as
the limit nears). The key stays in memory; it is written to disk, mode 0600,
only if the backend POST fails, so it can be re-uploaded with `gva_setup <file>`
instead of minting another key.

Set GVA_DRY_RUN=1 to print the planned calls without executing anything.
"""
import argparse
import base64
import json
import os
import subprocess
import time
import urllib.error
import urllib.request

IAM_BASE = "https://iam.googleapis.com/v1"
CRM_BASE = "https://cloudresourcemanager.googleapis.com/v1"
SU_BASE = "https://serviceusage.googleapis.com/v1"
HOMEGRAPH_SERVICE = "homegraph.googleapis.com"
REPORT_STATE_ROLE = "roles/iam.serviceAccountOpenIdTokenCreator"
DEFAULT_SA_NAME = "homegraph-agent"
SA_DESCRIPTION = "Service Account for report state token creation"
USER_KEY_LIMIT = 10  # Google's hard cap on user-managed keys per service account


class ApiError(Exception):
    def __init__(self, code, message):
        super().__init__(f"HTTP {code}: {message}")
        self.code = code


def get_access_token():
    token = os.environ.get("GOOGLE_ACCESS_TOKEN")
    if token:
        return token.strip()
    try:
        out = subprocess.run(["gcloud", "auth", "print-access-token"],
                             capture_output=True, text=True, check=True)
        return out.stdout.strip()
    except FileNotFoundError:
        raise SystemExit("gcloud not found: install the Google Cloud CLI and run "
                         "'gcloud auth login', or set GOOGLE_ACCESS_TOKEN")
    except subprocess.CalledProcessError as e:
        raise SystemExit(f"gcloud auth print-access-token failed: {e.stderr.strip()}\n"
                         "Run 'gcloud auth login' first, or set GOOGLE_ACCESS_TOKEN")


def api(token, method, url, body=None):
    """Minimal JSON REST call; raises ApiError with Google's error message."""
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method, headers={
        "Authorization": f"Bearer {token}", "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read() or "{}")
    except urllib.error.HTTPError as e:
        detail = e.read().decode(errors="replace")
        try:
            detail = json.loads(detail).get("error", {}).get("message", detail)
        except (ValueError, AttributeError):
            pass
        raise ApiError(e.code, detail)


# --------------------------------------------------------------------------- steps

def enable_homegraph(token, project_id):
    print("1. enabling HomeGraph API ...")
    api(token, "POST",
        f"{SU_BASE}/projects/{project_id}/services/{HOMEGRAPH_SERVICE}:enable", {})
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        svc = api(token, "GET",
                  f"{SU_BASE}/projects/{project_id}/services/{HOMEGRAPH_SERVICE}")
        state = svc.get("state")
        print(f"   service state: {state}")
        if state == "ENABLED":
            return
        time.sleep(5)
    raise SystemExit("timed out waiting for the HomeGraph API to enable")


def ensure_service_account(token, project_id, name, email):
    print(f"2. creating service account {email} ...")
    try:
        api(token, "POST", f"{IAM_BASE}/projects/{project_id}/serviceAccounts",
            {"accountId": name,
             "serviceAccount": {"displayName": name, "description": SA_DESCRIPTION}})
        print("   created.")
    except ApiError as e:
        if e.code != 409:
            raise
        print("   already exists, reusing.")


def ensure_role_binding(token, project_id, email):
    print(f"3. granting {REPORT_STATE_ROLE} on the project ...")
    member = f"serviceAccount:{email}"
    policy = api(token, "POST", f"{CRM_BASE}/projects/{project_id}:getIamPolicy", {})
    bindings = policy.setdefault("bindings", [])
    binding = next((b for b in bindings if b.get("role") == REPORT_STATE_ROLE), None)
    if binding and member in binding.get("members", []):
        print("   binding already present.")
        return
    if binding:
        binding.setdefault("members", []).append(member)
    else:
        bindings.append({"role": REPORT_STATE_ROLE, "members": [member]})
    api(token, "POST", f"{CRM_BASE}/projects/{project_id}:setIamPolicy",
        {"policy": policy})
    print("   granted.")


def create_key(token, email):
    print("4. creating a JSON key ...")
    keys = api(token, "GET",
               f"{IAM_BASE}/projects/-/serviceAccounts/{email}/keys?keyTypes=USER_MANAGED"
               ).get("keys", [])
    if len(keys) >= USER_KEY_LIMIT - 2:
        print(f"   WARNING: {len(keys)} user-managed keys already exist (Google caps at "
              f"{USER_KEY_LIMIT}); delete unused keys in the console.")
    created = api(token, "POST", f"{IAM_BASE}/projects/-/serviceAccounts/{email}/keys", {})
    key_json = json.loads(base64.b64decode(created["privateKeyData"]))
    key_id = created["name"].rsplit("/", 1)[-1]
    print(f"   key {key_id} created.")
    return key_json


def save_key(key_json):
    path = f"gva_service_account_{key_json['private_key_id']}.json"
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(key_json, f, indent=2)
    return path


# --------------------------------------------------------------------------- entrypoints

def setup(post_config_fn, project_id, service_account_name=None):
    """Programmatic entry (used by the `admin integrations gva setup-auto` command). post_config_fn(service_account_json)
    performs the backend config-API POST as the super-admin user. Returns the
    service account email."""
    name = service_account_name or DEFAULT_SA_NAME
    if name.startswith("firebase-adminsdk"):
        raise SystemExit("do not reuse the Firebase Admin SDK service account: it can "
                         "authenticate but cannot write to the Home Graph "
                         "(docs/en/specs/gva.md, step 4)")
    email = f"{name}@{project_id}.iam.gserviceaccount.com"

    if os.environ.get("GVA_DRY_RUN"):
        print("DRY RUN, would perform:")
        print(f"  1. POST {SU_BASE}/projects/{project_id}/services/{HOMEGRAPH_SERVICE}:enable")
        print(f"  2. POST {IAM_BASE}/projects/{project_id}/serviceAccounts (accountId={name})")
        print(f"  3. grant {REPORT_STATE_ROLE} to serviceAccount:{email} "
              f"via {CRM_BASE}/projects/{project_id}:setIamPolicy")
        print(f"  4. POST {IAM_BASE}/projects/-/serviceAccounts/{email}/keys")
        print("  5. POST the key JSON to /v1/admin/integrations/gva/configuration")
        return None

    token = get_access_token()
    enable_homegraph(token, project_id)
    ensure_service_account(token, project_id, name, email)
    ensure_role_binding(token, project_id, email)
    key_json = create_key(token, email)

    print("5. storing the key ...")
    try:
        post_config_fn(key_json)
    # BaseException, not Exception: the poster raises SystemExit on API failure and the
    # fresh key must be saved before any exit path unwinds.
    except BaseException:
        path = save_key(key_json)
        print(f"   storing failed; key saved to {path} (0600). Fix the issue and "
              f"re-upload it with: gva_setup {path}")
        raise
    print(f"\nGVA report-state setup complete: {email}\n")
    return email


def main():
    ap = argparse.ArgumentParser(
        description="Automate the GCP side of the GVA report-state setup (enable "
                    "HomeGraph API, create service account, role and key). Writes the "
                    "key to a local file to upload with 'gva_setup <file>'; use "
                    "'admin integrations gva setup-auto' to also POST it in the same run.")
    ap.add_argument("project_id", help="GCP project linked to the Google Home project")
    ap.add_argument("--service-account-name", default=DEFAULT_SA_NAME)
    args = ap.parse_args()

    def write_key_fn(key_json):
        path = save_key(key_json)
        print(f"   key written to {path}; upload it with: gva_setup {path}")

    setup(write_key_fn, args.project_id, args.service_account_name)


if __name__ == "__main__":
    main()
