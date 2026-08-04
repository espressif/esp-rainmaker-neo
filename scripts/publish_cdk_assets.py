#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import argparse
import json
import os
import re
import sys
import logging
import shutil
from pathlib import Path
from collections import defaultdict

import boto3
from botocore.exceptions import ClientError
from concurrent.futures import ThreadPoolExecutor, as_completed

# -----------------------------
# Constants
# -----------------------------
DEFAULT_VERSION       = "1.0.0"                     # Default version of stack
DEFAULT_BUCKET_PREFIX = "rmng-publishing-bucket"    # Default bucket prefix
DEFAULT_REGION        = "us-east-1"                 # Default AWS region

# Regex to match CDK toolkit bucket patterns in Fn::Sub strings
# e.g. "cdk-rmng-assets-${AWS::AccountId}-${AWS::Region}"
CDK_BUCKET_PATTERN = re.compile(
    r'cdk-\w+-assets-\$\{AWS::AccountId\}-\$\{AWS::Region\}'
)

# -----------------------------
# Logging
# -----------------------------
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s"
)
log = logging.getLogger(__name__)

# -----------------------------
# CLI Args
# -----------------------------
def parse_args():
    parser = argparse.ArgumentParser(description="Publish CDK assets + templates")

    parser.add_argument("--stack", required=False, default=None)
    parser.add_argument("--version", default=DEFAULT_VERSION)
    parser.add_argument("--bucket-prefix", default=DEFAULT_BUCKET_PREFIX)
    parser.add_argument(
        "--guard-only",
        action="store_true",
        help="Only check whether {version}/ already exists in the target bucket; exit 2 if it does. Uploads nothing, creates nothing.",
    )
    parser.add_argument(
        "--sequential",
        action="store_true",
        help="Upload assets and templates one at a time (no parallel S3 uploads)",
    )

    args = parser.parse_args()
    if not args.guard_only and not args.stack:
        parser.error("--stack is required unless --guard-only")
    return args

# -----------------------------
# Get AWS Account ID
# -----------------------------
def get_user_account_id(session):
    sts = session.client('sts')
    identity = sts.get_caller_identity()
    return identity['Account']

# -----------------------------
# Ensure Bucket Exists
# -----------------------------
def ensure_bucket(s3, bucket_name, region):
    try:
        s3.head_bucket(Bucket=bucket_name)
        log.info(f"Bucket exists: {bucket_name}")
    except ClientError:
        log.warning(f"Bucket does not exist. Creating: {bucket_name}")

        create_kwargs = {"Bucket": bucket_name}

        if region != "us-east-1":
            create_kwargs["CreateBucketConfiguration"] = {
                "LocationConstraint": region
            }

        s3.create_bucket(**create_kwargs)
        log.info(f"Bucket created: {bucket_name}")


# -----------------------------
# Version-Exists Guard
# -----------------------------
def version_exists(s3, bucket, version):
    """True when any object already lives under '{version}/'; a missing bucket counts as absent so first-ever publishes pass the guard."""
    try:
        resp = s3.list_objects_v2(Bucket=bucket, Prefix=f"{version}/", MaxKeys=1)
    except ClientError as exc:
        if exc.response.get("Error", {}).get("Code") in ("NoSuchBucket", "404"):
            return False
        log.error(f"Version guard could not list s3://{bucket}: {exc}")
        raise
    return resp.get("KeyCount", 0) > 0


# -----------------------------
# Upload Helper
# -----------------------------
def upload_file(s3, bucket, local_path, s3_key):
    log.info(f"Uploading {local_path} → s3://{bucket}/{s3_key}")
    s3.upload_file(str(local_path), bucket, s3_key)


# -----------------------------
# List Existing S3 Keys
# -----------------------------
def list_existing_keys(s3, bucket, prefix):
    existing = set()
    paginator = s3.get_paginator("list_objects_v2")
    for page in paginator.paginate(Bucket=bucket, Prefix=prefix):
        for obj in page.get("Contents", []):
            existing.add(obj["Key"])
    return existing


