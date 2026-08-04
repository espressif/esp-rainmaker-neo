# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0


"""
Upload RMNG-Outputs and Swagger specs to S3
This script uploads the rmng-outputs.json file and docs/api/*.yaml
files to a public S3 bucket.

Usage
    AWS_REGION=us-east-1 python3 upload_rmng_outputs.py

Uses AWS_REGION from the environment (default us-east-1 if unset).
"""

import boto3
import glob
import io
import os
import sys
import json
import copy
import segno
from botocore.exceptions import ClientError

RMNG_OUTPUTS        = "rmng-outputs.json"
SWAGGER_DIR          = "docs/api"
PUBLIC_BUCKET_NAME_PREFIX = "rmng-public-assets"
PUBLIC_BUCKET_REGION  = "us-east-1"

SENSITIVE_PATHS = [
    ["espuser-base", "EspVaClientSecret"],             # espuser-base > EspVaClientSecret
    ["rmng-base", "VAClientSecret"],                   # rmng-base > VAClientSecret
    ["espuser-core", "AdminTempPassword"],             # shared admin bootstrap password (operator-only)
    ["espuser-core", "AdminUserRegistrationResults"]   # names the registered admin emails (operator-only)
]

def get_user_account_id(session):
    sts = session.client('sts')
    identity = sts.get_caller_identity()
    return identity['Account']

def get_bucket_name(account_id):
    return f"{PUBLIC_BUCKET_NAME_PREFIX}-{account_id}"

def _public_read_policy_document(bucket_name):
    policy = {
        "Version": "2012-10-17",
        "Statement": [
            {
                "Sid": "PublicReadGetObject",
                "Effect": "Allow",
                "Principal": "*",
                "Action": "s3:GetObject",
                "Resource": f"arn:aws:s3:::{bucket_name}/*",
            }
        ],
    }
    return json.dumps(policy)

def _s3_bucket_client(session):
    """Bucket always lives in PUBLIC_BUCKET_REGION (see get_bucket_name)."""
    return session.client("s3", region_name=PUBLIC_BUCKET_REGION)

def ensure_public_read_access(session, bucket_name):
    """
    Allow anonymous GetObject via bucket policy. Clears bucket-level Block Public Access
    so the policy can take effect (required for new buckets).
    """
    s3 = _s3_bucket_client(session)
    try:
        s3.put_public_access_block(
            Bucket=bucket_name,
            PublicAccessBlockConfiguration={
                "BlockPublicAcls": False,
                "IgnorePublicAcls": False,
                "BlockPublicPolicy": False,
                "RestrictPublicBuckets": False,
            },
        )
        s3.put_bucket_policy(
            Bucket=bucket_name,
            Policy=_public_read_policy_document(bucket_name),
        )
        return True
    except ClientError as e:
        print(f"[ERROR] Failed to configure public read policy on {bucket_name}: {e}")
        return False

def ensure_bucket_cors(session, bucket_name):
    """
    Allow browsers to fetch the published outputs cross-origin. Public-read grants access
    but the browser still blocks a cross-origin fetch() without CORS response headers, so
    the admin dashboard needs GET allowed from any origin to read rmng-client-outputs.json.
    """
    s3 = _s3_bucket_client(session)
    try:
        s3.put_bucket_cors(
            Bucket=bucket_name,
            CORSConfiguration={
                "CORSRules": [
                    {
                        "AllowedOrigins": ["*"],
                        "AllowedMethods": ["GET", "HEAD"],
                        "AllowedHeaders": ["*"],
                        "MaxAgeSeconds": 3000,
                    }
                ]
            },
        )
        return True
    except ClientError as e:
        print(f"[ERROR] Failed to configure CORS on {bucket_name}: {e}")
        return False

def _create_bucket(session, bucket_name):
    """Create bucket in PUBLIC_BUCKET_REGION (matches get_bucket_name suffix)."""
    s3 = _s3_bucket_client(session)
    if PUBLIC_BUCKET_REGION == "us-east-1":
        s3.create_bucket(Bucket=bucket_name)
    else:
        s3.create_bucket(
            Bucket=bucket_name,
            CreateBucketConfiguration={"LocationConstraint": PUBLIC_BUCKET_REGION},
        )

def ensure_bucket_exists(session, bucket_name):
    """
    Verify the bucket exists; if missing (404), create it in PUBLIC_BUCKET_REGION.
    """
    s3_client = session.client("s3")
    try:
        s3_client.head_bucket(Bucket=bucket_name)
        return True
    except ClientError as e:
        error_code = e.response["Error"]["Code"]
        if error_code in ("404", "NoSuchBucket", "NotFound"):
            print(f"[INFO] Bucket not found: {bucket_name}. Creating in {PUBLIC_BUCKET_REGION}...")
            try:
                _create_bucket(session, bucket_name)
                print(f"[INFO] Created bucket: {bucket_name}")
                return True
            except ClientError as ce:
                code = ce.response["Error"]["Code"]
                if code in ("BucketAlreadyOwnedByYou",):
                    return True
                print(f"[ERROR] Failed to create bucket {bucket_name}: {ce}")
                return False
        if error_code == "403":
            print(f"[ERROR] ACCESS DENIED: {bucket_name}")
            print(f"        Bucket exists, but you do not have permission to access it.")
        else:
            print(f"[ERROR] Error checking bucket: {e}")
        return False

