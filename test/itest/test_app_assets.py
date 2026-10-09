# SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Integration tests for the app-assets CloudFront distribution: the web SPA tree, the short root paths, and the expo-updates OTA tree.

All of the routing lives in CloudFront Functions, behavior path patterns and a cache policy, none of which runs anywhere but CloudFront, so only a request through the deployed distribution shows it working.

Each test module seeds its own web build and OTA manifests under random path segments and deletes them afterwards, so it never reads or overwrites a real release.
"""
import json
import uuid

import boto3
import pytest
import requests

from test.itest.conftest import REGION, rmng_base_outputs

APP_ASSETS_URL = rmng_base_outputs['AppAssetsUrl'].rstrip('/')
APP_OTA_URL = rmng_base_outputs['AppOtaUrl'].rstrip('/')
APP_ASSETS_BUCKET = rmng_base_outputs['AppAssetsBucketName']

FINGERPRINT_HEADER = "expo-runtime-version"


def _get(url, **kwargs):
    return requests.get(url, timeout=15, allow_redirects=False, **kwargs)


def _fingerprint():
    return uuid.uuid4().hex + uuid.uuid4().hex[:8]


@pytest.fixture(scope="module")
def seeded():
    """A throwaway web build and two OTA manifests for one fingerprint-routed URL, removed after the module. Tag "3f9c" seeds web/itest/global/itest-3f9c/{index.html,expo/static/js/web/entry-3f9c.js} and ota/prod/itest-3f9c/ios/<fp>/latest/manifest.json, so the docstring examples' global/6.3.0 stands in for these segments."""
    s3 = boto3.client("s3", region_name=REGION)
    tag = uuid.uuid4().hex[:12]

    web_root = f"web/itest/global/itest-{tag}"
    index_html = f"<!doctype html><title>itest {tag}</title>"
    bundle_js = f"console.log('itest {tag}');"
    bundle_key = f"{web_root}/expo/static/js/web/entry-{tag}.js"

    ota_root = f"ota/prod/itest-{tag}/ios"
    fingerprints = [_fingerprint(), _fingerprint()]
    manifests = {fp: json.dumps({"id": str(uuid.uuid4()), "fingerprint": fp}) for fp in fingerprints}
    signature = f'sig="itest-{tag}", keyid="main"'

    objects = [
        (f"{web_root}/index.html", index_html, "text/html", {}),
        (bundle_key, bundle_js, "application/javascript", {}),
    ] + [
        (f"{ota_root}/{fp}/latest/manifest.json", body, "application/json", {"expo-signature": signature})
        for fp, body in manifests.items()
    ]
    for key, body, content_type, metadata in objects:
        s3.put_object(Bucket=APP_ASSETS_BUCKET, Key=key, Body=body.encode(), ContentType=content_type, Metadata=metadata)

    yield {
        "web_url": f"{APP_ASSETS_URL}/{web_root}",
        "index_html": index_html,
        "bundle_url": f"{APP_ASSETS_URL}/{bundle_key}",
        "bundle_js": bundle_js,
        "manifest_url": f"{APP_OTA_URL}/prod/itest-{tag}/ios/manifest.json",
        "manifests": manifests,
        "signature": signature,
    }

    for key, *_ in objects:
        s3.delete_object(Bucket=APP_ASSETS_BUCKET, Key=key)


# --- Root and region short paths --------------------------------------------------------------------

@pytest.mark.parametrize("path, region", [
    ("/", "global"),
    ("/index.html", "global"),
    ("/global", "global"),
    ("/global/", "global"),
    ("/cn", "cn"),
    ("/cn/", "cn"),
])
def test_short_path_redirects_to_region_latest(path, region):
    """The bare domain and /<region> land on that region's latest/ pointer, which the web deploy script keeps aimed at the newest build: GET /cn -> 302 Location: /web/prod/cn/latest/."""
    resp = _get(f"{APP_ASSETS_URL}{path}")
    assert resp.status_code == 302, f"{path} answered {resp.status_code}: {resp.text[:200]}"
    assert resp.headers.get("location") == f"/web/prod/{region}/latest/"


@pytest.mark.parametrize("path", ["/china", "/globalx", "/cn/devices"])
def test_unknown_short_path_is_not_redirected(path):
    """Only an exact region name redirects: GET /china -> 404 from S3, not a 302 to /web/prod/china/latest/."""
    resp = _get(f"{APP_ASSETS_URL}{path}")
    assert resp.status_code != 302, f"{path} redirected to {resp.headers.get('location')}"


# --- Web SPA tree -----------------------------------------------------------------------------------