# -----------------------------
# Rewrite Templates Safely
# -----------------------------
def rewrite_template(template_path, asset_key_map, bucket):
    """Rewrite CDK template to use publishing bucket instead of CDK toolkit bucket.

    Handles three rewrite patterns:
    1. S3Key values that appear in asset_key_map (old_key → new_key)
    2. S3Bucket siblings of rewritten S3Keys (CDK Fn::Sub → bucket string)
    3. ANY remaining Fn::Sub values matching cdk-*-assets-${AWS::AccountId}-${AWS::Region}
       (catches IAM policies, ECS commands, etc.)
    4. Bare asset keys (hash.zip) embedded in strings → versioned key
    """
    with open(template_path) as f:
        template = json.load(f)

    # --- Strip out the CDK Bootstrap requirements ---
    if "Parameters" in template and "BootstrapVersion" in template["Parameters"]:
        del template["Parameters"]["BootstrapVersion"]
        
    if "Rules" in template and "CheckBootstrapVersion" in template["Rules"]:
        del template["Rules"]["CheckBootstrapVersion"]
    # ------------------------------------------------

    cdk_bucket_rewrites = 0

    def rewrite_obj(obj):
        nonlocal cdk_bucket_rewrites

        if isinstance(obj, dict):
            # Pass 1: rewrite S3Key values
            for k, v in obj.items():
                if k == "S3Key" and v in asset_key_map:
                    obj[k] = asset_key_map[v]

            # Pass 2: rewrite S3Bucket if its sibling S3Key was rewritten
            # S3Bucket may be a plain string OR a dict like {"Fn::Sub": "cdk-...-assets-..."}
            if "S3Bucket" in obj and isinstance(obj.get("S3Key"), str):
                if obj["S3Key"] in asset_key_map.values():
                    obj["S3Bucket"] = bucket

            # Pass 3: rewrite any Fn::Sub CDK toolkit bucket references
            # These appear in IAM policies, ECS commands, etc.
            if "Fn::Sub" in obj and isinstance(obj["Fn::Sub"], str):
                original = obj["Fn::Sub"]
                replaced = CDK_BUCKET_PATTERN.sub(bucket, original)
                if replaced != original:
                    # If the entire Fn::Sub was just the bucket name, simplify to plain string
                    if replaced == bucket:
                        # Replace the parent dict content: remove Fn::Sub, can't do in-place
                        # We'll handle this at the parent level instead
                        pass
                    obj["Fn::Sub"] = replaced
                    cdk_bucket_rewrites += 1

            # Pass 4: rewrite bare asset keys in Fn::Join string segments
            # e.g. "/fadc296...zip" in ECS commands
            if "Fn::Join" in obj and isinstance(obj["Fn::Join"], list):
                join_parts = obj["Fn::Join"]
                if len(join_parts) == 2 and isinstance(join_parts[1], list):
                    for i, part in enumerate(join_parts[1]):
                        if isinstance(part, str):
                            for old_key, new_key in asset_key_map.items():
                                if old_key in part:
                                    join_parts[1][i] = part.replace(old_key, new_key)

            # Recurse into child values
            for k, v in obj.items():
                rewrite_obj(v)

        elif isinstance(obj, list):
            for item in obj:
                rewrite_obj(item)

    rewrite_obj(template)

    if cdk_bucket_rewrites > 0:
        log.info(f"  Rewrote {cdk_bucket_rewrites} additional CDK toolkit bucket reference(s)")

    return template