def filter_sensitive_data(data, paths_to_remove):
    """
    Middleware function that traverses the JSON data and redacts/removes
    the keys specified in the paths_to_remove list.
    """
    filtered_data = copy.deepcopy(data)

    for path in paths_to_remove:
        current = filtered_data

        for key in path[:-1]:
            if isinstance(current, dict) and key in current:
                current = current[key]
            else:
                current = None
                break

        if isinstance(current, dict) and path[-1] in current:
            del current[path[-1]]

    return filtered_data

def upload_to_s3(session, rmng_outputs_path, bucket_name, region):

    print("Uploading RMNG Outputs to S3...")
    if not os.path.exists(rmng_outputs_path):
        print(f"[ERROR] RMNG Outputs file not found: {rmng_outputs_path}")
        sys.exit(1)

    try:
        with open(rmng_outputs_path, 'r') as f:
            raw_data = json.load(f)

        sanitized_data = filter_sensitive_data(raw_data, SENSITIVE_PATHS)

        if not ensure_bucket_exists(session, bucket_name):
            sys.exit(1)

        if not ensure_public_read_access(session, bucket_name):
            sys.exit(1)

        if not ensure_bucket_cors(session, bucket_name):
            sys.exit(1)

        s3_client = _s3_bucket_client(session)
        key = f"{region}/rmng-client-outputs.json"
        body = json.dumps(sanitized_data, indent=2)
        s3_client.put_object(
            Bucket=bucket_name,
            Key=key,
            Body=body,
            ContentType="application/json",
            ContentDisposition="inline",
        )

        url = f"https://{bucket_name}.s3.{PUBLIC_BUCKET_REGION}.amazonaws.com/{key}"
        print(f"[INFO] Successfully uploaded {rmng_outputs_path} to S3 Bucket: {bucket_name}")
        print(f"[INFO] Public URL: {url}")
    except Exception as e:
        print(f"[ERROR] Failed to upload {rmng_outputs_path} to S3 Bucket: {bucket_name}...")
        print(f"[ERROR] {e}")
        sys.exit(1)

def upload_qr_code(session, bucket_name, region, outputs_url):
    """Generate a QR code PNG for the outputs URL, upload it, and print it to the terminal."""
    qr = segno.make(outputs_url)

    buf = io.BytesIO()
    qr.save(buf, kind="png", scale=5)
    png_bytes = buf.getvalue()

    s3_client = _s3_bucket_client(session)
    key = f"{region}/rmng-client-outputs-qr.png"
    s3_client.put_object(
        Bucket=bucket_name,
        Key=key,
        Body=png_bytes,
        ContentType="image/png",
        ContentDisposition="inline",
    )

    qr_url = f"https://{bucket_name}.s3.{PUBLIC_BUCKET_REGION}.amazonaws.com/{key}"
    print(f"[INFO] Uploaded QR code -> {qr_url}")

    print()
    qr.terminal(compact=True)
    print()
    print(f"[INFO] QR Code URL: {qr_url}")

def upload_swagger_to_s3(session, bucket_name, region):
    """Upload docs/api/*.yaml files to {region}/swagger/ in the public bucket."""

    yaml_files = sorted(glob.glob(os.path.join(SWAGGER_DIR, "*.yaml")))
    if not yaml_files:
        print(f"[WARN] No YAML files found in {SWAGGER_DIR}/. Skipping swagger upload.")
        return

    print(f"Uploading {len(yaml_files)} Swagger spec(s) to S3...")

    s3_client = _s3_bucket_client(session)
    for path in yaml_files:
        filename = os.path.basename(path)
        key = f"{region}/swagger/{filename}"
        try:
            with open(path, "r") as f:
                body = f.read()
            s3_client.put_object(
                Bucket=bucket_name,
                Key=key,
                Body=body,
                ContentType="text/yaml",
                ContentDisposition="inline",
            )
            url = f"https://{bucket_name}.s3.{PUBLIC_BUCKET_REGION}.amazonaws.com/{key}"
            print(f"[INFO] Uploaded {filename} -> {url}")
        except Exception as e:
            print(f"[ERROR] Failed to upload {filename}: {e}")
            sys.exit(1)

def main():
    print("=" * 60)
    print("[INFO] Uploading RMNG Outputs to S3 Bucket")
    region = os.environ.get("AWS_REGION", "us-east-1")

    try:
        boto3_session = boto3.Session(region_name=region)
    except Exception as e:
        print(f"[ERROR] Failed to create boto3 session: {e}")
        sys.exit(1)

    account_id = get_user_account_id(boto3_session)

    bucket_name = get_bucket_name(
        account_id
    )

    upload_to_s3(
        boto3_session,
        RMNG_OUTPUTS,
        bucket_name,
        region
    )

    outputs_key = f"{region}/rmng-client-outputs.json"
    outputs_url = f"https://{bucket_name}.s3.{PUBLIC_BUCKET_REGION}.amazonaws.com/{outputs_key}"
    upload_qr_code(boto3_session, bucket_name, region, outputs_url)

    upload_swagger_to_s3(
        boto3_session,
        bucket_name,
        region,
    )

    print()
    print("=" * 60)

if __name__ == '__main__':
    main()
