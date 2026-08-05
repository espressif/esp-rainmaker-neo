#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0
#
# Configure an EXISTING S3 bucket and CloudFront distribution to serve the API
# docs site layout that .gitlab-ci.yml publishes (/http/, /mqtt/, /events/).
#
# Creates no bucket and no distribution. It applies only the settings the sync_*
# CI jobs cannot set for themselves:
#
#   1. DefaultRootObject=index.html, so the bare hostname serves the landing page.
#   2. A viewer-request CloudFront Function appending index.html to directory
#      URIs. Without it a REST/OAC S3 origin returns 403 for /http/, /mqtt/node/,
#      /mqtt/user/ and /events/ — only an S3 *website* origin appends index.html
#      on its own. Created if missing, then attached to the default behaviour.
#
# Every other distribution setting (aliases, certificates, cache policies) is
# read and written back unchanged.
#
# Usage:
#   ./scripts/configure_docs_site.sh check   <bucket> <distribution-id>
#   ./scripts/configure_docs_site.sh apply   <bucket> <distribution-id>
#   ./scripts/configure_docs_site.sh verify   <distribution-id>
#
# Credentials must be for the account owning these resources, or calls will 403.
set -euo pipefail

FN_NAME="${FN_NAME:-rmng-docs-index-rewrite}"

log()  { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; }
warn() { printf '  \033[33m!\033[0m %s\n' "$*"; }
die()  { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  sed -n '20,24p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
  exit 2
}

# Rendered pages. The directory-style ones are the entries that depend on the
# function; index.html and the logo would resolve without it.
PAGES=(/ /http/ /mqtt/node/ /mqtt/user/ /events/ /logo-neo-vertical.png)

# Raw specs, served alongside their renderings for codegen and diffing. These
# are easy to break silently: they ride --exclude rules and individual s3 cp
# lines rather than the folder syncs, so a prefix change can drop them while
# every rendered page still returns 200.
RAW=(
  /http/Api_Swagger.yaml
  /http/User_Api_Swagger.yaml
  /mqtt/MQTT_Node.yaml
  /mqtt/MQTT_User.yaml
  /events/Push_User.yaml
)

FN_CODE='function handler(event) {
    var request = event.request;
    var uri = request.uri;
    if (uri.endsWith("/")) {
        request.uri = uri + "index.html";
    } else if (!uri.split("/").pop().includes(".")) {
        request.uri = uri + "/index.html";
    }
    return request;
}'

account() { aws sts get-caller-identity --query Account --output text 2>/dev/null || echo unknown; }

require_dist() {
  aws cloudfront get-distribution --id "$1" >/dev/null 2>&1 || \
    die "cannot read distribution $1 in account $(account) — wrong credentials or wrong account?"
}

report_state() { # $1=bucket $2=dist
  log "Targets"
  echo "  bucket:       s3://$1"
  echo "  distribution: $2"
  echo "  account:      $(account)"

  if aws s3api head-bucket --bucket "$1" 2>/dev/null; then ok "bucket reachable"; else warn "bucket not reachable from this account"; fi
  require_dist "$2"

  log "Distribution settings"
  local cfg root fnq origin
  cfg="$(aws cloudfront get-distribution-config --id "$2" --query 'DistributionConfig' --output json)"
  root="$(printf '%s' "$cfg"   | python3 -c 'import json,sys; print(json.load(sys.stdin).get("DefaultRootObject") or "")')"
  fnq="$(printf '%s' "$cfg"    | python3 -c 'import json,sys; b=json.load(sys.stdin)["DefaultCacheBehavior"]; print((b.get("FunctionAssociations") or {}).get("Quantity",0))')"
  origin="$(printf '%s' "$cfg" | python3 -c 'import json,sys; print(json.load(sys.stdin)["Origins"]["Items"][0]["DomainName"])')"

  [ "$root" = "index.html" ] && ok "DefaultRootObject=index.html" || warn "DefaultRootObject='${root}' (want index.html)"
  [ "$fnq" != "0" ] && ok "viewer-request function attached (${fnq})" || warn "no function attached — directory URLs will 403"
  case "$origin" in
    *s3-website*) ok "origin is an S3 website endpoint (appends index.html natively)" ;;
    *)            warn "origin '${origin}' is REST/OAC — the function is required" ;;
  esac
}

