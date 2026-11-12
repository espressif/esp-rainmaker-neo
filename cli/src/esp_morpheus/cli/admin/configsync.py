# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""test_config.json kept in one S3 bucket per account and region, so a lost local copy is fetched back instead of replaced by fresh passwords the provisioned users do not have.

`test-data setup` syncs both ways. Every other command only fetches a missing file (Session.config_path) and otherwise reads the local copy.
"""

import hashlib
import json

import boto3
from botocore.exceptions import BotoCoreError, ClientError

from .. import output
from ..context import DEPLOYMENT_KEY

BUCKET_NAME_PREFIX = 'rmng-test-config'
OBJECT_KEY = 'test_config.json'
HASH_METADATA = 'content-sha256'

# Hash of the content at the last upload or download; kept only locally, so a local edit and a newer S3 copy can be told apart.
SYNCED_KEY = 'synced_sha256'

_NOT_FOUND = ('404', 'NoSuchKey', 'NoSuchBucket')
_PRECONDITION_FAILED = ('PreconditionFailed', 'ConditionalRequestConflict')


def bucket_name(settings):
    """The account-regional bucket: account 111122223333 in ap-south-1 -> rmng-test-config-111122223333-ap-south-1-an."""
    return f"{BUCKET_NAME_PREFIX}-{settings.account_id}-{settings.region}-an"


def content_hash(config):
    """sha256 of the config without SYNCED_KEY, independent of key order and indentation."""
    content = {k: v for k, v in config.items() if k != SYNCED_KEY}
    return hashlib.sha256(json.dumps(content, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def _store(session):
    settings = session.settings
    return _Store(_s3_client(settings.region), bucket_name(settings), session)


def _s3_client(region):
    return boto3.client('s3', region_name=region)


def fetch(session):
    """Download this deployment's copy when S3 has one. Quiet on failure: most commands run without AWS credentials."""
    store = _store(session)
    try:
        if store.head():
            store.pull()
    except (ClientError, BotoCoreError) as e:
        output.debug(f"Could not fetch {store.url}: {e}")


def sync(session):
    """Make the local test_config.json and this deployment's S3 copy the same, failing rather than discarding either side's changes."""
    store = _store(session)
    try:
        _sync(session, store)
    except ClientError as e:
        output.fail(f"Could not sync test_config.json with {store.url}: {e}")


def _sync(session, store):
    remote = store.head()
    # A file without a stamp predates the S3 copy and could belong to any account, so it is replaced rather than trusted.
    if not session.config_exists() or DEPLOYMENT_KEY not in session.config:
        if session.config_exists():
            output.warn(f"Replacing {store.local_path}: it names no deployment.")
        if remote:
            store.pull()
            return
        session.generate_config()
        if not store.push():
            # Another setup uploaded first; its passwords win since it may already have provisioned them.
            store.pull()
        return

    config = session.config
    local, synced = content_hash(config), config.get(SYNCED_KEY)
    if remote is None:
        if not store.push():
            output.fail(f"{store.url} was created while this setup ran. Rerun to compare against it.")
    elif remote.digest == local:
        if synced != local:
            config[SYNCED_KEY] = local
            session.write_config(config)
        output.debug(f"test_config.json matches {store.url}")
    elif remote.digest == synced:
        if not store.push(remote.etag):
            output.fail(f"{store.url} changed while this setup ran. Rerun to compare against it.")
    elif local == synced:
        store.pull()
    elif synced is None:
        output.fail(f"{store.url} already holds a different test_config.json for this deployment, with the "
                    "passwords its users were provisioned with. Move the local file aside and rerun to fetch it.")
    else:
        output.fail(f"{store.local_path} and {store.url} both changed since they last matched. The S3 copy "
                    "holds the passwords this deployment's users were provisioned with: move the local file "
                    "aside and rerun to fetch it, then re-add any local nodes with `test-data gen-device`.")


class _Remote:
    def __init__(self, digest, etag):
        self.digest = digest
        self.etag = etag


class _Store:
    def __init__(self, s3, bucket, session):
        self.s3 = s3
        self.bucket = bucket
        self.session = session
        self.url = f"s3://{bucket}/{OBJECT_KEY}"
        self.local_path = session.config_path()

    def head(self):
        try:
            response = self.s3.head_object(Bucket=self.bucket, Key=OBJECT_KEY)
        except ClientError as e:
            if e.response['Error']['Code'] in _NOT_FOUND:
                return None
            raise
        return _Remote(response.get('Metadata', {}).get(HASH_METADATA), response['ETag'])

    def pull(self):
        config = json.loads(self.s3.get_object(Bucket=self.bucket, Key=OBJECT_KEY)['Body'].read())
        config[SYNCED_KEY] = content_hash(config)
        path = self.session.write_config(config)
        output.ok(f"Fetched {path} from {self.url}")

    def push(self, etag=None):
        """Upload the local config; False when S3 no longer matches what the caller compared against."""
        self._ensure_bucket()
        config = self.session.config
        digest = content_hash(config)
        body = {k: v for k, v in config.items() if k != SYNCED_KEY}
        condition = {'IfMatch': etag} if etag else {'IfNoneMatch': '*'}
        try:
            self.s3.put_object(Bucket=self.bucket, Key=OBJECT_KEY, Body=json.dumps(body, indent=2).encode(),
                               ContentType='application/json', Metadata={HASH_METADATA: digest}, **condition)
        except ClientError as e:
            if e.response['Error']['Code'] in _PRECONDITION_FAILED:
                return False
            raise
        config[SYNCED_KEY] = digest
        self.session.write_config(config)
        output.ok(f"Uploaded test_config.json to {self.url}")
        return True

    def _ensure_bucket(self):
        region = self.session.settings.region
        kwargs = {'Bucket': self.bucket, 'BucketNamespace': 'account-regional'}
        if region != 'us-east-1':
            kwargs['CreateBucketConfiguration'] = {'LocationConstraint': region}
        try:
            self.s3.create_bucket(**kwargs)
            output.ok(f"Created bucket {self.bucket}")
        except ClientError as e:
            if e.response['Error']['Code'] != 'BucketAlreadyOwnedByYou':
                raise
        # Re-applied on every upload, so a create that failed part-way is completed by the next run.
        self.s3.put_public_access_block(Bucket=self.bucket, PublicAccessBlockConfiguration={
            'BlockPublicAcls': True, 'IgnorePublicAcls': True,
            'BlockPublicPolicy': True, 'RestrictPublicBuckets': True})
        self.s3.put_bucket_versioning(Bucket=self.bucket, VersioningConfiguration={'Status': 'Enabled'})
        self.s3.put_bucket_policy(Bucket=self.bucket, Policy=json.dumps({
            'Version': '2012-10-17',
            'Statement': [{
                'Sid': 'DenyInsecureTransport', 'Effect': 'Deny', 'Principal': '*', 'Action': 's3:*',
                'Resource': [f"arn:aws:s3:::{self.bucket}", f"arn:aws:s3:::{self.bucket}/*"],
                'Condition': {'Bool': {'aws:SecureTransport': 'false'}},
            }],
        }))
