#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
Enable public access on an existing S3 bucket (ACLs disabled) and upload
folders/objects with public read access via bucket policy.

Usage:
    python s3_public_upload.py --bucket my-bucket --folder ./local-folder [--prefix remote/prefix/]
    python s3_public_upload.py --bucket my-bucket --folder ./folder1 --folder ./folder2
    python s3_public_upload.py --bucket my-bucket --folder ./bins --prefix firmware_binaries --generate-index
"""

import argparse
import json
import mimetypes
import os
import sys
from collections import defaultdict

import boto3
from botocore.exceptions import ClientError


def enable_public_access(s3_client, bucket_name: str):
    """Remove the public access block so the bucket policy can grant public read."""
    print(f"[1/3] Disabling public access block on '{bucket_name}'...")
    s3_client.put_public_access_block(
        Bucket=bucket_name,
        PublicAccessBlockConfiguration={
            "BlockPublicAcls": False,
            "IgnorePublicAcls": False,
            "BlockPublicPolicy": False,
            "RestrictPublicBuckets": False,
        },
    )
    print("      Done.")


def apply_public_read_policy(s3_client, bucket_name: str):
    """Attach a bucket policy that grants s3:ListBucket and s3:GetObject to everyone."""
    print(f"[2/3] Applying public-read bucket policy on '{bucket_name}'...")
    policy = {
        "Version": "2012-10-17",
        "Statement": [
            {
                "Sid": "PublicListBucket",
                "Effect": "Allow",
                "Principal": "*",
                "Action": "s3:ListBucket",
                "Resource": f"arn:aws:s3:::{bucket_name}",
            },
            {
                "Sid": "PublicReadGetObject",
                "Effect": "Allow",
                "Principal": "*",
                "Action": "s3:GetObject",
                "Resource": f"arn:aws:s3:::{bucket_name}/*",
            },
        ],
    }
    s3_client.put_bucket_policy(
        Bucket=bucket_name,
        Policy=json.dumps(policy),
    )
    print("      Done.")


def upload_folder(s3_client, bucket_name: str, local_folder: str, prefix: str = ""):
    """Upload all files in local_folder to bucket_name under the given prefix.

    Contents of local_folder are uploaded directly under prefix — the folder
    name itself is not included in the S3 key.
    e.g. local_folder=./bins, prefix=firmware_binaries
         bins/E4A61623/fw.bin  ->  firmware_binaries/E4A61623/fw.bin
    """
    local_folder = os.path.abspath(local_folder)
    if not os.path.isdir(local_folder):
        print(f"ERROR: '{local_folder}' is not a directory.", file=sys.stderr)
        sys.exit(1)

    uploaded_keys = []

    for root, _, files in os.walk(local_folder):
        for filename in files:
            if filename == ".DS_Store":
                continue
            local_path = os.path.join(root, filename)
            relative_path = os.path.relpath(local_path, local_folder)
            parts = [p for p in [prefix.strip("/"), relative_path] if p]
            s3_key = "/".join(parts)

            content_type, _ = mimetypes.guess_type(local_path)
            extra_args = {}
            if content_type:
                extra_args["ContentType"] = content_type

            print(f"      Uploading {local_path} -> s3://{bucket_name}/{s3_key}")
            s3_client.upload_file(local_path, bucket_name, s3_key, ExtraArgs=extra_args)
            uploaded_keys.append(s3_key)

    return uploaded_keys


def generate_index(s3_client, bucket_name: str, prefix: str, all_keys: list, region: str):
    """Generate and upload an index.html for each folder level under prefix."""
    base_url = f"https://{bucket_name}.s3.{region}.amazonaws.com"

    # Group keys by their immediate parent folder
    # e.g. firmware_binaries/E4A61623/fw.bin -> parent = firmware_binaries/E4A61623
    folders = defaultdict(list)
    for key in all_keys:
        parent = key.rsplit("/", 1)[0] if "/" in key else ""
        folders[parent].append(key)

    # Also collect unique sub-prefixes per folder for navigation
    subfolders = defaultdict(set)
    for key in all_keys:
        parts = key.split("/")
        for depth in range(1, len(parts)):
            parent = "/".join(parts[:depth])
            child = "/".join(parts[:depth + 1])
            if child != key:
                subfolders[parent].add(child)

    # Generate index.html for each unique folder
    all_folders = set(folders.keys()) | set(subfolders.keys())
    # Also generate root-level index at prefix
    all_folders.add(prefix.strip("/"))

    for folder in sorted(all_folders):
        folder_files = folders.get(folder, [])
        folder_subs = sorted(subfolders.get(folder, []))
        _upload_index_html(s3_client, bucket_name, base_url, folder, folder_subs, folder_files)

    root_index_url = f"{base_url}/{prefix.strip('/')}/index.html"
    print(f"\n      Index page: {root_index_url}")


def _upload_index_html(s3_client, bucket_name, base_url, folder, subfolders, files):
    """Build and upload a single index.html for a folder."""
    title = folder or bucket_name
    rows = []

    for sub in subfolders:
        name = sub.split("/")[-1] + "/"
        link = f"{base_url}/{sub}/index.html"
        rows.append(f'<tr><td><a href="{link}">📁 {name}</a></td><td>—</td></tr>')

    for key in sorted(files):
        name = key.split("/")[-1]
        link = f"{base_url}/{key}"
        rows.append(f'<tr><td><a href="{link}">📄 {name}</a></td><td><a href="{link}">Download</a></td></tr>')

    rows_html = "\n".join(rows) if rows else "<tr><td colspan='2'>No files</td></tr>"

    # Build breadcrumb
    parts = folder.split("/") if folder else []
    breadcrumb = '<a href="#">root</a>'
    for i, part in enumerate(parts):
        path = "/".join(parts[:i + 1])
        breadcrumb += f' / <a href="{base_url}/{path}/index.html">{part}</a>'

    html = f"""<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>{title}</title>
  <style>
    body {{ font-family: sans-serif; max-width: 900px; margin: 40px auto; padding: 0 20px; }}
    h1 {{ font-size: 1.4em; color: #333; }}
    .breadcrumb {{ font-size: 0.9em; color: #666; margin-bottom: 16px; }}
    .breadcrumb a {{ color: #0066cc; text-decoration: none; }}
    table {{ width: 100%; border-collapse: collapse; }}
    th {{ text-align: left; border-bottom: 2px solid #ddd; padding: 8px; background: #f5f5f5; }}
    td {{ padding: 8px; border-bottom: 1px solid #eee; }}
    a {{ color: #0066cc; text-decoration: none; }}
    a:hover {{ text-decoration: underline; }}
  </style>
</head>
<body>
  <div class="breadcrumb">{breadcrumb}</div>
  <h1>/{title}</h1>
  <table>
    <thead><tr><th>Name</th><th>Link</th></tr></thead>
    <tbody>
{rows_html}
    </tbody>
  </table>
</body>
</html>"""

    index_key = f"{folder}/index.html" if folder else "index.html"
    s3_client.put_object(
        Bucket=bucket_name,
        Key=index_key,
        Body=html.encode("utf-8"),
        ContentType="text/html",
    )
    print(f"      Index uploaded: s3://{bucket_name}/{index_key}")


def main():
    parser = argparse.ArgumentParser(
        description="Enable public access on an S3 bucket and upload folders."
    )
    parser.add_argument("--bucket", required=True, help="Target S3 bucket name")
    parser.add_argument(
        "--folder",
        action="append",
        dest="folders",
        required=True,
        metavar="PATH",
        help="Local folder to upload (can be specified multiple times)",
    )
    parser.add_argument(
        "--prefix",
        default="",
        help="Optional S3 key prefix (e.g. 'firmware_binaries')",
    )
    parser.add_argument(
        "--region",
        default="ap-south-1",
        help="AWS region (default: ap-south-1)",
    )
    parser.add_argument(
        "--profile",
        default=None,
        help="AWS CLI profile name",
    )
    parser.add_argument(
        "--create-bucket",
        action="store_true",
        help="Create the bucket if it does not exist",
    )
    parser.add_argument(
        "--skip-policy",
        action="store_true",
        help="Skip public access block + bucket policy steps (upload only)",
    )
    parser.add_argument(
        "--generate-index",
        action="store_true",
        help="Generate and upload index.html pages for browsing in a browser",
    )
    args = parser.parse_args()

    session = boto3.Session(profile_name=args.profile, region_name=args.region)
    s3_client = session.client("s3")

    # Verify bucket exists, optionally create it
    try:
        s3_client.head_bucket(Bucket=args.bucket)
    except ClientError as e:
        code = e.response["Error"]["Code"]
        if code == "404" and args.create_bucket:
            print(f"Bucket '{args.bucket}' not found. Creating...")
            create_params = {"Bucket": args.bucket}
            if args.region != "us-east-1":
                create_params["CreateBucketConfiguration"] = {
                    "LocationConstraint": args.region
                }
            s3_client.create_bucket(**create_params)
            print(f"      Bucket '{args.bucket}' created in {args.region}.")
        else:
            print(f"ERROR: Cannot access bucket '{args.bucket}' (code={code}).", file=sys.stderr)
            if code == "404":
                print("Hint: Use --create-bucket to create it automatically.", file=sys.stderr)
            sys.exit(1)

    if not args.skip_policy:
        enable_public_access(s3_client, args.bucket)
        apply_public_read_policy(s3_client, args.bucket)

    print(f"[3/3] Uploading {len(args.folders)} folder(s)...")
    all_keys = []
    for folder in args.folders:
        keys = upload_folder(s3_client, args.bucket, folder, prefix=args.prefix)
        all_keys.extend(keys)
        print(f"      '{folder}': {len(keys)} file(s) uploaded.")

    if args.generate_index:
        print("\n[4/4] Generating index pages...")
        generate_index(s3_client, args.bucket, args.prefix, all_keys, args.region)

    print(f"\nDone. {len(all_keys)} file(s) uploaded to s3://{args.bucket}/")
    base_url = f"https://{args.bucket}.s3.{args.region}.amazonaws.com"
    if args.generate_index and args.prefix:
        print(f"Browse: {base_url}/{args.prefix.strip('/')}/index.html")
    else:
        print(f"Public URL pattern: {base_url}/<key>")


if __name__ == "__main__":
    main()


# python tools/s3_public_upload.py \
#   --bucket rmng-firmware-staging \
#   --prefix firmware_binaries \
#   --folder bins \
#   --generate-index