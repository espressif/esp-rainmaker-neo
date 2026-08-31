# SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""App assets: S3 origin + CloudFront CDN for the ESP RainMaker Home web app (Expo export).

Ported from esp-rainmaker-home's standalone `WebHostingStack` so the web app is
hosted by the same deployment that owns every other RMNG resource, instead of a
second CDK app the operator has to deploy separately.

Bucket layout served by the distribution:
    web/<env>/<region>/<version>/   one immutable folder per web build; SPA routes
                                    within it resolve to its index.html
    ota/                            Expo OTA tree (fingerprint model); this
                                    distribution serves it but does not rewrite it
"""

from aws_cdk import (
    Duration,
    Fn,
    aws_cloudfront as cloudfront,
    aws_cloudfront_origins as origins,
    aws_iam as iam,
)
from constructs import Construct

from arn_utils import get_cloudfront_distribution_arn
from app_common import (
    CommonResources,
    create_cloudfront_behavior,
    create_cloudfront_cache_policy,
    create_cloudfront_distribution,
    create_cloudfront_function,
    create_cloudfront_oac,
    create_cloudfront_response_headers_policy,
    create_s3_bucket,
    discover_cloudfront_custom_domain,
)

BUCKET_PURPOSE = "app-assets"

RESOURCE_NAME = "rmng-app-assets"

# Mirrors the app's `public/_headers`.
PERMISSIONS_POLICY_HEADER = (
    "bluetooth=(self), camera=(self), microphone=(self), geolocation=(self), "
    "clipboard-read=(self), clipboard-write=(self)"
)

# Root prefix of the web SPA within the bucket; `ota/` is the other tree and is
# left untouched by this function.
WEB_PREFIX = "web"

OTA_PREFIX = "ota"

# expo-updates rejects a manifest as legacy unless the response carries these, and
# it is not a browser, so it gets none of the browser security headers.
OTA_PROTOCOL_HEADERS = [
    ("expo-protocol-version", "1"),
    ("expo-sfv-version", "0"),
]

# The app sends its native-surface fingerprint here; it is the cache key for a
# manifest, since one flat URL resolves to a different build per fingerprint.
OTA_FINGERPRINT_HEADER = "expo-runtime-version"

# Hex-only is the part that matters: it is interpolated into an S3 key, so the
# charset is what prevents traversal. The range spans SHA-1 (40) and SHA-256 (64)
# so a change of digest does not 400 every device at once.
OTA_FINGERPRINT_PATTERN = "^[0-9a-f]{32,64}$"

# The publish job stores the RSA manifest signature as S3 object metadata, which S3
# returns under this name; expo-updates only looks at `expo-signature`.
OTA_SIGNATURE_SOURCE_HEADER = "x-amz-meta-expo-signature"
OTA_SIGNATURE_HEADER = "expo-signature"

# Resolves the flat manifest URL to the requesting app's fingerprint folder. A URI
# that already carries a fingerprint is immutable and passes through untouched, so
# this is safe on a behavior whose path pattern also matches the versioned paths.
OTA_MANIFEST_ROUTE_CODE = f"""
function handler(event) {{
  var request = event.request;

  var flat = request.uri.match(/^\\/{OTA_PREFIX}\\/prod\\/([^/]+)\\/([^/]+)\\/manifest\\.json$/);
  if (!flat) {{
    return request;
  }}

  var header = request.headers["{OTA_FINGERPRINT_HEADER}"];
  var fingerprint = header && header.value;
  if (!fingerprint || !/{OTA_FINGERPRINT_PATTERN}/.test(fingerprint)) {{
    return {{
      statusCode: 400,
      statusDescription: "Bad Request",
    }};
  }}

  request.uri = "/{OTA_PREFIX}/prod/" + flat[1] + "/" + flat[2] + "/" + fingerprint
    + "/latest/manifest.json";
  return request;
}}
""".strip()

# Renames the S3 metadata header. A response headers policy cannot do this: it only
# sets static values, and the signature differs per manifest.
OTA_SIGNATURE_HEADER_CODE = f"""
function handler(event) {{
  var headers = event.response.headers;
  var signature = headers["{OTA_SIGNATURE_SOURCE_HEADER}"];
  if (signature) {{
    headers["{OTA_SIGNATURE_HEADER}"] = {{ value: signature.value }};
    delete headers["{OTA_SIGNATURE_SOURCE_HEADER}"];
  }}
  return event.response;
}}
""".strip()

# SPA routing for /web/<env>/<region>/<version>/…: an extension-less path is a
# client-side route, so it resolves to that build's index.html. Anything with a
# file extension keeps its real key, so a missing bundle 404s instead of being
# served index.html with a 200.
WEB_SPA_REWRITE_CODE = f"""
function handler(event) {{
  var request = event.request;
  var uri = request.uri;

  // Safeguard: the /ota/* behaviors do not attach this function, but if an OTA
  // path ever reaches the default behavior it must pass through untouched.
  if (uri.startsWith("/{OTA_PREFIX}/")) {{
    return request;
  }}

  var buildMatch = uri.match(/^\\/{WEB_PREFIX}\\/([^/]+)\\/([^/]+)\\/([^/]+)(\\/.*)?$/);
  if (!buildMatch) {{
    return request;
  }}

  var buildRoot = "/{WEB_PREFIX}/" + buildMatch[1] + "/" + buildMatch[2] + "/" + buildMatch[3];
  var rest = buildMatch[4] || "";

  if (rest === "" || rest === "/") {{
    request.uri = buildRoot + "/index.html";
    return request;
  }}

  var lastSegment = rest.substring(rest.lastIndexOf("/") + 1);
  if (lastSegment.indexOf(".") === -1) {{
    request.uri = buildRoot + "/index.html";
  }}
  return request;
}}
""".strip()


class AppAssets(Construct):
    """Private S3 bucket + CloudFront distribution serving the Expo web export.

    Exposes `bucket`, `distribution`, `url` and `ota_url` for the owning stack to publish as outputs, which the web deploy script reads to upload and invalidate.
    """

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        self.bucket = create_s3_bucket(self, "AppAssetsBucket", common_resources, BUCKET_PURPOSE)

        app_assets_oac = create_cloudfront_oac(
            self, "AppAssetsS3OAC", name=f"{RESOURCE_NAME}-oac")

        s3_origin = origins.S3BucketOrigin.with_origin_access_control(
            bucket=self.bucket,
            origin_access_control=app_assets_oac,
        )

        response_headers_policy = create_cloudfront_response_headers_policy(
            self, "AppAssetsResponseHeaders",
            name=f"{RESOURCE_NAME}-headers",
            comment="Permissions-Policy and baseline security headers for the RMNG app assets",
            custom_headers=[("Permissions-Policy", PERMISSIONS_POLICY_HEADER)],
        )

        # security_headers_behavior=None: HSTS and friends are meaningless to the
        # expo-updates client and Permissions-Policy would be noise on a manifest.
        ota_response_headers_policy = create_cloudfront_response_headers_policy(
            self, "AppAssetsOtaResponseHeaders",
            name=f"{RESOURCE_NAME}-ota-headers",
            comment="expo-updates protocol headers for the RMNG OTA tree",
            custom_headers=OTA_PROTOCOL_HEADERS,
            security_headers_behavior=None,
        )

        spa_rewrite_function = create_cloudfront_function(
            self, "AppAssetsSpaRewriteFunction",
            name=f"{RESOURCE_NAME}-spa-rewrite",
            code=WEB_SPA_REWRITE_CODE,
            comment="Map SPA routes under /web/<env>/<region>/<version>/ to that build's index.html",
        )

        spa_function_association = cloudfront.FunctionAssociation(
            function=spa_rewrite_function,
            event_type=cloudfront.FunctionEventType.VIEWER_REQUEST,
        )

        ota_manifest_functions = [
            cloudfront.FunctionAssociation(
                function=create_cloudfront_function(
                    self, "AppAssetsOtaManifestRouteFunction",
                    name=f"{RESOURCE_NAME}-ota-manifest-route",
                    code=OTA_MANIFEST_ROUTE_CODE,
                    comment="Resolve the flat OTA manifest URL to the caller's fingerprint folder",
                ),
                event_type=cloudfront.FunctionEventType.VIEWER_REQUEST,
            ),
            cloudfront.FunctionAssociation(
                function=create_cloudfront_function(
                    self, "AppAssetsOtaSignatureFunction",
                    name=f"{RESOURCE_NAME}-ota-signature",
                    code=OTA_SIGNATURE_HEADER_CODE,
                    comment="Expose the S3 signature metadata as expo-signature",
                ),
                event_type=cloudfront.FunctionEventType.VIEWER_RESPONSE,
            ),
        ]

        # One flat URL serves a different manifest per fingerprint, so the header
        # must be in the cache key or a device gets a build for another native
        # surface. TTLs are short because latest/ is a mutable pointer.
        ota_manifest_cache_policy = create_cloudfront_cache_policy(
            self, "AppAssetsOtaManifestCachePolicy",
            name=f"{RESOURCE_NAME}-ota-manifest",
            comment="Fingerprint-routed OTA manifests",
            min_ttl=Duration.seconds(0),
            default_ttl=Duration.seconds(60),
            max_ttl=Duration.seconds(3600),
            header_behavior=cloudfront.CacheHeaderBehavior.allow_list(OTA_FINGERPRINT_HEADER),
            cookie_behavior=cloudfront.CacheCookieBehavior.none(),
            query_string_behavior=cloudfront.CacheQueryStringBehavior.none(),
            enable_accept_encoding_gzip=True,
            enable_accept_encoding_brotli=True,
        )

        # Managed policies: a custom no-cache policy cannot enable gzip/brotli negotiation.
        # Entry HTML and the service worker: a cached one outlives the bundle it names.
        no_cache_policy = cloudfront.CachePolicy.CACHING_DISABLED
        # Content-hashed bundles, immutable by construction.
        immutable_asset_cache_policy = cloudfront.CachePolicy.CACHING_OPTIMIZED

        def web_behavior(cache_policy) -> cloudfront.BehaviorOptions:
            return create_cloudfront_behavior(
                self, origin=s3_origin, cache_policy=cache_policy,
                response_headers_policy=response_headers_policy,
                function_associations=[spa_function_association],
            )

        def ota_behavior(cache_policy, functions=None) -> cloudfront.BehaviorOptions:
            return create_cloudfront_behavior(
                self, origin=s3_origin, cache_policy=cache_policy,
                response_headers_policy=ota_response_headers_policy,
                function_associations=functions,
            )

        self.distribution = create_cloudfront_distribution(
            self, "AppAssetsDistribution",
            distribution_name=RESOURCE_NAME,
            default_behavior=web_behavior(no_cache_policy),
            default_root_object="index.html",
            comment="ESP RainMaker Home app assets",
            # PRICE_CLASS_100 would be cheaper per GB but serves Asia-Pacific from
            # US/Europe edges, and this is the consumer-facing app.
            price_class=cloudfront.PriceClass.PRICE_CLASS_ALL,
            additional_behaviors={
                # Expo exports hashed bundles to _expo/static/ and assets/ inside each
                # build folder, so the patterns wildcard over <env>/<region>/<version>.
                f"/{WEB_PREFIX}/*/_expo/*": web_behavior(immutable_asset_cache_policy),
                f"/{WEB_PREFIX}/*/assets/*": web_behavior(immutable_asset_cache_policy),
                f"/{WEB_PREFIX}/*/index.html": web_behavior(no_cache_policy),
                f"/{WEB_PREFIX}/*/firebase-messaging-sw.js": web_behavior(no_cache_policy),
                # Ordered before /ota/*: CloudFront takes the first pattern that
                # matches. A CloudFront `*` spans "/", so this also matches the
                # versioned manifests — the routing function leaves those alone.
                f"/{OTA_PREFIX}/prod/*/*/manifest.json": ota_behavior(
                    ota_manifest_cache_policy, functions=ota_manifest_functions),
                # Mutable pointers outside the fingerprint tree, so never cached.
                f"/{OTA_PREFIX}/version-manifest.json": ota_behavior(no_cache_policy),
                f"/{OTA_PREFIX}/fingerprint-registry.json": ota_behavior(no_cache_policy),
                # Everything else under ota/ is written once under <versionCode>/.
                f"/{OTA_PREFIX}/*": ota_behavior(immutable_asset_cache_policy),
            },
        )

        # CloudFront's OAC grant is s3:GetObject only, so S3 answers a missing key
        # with 403 AccessDenied — expo-updates needs a real 404 for an unpublished
        # manifest. ListBucket lets S3 tell "absent" from "forbidden".
        self.bucket.add_to_resource_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["s3:ListBucket"],
            resources=[self.bucket.bucket_arn],
            principals=[iam.ServicePrincipal("cloudfront.amazonaws.com")],
            conditions={"StringEquals": {
                "AWS:SourceArn": get_cloudfront_distribution_arn(self.distribution.distribution_id),
            }},
        ))

        # An alias attached out of band wins; the CloudFront hostname is the fallback.
        _, host = discover_cloudfront_custom_domain(
            self, "AppAssetsCustomDomain",
            name="app-assets-custom-domain",
            distribution_id=self.distribution.distribution_id,
            default_host=self.distribution.distribution_domain_name,
        )
        self.url = Fn.join("", ["https://", host])
        self.ota_url = Fn.join("", ["https://", host, f"/{OTA_PREFIX}"])