cmd_check() {
  [ $# -eq 2 ] || usage
  report_state "$1" "$2"
}

cmd_apply() {
  [ $# -eq 2 ] || usage
  local bucket="$1" dist="$2"
  report_state "$bucket" "$dist"

  log "CloudFront Function: ${FN_NAME}"
  local fn_arn tmp etag
  if fn_arn="$(aws cloudfront describe-function --name "$FN_NAME" \
        --query 'FunctionSummary.FunctionMetadata.FunctionARN' --output text 2>/dev/null)"; then
    ok "exists: ${fn_arn}"
  else
    tmp="$(mktemp -d)"; printf '%s' "$FN_CODE" > "${tmp}/fn.js"
    fn_arn="$(aws cloudfront create-function --name "$FN_NAME" \
      --function-config "Comment=append index.html to directory URIs,Runtime=cloudfront-js-2.0" \
      --function-code "fileb://${tmp}/fn.js" \
      --query 'FunctionSummary.FunctionMetadata.FunctionARN' --output text)"
    etag="$(aws cloudfront describe-function --name "$FN_NAME" --query ETag --output text)"
    aws cloudfront publish-function --name "$FN_NAME" --if-match "$etag" >/dev/null
    rm -rf "$tmp"
    ok "created and published: ${fn_arn}"
  fi

  log "Updating distribution ${dist}"
  local cur_etag cfg new_cfg
  cur_etag="$(aws cloudfront get-distribution-config --id "$dist" --query ETag --output text)"
  cfg="$(aws cloudfront get-distribution-config --id "$dist" --query 'DistributionConfig' --output json)"

  # Merge in place so nothing else in the live config is clobbered. The function
  # is only appended when the default behaviour has no viewer-request already.
  new_cfg="$(FN_ARN="$fn_arn" python3 - "$cfg" <<'PY'
import json, os, sys
cfg = json.loads(sys.argv[1])
cfg["DefaultRootObject"] = "index.html"
b = cfg["DefaultCacheBehavior"]
fa = b.get("FunctionAssociations") or {"Quantity": 0}
items = list(fa.get("Items") or [])
if not any(i.get("EventType") == "viewer-request" for i in items):
    items.append({"EventType": "viewer-request", "FunctionARN": os.environ["FN_ARN"]})
b["FunctionAssociations"] = {"Quantity": len(items), "Items": items}
print(json.dumps(cfg, sort_keys=True))
PY
)"

  if [ "$(printf '%s' "$cfg" | python3 -c 'import json,sys; print(json.dumps(json.load(sys.stdin),sort_keys=True))')" = "$new_cfg" ]; then
    ok "already configured — nothing to change"
  else
    aws cloudfront update-distribution --id "$dist" \
      --if-match "$cur_etag" --distribution-config "$new_cfg" >/dev/null
    ok "updated"
    aws cloudfront create-invalidation --distribution-id "$dist" --paths '/*' >/dev/null
    ok "invalidated /*"
    echo
    echo "  Propagation takes ~5-15 min, then: $0 verify ${dist}"
  fi
}

cmd_verify() {
  [ $# -eq 1 ] || usage
  local dist="$1" host fail=0 code
  require_dist "$dist"
  host="$(aws cloudfront get-distribution --id "$dist" --query 'Distribution.DistributionConfig.Aliases.Items[0]' --output text 2>/dev/null || true)"
  if [ -z "$host" ] || [ "$host" = "None" ]; then
    host="$(aws cloudfront get-distribution --id "$dist" --query 'Distribution.DomainName' --output text)"
  fi

  log "Rendered pages — https://${host}"
  for p in "${PAGES[@]}"; do
    code="$(curl -s -o /dev/null -w '%{http_code}' "https://${host}${p}" || echo 000)"
    if [ "$code" = "200" ]; then ok "${code}  ${p}"; else warn "${code}  ${p}"; fail=1; fi
  done

  # A 200 alone is not enough here: the index-rewrite function turns a missing
  # object into a request for <path>/index.html, which can return an HTML page
  # with a 200. Check the body really is a spec.
  log "Raw specs"
  local body first
  for p in "${RAW[@]}"; do
    code="$(curl -s -o /dev/null -w '%{http_code}' "https://${host}${p}" || echo 000)"
    if [ "$code" != "200" ]; then warn "${code}  ${p}"; fail=1; continue; fi
    body="$(curl -s --max-time 30 "https://${host}${p}" || true)"
    first="$(printf '%s' "$body" | grep -m1 -E '^(openapi|swagger|asyncapi):' || true)"
    if [ -n "$first" ]; then
      ok "${code}  ${p}  (${first})"
    elif printf '%s' "$body" | grep -qi '<html'; then
      warn "${code}  ${p}  served HTML, not YAML — spec missing behind the index rewrite"
      fail=1
    else
      warn "${code}  ${p}  no openapi/swagger/asyncapi key found"
      fail=1
    fi
  done

  [ "$fail" = "0" ] || die "see the warnings above: a missing function, unpropagated change, or a spec that never synced"
}

sub="${1:-}"; shift || true
case "$sub" in
  check)  cmd_check  "$@" ;;
  apply)  cmd_apply  "$@" ;;
  verify) cmd_verify "$@" ;;
  *) usage ;;
esac