@pytest.mark.parametrize("suffix", ["", "/", "/devices/abc", "/embed"])
def test_build_root_and_client_routes_serve_build_index(seeded, suffix):
    """An extension-less path under a build folder is a client-side route, so it gets that build's index.html: GET /web/prod/global/6.3.0/devices/abc -> web/prod/global/6.3.0/index.html."""
    resp = _get(f"{seeded['web_url']}{suffix}")
    assert resp.status_code == 200, f"{suffix!r} answered {resp.status_code}: {resp.text[:200]}"
    assert resp.text == seeded["index_html"]


def test_bundle_is_served_as_is(seeded):
    resp = _get(seeded["bundle_url"])
    assert resp.status_code == 200, resp.text[:200]
    assert resp.text == seeded["bundle_js"]


def test_missing_bundle_is_404_not_index(seeded):
    """A missing file must 404, since index.html with a 200 would be run as JavaScript: GET …/6.3.0/expo/static/js/web/entry-missing.js -> 404. The 404 (not 403) depends on CloudFront's s3:ListBucket grant."""
    resp = _get(f"{seeded['web_url']}/expo/static/js/web/entry-missing.js")
    assert resp.status_code == 404, f"answered {resp.status_code}: {resp.text[:200]}"
    assert resp.text != seeded["index_html"]


def test_web_responses_carry_browser_security_headers(seeded):
    """The app is embeddable in any site, so X-Frame-Options must be absent while the rest of the baseline stays."""
    headers = _get(f"{seeded['web_url']}/").headers
    assert "camera=(self)" in headers.get("permissions-policy", "")
    assert headers.get("x-content-type-options") == "nosniff"
    assert "max-age=" in headers.get("strict-transport-security", "")
    assert "x-frame-options" not in headers


# --- OTA manifests ----------------------------------------------------------------------------------

def test_manifest_without_fingerprint_is_rejected(seeded):
    """GET /ota/prod/global/ios/manifest.json with no expo-runtime-version header -> 400."""
    resp = _get(seeded["manifest_url"])
    assert resp.status_code == 400, f"answered {resp.status_code}: {resp.text[:200]}"


@pytest.mark.parametrize("fingerprint", ["../../web", "ABCDEF" * 6, "abc123", "a" * 65])
def test_manifest_with_malformed_fingerprint_is_rejected(seeded, fingerprint):
    """The fingerprint becomes part of an S3 key, so anything but 32-64 lowercase hex chars is refused: expo-runtime-version: ../../web -> 400, never a read of ota/prod/global/ios/../../web/latest/manifest.json."""
    resp = _get(seeded["manifest_url"], headers={FINGERPRINT_HEADER: fingerprint})
    assert resp.status_code == 400, f"{fingerprint!r} answered {resp.status_code}: {resp.text[:200]}"


def test_manifest_resolves_to_callers_fingerprint(seeded):
    """One flat URL serves a different manifest per fingerprint: GET /ota/prod/global/ios/manifest.json + expo-runtime-version: <fp> -> ota/prod/global/ios/<fp>/latest/manifest.json. Asking for both fingerprints proves the header is in the cache key, or the second would get the first one's build."""
    for fingerprint, manifest in seeded["manifests"].items():
        resp = _get(seeded["manifest_url"], headers={FINGERPRINT_HEADER: fingerprint})
        assert resp.status_code == 200, f"answered {resp.status_code}: {resp.text[:200]}"
        assert resp.text == manifest


def test_manifest_carries_expo_protocol_headers(seeded):
    """expo-updates treats a manifest without these as legacy, and only reads the signature under expo-signature: S3 metadata x-amz-meta-expo-signature: sig="…" -> response header expo-signature: sig="…"."""
    fingerprint = next(iter(seeded["manifests"]))
    headers = _get(seeded["manifest_url"], headers={FINGERPRINT_HEADER: fingerprint}).headers
    assert headers.get("expo-protocol-version") == "1"
    assert headers.get("expo-sfv-version") == "0"
    assert headers.get("expo-signature") == seeded["signature"]
    assert "x-amz-meta-expo-signature" not in headers
    assert "permissions-policy" not in headers


def test_unpublished_fingerprint_is_404(seeded):
    """expo-updates reads a 404 as "no update for this build"; a 403 would surface as an error on the device."""
    resp = _get(seeded["manifest_url"], headers={FINGERPRINT_HEADER: _fingerprint()})
    assert resp.status_code == 404, f"answered {resp.status_code}: {resp.text[:200]}"


def test_versioned_manifest_url_passes_through(seeded):
    """A URL that already names the fingerprint is served as-is, whatever the header says: GET /ota/prod/global/ios/<fp>/latest/manifest.json + expo-runtime-version: <other> -> the <fp> manifest."""
    fingerprint, manifest = next(iter(seeded["manifests"].items()))
    url = seeded["manifest_url"].replace("/manifest.json", f"/{fingerprint}/latest/manifest.json")
    resp = _get(url, headers={FINGERPRINT_HEADER: _fingerprint()})
    assert resp.status_code == 200, f"answered {resp.status_code}: {resp.text[:200]}"
    assert resp.text == manifest