# -----------------------------
# Upload Assets (Parallel)
# -----------------------------
def upload_assets(
    s3,
    bucket,
    assembly_dir,
    files_assets,
    version,
    max_workers=8
):
    asset_key_map = {}
    upload_tasks = []

    if not files_assets:
        log.warning("No file assets to upload")
        return asset_key_map

    existing_keys = list_existing_keys(s3, bucket, f"{version}/assets/")

    for asset_id, asset_data in files_assets.items():

        source = asset_data.get("source", {})
        source_path = source.get("path")

        destinations = asset_data.get("destinations", {})

        if not source_path or not destinations:
            continue

        # CDK uses account-region key
        dest = list(destinations.values())[0]

        old_key = dest.get("objectKey")

        if not old_key:
            continue

        local_asset_path = assembly_dir / source_path

        if not local_asset_path.exists():
            log.warning(f"Missing asset: {local_asset_path}")
            continue

        if local_asset_path.is_dir():
            zip_base = str(local_asset_path)
            log.info(f"Zipping directory asset: {local_asset_path} -> {zip_base}.zip")
            shutil.make_archive(zip_base, 'zip', local_asset_path)
            local_asset_path = Path(f"{zip_base}.zip")

        filename = os.path.basename(old_key)
        new_key = f"{version}/assets/{filename}"

        if new_key in existing_keys:
            log.info(f"Skipping (already in S3): {new_key}")
            asset_key_map[old_key] = new_key
            continue

        upload_tasks.append(
            (local_asset_path, old_key, new_key)
        )

    skipped = len(asset_key_map)
    total = skipped + len(upload_tasks)
    log.info(f"Assets: {total} total, {skipped} already in S3, {len(upload_tasks)} to upload")

    with ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = {
            executor.submit(
                upload_file,
                s3,
                bucket,
                task[0],
                task[2]
            ): task
            for task in upload_tasks
        }

        for future in as_completed(futures):
            local_path, old_key, new_key = futures[future]

            try:
                future.result()
                asset_key_map[old_key] = new_key
            except Exception as e:
                log.error(f"Failed upload: {local_path} → {e}")
                raise

    log.info(f"Uploaded {len(upload_tasks)} assets ({skipped} skipped)")
    return asset_key_map

# -----------------------------
# Upload Templates (Parallel)
# -----------------------------
def upload_templates(
    s3,
    bucket,
    assembly_dir,
    version,
    asset_key_map,
    max_workers=4
):
    # Only pick original templates, not the ones we've already modified
    template_files = [
        f for f in assembly_dir.glob("*.template.json")
        if not f.name.startswith("modified-")
    ]

    if not template_files:
        log.warning("No templates found")
        return

    existing_keys = list_existing_keys(s3, bucket, f"{version}/templates/")
    upload_tasks = []
    skipped = 0

    for template_file in template_files:
        log.info(f"Rewriting template: {template_file.name}")

        rewritten = rewrite_template(
            template_file,
            asset_key_map,
            bucket
        )

        rewritten_bytes = json.dumps(rewritten, indent=2).encode("utf-8")
        s3_key = f"{version}/templates/{template_file.name}"

        if s3_key in existing_keys:
            existing_obj = s3.get_object(Bucket=bucket, Key=s3_key)
            existing_bytes = existing_obj["Body"].read()
            if existing_bytes == rewritten_bytes:
                log.info(f"Skipping (unchanged): {s3_key}")
                skipped += 1
                continue

        with open(template_file, "wb") as f:
            f.write(rewritten_bytes)

        upload_tasks.append((template_file, s3_key))

    log.info(f"Templates: {len(template_files)} total, {skipped} unchanged, {len(upload_tasks)} to upload")

    with ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = {
            executor.submit(
                upload_file,
                s3,
                bucket,
                task[0],
                task[1]
            ): task
            for task in upload_tasks
        }

        for future in as_completed(futures):
            template_path, s3_key = futures[future]

            try:
                future.result()
            except Exception as e:
                log.error(f"Failed template upload: {template_path} → {e}")
                raise

    log.info(f"Uploaded {len(upload_tasks)} templates ({skipped} skipped)")


# -----------------------------
# Print S3 Folder Structure
# -----------------------------
def print_s3_summary(s3, bucket, version):
    prefix = f"{version}/"

    paginator = s3.get_paginator("list_objects_v2")
    pages = paginator.paginate(Bucket=bucket, Prefix=prefix)

    objects = []

    for page in pages:
        for obj in page.get("Contents", []):
            objects.append(obj["Key"])

    if not objects:
        log.warning("No objects found in published version.")
        return

    log.info("\nS3 Folder Structure Summary:\n")

    tree = defaultdict(list)

    for key in objects:
        relative = key[len(prefix):]
        parts = relative.split("/", 1)

        if len(parts) == 1:
            tree["root"].append(parts[0])
        else:
            tree[parts[0]].append(parts[1])

    print(f"s3://{bucket}/{version}/")

    for folder in sorted(tree.keys()):
        if folder == "root":
            for file in sorted(tree["root"]):
                print(f"  ├── {file}")
            continue

        print(f"  ├── {folder}/")

        for file in sorted(tree[folder]):
            print(f"  │   ├── {file}")

    print()

