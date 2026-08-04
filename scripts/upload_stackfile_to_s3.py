#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
upload_to_s3.py -- Upload the cdk/Stackfile.yaml to the publishing S3 bucket.

Called as part of the --publish flow in deploy.sh, after CDK assets and
templates have been published.

Usage:
    python3 scripts/upload_stackfile_to_s3.py --version 1.0.0

    Uses AWS_PROFILE and AWS_REGION from environment.
"""

import argparse
import logging
import os
import sys
from pathlib import Path

import boto3
from botocore.exceptions import ClientError

# -----------------------------
# Constants
# -----------------------------
DEFAULT_VERSION       = "1.0.0"
DEFAULT_BUCKET_PREFIX = "rmng-publishing-bucket"
DEFAULT_REGION        = "us-east-1"
DEFAULT_STACKFILE     = "cdk/Stackfile.yaml"

# -----------------------------
# Logging
# -----------------------------
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
)
log = logging.getLogger(__name__)


# -----------------------------
# CLI Args
# -----------------------------
def parse_args():
    parser = argparse.ArgumentParser(
        description="Upload cdk/Stackfile.yaml to the publishing S3 bucket."
    )
    parser.add_argument("--version", default=DEFAULT_VERSION)
    parser.add_argument("--bucket-prefix", default=DEFAULT_BUCKET_PREFIX)
    return parser.parse_args()


# -----------------------------
# Helpers
# -----------------------------
def get_user_account_id(session):
    sts = session.client("sts")
    identity = sts.get_caller_identity()
    return identity["Account"]


# -----------------------------
# Main
# -----------------------------
def main():
    args = parse_args()

    stackfile = Path(DEFAULT_STACKFILE)
    if not stackfile.exists():
        log.error("Stackfile not found: %s", stackfile)
        sys.exit(1)

    region = os.environ.get("AWS_REGION") or os.environ.get("AWS_DEFAULT_REGION") or DEFAULT_REGION
    session = boto3.Session(region_name=region)
    # Full-name override (Jenkins prod publish exports APPLICATION_PUBLISHER_BUCKET); unset/empty keeps the legacy prefix-account-region derivation so existing flows are untouched.
    bucket_override = os.environ.get("APPLICATION_PUBLISHER_BUCKET")
    if bucket_override:
        bucket = bucket_override
        log.info("Using APPLICATION_PUBLISHER_BUCKET override: %s", bucket)
    else:
        account_id = get_user_account_id(session)
        bucket = f"{args.bucket_prefix}-{account_id}-{region}"

    s3 = session.client("s3")

    s3_key = f"{args.version}/{stackfile.name}"

    log.info("Uploading %s -> s3://%s/%s", stackfile, bucket, s3_key)

    try:
        s3.upload_file(str(stackfile), bucket, s3_key)
        log.info("Upload complete")
    except ClientError as exc:
        log.error("Failed to upload %s: %s", stackfile, exc)
        sys.exit(1)


if __name__ == "__main__":
    main()