# -----------------------------
# Collect File Assets
# -----------------------------
def collect_file_assets(manifest, assembly_dir):
    all_files_assets = manifest.get("files", {})
    artifacts = manifest.get("artifacts", {})

    for art_id, art_data in artifacts.items():
        if art_data.get("type") == "cdk:asset-manifest":
            asset_manifest_file = art_data.get("properties", {}).get("file")
            if asset_manifest_file:
                asset_manifest_path = assembly_dir / asset_manifest_file
                if asset_manifest_path.exists():
                    log.info(f"Loading asset manifest: {asset_manifest_file}")
                    with open(asset_manifest_path) as f:
                        am = json.load(f)
                        all_files_assets.update(am.get("files", {}))
                else:
                    log.warning(f"Asset manifest not found: {asset_manifest_path}")

    return all_files_assets


# -----------------------------
# Main
# -----------------------------
def main():
    args = parse_args()

    stack = args.stack
    version = args.version
    bucket_prefix = args.bucket_prefix
    region = os.environ.get("AWS_REGION") or os.environ.get("AWS_DEFAULT_REGION") or DEFAULT_REGION

    # Legacy fail-fast: check the assembly dir before touching AWS at all, so broken
    # creds never mask a plain "wrong folder" mistake. Skipped for --guard-only, which
    # never needs cdk.out.* to exist.
    if not args.guard_only:
        assembly_dir = Path(f"cdk.out.{stack}")

        if not assembly_dir.exists():
            log.error(f"Assembly folder not found: {assembly_dir}")
            sys.exit(1)

    session = boto3.Session(region_name=region)

    # Full-name override (Jenkins prod publish exports APPLICATION_PUBLISHER_BUCKET); unset/empty keeps the legacy prefix-account-region derivation so existing flows are untouched.
    bucket_override = os.environ.get("APPLICATION_PUBLISHER_BUCKET")
    if bucket_override:
        bucket = bucket_override
        log.info(f"Using APPLICATION_PUBLISHER_BUCKET override: {bucket}")
    else:
        account_id = get_user_account_id(session)
        bucket = f"{bucket_prefix}-{account_id}-{region}"

    s3 = session.client("s3")

    # Pre-flight guard mode: the pipeline runs this ONCE before `make publish` because the per-group publish invocations all share the same {version}/ prefix. Never creates buckets.
    if args.guard_only:
        if version_exists(s3, bucket, version):
            log.error(
                f"Version {version} already exists at s3://{bucket}/{version}/ — bump VERSION or rerun with ALLOW_VERSION_OVERWRITE."
            )
            sys.exit(2)
        log.info(f"Version {version} not present in s3://{bucket} — safe to publish.")
        return

    ensure_bucket(s3, bucket, region)

    manifest_path = assembly_dir / "manifest.json"
    if not manifest_path.exists():
        log.error("manifest.json not found")
        sys.exit(1)

    with open(manifest_path) as f:
        manifest = json.load(f)

    # Collect all file assets.
    all_files_assets = collect_file_assets(manifest, assembly_dir)

    if args.sequential:
        log.info("Sequential upload mode (no parallel S3 uploads)")
    asset_workers = 1 if args.sequential else 8
    template_workers = 1 if args.sequential else 4

    asset_key_map = upload_assets(
        s3=s3,
        bucket=bucket,
        assembly_dir=assembly_dir,
        files_assets=all_files_assets,
        version=version,
        max_workers=asset_workers,
    )

    upload_templates(
        s3=s3,
        bucket=bucket,
        assembly_dir=assembly_dir,
        version=version,
        asset_key_map=asset_key_map,
        max_workers=template_workers,
    )

    log.info("Publishing complete")

    print_s3_summary(s3, bucket, version)


if __name__ == "__main__":
    main()