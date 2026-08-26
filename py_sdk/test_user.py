# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import boto3
import hashlib
import requests
from botocore.auth import SigV4Auth
from botocore.awsrequest import AWSRequest
from boto3.dynamodb.conditions import Key
import json
import base64
from awscrt import auth
from awsiot import mqtt_connection_builder
import jwt
from awsiot import mqtt
from awsiot import iotshadow
from scripts.rmng_outputs import DEFAULT_ESP_USER_CLIENT_ID, RmngSettings
from py_sdk.test_util import shadow_to_unstructured
from py_sdk import test_smartthings as smartthings
import random
import string
import queue
import uuid
import os
import time
from urllib.parse import urlencode, quote
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.hazmat.primitives import hashes, serialization
from cryptography import x509

blue = "\033[94m"
reset = "\033[0m"

# Seeded end-user OIDC client id, read from CDK outputs when available (falls back to the stable
# seeded literal for contexts without rmng-outputs.json). The OTP login path issues tokens to this
# client. Resolved from the repo-anchored outputs, so the value no longer depends on the CWD the
# importing process happened to start in.
def _esp_user_client_id():
    try:
        return RmngSettings.from_source().esp_user_client_id
    except (FileNotFoundError, json.JSONDecodeError, SystemExit):
        return DEFAULT_ESP_USER_CLIENT_ID

ESP_USER_CLIENT_ID = _esp_user_client_id()

request_logging = False
# Track verified CORS paths per API gateway URL and HTTP method to handle multiple gateways
_verified_cors_paths = {}  # {(api_gateway_url, http_method): set(paths)}

# Define colored logging function for User
def user_log(message):
    """Print message with User prefix in cyan color."""
    # ANSI color codes: Cyan for User
    CYAN = '\033[96m'
    RESET = '\033[0m'
    print(f"{CYAN}[User]{RESET} {message}")


class _CognitoResponse:
    """requests.Response-compatible shim for admin Cognito auth calls."""
    def __init__(self, status_code, payload):
        self.status_code = status_code
        self._payload = payload
        self.text = json.dumps(payload)

    @property
    def ok(self):
        return 200 <= self.status_code < 300

    def json(self):
        return self._payload


# Must match userIDNamespace in util/id_utils.go — never change it.
_USER_ID_NAMESPACE = uuid.UUID("6f2a1c9e-3b4d-5e6f-8a9b-0c1d2e3f4a5b")


def derive_user_id(identifier):
    """Deterministic user_id for an identity — mirrors utils.DeriveUserID in Go."""
    return str(uuid.uuid5(_USER_ID_NAMESPACE, identifier.strip().lower()))


def _admin_cognito_auth(region, client_id, auth_flow, auth_parameters):
    """Run an admin Cognito initiate_auth and return a requests.Response-like shim."""
    client = boto3.client('cognito-idp', region_name=region)
    try:
        result = client.initiate_auth(
            ClientId=client_id,
            AuthFlow=auth_flow,
            AuthParameters=auth_parameters,
        )['AuthenticationResult']
    except (client.exceptions.NotAuthorizedException, client.exceptions.UserNotFoundException) as e:
        return _CognitoResponse(401, {"message": str(e)})
    except Exception as e:  # noqa: BLE001 - surface as a server-style failure
        return _CognitoResponse(500, {"message": str(e)})

    payload = {
        "access_token": result["AccessToken"],
        "id_token": result.get("IdToken"),
        "token_type": result.get("TokenType", "Bearer"),
        "expires_in": result.get("ExpiresIn"),
    }
    # REFRESH_TOKEN_AUTH does not return a new refresh token.
    if "RefreshToken" in result:
        payload["refresh_token"] = result["RefreshToken"]
    return _CognitoResponse(200, payload)

class User:
    def __init__(self, username, password, region, identity_pool_id, api_gateway_url, user_api_gateway_url, iot_endpoint, admin_user_pool_id="", admin_client_id="", is_super_admin=False, end_user_pool_id=""):
        self.username = username
        self.password = password
        self.region = region
        # Admin pool for admin sign-in; end_user_pool_id is the provider pool an end
        # user is provisioned into. Each caller sets only the one it needs.
        self.admin_user_pool_id = admin_user_pool_id
        self.admin_client_id = admin_client_id
        self.end_user_pool_id = end_user_pool_id
        self.identity_pool_id = identity_pool_id
        self.api_gateway_url = api_gateway_url
        self.user_api_gateway_url = user_api_gateway_url
        self.token = None
        self.access_token = None
        self.refresh_token = None
        self.credentials = None
        self.sub = None  # User ID extracted from token
        self._user_id = None # surfaced by GET /v1/users/me.
        self.iot_endpoint = iot_endpoint
        # Per-session MQTT client-id suffix. The vended iot:Connect policy scopes
        # to "user:<username>:*", so every session needs a unique suffix (a user's
        # phone and dashboard must not collide on one client id). Stable per User
        # instance so reconnects keep the same client id.
        self.mqtt_session_suffix = "".join(random.choices(string.digits, k=6))
        self.group_ids = []  # New array to store group IDs
        self.devices = [] # New array to store devices associated with the user
        self.mqtt_connection = None  # Initialize mqtt_connection
        self.mqtt_credentials = None # Initialize MQTT credentials
        self.shadow_client = None  # Initialize shadow_client
        self.shadow_queue = queue.Queue()
        self.connection_queue = queue.Queue()
        self.disable_reconnect = False
        self.is_super_admin = is_super_admin
        self.disconnect_future = None
        self.previous_disconnect_time = 0
        self._session = None  # Cached requests session for connection reuse
        self._boto3_session = None  # Cached boto3 session for SigV4 signing
        self._boto3_session_creds = None  # Credentials used to create cached boto3 session

    @property
    def session(self):
        """Lazily create and return a requests Session for connection reuse."""
        if self._session is None:
            self._session = requests.Session()
        return self._session

    @property
    def boto3_session(self):
        """Return a cached boto3 Session, recreating only when credentials change."""
        if self._boto3_session is None or self._boto3_session_creds is not self.credentials:
            self._boto3_session = boto3.Session(
                aws_access_key_id=self.credentials['AccessKeyId'],
                aws_secret_access_key=self.credentials['SecretKey'],
                aws_session_token=self.credentials['SessionToken'],
                region_name=self.region
            )
            self._boto3_session_creds = self.credentials
        return self._boto3_session

    def close_session(self):
        """Close the cached session to release connections."""
        if self._session is not None:
            self._session.close()
            self._session = None

    def create_super_admin_via_cognito(self, email=None, password=None, user_id=None):
        """Provision a super admin in the admin Cognito pool (no DB record). Returns the user_id.

        `user_id` overrides the derived value for identities that must keep a stable id. Pass
        `password=False` for an identity that never signs in, so no password exists to be used.
        """
        if email is None:
            email = self.username
        if password is None:
            password = self.password
        if user_id is None:
            user_id = derive_user_id(email)

        cognito = boto3.client('cognito-idp', region_name=self.region)
        try:
            cognito.admin_create_user(
                UserPoolId=self.admin_user_pool_id,
                Username=email,
                MessageAction='SUPPRESS',
                UserAttributes=[
                    {"Name": "email", "Value": email},
                    {"Name": "email_verified", "Value": "true"},
                ],
            )
        except cognito.exceptions.UsernameExistsException:
            user_log(f"Super admin {email} already exists; refreshing its attributes")
        except Exception as e:  # noqa: BLE001
            user_log(f"Failed to create super admin {email}: {e}")
            return None

        # Stamped separately, and on every call: an account that already existed would otherwise keep
        # whatever attributes it has, and a super admin missing custom:user_id resolves to no accessor
        # and is refused by every admin gate.
        try:
            cognito.admin_update_user_attributes(
                UserPoolId=self.admin_user_pool_id,
                Username=email,
                UserAttributes=[
                    {"Name": "custom:super_admin", "Value": "true"},
                    {"Name": "custom:user_id", "Value": user_id},
                ],
            )
        except Exception as e:  # noqa: BLE001
            user_log(f"Failed to stamp super admin attributes for {email}: {e}")
            return None

        if password is False:
            return user_id
        try:
            cognito.admin_set_user_password(
                UserPoolId=self.admin_user_pool_id,
                Username=email,
                Password=password,
                Permanent=True,
            )
        except Exception as e:  # noqa: BLE001
            user_log(f"Failed to set super admin password for {email}: {e}")
            return None
        return user_id

    def create_user_via_cognito(self, email=None, password=None, profile=None):
        """Provision a regular end user directly in the end-user Cognito pool. Used by the general
        fixtures so they don't need OTP email delivery. `profile` optionally sets standard OIDC
        profile attributes (name/locale/picture) that a federated login carries over. Returns the
        user_id."""
        if email is None:
            email = self.username
        if password is None:
            password = self.password
        user_id = derive_user_id(email)
        attributes = [
            {"Name": "email", "Value": email},
            {"Name": "email_verified", "Value": "true"},
        ]
        for name, value in (profile or {}).items():
            attributes.append({"Name": name, "Value": value})
        cognito = boto3.client('cognito-idp', region_name=self.region)
        try:
            cognito.admin_create_user(
                UserPoolId=self.end_user_pool_id,
                Username=email,
                MessageAction='SUPPRESS',
                UserAttributes=attributes,
            )
        except cognito.exceptions.UsernameExistsException:
            user_log(f"User {email} already exists; updating attributes and password")
            cognito.admin_update_user_attributes(
                UserPoolId=self.end_user_pool_id, Username=email, UserAttributes=attributes,
            )
        except Exception as e:  # noqa: BLE001
            user_log(f"Failed to create user {email}: {e}")
            return None
        try:
            cognito.admin_set_user_password(
                UserPoolId=self.end_user_pool_id, Username=email, Password=password, Permanent=True,
            )
        except Exception as e:  # noqa: BLE001
            user_log(f"Failed to set password for {email}: {e}")
            return None
        return user_id

    def register_user_via_lambda(self, email=None, phone_number=None, password=None):
        """Provision a normal user via Cognito (AdminCreateUser) and sign in through the
        Native /v1/user/auth/token converting API. Name/signature kept stable for
        existing callers. `phone_number` is unused. Returns the signin() response (token set on
        success), or None if no email is available."""
        if email is None:
            email = self.username if '@' in self.username else None
        if email is None:
            user_log("register_user_via_lambda requires an email address")
            return None
        if password is None:
            password = self.password
        self.create_user_via_cognito(email=email, password=password)
        return self.signin(username=email, password=password)

    def get_cognito_token(self):

        try:
            if self.is_super_admin:
                response = self.signin(is_admin=True)
            else:
                response = self.signin()

            if response.status_code not in (200, 201):
                user_log(f"Authentication failed: Status {response.status_code}, Response: {response.text}")
                return None

            try:
                response_data = response.json()
                self.token = response_data.get('id_token')
                self.access_token = response_data.get('access_token')
                self.refresh_token = response_data.get('refresh_token')

                if not self.token:
                    user_log("Authentication failed: No id_token in response")
                    return None
            except (ValueError, json.JSONDecodeError, KeyError) as e:
                user_log(f"Authentication failed: Failed to parse response: {str(e)}")
                return None

            # Extract sub from token
            self._extract_sub_from_token()
            return self.token
        except Exception as e:
            user_log(f"Authentication failed: {str(e)}")
            return None

    def _extract_sub_from_token(self):
        """Extract user sub (user_id) from the current token. Sets self.sub if token is available."""
        if not self.token:
            self.sub = None
            return

        try:
            decoded_id_token = jwt.decode(self.token, options={"verify_signature": False})
            # Try to get custom:user_id from ID token, fallback to regular 'sub' claim
            self.sub = decoded_id_token.get('custom:user_id') or decoded_id_token.get('sub')
        except Exception as token_error:
            # If ID token decode fails, try access token as fallback
            try:
                if self.access_token:
                    decoded_access_token = jwt.decode(self.access_token, options={"verify_signature": False})
                    self.sub = decoded_access_token.get('custom:user_id') or decoded_access_token.get('sub')
                else:
                    self.sub = None
            except Exception:
                user_log(f"Warning: Could not extract user_id from tokens: {token_error}")
                self.sub = None

    def get_user_creds_api(self):
        """
        Helper function to call POST /v1/user/credentials API endpoint.

        Returns:
            bool: True if credentials are available, False otherwise
        """
        if self.credentials:
            return True

        if not self.token:
            self.get_cognito_token()

        if not self.token:
            user_log("Failed to get user credentials: No valid token available")
            return False

        try:
            # Access token in the header (the method declares API Gateway authorization
            # scopes, so the authorizer validates that), ID token in the body for the
            # Identity Pool exchange, which accepts an OIDC ID token and nothing else.
            # The server requires both to come from one sign-in.
            response = self.make_api_request_with_token(
                'POST', '/v1/user/credentials',
                data=json.dumps({"id_token": self.token}),
                token=self.access_token)

            print(f"access_token: {self.access_token}")
            if response.status_code not in (200, 201):
                user_log(f"Failed to get user credentials. Status code: {response.status_code}")
                user_log(f"Response: {response.text}")
                return False

            try:
                creds_response = json.loads(response.text)
            except (json.JSONDecodeError, ValueError) as json_err:
                user_log(f"Failed to parse JSON response: {str(json_err)}")
                user_log(f"Response text: {response.text[:500]}")
                return False

            if not isinstance(creds_response, dict):
                user_log(f"Invalid response format: expected dict, got {type(creds_response)}")
                return False

            required_fields = ['access_key_id', 'secret_access_key', 'session_token', 'expiration']
            for field in required_fields:
                if field not in creds_response:
                    user_log(f"Missing required field in credentials response: {field}")
                    return False

            self.credentials = {
                'AccessKeyId': creds_response['access_key_id'],
                'SecretKey': creds_response['secret_access_key'],
                'SessionToken': creds_response['session_token'],
                'Expiration': creds_response['expiration']
            }

            return True
        except Exception as e:
            user_log(f"Failed to get user credentials: {str(e)}")
            return False

    def get_aws_credentials(self):
        if not self.token:
            self.get_cognito_token()

        if not self.token:
            user_log("Failed to get AWS credentials: No valid token available")
            return None

        try:
            if not self.get_user_creds_api():
                user_log("Failed to get AWS credentials: No valid credentials available")
                return None

            if not self.credentials:
                user_log("Failed to get AWS credentials: No valid credentials available")
                return None

            return self.credentials
        except Exception as e:
            user_log(f"Failed to get AWS credentials: {str(e)}")
            return None

    def make_api_request_with_token(self, method, path, data=None, api_url=None, skip_cors_check=False, token=None):
        """Make a Bearer-authenticated request.

        token: the JWT to put in the Authorization header. Left unset it is the
            access token, except for a superadmin: the admin methods and the
            identity pool's cognito-idp provider both reject a Cognito access
            token, which carries no aud, so those take the ID token.
        """
        if not self.access_token:
            self.get_cognito_token()

        if not api_url:
            api_url = self.api_gateway_url

        url = f"{api_url}{path}"

        if not skip_cors_check:
            self._verify_cors(path, intended_method=method, api_gateway_url=api_url)

        if token is None:
            token = self.token if self.is_super_admin else self.access_token
        headers = {}
        if data is not None:
            headers["Content-Type"] = "application/json"
        if token:
            headers["Authorization"] = f"Bearer {token}"

        print(f"-----------------------------------------")
        print(f"Request: {method} {url} {data}")
        response = self.session.request(
            method=method,
            url=url,
            headers=headers,
            data=data if data is not None else None
        )

        if "signin" not in path:
            print(f"Response: {response.status_code} {response.text}")
        print(f"-----------------------------------------")
        return response


    def _verify_cors(self, path, intended_method, expected_cors_origin="*", api_gateway_url=None):
        """
        Internal helper: verifies CORS preflight configuration for a path.

        Performs a complete CORS preflight check according to W3C CORS specification:
        - Verifies OPTIONS request returns 200/204
        - Validates Access-Control-Allow-Origin header
        - Validates Access-Control-Allow-Methods includes the intended method
        - Validates Access-Control-Allow-Headers includes required headers
        - Optionally validates Access-Control-Max-Age if present

        Args:
            path: API path to verify
            intended_method: HTTP method that will be used in the actual request
            expected_cors_origin: Expected CORS origin (default: "*")
            api_gateway_url: Optional API gateway URL (defaults to self.api_gateway_url)
        """
        api_url = api_gateway_url or self.api_gateway_url

        # Track verified paths per API gateway URL and HTTP method
        key = (api_url, intended_method)
        if key not in _verified_cors_paths:
            _verified_cors_paths[key] = set()

        if path in _verified_cors_paths[key] or intended_method == "OPTIONS":
            return

        request_origin = "http://localhost:3000" if expected_cors_origin == "*" else expected_cors_origin

        # Headers that will be sent in actual requests (SigV4Auth adds these)
        request_headers = [
            "Content-Type",
            "X-Amz-Date",
            "Authorization",
            "X-Api-Key",
            "X-Amz-Security-Token"
        ]

        if request_logging:
            print(f"[INFO] Verifying CORS Preflight for {path} (method: {intended_method})...")

        clean_path = path.lstrip('/')
        url = f"{api_url}/{clean_path}"

        try:
            # Send proper CORS preflight request with required headers
            response = requests.options(
                url,
                headers={
                    "Origin": request_origin,
                    "Access-Control-Request-Method": intended_method,
                    "Access-Control-Request-Headers": ",".join(request_headers),
                },
                timeout=5
            )

            # CORS preflight should return 200 or 204
            if response.status_code not in (200, 204):
                error_msg = f"CORS preflight failed: {path} OPTIONS returned {response.status_code}"
                if request_logging:
                    print(f"[CORS FAILURE] {error_msg}")
                raise AssertionError(error_msg)

            # Verify Access-Control-Allow-Origin
            allow_origin = response.headers.get('Access-Control-Allow-Origin')
            if not allow_origin:
                error_msg = f"CORS preflight failed: {path} missing Access-Control-Allow-Origin header"
                if request_logging:
                    print(f"[CORS FAILURE] {error_msg}")
                raise AssertionError(error_msg)

            is_valid_origin = (
                allow_origin == "*" or
                allow_origin == request_origin
            )
            if not is_valid_origin:
                error_msg = f"CORS preflight failed: {path} incorrect Origin. Expected '{expected_cors_origin}' or '*', got '{allow_origin}'"
                if request_logging:
                    print(f"[CORS FAILURE] {error_msg}")
                raise AssertionError(error_msg)

            # Verify Access-Control-Allow-Methods includes the intended method
            allow_methods = response.headers.get('Access-Control-Allow-Methods', '')
            allowed_methods_list = [m.strip().upper() for m in allow_methods.split(',')]
            if intended_method.upper() not in allowed_methods_list:
                error_msg = f"CORS preflight failed: {path} method '{intended_method}' not in allowed methods: {allow_methods}"
                if request_logging:
                    print(f"[CORS FAILURE] {error_msg}")
                raise AssertionError(error_msg)

            # Verify Access-Control-Allow-Headers includes required headers
            allow_headers = response.headers.get('Access-Control-Allow-Headers', '')
            allowed_headers_list = [h.strip().lower() for h in allow_headers.split(',')]
            missing_headers = [
                h for h in request_headers
                if h.lower() not in allowed_headers_list
            ]
            if missing_headers:
                error_msg = f"CORS preflight failed: {path} missing required headers: {missing_headers}. Allowed: {allow_headers}"
                if request_logging:
                    print(f"[CORS FAILURE] {error_msg}")
                raise AssertionError(error_msg)

            # Log success
            if request_logging:
                max_age = response.headers.get('Access-Control-Max-Age', 'not set')
                print(f"[INFO] CORS Verified successfully for {path}")
                print(f"  Allow-Origin: {allow_origin}")
                print(f"  Allow-Methods: {allow_methods}")
                print(f"  Allow-Headers: {allow_headers}")
                print(f"  Max-Age: {max_age}")

            # Mark as verified
            _verified_cors_paths[key].add(path)

        except requests.exceptions.RequestException as e:
            error_msg = f"CORS preflight failed: Could not verify options for {path}: {e}"
            print(f"[CORS ERROR] {error_msg}")
            raise AssertionError(error_msg) from e
        except AssertionError:
            # Re-raise assertion errors
            raise
        except Exception as e:
            error_msg = f"CORS preflight failed: Unexpected error verifying {path}: {e}"
            print(f"[CORS ERROR] {error_msg}")
            raise AssertionError(error_msg) from e

    def make_api_request(self, method, path, data=None, params=None, skip_cors_check=False, api_gateway_url=None):
        """Make an API request with the user's credentials.

        Args:
            method (str): HTTP method (GET, POST, etc.)
            path (str): API path
            params (dict): Query parameters for GET requests
            data (dict): JSON data for POST/PUT requests
            skip_cors_check (bool): Skip CORS preflight verification
            api_gateway_url (str): Optional API gateway URL (defaults to self.api_gateway_url)

        Returns:
            requests.Response: The API response
        """
        api_url = api_gateway_url or self.api_gateway_url

        if not skip_cors_check:
            self._verify_cors(path, intended_method=method, api_gateway_url=api_url)


        if not self.credentials:
            self.get_aws_credentials()

        if not self.credentials:
            raise Exception("Failed to obtain AWS credentials. User may need to authenticate first.")

        # Build the request URL and ensure path starts without a slash
        path = path.lstrip('/')
        url = f"{api_url}/{path}"

        if request_logging:
            # ===== DETAILED REQUEST LOGGING =====
            print(f"\n{'='*80}")
            print(f"🚀 API REQUEST:")
            print(f"Method: {method}")
            print(f"URL: {url}")
            if params:
                print(f"Query Parameters:")
                for key, value in params.items():
                    print(f"  {key}: {value}")
            if data:
                print(f"Request Body: {data}")
            print(f"{'='*80}")

        # Convert all parameter values to strings to avoid encoding issues
        if params is not None:
            params = {k: str(v) for k, v in params.items()}

        # Create AWS request with properly formatted parameters
        request = AWSRequest(
            method=method,
            url=url,
            params=params,
            data=data
        )

        # Sign the request using cached boto3 session
        SigV4Auth(self.boto3_session.get_credentials(), "execute-api", self.region).add_auth(request)
        prepared_request = request.prepare()

        # Use the prepared URL which has properly encoded parameters
        response = self.session.request(
            method=method,
            url=prepared_request.url,
            headers=prepared_request.headers,
            data=data if data is not None else None
        )

        if request_logging:
            # ===== DETAILED RESPONSE LOGGING =====
            print(f"📥 API RESPONSE:")
            print(f"Status Code: {response.status_code}")
            print(f"Response Headers:")
            for key, value in response.headers.items():
                print(f"  {key}: {value}")

            try:
                response_json = response.json()
                print(f"Response Body (JSON):")
                print(json.dumps(response_json, indent=2))
            except (ValueError, json.JSONDecodeError):
                print(f"Response Body (Text): {response.text}")

            print(f"{'='*80}\n")

        return response

    def upload_file(self, file_path, file_type):
        """Upload a file using the file upload API.

        Args:
            file_path (str): Path to the file to upload
            file_type (str): Type of file (e.g., 'node_cert')

        Returns:
            bool: True if successful, False otherwise
            str: S3 path where the file was uploaded if successful, error message if failed
        """
        # Get the filename from the path
        file_name = os.path.basename(file_path)

        # Get upload URL from API
        request_body = {
            'file_type': file_type,
            'file_name': file_name
        }
        response = self.make_api_request('POST', '/v1/admin/files/upload-urls', data=json.dumps(request_body))

        if response.status_code != 201:
            return False, f"Failed to get upload URL: {response.status_code} - {response.text}"

        try:
            upload_data = response.json()
            upload_url = upload_data['upload_url']
            s3_path = upload_data['s3_path']
            print(f"Upload URL: {upload_url}")
            print(f"S3 Path: {s3_path}")

            # Upload the file to S3 using the pre-signed URL
            with open(file_path, 'rb') as file_data:
                # Don't add any extra headers, use exactly what was signed
                headers = {
                    'Content-Type': 'application/octet-stream',
                    'If-None-Match': '*'
                }
                upload_response = self.session.put(
                    upload_url,
                    data=file_data,
                    headers=headers
                )

            if upload_response.status_code not in [200, 204]:
                return False, f"File upload failed: {upload_response.status_code} - {upload_response.text}"

            return True, s3_path

        except Exception as e:
            return False, f"Error during file upload: {str(e)}"

    def do_user_node_assoc(self, device, group_id, csr=None, capabilities=None):
        """Associate a device with a group.

        Args:
            device: Device object with node_thing_name and sign_challenge method
            group_id: ID of the group to associate with
            csr: Optional CSR (PEM string) for Matter devices. If provided, includes
                 CSR in verify request and calls confirm endpoint.
            capabilities: Optional list of capability names to enable

        Returns:
            For non-Matter (csr=None): None on success, error string on failure
            For Matter (csr provided): dict with 'noc', 'matter_node_id', 'request_id'
                                       on success, error string on failure
        """
        if not device.node_thing_name:
            return "ERROR_NO_THING_NAME"

        # Step 1: Initiate the association with group_id in path
        request_id, challenge = self.initiate_node_assoc(group_id)
        if not request_id:
            # On failure, challenge holds the error string
            return challenge

        # Step 2: Sign the challenge using the device's signing method
        challenge_response = device.sign_challenge(challenge)
        if not challenge_response:
            return "ERROR_CHALLENGE_SIGNING_FAILED"

        # Step 3: Verify the association
        verify_result = self.verify_node_assoc(
            group_id, request_id, challenge_response, device.node_thing_name
        )
        if isinstance(verify_result, str):
            return verify_result

        # For Matter devices, call confirm and return NOC info
        if csr:
            confirm_path = f'/v1/groups/{group_id}/node-assoc-requests/{request_id}/confirm'
            payload = {}
            if capabilities:
                payload['capabilities'] = capabilities
            confirm_data = json.dumps(payload) if payload else '{}'
            confirm_response = self.make_api_request('POST', confirm_path, data=confirm_data)

            if confirm_response.status_code not in (200, 201):
                return f"ERROR_CONFIRM_FAILED_{confirm_response.status_code}"

            user_log(f"Matter association successful")
            return {
                "noc": verify_result.get("noc"),
                "matter_node_id": verify_result.get("matter_node_id"),
                "request_id": request_id
            }

        user_log(f"Association successful")
        return None

    def initiate_node_assoc(self, group_id):
        """Initiate a node association request for a group.

        Returns:
            (request_id, challenge) on success.
            (None, error_string) on failure — callers test ``request_id`` falsiness.
        """
        initiate_path = f'/v1/groups/{group_id}/node-assoc-requests'
        initiate_response = self.make_api_request('POST', initiate_path, data=json.dumps({}))

        if initiate_response.status_code not in (200, 201):
            return None, f"ERROR_INITIATE_FAILED_{initiate_response.status_code}"

        initiate_result = json.loads(initiate_response.text)
        user_log(f"Initiate response: {initiate_result}")
        request_id = initiate_result.get('request_id')
        challenge = initiate_result.get('challenge')
        if not request_id or not challenge:
            return None, "ERROR_INVALID_INITIATE_RESPONSE"
        return request_id, challenge

    def verify_node_assoc(self, group_id, request_id, challenge_response, node_id):
        """Verify a node association request with a signed challenge response.

        Returns:
            The parsed verify response dict on success.
            An error string on failure.
        """
        verify_path = f'/v1/groups/{group_id}/node-assoc-requests/{request_id}/verify'
        verify_data = json.dumps({
            "challenge_response": challenge_response,
            "node_id": node_id,
        })
        verify_response = self.make_api_request('POST', verify_path, data=verify_data)

        if verify_response.status_code not in (200, 201):
            return f"ERROR_VERIFY_FAILED_{verify_response.status_code}"

        verify_result = json.loads(verify_response.text)
        if verify_result.get('message') != 'success':
            return f"ERROR_VERIFY_FAILED_{verify_result.get('message')}"
        return verify_result

    # -------------------------------------------------------------- claiming

    def claim_initiate(self, mac_addr, skip_cors_check=False):
        """Reserve (or look up) the node ID assigned to a device MAC.

        POST /v1/claim/initiate. Returns the raw response so callers can
        distinguish 201 (new reservation) from 200 (idempotent repeat).
        """
        return self.make_api_request(
            'POST', '/v1/claim/initiate', data=json.dumps({"mac_addr": mac_addr}),
            skip_cors_check=skip_cors_check
        )

    def claim_verify(self, mac_addr, csr_pem, capabilities=None):
        """Exchange a CSR for a certificate signed by the claiming CA.

        POST /v1/claim/verify. `capabilities` (e.g. ["camera"]) must be
        re-supplied on a re-claim, since they are applied to the new
        certificate. Returns the raw response.
        """
        body = {"mac_addr": mac_addr, "csr": csr_pem}
        if capabilities:
            body["capabilities"] = capabilities
        return self.make_api_request('POST', '/v1/claim/verify', data=json.dumps(body))

    @staticmethod
    def generate_claim_csr(common_name="ignored-by-the-server"):
        """Generate a P-256 key pair and a CSR for it.

        The server discards the CSR subject and names the certificate after the
        reserved node, so the common name is immaterial. Returns
        (csr_pem, private_key_pem).
        """
        key = ec.generate_private_key(ec.SECP256R1())
        csr = (
            x509.CertificateSigningRequestBuilder()
            .subject_name(x509.Name([x509.NameAttribute(x509.oid.NameOID.COMMON_NAME, common_name)]))
            .sign(key, hashes.SHA256())
        )
        csr_pem = csr.public_bytes(serialization.Encoding.PEM).decode()
        key_pem = key.private_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PrivateFormat.TraditionalOpenSSL,
            encryption_algorithm=serialization.NoEncryption(),
        ).decode()
        return csr_pem, key_pem

    def claim(self, mac_addr=None, capabilities=None):
        """Run a full assisted claim: initiate, then verify with a fresh CSR.

        Generates the device key pair locally (the private key never leaves the
        client, as a real device's would not). Returns a dict with node_id,
        certificate, ca_certificate, and private_key on success, or raises
        RuntimeError with the failing response's status and body.
        """
        mac_addr = mac_addr or ("AA" + uuid.uuid4().hex[:10].upper())

        init = self.claim_initiate(mac_addr)
        if init.status_code not in (200, 201):
            raise RuntimeError(f"claim initiate failed ({init.status_code}): {init.text}")
        node_id = init.json()["node_id"]

        csr_pem, key_pem = self.generate_claim_csr()
        resp = self.claim_verify(mac_addr, csr_pem, capabilities=capabilities)
        if resp.status_code != 201:
            raise RuntimeError(f"claim verify failed ({resp.status_code}): {resp.text}")
        body = resp.json()

        return {
            "node_id": node_id,
            "mac_addr": mac_addr,
            "certificate": body["certificate"],
            "ca_certificate": body["ca_certificate"],
            "private_key": key_pem,
        }

    # --- Assisted-claiming CA administration (superadmin only, §3.9) ---

    def claim_admin_set_config(self, config, skip_cors_check=False):
        """POST /v1/admin/claiming/config — set the certificate configuration."""
        return self.make_api_request('POST', '/v1/admin/claiming/config', data=json.dumps(config),
                                     skip_cors_check=skip_cors_check)

    def claim_admin_get_config(self):
        """GET /v1/admin/claiming/config — read the configuration and CA status."""
        return self.make_api_request('GET', '/v1/admin/claiming/config')

    def claim_admin_mint_ca(self, force=False, skip_cors_check=False):
        """POST /v1/admin/claiming/ca — mint the CA (mint-once); force rotates."""
        return self.make_api_request('POST', '/v1/admin/claiming/ca',
                                     data=json.dumps({"force": True} if force else {}),
                                     skip_cors_check=skip_cors_check)

    def claim_admin_get_ca(self):
        """GET /v1/admin/claiming/ca — return the published CA certificate."""
        return self.make_api_request('GET', '/v1/admin/claiming/ca')

    def assume_role(self):

        response = self.make_api_request('POST', '/v1/assumed-roles')
        if response.status_code not in (200, 201):
            user_log(f"Assume Role failed. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None

        assumed_credentials = json.loads(response.text)

        return assumed_credentials

    def assume_role_admin(self, group_id, subgroup_id=None):
        """Assume role with admin privileges for a specific group/subgroup.

        This is only available for super admin users.

        Args:
            group_id (str): The group ID to get access to
            subgroup_id (str, optional): The subgroup ID to get access to (for more restrictive access)

        Returns:
            dict: Assumed credentials with access to the specified group/subgroup, or None if failed
        """
        # Target group/subgroup are carried as path params, mirroring the other
        # group/subgroup APIs.
        if subgroup_id:
            path = f"/v1/groups/{group_id}/subgroups/{subgroup_id}/assumed-roles"
        else:
            path = f"/v1/groups/{group_id}/assumed-roles"

        response = self.make_api_request('POST', path)
        if response.status_code not in (200, 201):
            user_log(f"Admin Assume Role failed. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None

        assumed_credentials = json.loads(response.text)
        user_log(f"Admin assumed role successfully for group: {group_id}" + (f", subgroup: {subgroup_id}" if subgroup_id else ""))

        return assumed_credentials

    def get_s3_client(self, group_id, node_id):
        """Get a boto3 S3 client using assume-role credentials scoped to one node.

        Args:
            group_id (str): The group the node belongs to.
            node_id (str): The node ID to scope S3 access to.

        Returns:
            boto3.client: An S3 client configured with assumed-role credentials.
        """
        data = json.dumps({"services": ["s3"]})

        path = f"/v1/groups/{group_id}/nodes/{node_id}/assumed-roles"
        response = self.make_api_request('POST', path, data=data)
        assert response.status_code in (200, 201), f"assume_role failed: {response.status_code} {response.text}"
        assumed = response.json()

        return boto3.client(
            "s3",
            region_name=self.region,
            aws_access_key_id=assumed["access_key"],
            aws_secret_access_key=assumed["secret_key"],
            aws_session_token=assumed["session_token"],
        )

    def admin_get_node_groups(self, node_id):
        """Get the group and subgroups for a node (admin only).

        Args:
            node_id (str): The node ID to look up

        Returns:
            dict: Group info with 'group' and 'sub_groups', or None if failed
        """
        response = self.make_api_request('GET', f'/v1/admin/nodes/{node_id}/groups')
        if response.status_code not in (200, 201):
            user_log(f"Admin get node groups failed. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None
        return response.json()

    def admin_get_iot_event_mode(self):
        """Get the current IoT-rule action mode for presence/publish_input
        (admin only). Returns the parsed body on 200, the raw response on
        non-200, or None on transport errors. Tests that need the status code
        should use admin_get_iot_event_mode_response() instead.

        Returns:
            dict: {"presence": "direct"|"sqs", "publish_input": "direct"|"sqs"} on 200,
                  the raw response object on non-200, or None on transport error.
        """
        response = self.make_api_request('GET', '/v1/admin/iot-event-mode')
        if response.status_code != 200:
            user_log(f"Admin get iot-event-mode failed. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return response
        return response.json()

    def admin_put_iot_event_mode(self, mode):
        """Flip the IoT-rule action mode for both node_disconnected_rule and
        node_to_cloud_rule (admin only).

        Args:
            mode (str): "direct" or "sqs"

        Returns:
            dict: post-flip {"presence","publish_input"} state on 200,
                  the raw response object on non-200.
        """
        body = {"mode": mode}
        response = self.make_api_request('PUT', '/v1/admin/iot-event-mode',
                                         data=json.dumps(body))
        if response.status_code != 200:
            user_log(f"Admin put iot-event-mode failed. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return response
        return response.json()

    def admin_get_node_tags(self, node_id):
        """Get all tags (admin, device, user) for a node (admin only).

        Args:
            node_id (str): The node ID to look up

        Returns:
            dict: Tags with 'admin', 'device', 'user' keys, or None if failed
        """
        response = self.make_api_request('GET', f'/v1/admin/nodes/{node_id}/tags')
        if response.status_code not in (200, 201):
            user_log(f"Admin get node tags failed. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None
        return response.json()

    def admin_put_node_tags(self, node_id, admin_tags=None, user_tags=None):
        """Update admin and/or user tags for a node (admin only).
        Set a tag value to None to delete it.

        Args:
            node_id (str): The node ID
            admin_tags (dict, optional): Admin tags to set/update/delete
            user_tags (dict, optional): User tags to set/update/delete

        Returns:
            dict: Response body, or None if failed
        """
        body = {}
        if admin_tags is not None:
            body["admin"] = admin_tags
        if user_tags is not None:
            body["user"] = user_tags

        response = self.make_api_request('PUT', f'/v1/admin/nodes/{node_id}/tags',
                                        data=json.dumps(body))
        if response.status_code not in (200, 201):
            user_log(f"Admin put node tags failed. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None
        return response.json()

    def get_node_tags(self, group_id, node_id):
        """Get user tags for a node within a group (regular user).

        Args:
            group_id (str): The group ID (user must own this group)
            node_id (str): The node ID

        Returns:
            dict: Tags with 'user' key, or None if failed
        """
        response = self.make_api_request('GET', f'/v1/groups/{group_id}/nodes/{node_id}/tags')
        if response.status_code not in (200, 201):
            user_log(f"Get node tags failed. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None
        return response.json()

    def put_node_tags(self, group_id, node_id, user_tags):
        """Update user tags for a node within a group (regular user).
        Set a tag value to None to delete it.

        Args:
            group_id (str): The group ID (user must own this group)
            node_id (str): The node ID
            user_tags (dict): User tags to set/update/delete

        Returns:
            dict: Response body, or None if failed
        """
        body = {"user": user_tags}
        response = self.make_api_request('PUT', f'/v1/groups/{group_id}/nodes/{node_id}/tags',
                                        data=json.dumps(body))
        if response.status_code not in (200, 201):
            user_log(f"Put node tags failed. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None
        return response.json()

    def mqtt_disconnect_and_wait(self):
        self.mqtt_disconnect()
        if self.disconnect_future:
            self.disconnect_future.result()
            self.disconnect_future = None

    def mqtt_disconnect(self):
        if self.mqtt_connection:
            self.disconnect_future = self.mqtt_connection.disconnect()
            self.mqtt_connection = None
            self.shadow_client = None
            self.previous_disconnect_time = time.time()

    def _aws_credentials_delegate(self):
        if not self.mqtt_credentials:
            raise Exception("MQTT credentials not found")
        return auth.AwsCredentials(
            self.mqtt_credentials['access_key'],
            self.mqtt_credentials['secret_key'],
            self.mqtt_credentials['session_token']
        )

    def mqtt_refresh_credentials(self):
        self.mqtt_credentials = self.assume_role()
        if not self.mqtt_credentials:
            user_log("Failed to assume role for MQTT credentials.")
            return False
        return True

    def mqtt_connect(self, credentials=None):
        if self.disconnect_future:
            self.disconnect_future.result()
            self.disconnect_future = None

        if time.time() - self.previous_disconnect_time < 6:
            user_log(f"Waiting for 6 seconds before reconnecting")
            time.sleep(6)

        self.previous_disconnect_time = 0
        if self.mqtt_connection:
            # Clear the shadow queue to remove any stale data from previous sessions
            while not self.shadow_queue.empty():
                try:
                    self.shadow_queue.get_nowait()
                except queue.Empty:
                    break
            user_log("MQTT connection already established.")
            return True

        if credentials:
            self.mqtt_credentials = credentials
        elif not self.mqtt_refresh_credentials():
            user_log("Failed to assume role for MQTT credentials.")
            return False

        credentials_provider = auth.AwsCredentialsProvider.new_delegate(self._aws_credentials_delegate)

        def on_connection_interrupted(connection, error, **kwargs):
            user_log(f"Connection interrupted. error: {error}")
            # Ensure the connection queue gets updated even if there's an error
            try:
                self.connection_queue.put("interrupted")
            except Exception as e:
                user_log(f"Error updating connection queue: {e}")

            if self.disable_reconnect:
                user_log("Reconnection disabled, disconnecting MQTT")
                self.mqtt_disconnect()
        def on_connection_resumed(connection, return_code, session_present, **kwargs):
            user_log(f"Connection resumed. return_code: {return_code} session_present: {session_present}")
            try:
                self.connection_queue.put("resumed")
            except Exception as e:
                user_log(f"Error updating connection queue: {e}")

        mqtt_connection = mqtt_connection_builder.websockets_with_default_aws_signing(
            endpoint=self.iot_endpoint,
            region=self.region,
            credentials_provider=credentials_provider,
            http_proxy_options=None,
            on_connection_interrupted=on_connection_interrupted,
            on_connection_resumed=on_connection_resumed,
            client_id=f"user:{self.username}:{self.mqtt_session_suffix}",
            clean_session=False,
            keep_alive_secs=30,
        )

        user_log(f"Connecting to {self.iot_endpoint} with client ID 'user:{self.username}:{self.mqtt_session_suffix}'...")
        connect_future = mqtt_connection.connect()
        connect_future.result()
        user_log("Connected!")

        self.mqtt_connection = mqtt_connection
        self.shadow_client = iotshadow.IotShadowClient(mqtt_connection)

        # Clear the shadow queue to remove any stale data from previous sessions
        while not self.shadow_queue.empty():
            try:
                self.shadow_queue.get_nowait()
            except queue.Empty:
                break
        return True

    def _manage_subscriptions_to_named_shadows(self, thing_name, named_shadows, subscribe=True):
        if not self.mqtt_connection:
            user_log("MQTT not connected. Attempting to connect...")
            if not self.mqtt_connect():
                user_log("Failed to establish MQTT connection.")
                return False

        if not named_shadows or len(named_shadows) == 0:
            user_log("Error: At least one named shadow must be specified.")
            return False

        # Subscribe to named shadow events
        success = True
        last_error = None

        for shadow_name in named_shadows:
            try:
                self._manage_subscriptions_to_named_shadow_events(thing_name, shadow_name, subscribe=subscribe)
            except Exception as e:
                user_log(f"Error subscribing to shadow {shadow_name}: {e}")
                success = False
                last_error = e
                break

        if success:
            user_log(f"{'Subscribed' if subscribe else 'Unsubscribed'} to named shadows for thing '{thing_name}': {', '.join(named_shadows)}")
            return True
        else:
            # Propagate the exception so tests expecting exceptions will pass
            if last_error:
                raise last_error
            return False

    def _manage_subscriptions_to_named_shadow_events(self, thing_name, shadow_name, subscribe=True):
        user_log(f"{'Subscribing' if subscribe else 'Unsubscribing'} to named shadow events for thing '{thing_name}', shadow '{shadow_name}'...")

        try:
            # Subscribe to shadow update accepted topic
            update_topic = f"$aws/things/{thing_name}/shadow/name/{shadow_name}/update/accepted"
            if subscribe:
                update_future, _ = self.mqtt_connection.subscribe(
                    topic=update_topic,
                    qos=mqtt.QoS.AT_LEAST_ONCE,
                    callback=lambda topic, payload, **kwargs: self.on_named_shadow_updated(shadow_name, {"topic": topic, "state": json.loads(payload)})
                )
            else:
                update_future, _ = self.mqtt_connection.unsubscribe(
                    topic=update_topic
                )

            # Subscribe to shadow get accepted topic
            get_topic = f"$aws/things/{thing_name}/shadow/name/{shadow_name}/get/accepted"
            if subscribe:
                get_future, _ = self.mqtt_connection.subscribe(
                    topic=get_topic,
                    qos=mqtt.QoS.AT_LEAST_ONCE,
                    callback=lambda topic, payload, **kwargs: self.on_named_shadow_updated(shadow_name, {"topic": topic, "state": json.loads(payload)})
                )
            else:
                get_future, _ = self.mqtt_connection.unsubscribe(
                    topic=get_topic
                )

            # Subscribe to shadow delta topic
            delta_topic = f"$aws/things/{thing_name}/shadow/name/{shadow_name}/update/delta"
            if subscribe:
                delta_future, _ = self.mqtt_connection.subscribe(
                    topic=delta_topic,
                    qos=mqtt.QoS.AT_LEAST_ONCE,
                    callback=lambda topic, payload, **kwargs: self.on_named_shadow_delta_updated(shadow_name, json.loads(payload))
                )
            else:
                delta_future, _ = self.mqtt_connection.unsubscribe(
                    topic=delta_topic
                )

            # Wait for all subscriptions to succeed
            update_future.result()
            get_future.result()
            delta_future.result()

            user_log(f"{'Subscribed' if subscribe else 'Unsubscribed'} to named shadow events for '{shadow_name}' successfully")
        except Exception as e:
            user_log(f"Failed to {'subscribe' if subscribe else 'unsubscribe'} to shadow events: {e}")
            # This exception will be caught by subscribe_to_named_shadows method
            raise

    def subscribe_to_named_shadows(self, thing_name, named_shadows):
        return self._manage_subscriptions_to_named_shadows(thing_name, named_shadows, subscribe=True)

    def unsubscribe_from_named_shadows(self, thing_name, named_shadows):
        return self._manage_subscriptions_to_named_shadows(thing_name, named_shadows, subscribe=False)

    def on_named_shadow_updated(self, shadow_name, payload):
        # Payload is a dict with keys 'topic' and 'state'
        shadow_state = payload["state"]

        shadow_state = shadow_to_unstructured(shadow_state)
        # Check if this is a shadow document with state
        if "state" in shadow_state:
            # Extract reported and desired states if they exist
            if "reported" in shadow_state["state"]:
                reported = shadow_state["state"]["reported"]
                version = shadow_state.get("version", "?")
                user_log(f"[{shadow_name}][v{version}][reported] {blue}updated to{reset} {reported}")

            if "desired" in shadow_state["state"]:
                desired = shadow_state["state"]["desired"]
                version = shadow_state.get("version", "?")
                user_log(f"[{shadow_name}][v{version}][desired] {blue}updated to{reset} {desired}")

            # Add shadow state to the queue for tests to process
            try:
                self.shadow_queue.put(shadow_state)
            except Exception as e:
                user_log(f"Error adding shadow state to queue: {e}")

    def on_named_shadow_delta_updated(self, shadow_name, payload):
        user_log(f"Named shadow delta updated for '{shadow_name}':")
        if 'version' in payload:
            user_log(f"Version: {payload['version']}")
        if 'state' in payload:
            user_log("Delta State:")
            user_log(payload['state'])
        if 'metadata' in payload:
            user_log("Delta Metadata:")
            user_log(payload['metadata'])

    def mqtt_publish(self, thing_name, data, shadow_name=None):
        if not self.mqtt_connection:
            user_log("Error: MQTT not connected. Call mqtt_connect first.")
            return False

        try:
            # Parse the JSON string into a dictionary if it's a string
            state = data if isinstance(data, dict) else json.loads(data)
            user_log(f"Updating {'named ' if shadow_name else ''}shadow state for thing '{thing_name}': {state}")

            if shadow_name:
                topic = f"$aws/things/{thing_name}/shadow/name/{shadow_name}/update"
                payload = json.dumps({
                    "state": {
                        "reported": state
                    }
                })
            else:
                topic = f"$aws/things/{thing_name}/shadow/update"
                payload = json.dumps({
                    "state": {
                        "reported": state
                    }
                })

            future, _ = self.mqtt_connection.publish(
                topic=topic,
                payload=payload,
                qos=mqtt.QoS.AT_LEAST_ONCE
            )
            future.result()
            user_log(f"{'Named shadow' if shadow_name else 'Shadow'} state updated successfully")
            return True
        except json.JSONDecodeError:
            user_log("Error: Invalid JSON data")
            return False
        except Exception as e:
            user_log(f"Error updating shadow: {str(e)}")
            return False

    def add_group_id(self, group_id):
        if group_id not in self.group_ids:
            self.group_ids.append(group_id)

    def get_group_ids(self):
        return self.group_ids

    def add_device(self, device):
        self.devices.append(device)

    def get_devices(self):
        return self.devices

    def clear_group_ids(self):
        self.group_ids = []

    def mqtt_publish_to_topic(self, thing_name, topic_name, data):
        if not self.mqtt_connection:
            user_log("Error: MQTT not connected. Call mqtt_connect() first.")
            return False

        try:
            full_topic = f"rainmaker/nodes/{thing_name}/user/{topic_name}"
            user_log(f"Publishing to topic '{full_topic}' for thing '{thing_name}'")
            # data is already a Python object, so we just need to convert it to a JSON string
            message_json = json.dumps(data)
            future, _ = self.mqtt_connection.publish(
                topic=full_topic,
                payload=message_json,
                qos=mqtt.QoS.AT_LEAST_ONCE
            )
            future.result()  # Wait for the publish operation to complete
            user_log(f"Published message to topic '{full_topic}': {message_json}")
            return True
        except Exception as e:
            user_log(f"Error publishing message: {str(e)}")
            return False

    def mqtt_publish_to_group_control(self, group_id, data, subgroup_id=None):
        """Publish to a device-type-addressed group/subgroup control topic.

        Builds topic:
          rainmaker/nodes/groups/<group_id>/control                          (group)
          rainmaker/nodes/groups/<group_id>/subgroups/<subgroup_id>/control  (subgroup)

        Payload is keyed by device type, e.g.
        {"esp.device.light": {"params": {"esp.param.power": True}}}.
        """
        if not self.mqtt_connection:
            user_log("Error: MQTT not connected. Call mqtt_connect() first.")
            return False

        if subgroup_id is None:
            full_topic = f"rainmaker/nodes/groups/{group_id}/control"
        else:
            full_topic = f"rainmaker/nodes/groups/{group_id}/subgroups/{subgroup_id}/control"

        try:
            user_log(f"Publishing to group devtype-control topic '{full_topic}'")
            message_json = json.dumps(data)
            future, _ = self.mqtt_connection.publish(
                topic=full_topic,
                payload=message_json,
                qos=mqtt.QoS.AT_LEAST_ONCE
            )
            future.result()
            user_log(f"Published message to group devtype-control topic '{full_topic}': {message_json}")
            return True
        except Exception as e:
            user_log(f"Error publishing group devtype-control message: {str(e)}")
            return False

    def read_shadow(self, thing_name, shadow_name):
        if not self.mqtt_connection:
            user_log("Error: MQTT not connected. Call mqtt_connect() first.")
            return False

        topic = f"$aws/things/{thing_name}/shadow/name/{shadow_name}/get"
        message = "{}"

        # Clear shadow queue before requesting to avoid stale data
        while not self.shadow_queue.empty():
            try:
                self.shadow_queue.get_nowait()
            except queue.Empty:
                break

        try:
            future, _ = self.mqtt_connection.publish(
                topic=topic,
                payload=message,
                qos=mqtt.QoS.AT_LEAST_ONCE
            )
            future.result()  # Wait for the publish operation to complete
            user_log(f"Published shadow get request to topic '{topic}'")
            return True
        except Exception as e:
            user_log(f"Error publishing to shadow get topic: {str(e)}")
            return False

    # Map of legacy SNS platform names → public swagger integration_type. Callers may pass either casing; we normalise to lowercase and look up here. Unknown values (e.g. "alexa", "MOCK_APNS_cafebabe") pass through unchanged.
    _PUSH_TYPE_PREFIX = {
        "apns": "apns",
        "apns_sandbox": "apns_sandbox",
        "gcm": "gcm",
    }

    def _integration_id_for(self, platform_type, platform_app_name=None):
        """Build the public integration_id from a (platform_type, platform_app_name) pair."""
        prefix = self._PUSH_TYPE_PREFIX.get(platform_type.lower(), platform_type)
        return f"{prefix}_{platform_app_name}" if platform_app_name else prefix


    def _delivery_credentials_for(self, mobile_device_token):
        """Build the delivery_credentials body block from a legacy mobile_device_token value.

        If the value JSON-parses to an OAuth bundle ({access_token, refresh_token, expires_at}),
        spread those fields. Otherwise treat it as a raw app_token.
        """
        try:
            parsed = json.loads(mobile_device_token)
            if isinstance(parsed, dict) and (
                "access_token" in parsed or "refresh_token" in parsed
            ):
                bundle = {}
                for k in ("access_token", "refresh_token", "expires_at"):
                    if k in parsed:
                        bundle[k] = parsed[k]
                return bundle
        except (ValueError, TypeError):
            pass
        return {"app_token": mobile_device_token}

    def register_client(self, platform_type, mobile_device_token, platform_app_name=None, locale=None):
        """Register an endpoint via PUT /v1/integrations/{integrationId}/endpoints. PUT is idempotent: re-calling with the same body is a no-op; different credentials replace the stored ones. Returns the endpoint_id from the response, or None on failure. user_id is unrelated to this endpoint and is fetched lazily via the user_id property (GET /v1/users/me)."""
        integration_id = self._integration_id_for(platform_type, platform_app_name)
        path = f"/v1/integrations/{integration_id}/endpoints"
        data = {"delivery_credentials": self._delivery_credentials_for(mobile_device_token)}
        if locale:
            data["locale"] = locale

        response = self.make_api_request('PUT', path, data=json.dumps(data))

        if response.status_code in (200, 201):
            return json.loads(response.text).get('endpoint_id')
        else:
            user_log(f"Failed to register client. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None

    def unregister_client(self, platform_type, endpoint_id, platform_app_name=None):
        """Unregister an endpoint via DELETE /v1/integrations/{integrationId}/endpoints/{endpointId}. Callers must pass the endpoint_id returned by register_client — multiple endpoints may exist per (user_id, integration_id), so the endpoint_id is required to identify which one to drop.

        Returns the raw response so callers can assert on status and body."""
        integration_id = self._integration_id_for(platform_type, platform_app_name)
        path = f"/v1/integrations/{integration_id}/endpoints/{endpoint_id}"

        response = self.make_api_request('DELETE', path)

        if not response.ok:
            user_log(f"Failed to unregister client. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")

        return response

    def generate_mobile_device_token(self):
        return ''.join(random.choices(string.ascii_uppercase + string.digits, key=16))

    def get_sharing_requests(self):
        response = self.make_api_request('GET', '/v1/sharing-requests/received')
        if response.status_code in (200, 201):
            return json.loads(response.text).get('sharing_requests', [])
        else:
            user_log(f"Failed to get sharing requests. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None

    def process_sharing_request(self, sharing_request_id, action):
        user_log(f"Processing sharing request {sharing_request_id} with action {action}")
        if action not in ['accept', 'reject']:
            user_log(f"Invalid action: {action}. Must be 'accept' or 'reject'.")
            return False

        path = f'/v1/sharing-requests/{sharing_request_id}/{action}'
        response = self.make_api_request('POST', path)

        if response.status_code in (200, 201):
            user_log(f"Successfully {action}ed sharing request: {sharing_request_id}")
            return True
        else:
            user_log(f"Failed to {action} sharing request. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return False

    def accept_sharing_request(self, sharing_request_id):
        return self.process_sharing_request(sharing_request_id, 'accept')

    def reject_sharing_request(self, sharing_request_id):
        return self.process_sharing_request(sharing_request_id, 'reject')

    def read_shadow_queue(self, timeout=5):
        """Get shadow update from queue with timeout"""
        try:
            return self.shadow_queue.get(timeout=timeout)
        except queue.Empty:
            return None

    def read_connection_queue(self, timeout=5):
        """Get connection status from queue with timeout"""
        try:
            return self.connection_queue.get(timeout=timeout)
        except queue.Empty:
            return None

    def get_node_config(self, group_id, subgroup_id, node_id):
        """Get node configuration through the group API.

        Args:
            group_id (str): ID of the group containing the node
            subgroup_id (str): ID of the subgroup containing the node
            node_id (str): ID of the node

        Returns:
            dict: Node configuration if successful, None if failed
        """
        url = f'/v1/groups/{group_id}/nodes/{node_id}/config'
        response = self.make_api_request('GET', url)
        if response.status_code not in (200, 201):
            user_log(f"Failed to get node config. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None

        try:
            return json.loads(response.text)
        except json.JSONDecodeError:
            user_log("Error: Invalid JSON response")
            return None

    def put_node_config(self, group_id, node_id, config):
        """Write node config (pure Matter nodes only); returns the raw response."""
        url = f'/v1/groups/{group_id}/nodes/{node_id}/config'
        return self.make_api_request('PUT', url, data=json.dumps(config))

    def get_node_schedule(self, group_id, subgroup_id, node_id):
        """Get schedule for a node through the group API.

        Args:
            group_id (str): ID of the group containing the node
            subgroup_id (str): ID of the subgroup containing the node
            node_id (str): ID of the node

        Returns:
            dict: Schedule data if successful, None if failed
        """
        url = f'/v1/groups/{group_id}/nodes/{node_id}/schedules'
        response = self.make_api_request('GET', url)
        if response.status_code not in (200, 201):
            user_log(f"Failed to get node schedule. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None

        try:
            return json.loads(response.text)
        except json.JSONDecodeError:
            user_log("Error: Invalid JSON response")
            return None

    def set_node_schedule(self, group_id, subgroup_id, node_id, schedule_data):
        """Set schedule for a node through the group API.

        Args:
            group_id (str): ID of the group containing the node
            subgroup_id (str): ID of the subgroup containing the node
            node_id (str): ID of the node
            schedule_data (dict): Schedule data to set

        Returns:
            bool: True if successful, False otherwise
        """
        # Always use the root group URL pattern for setting schedules
        url = f'/v1/groups/{group_id}/nodes/{node_id}/schedules'

        response = self.make_api_request('PUT', url, json.dumps(schedule_data))
        if response.status_code not in (200, 201):
            user_log(f"Failed to set node schedule. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return False

        return True

    def alexa_set_lambda_arn(self, lambda_arn):
        self.alexa_lambda_arn = lambda_arn

    def alexa_discover_devices(self, region=None):
        # region: optional AWS region for the Lambda client (e.g. us-east-1 for rmng-alexa-core). If None, uses self.region.
        # Create discovery request payload
        discovery_request = {
            "directive": {
                "header": {
                    "namespace": "Alexa.Discovery",
                    "name": "Discover",
                    "payloadVersion": "3",
                    "messageId": str(uuid.uuid4())
                },
                "payload": {
                    "scope": {
                        "type": "BearerToken",
                        "token": self.access_token #This has the normal pool token, it will be va client token in case of Alexa
                    }
                }
            }
        }

        invoke_region = region if region is not None else self.region
        lambda_client = boto3.client('lambda', region_name=invoke_region)
        response = lambda_client.invoke(
            FunctionName=self.alexa_lambda_arn,
            InvocationType='RequestResponse',
            Payload=json.dumps(discovery_request)
        )

        # Parse and validate response
        discovery_response = json.loads(response['Payload'].read())
        return discovery_response

    '''
    device_thing_name: nodeId
    capability: capability to control
    action: action to perform
    device_name: name of the device endpoint
    group_id: group id of the device
    '''
    def alexa_control_device(self, device_thing_name, capability, action, cookie, payload=None, region=None):
        # region: optional AWS region for the Lambda client. If None, uses self.region.
        # Create control request payload
        control_request = {
            "directive": {
                "header": {
                    "namespace": capability,
                    "name": action,
                    "payloadVersion": "3",
                    "messageId": str(uuid.uuid4()),
                    "correlationToken": str(uuid.uuid4())
                },
                "endpoint": {
                    "endpointId": device_thing_name,
                    "scope": {
                        "type": "BearerToken",
                        "token": self.access_token
                    },
                    "cookie": cookie
                }
            }
        }
        if payload:
            control_request['directive']['payload'] = payload

        invoke_region = region if region is not None else self.region
        lambda_client = boto3.client('lambda', region_name=invoke_region)
        response = lambda_client.invoke(
            FunctionName=self.alexa_lambda_arn,
            InvocationType='RequestResponse',
            Payload=json.dumps(control_request)
        )

        # Parse and validate response
        control_response = json.loads(response['Payload'].read())
        return control_response

    def alexa_post_configuration(self, redirect_uris, client_id, client_secret, skill_id, manufacturer_name=None):
        """POST /v1/admin/integrations/alexa/configuration to store Alexa config.

        manufacturer_name is optional: pass None to leave the stored brand untouched, or a
        string (including "") to set it.
        """
        params = {'redirect_uris': redirect_uris, 'client_id': client_id, 'client_secret': client_secret, 'skill_id': skill_id}
        missing = [k for k, v in params.items() if not v]
        if missing:
            raise ValueError(f"Missing required fields: {', '.join(missing)}")

        cfg_data = {
            "redirect_uris": redirect_uris,
            "client_id": client_id,
            "client_secret": client_secret,
            "skill_id": skill_id
        }
        if manufacturer_name is not None:
            cfg_data["manufacturer_name"] = manufacturer_name
        response = self.make_api_request('POST', '/v1/admin/integrations/alexa/configuration', data=json.dumps(cfg_data))
        return response

    def alexa_get_configuration(self):
        """GET /v1/admin/integrations/alexa/configuration to retrieve Alexa config."""
        response = self.make_api_request('GET', '/v1/admin/integrations/alexa/configuration')
        return response

    # GVA (Google Voice Assistant) methods
    def _gva_request(self, intent, payload=None):
        """Send a GVA fulfillment request via the HTTP API.

        The GVA endpoint uses authorization_type=NONE on API Gateway
        (Google calls it directly), so we send the user's access token
        as a Bearer token instead of using SigV4.
        """
        request_body = {
            "requestId": str(uuid.uuid4()),
            "inputs": [
                {
                    "intent": intent,
                    "payload": payload or {}
                }
            ]
        }
        url = f"{self.api_gateway_url}/v1/integrations/gva"
        headers = {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {self.access_token}",
        }
        response = self.session.request(
            'POST', url,
            headers=headers,
            data=json.dumps(request_body))
        if response.status_code in (200, 201):
            return response.json()
        else:
            raise Exception(
                f"GVA {intent} failed: {response.text}")


    def gva_discover_devices(self):
        """Discover devices through GVA fulfillment endpoint."""
        return self._gva_request("action.devices.SYNC")

    def gva_query_device(self, device_id, custom_data):
        """Query device state through GVA fulfillment endpoint."""
        return self._gva_request("action.devices.QUERY", {
            "devices": [
                {"id": device_id, "customData": custom_data}
            ]
        })

    def gva_control_device(self, device_id, custom_data,
                           command, params):
        """Control device through GVA fulfillment endpoint."""
        return self._gva_request("action.devices.EXECUTE", {
            "commands": [
                {
                    "devices": [
                        {
                            "id": device_id,
                            "customData": custom_data
                        }
                    ],
                    "execution": [
                        {
                            "command": command,
                            "params": params
                        }
                    ]
                }
            ]
        })

    def gva_disconnect(self):
        """Disconnect from GVA via fulfillment endpoint."""
        return self._gva_request("action.devices.DISCONNECT")

    def gva_post_configuration(self, service_account_json):
        """POST /v1/admin/integrations/gva/configuration to store GVA service account JSON."""
        if isinstance(service_account_json, dict):
            service_account_json = json.dumps(service_account_json)
        response = self.make_api_request('POST', '/v1/admin/integrations/gva/configuration', data=service_account_json)
        return response

    def gva_get_configuration(self):
        """GET /v1/admin/integrations/gva/configuration to retrieve GVA config."""
        response = self.make_api_request('GET', '/v1/admin/integrations/gva/configuration')
        return response

    def st_post_configuration(self, client_id, client_secret):
        """POST /v1/admin/integrations/smartthings/configuration to store the credentials
        SmartThings issued for the Schema App. Also registers the SmartThings OAuth callback
        URLs on the shared va-client OIDC row."""
        payload = json.dumps({'client_id': client_id, 'client_secret': client_secret})
        response = self.make_api_request('POST', '/v1/admin/integrations/smartthings/configuration', data=payload)
        return response

    def st_get_configuration(self):
        """GET /v1/admin/integrations/smartthings/configuration. Returns client_id only;
        the secret is never returned."""
        response = self.make_api_request('GET', '/v1/admin/integrations/smartthings/configuration')
        return response

    def st_delete_configuration(self):
        """DELETE /v1/admin/integrations/smartthings/configuration."""
        response = self.make_api_request('DELETE', '/v1/admin/integrations/smartthings/configuration')
        return response

    def st_set_lambda_arn(self, lambda_arn):
        """Set the default Schema App Lambda ARN for the st_* request helpers."""
        self.st_lambda_arn = lambda_arn

    def _st_invoke(self, request_body, lambda_arn=None, region=None):
        """Invoke the Schema App Lambda with this user's ARN and region defaults."""
        arn = lambda_arn if lambda_arn is not None else getattr(self, 'st_lambda_arn', None)
        if not arn:
            raise Exception("SmartThings Schema App Lambda ARN is not set. Call st_set_lambda_arn() or pass lambda_arn.")
        return smartthings.invoke_schema_app(
            request_body, arn, region if region is not None else self.region)

    def st_discover_devices(self, lambda_arn=None, region=None):
        """Send a discoveryRequest with this user's access token."""
        request_body = smartthings.discovery_request(self.access_token)
        return self._st_invoke(request_body, lambda_arn=lambda_arn, region=region)

    def st_control_devices(self, devices, lambda_arn=None, region=None):
        """Send a commandRequest for several devices.

        devices: list of entries from smartthings.command_device().
        """
        request_body = smartthings.command_request(self.access_token, devices)
        return self._st_invoke(request_body, lambda_arn=lambda_arn, region=region)

    def st_control_device(self, external_device_id, commands, device_cookie=None, lambda_arn=None, region=None):
        """Send a commandRequest for a single device.

        commands: list of {"component", "capability", "command", "arguments"}
        device_cookie: the cookie discovery returned for this device, which SmartThings
            echoes back on every command. Omit it to exercise the node-config fallback.
        """
        device = smartthings.command_device(external_device_id, commands, device_cookie)
        return self.st_control_devices([device], lambda_arn=lambda_arn, region=region)

    def st_state_refresh(self, external_device_ids, lambda_arn=None, region=None):
        """Send a stateRefreshRequest for the given device ids."""
        request_body = smartthings.state_refresh_request(self.access_token, external_device_ids)
        return self._st_invoke(request_body, lambda_arn=lambda_arn, region=region)

    def get_node_trigger(self, group_id, node_id):
        """Get trigger data for a node."""
        # Always use the group-level path regardless of subgroup
        path = f"/v1/groups/{group_id}/nodes/{node_id}/triggers"
        response = self.make_api_request('GET', path)
        if response.status_code in (200, 201):
            return response.json()
        user_log(f"Failed to get node trigger. Status code: {response.status_code}")
        user_log(f"Response: {response.text}")
        return None

    def set_node_trigger(self, group_id, node_id, trigger_data):
        """Set trigger data for a node."""
        # Always use the group-level path regardless of subgroup
        path = f"/v1/groups/{group_id}/nodes/{node_id}/triggers"
        response = self.make_api_request('PUT', path, trigger_data)
        if response.status_code not in (200, 201):
            user_log(f"Failed to set node trigger. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return False
        return True

    def delete_node_trigger(self, group_id, node_id):
        """Delete trigger data for a node."""
        # Always use the group-level path regardless of subgroup
        path = f"/v1/groups/{group_id}/nodes/{node_id}/triggers"
        response = self.make_api_request('DELETE', path)
        if response.status_code not in (200, 201):
            user_log(f"Failed to delete node trigger. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return False
        return True

    # Automation API methods
    def get_automations(self, group_id):
        """Get all automations for a group.

        The API response is wrapped as `{"automations": [...]}`; this helper
        unwraps and returns the inner list so callers can treat it as a list.

        Args:
            group_id (str): ID of the group

        Returns:
            list: List of automations if successful, None if failed
        """
        path = f"/v1/groups/{group_id}/service/automations"
        response = self.make_api_request('GET', path)
        if response.status_code not in (200, 201):
            user_log(f"Failed to get automations. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None

        try:
            body = json.loads(response.text)
        except json.JSONDecodeError:
            user_log("Error: Invalid JSON response")
            return None

        if not isinstance(body, dict) or "automations" not in body:
            user_log(f"Error: Unexpected response shape: {body!r}")
            return None
        return body["automations"]

    def get_automation(self, group_id, automation_id):
        """Get a specific automation by ID.

        Args:
            group_id (str): ID of the group
            automation_id (str): ID of the automation

        Returns:
            dict: Automation data if successful, None if failed
        """
        path = f"/v1/groups/{group_id}/service/automations/{automation_id}"
        response = self.make_api_request('GET', path)
        if response.status_code not in (200, 201):
            user_log(f"Failed to get automation. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None

        try:
            return json.loads(response.text)
        except json.JSONDecodeError:
            user_log("Error: Invalid JSON response")
            return None

    def create_automation(self, group_id, automation_data):
        """Create a new automation.

        Trigger condition IDs use the format ``nodeID~automationID~triggerIndex``.
        Because the automation_id is not known until after the initial POST, this
        method automatically issues a follow-up update that replaces any
        placeholder automation IDs in the conditions with the real one returned
        by the server (the same two-step pattern used in
        ``test_automation_end_to_end_execution``).

        Args:
            group_id (str): ID of the group
            automation_data (dict): Automation data to create

        Returns:
            dict: Response data if successful, None if failed
        """
        path = f"/v1/groups/{group_id}/service/automations"
        response = self.make_api_request('POST', path, json.dumps(automation_data))
        if response.status_code not in (200, 201):
            user_log(f"Failed to create automation. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None

        try:
            result = json.loads(response.text)
        except json.JSONDecodeError:
            user_log("Error: Invalid JSON response")
            return None

        automation_id = result.get("automation_id")
        if not automation_id:
            return result

        # Re-issue a PUT with the real automation_id so that trigger condition
        # IDs (nodeID~automationID~triggerIndex) reference the correct ID.
        updated = self._replace_placeholder_automation_id(automation_data, automation_id)
        if updated:
            update_result = self.update_automation(group_id, automation_id, automation_data)
            if update_result is None:
                user_log("Warning: follow-up update_automation after create failed")

        return result

    @staticmethod
    def _replace_placeholder_automation_id(automation_data, real_id):
        """Replace placeholder automation IDs in condition strings with *real_id*.

        Condition strings look like ``nodeID~placeholderID~triggerIndex``.  If
        any condition contains a ``~`` separator whose middle segment is NOT
        already *real_id*, it is rewritten in-place and the method returns True.
        """
        conditions = automation_data.get("conditions", {})
        changed = False
        for key in ("and", "or"):
            entries = conditions.get(key)
            if not isinstance(entries, list):
                continue
            for i, entry in enumerate(entries):
                if not isinstance(entry, str) or "~" not in entry:
                    continue
                parts = entry.split("~")
                if len(parts) >= 2 and parts[1] != real_id:
                    parts[1] = real_id
                    conditions[key][i] = "~".join(parts)
                    changed = True
        return changed

    def update_automation(self, group_id, automation_id, automation_data):
        """Update an existing automation.

        Args:
            group_id (str): ID of the group
            automation_id (str): ID of the automation to update
            automation_data (dict): Updated automation data

        Returns:
            dict: Response data if successful, None if failed
        """
        path = f"/v1/groups/{group_id}/service/automations/{automation_id}"
        response = self.make_api_request('PUT', path, json.dumps(automation_data))
        if response.status_code not in (200, 201):
            user_log(f"Failed to update automation. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None

        try:
            return json.loads(response.text)
        except json.JSONDecodeError:
            user_log("Error: Invalid JSON response")
            return None

    def delete_automation(self, group_id, automation_id):
        """Delete a specific automation.

        Args:
            group_id (str): ID of the group
            automation_id (str): ID of the automation to delete

        Returns:
            bool: True if successful, False otherwise
        """
        path = f"/v1/groups/{group_id}/service/automations/{automation_id}"
        response = self.make_api_request('DELETE', path)
        if response.status_code not in (200, 201):
            user_log(f"Failed to delete automation. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return False

        return True

    def delete_all_automations(self, group_id):
        """Delete all automations for a group.

        Args:
            group_id (str): ID of the group

        Returns:
            bool: True if successful, False otherwise
        """
        path = f"/v1/groups/{group_id}/service/automations"
        response = self.make_api_request('DELETE', path)
        if response.status_code not in (200, 201):
            user_log(f"Failed to delete all automations. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return False

        return True

    def register_node(self, device, tags=None, admin_group_names=None, admin_parent_group_name=None, capabilities=None):
        """Register a node with the admin API.

        Args:
            device (Device): Device object containing node information
            tags (list, optional): List of tags to apply to the node
            admin_group_names (list, optional): List of admin group names to add the node to
            admin_parent_group_name (str, optional): Parent group name for admin groups
            capabilities (list, optional): Capability strings (e.g.
                ["kvs"], ["s3"]) that drive per-capability policy attachment
                via awsiot.AttachDefaultPolicy.

        Returns:
            bool: True if successful, False otherwise
        """

        # Prepare request body
        request_body = {
            "cert": device.node_cert,
        }

        if device.node_ca_cert:
            request_body["ca_cert"] = device.node_ca_cert

        if tags:
            request_body["tags"] = tags

        if admin_group_names:
            request_body["admin_group_names"] = admin_group_names

        if admin_parent_group_name:
            request_body["admin_parent_group_name"] = admin_parent_group_name

        if capabilities:
            request_body["capabilities"] = capabilities

        try:
            response = self.make_api_request('POST', '/v1/admin/nodes',
                                           data=json.dumps(request_body))

            if response.status_code not in (200, 201):
                user_log(f"Failed to register node. Status code: {response.status_code}")
                user_log(f"Response: {response.text}")
                return False

            response_body = json.loads(response.text)
            if not response_body.get("node_id"):
                user_log(f"Node registration failed: {response_body.get('message', 'Unknown error')}")
                return False

            user_log(f"Node '{device.node_thing_name}' registered successfully")
            user_log(f"Node ID: {response_body['node_id']}")
            return True
        except Exception as e:
            user_log(f"Error registering node: {str(e)}")
            return False

    def register_mobile_platform(self, platform, authentication_key=None, key_id=None, team_id=None, bundle_id=None, api_key=None, apns_sandbox=False):
        """Register a push integration via POST /v1/admin/integrations?integration_type=...

        Args:
            platform (str): "apns" for iOS or "GCM" for Android (GCM). The
                            uppercase legacy name is preserved here for caller
                            compatibility; it is mapped to the public lowercase
                            integration_type (apns / apns_sandbox / gcm).
        """
        platform = platform.lower()
        if platform not in ["apns", "gcm"]:
            user_log(f"Error: Invalid platform '{platform}'. Must be 'apns' or 'gcm'")
            return None

        if platform == "apns" and (not authentication_key or not key_id or not team_id or not bundle_id):
            user_log("Error: Authentication key, key ID, team ID, and bundle ID are required for APNS platform")
            return None

        if platform == "gcm" and not api_key:
            user_log("Error: API key is required for GCM platform")
            return None

        # Map platform to the lowercase integration_type query value.
        if platform == "apns":
            integration_type = "apns_sandbox" if apns_sandbox else "apns"
        else:
            integration_type = "gcm"

        request_data = {}
        if platform == "apns":
            request_data["authentication_key"] = authentication_key
            request_data["key_id"] = key_id
            request_data["team_id"] = team_id
            request_data["bundle_id"] = bundle_id
        else:
            # GCM: the handler embeds the Google service-account fields inline in
            # the request body (no api_key wrapper). api_key is the raw JSON file
            # contents, so parse it and send its fields as the body.
            try:
                request_data = json.loads(api_key)
            except (TypeError, json.JSONDecodeError) as e:
                user_log(f"Error: GCM api_key must be the service-account JSON: {e}")
                return None

        try:
            response = self.make_api_request(
                'POST',
                f'/v1/admin/integrations?integration_type={integration_type}',
                data=json.dumps(request_data),
            )

            if response.status_code not in (200, 201):
                user_log(f"Failed to register integration. Status code: {response.status_code}")
                user_log(f"Response: {response.text}")
                return None

            response_data = json.loads(response.text)
            if response_data.get("integration_id"):
                user_log(f"Integration '{integration_type}' registered: {response_data['integration_id']}")
                return response_data
            else:
                user_log(f"Integration registration failed: {response_data}")
                return None
        except Exception as e:
            user_log(f"Error registering integration: {str(e)}")
            return None

    def register_ios_platform(self, authentication_key, key_id, team_id, bundle_id, sandbox=False):
        """Register an iOS APNS platform using token-based authentication.

        Args:
            authentication_key (str): P8 key content for APNS
            key_id (str): Key ID for APNS
            team_id (str): Team ID for APNS
            bundle_id (str): Bundle ID for APNS
            sandbox (bool, optional): True for sandbox environment, False for production

        Returns:
            dict: Response data if successful, None if failed
        """
        return self.register_mobile_platform("apns", authentication_key, key_id, team_id, bundle_id, apns_sandbox=sandbox)

    def register_android_platform(self, json_content):
        """Register an Android GCM/GCM platform.

        Args:
            json_content (str): JSON content of the GCM service account key

        Returns:
            dict: Response data if successful, None if failed
        """
        return self.register_mobile_platform("gcm", api_key=json_content)

    def list_mobile_platforms(self):
        """List all configured integrations via GET /v1/admin/integrations."""
        try:
            response = self.make_api_request('GET', '/v1/admin/integrations')

            if response.status_code not in (200, 201):
                user_log(f"Failed to list integrations. Status code: {response.status_code}")
                user_log(f"Response: {response.text}")
                return None

            response_data = json.loads(response.text)
            integrations = response_data.get("integrations", [])
            if integrations:
                user_log("Configured integrations (use the integration_id with register_client):")
                for item in integrations:
                    user_log(f"  integration_id={item.get('integration_id')}  (type: {item.get('integration_type')})")
            else:
                user_log("No integrations configured")
            return response_data
        except Exception as e:
            user_log(f"Error listing integrations: {str(e)}")
            return None

    def list_public_integrations(self, integration_type=None):
        """List integrations via the non-admin GET /v1/integrations.

        Any authenticated user may call this to discover the
        integration_id / integration_type pairs needed to register a delivery
        endpoint. The response carries only those two public identifiers per
        entry (no credentials). Returns the parsed response dict, or None on
        failure.

        Args:
            integration_type (str, optional): filter to one type
                (apns / apns_sandbox / gcm / ...). Omit for all types.
        """
        path = '/v1/integrations'
        if integration_type:
            path += f'?integration_type={integration_type}'
        try:
            response = self.make_api_request('GET', path)
            if response.status_code != 200:
                user_log(f"Failed to list public integrations. Status code: {response.status_code}")
                user_log(f"Response: {response.text}")
                return None
            return json.loads(response.text)
        except Exception as e:
            user_log(f"Error listing public integrations: {str(e)}")
            return None

    def get_mobile_platform(self, integration_id):
        """Get one integration's admin detail via GET /v1/admin/integrations/{integrationId}.

        Returns (status_code, parsed_body_or_text). Super-admin only — a
        non-admin caller gets 403. Used by tests to assert the admin GET-one
        detail (e.g. bundle_id / project_id) that the public list omits.
        """
        path = f"/v1/admin/integrations/{integration_id}"
        try:
            response = self.make_api_request('GET', path)
            try:
                return response.status_code, json.loads(response.text)
            except (TypeError, json.JSONDecodeError):
                return response.status_code, response.text
        except Exception as e:
            user_log(f"Error getting integration: {str(e)}")
            return None, None

    def update_mobile_platform(self, platform, authentication_key=None, key_id=None, team_id=None, bundle_id=None, api_key=None, apns_sandbox=False, platform_app_name=None):
        """Update a mobile platform's credentials.

        Args:
            platform (str): Platform type - "apns" for iOS or "GCM" for Android
            authentication_key (str, optional): P8 key content for APNS (required for iOS)
            key_id (str, optional): Key ID for APNS (required for iOS)
            team_id (str, optional): Team ID for APNS (required for iOS)
            bundle_id (str, optional): Bundle ID for APNS (required for iOS)
            api_key (str, optional): API key for GCM (required for Android)
            apns_sandbox (bool, optional): True for APNS sandbox, False for production (iOS only)
            platform_app_name (str, optional): For GCM, project ID (required for path if api_key not provided)

        Returns:
            dict: Response data if successful, None if failed
        """
        platform = platform.lower()
        if platform not in ["apns", "gcm"]:
            user_log(f"Error: Invalid platform '{platform}'. Must be 'apns' or 'gcm'")
            return None

        if platform == "apns" and (not authentication_key or not key_id or not team_id or not bundle_id):
            user_log("Error: Authentication key, key ID, team ID, and bundle ID are required for APNS platform")
            return None

        if platform == "gcm" and not api_key:
            user_log("Error: API key is required for GCM platform")
            return None

        # Build appPlatformId: {platform_type}_{platform_name} format
        platform_type = "apns_sandbox" if (platform == "apns" and apns_sandbox) else platform
        if platform == "apns" or platform_type == "apns_sandbox":
            platform_name = bundle_id
        else:
            if platform_app_name:
                platform_name = platform_app_name
            elif api_key:
                try:
                    key_data = json.loads(api_key)
                    platform_name = key_data.get("project_id", "")
                except (json.JSONDecodeError, TypeError):
                    platform_name = ""
            else:
                platform_name = ""
            if not platform_name:
                user_log("Error: For GCM, platform_app_name or api_key with project_id is required")
                return None

        # Map the platform name to the integration_id prefix.
        type_prefix = {
            "apns": "apns",
            "apns_sandbox": "apns_sandbox",
            "gcm": "gcm",
        }[platform_type]
        integration_id = f"{type_prefix}_{platform_name}"
        path = f"/v1/admin/integrations/{integration_id}"
        request_data = {}
        if platform == "apns" or platform_type != "gcm":
            request_data["authentication_key"] = authentication_key
            request_data["key_id"] = key_id
            request_data["team_id"] = team_id
            request_data["bundle_id"] = bundle_id
        else:
            # GCM: send the Google service-account fields inline (no api_key wrapper).
            try:
                request_data = json.loads(api_key)
            except (TypeError, json.JSONDecodeError) as e:
                user_log(f"Error: GCM api_key must be the service-account JSON: {e}")
                return None

        try:
            response = self.make_api_request('PUT', path, data=json.dumps(request_data))

            if response.status_code not in (200, 201):
                user_log(f"Failed to update integration. Status code: {response.status_code}")
                user_log(f"Response: {response.text}")
                return None

            response_data = json.loads(response.text)
            user_log(f"Integration '{integration_id}' updated successfully")
            return response_data
        except Exception as e:
            user_log(f"Error updating integration: {str(e)}")
            return None

    def delete_mobile_platform(self, platform, platform_app_name):
        """Delete a mobile platform.

        Args:
            platform (str): Platform type - "apns", "apns_sandbox" for iOS or "GCM" for Android
            platform_app_name (str): Platform application name (bundle ID for APNS, project ID for GCM)

        Returns:
            dict: Response data if successful, None if failed
        """
        platform = platform.lower()
        if platform not in ["apns", "gcm", "apns_sandbox"]:
            user_log(f"Error: Invalid platform '{platform}'. Must be 'apns', 'apns_sandbox' or 'gcm'")
            return None

        if not platform_app_name:
            user_log("Error: Platform application name is required")
            return None

        try:
            type_prefix = {
                "apns": "apns",
                "apns_sandbox": "apns_sandbox",
                "gcm": "gcm",
            }[platform]
            integration_id = f"{type_prefix}_{platform_app_name}"
            path = f"/v1/admin/integrations/{integration_id}"
            response = self.make_api_request('DELETE', path)

            if response.status_code not in (200, 201):
                user_log(f"Failed to delete integration. Status code: {response.status_code}")
                user_log(f"Response: {response.text}")
                return None

            response_data = json.loads(response.text)
            user_log(f"Integration '{integration_id}' deleted successfully")
            return response_data
        except Exception as e:
            user_log(f"Error deleting integration: {str(e)}")
            return None

    def bulk_register_nodes(self, cert_file_s3_path, admin_group_names=None, admin_parent_group_name=None, tags=None):
        """Bulk register nodes using a cert file in S3.
        Args:
            cert_file_s3_path (str): S3 path to the certificate file (e.g., s3://bucket/path/to/file.csv)
            admin_group_names (list, optional): List of admin group names
            admin_parent_group_name (str, optional): Parent group name for admin groups
            tags (list, optional): List of tags
        Returns:
            dict: Response data if successful, None if failed
        """
        request_body = {
            "cert_file_s3_path": cert_file_s3_path,
        }
        if admin_group_names:
            request_body["admin_group_names"] = admin_group_names
        if admin_parent_group_name:
            request_body["admin_parent_group_name"] = admin_parent_group_name
        if tags:
            request_body["tags"] = tags
        try:
            response = self.make_api_request('POST', '/v1/admin/nodes/registration-jobs', data=json.dumps(request_body))
            if response.status_code != 202:
                user_log(f"Failed to bulk register nodes. Status code: {response.status_code}")
                user_log(f"Response: {response.text}")
                return None
            response_body = json.loads(response.text)
            if not response_body.get("request_id"):
                user_log(f"Bulk node registration failed: {response_body.get('message', 'Unknown error')}")
                return None
            user_log(f"Bulk node registration request created. Request ID: {response_body.get('request_id')}")
            return response_body
        except Exception as e:
            user_log(f"Error in bulk node registration: {str(e)}")
            return None

    def get_bulk_register_status(self, request_id):
        """Get the status of a bulk node registration request.
        Args:
            request_id (str): The request ID returned by bulk_register_nodes
        Returns:
            dict: Status response if successful, None if failed
        """
        try:
            response = self.make_api_request('GET', f'/v1/admin/nodes/registration-jobs/{request_id}')
            if response.status_code not in (200, 201):
                user_log(f"Failed to get bulk register status. Status code: {response.status_code}")
                user_log(f"Response: {response.text}")
                return None
            response_body = json.loads(response.text)
            user_log(f"Bulk register status for request {request_id}: {response_body}")
            return response_body
        except Exception as e:
            user_log(f"Error getting bulk register status: {str(e)}")
            return None

    def list_registration_jobs(self, page_size=None, start_key=None):
        """List bulk node registration jobs.
        Args:
            page_size (int, optional): Maximum number of jobs per page
            start_key (str, optional): Pagination token from a previous response
        Returns:
            dict: Response with 'jobs' list and optional 'next_key', or None if failed
        """
        try:
            params = {}
            if page_size is not None:
                params["page_size"] = str(page_size)
            if start_key is not None:
                params["start_key"] = start_key
            query_string = "&".join(f"{k}={v}" for k, v in params.items())
            path = "/v1/admin/nodes/registration-jobs"
            if query_string:
                path = f"{path}?{query_string}"
            response = self.make_api_request('GET', path)
            if response.status_code not in (200, 201):
                user_log(f"Failed to list registration jobs. Status code: {response.status_code}")
                user_log(f"Response: {response.text}")
                return None
            response_body = json.loads(response.text)
            user_log(f"List registration jobs: {len(response_body.get('jobs', []))} jobs returned")
            return response_body
        except Exception as e:
            user_log(f"Error listing registration jobs: {str(e)}")
            return None

    def bulk_update_nodes(self, cert_file_s3_path, admin_group_names=None, admin_parent_group_name=None, tags=None):
        """Bulk update existing nodes using an input CSV in S3.

        The CSV's certs column is optional per row: empty leaves the cert
        binding alone, non-empty replaces the registered cert (cloud-side
        cert update -- not pushed to the device).
        """
        request_body = {
            "cert_file_s3_path": cert_file_s3_path,
        }
        if admin_group_names:
            request_body["admin_group_names"] = admin_group_names
        if admin_parent_group_name:
            request_body["admin_parent_group_name"] = admin_parent_group_name
        if tags:
            request_body["tags"] = tags
        try:
            response = self.make_api_request('POST', '/v1/admin/nodes/update-jobs', data=json.dumps(request_body))
            if response.status_code != 202:
                user_log(f"Failed to bulk update nodes. Status code: {response.status_code}")
                user_log(f"Response: {response.text}")
                return None
            response_body = json.loads(response.text)
            if response_body.get("status") != "success":
                user_log(f"Bulk node update failed: {response_body.get('message', 'Unknown error')}")
                return None
            user_log(f"Bulk node update request created. Request ID: {response_body.get('request_id')}")
            return response_body
        except Exception as e:
            user_log(f"Error in bulk node update: {str(e)}")
            return None

    def get_bulk_update_status(self, request_id):
        """Get the status of a bulk node update request."""
        try:
            response = self.make_api_request('GET', f'/v1/admin/nodes/update-jobs/{request_id}')
            if response.status_code != 200:
                user_log(f"Failed to get bulk update status. Status code: {response.status_code}")
                user_log(f"Response: {response.text}")
                return None
            response_body = json.loads(response.text)
            user_log(f"Bulk update status for request {request_id}: {response_body}")
            return response_body
        except Exception as e:
            user_log(f"Error getting bulk update status: {str(e)}")
            return None

    def list_failed_nodes(self, request_id, job_type='register', page_size=None, start_key=None):
        """List failed-node detail for a bulk job.

        job_type selects which Lambda's endpoint to hit ('register' or 'update');
        the failures table is shared but each Lambda only surfaces its own jobs.
        """
        if job_type not in ('register', 'update'):
            user_log(f"Invalid job_type: {job_type}")
            return None
        try:
            params = {}
            if page_size is not None:
                params["page_size"] = str(page_size)
            if start_key is not None:
                params["start_key"] = start_key
            query_string = "&".join(f"{k}={v}" for k, v in params.items())
            jobs_segment = "registration-jobs" if job_type == "register" else "update-jobs"
            path = f"/v1/admin/nodes/{jobs_segment}/{request_id}/failed-nodes"
            if query_string:
                path = f"{path}?{query_string}"
            response = self.make_api_request('GET', path)
            if response.status_code != 200:
                user_log(f"Failed to list failed nodes. Status code: {response.status_code}")
                user_log(f"Response: {response.text}")
                return None
            response_body = json.loads(response.text)
            user_log(f"Failed nodes for request {request_id}: {len(response_body.get('failed_nodes', []))} entries")
            return response_body
        except Exception as e:
            user_log(f"Error listing failed nodes: {str(e)}")
            return None

    def get_timeseries_data(self, group_id, node_id, key, data_type, start_time=None, end_time=None, page_size=None, start_key=None, type=None, window=None, date=None, start_date=None, end_date=None):
        """Get timeseries data for a specific parameter.

        Args:
            group_id (str): Group ID
            node_id (str): Node ID
            key (str): Data point key
            data_type (str): Data type (int, float, bool, string)
            start_time (int): Start timestamp (optional)
            end_time (int): End timestamp (optional)
            page_size (int): Maximum number of records per page (optional)
            start_key (str): Pagination token from a previous response's next_key (optional)
            type (str): Data type - raw (default), latest, aggregates (optional)
            window (str): Specific window for aggregates (hourly, daily, weekly, monthly)
            date (str): Specific date for historical aggregates (YYYY-MM-DD format)
            start_date (str): Start date for historical aggregates date range (YYYY-MM-DD format)
            end_date (str): End date for historical aggregates date range (YYYY-MM-DD format)

        Returns:
            dict: API response containing timeseries data with pagination metadata or aggregates
        """
        params = {
            'key': key,
            'data_type': data_type
        }

        if start_time is not None:
            params['start_time'] = start_time
        if end_time is not None:
            params['end_time'] = end_time
        if page_size is not None:
            params['page_size'] = page_size
        if start_key is not None:
            params['start_key'] = start_key
        if type is not None:
            params['type'] = type
        if window is not None:
            params['window'] = window
        if date is not None:
            params['date'] = date
        if start_date is not None:
            params['start_date'] = start_date
        if end_date is not None:
            params['end_date'] = end_date

        response = self.make_api_request('GET', f'/v1/groups/{group_id}/nodes/{node_id}/timeseries', params=params)
        if response.status_code in (200, 201):
            return response.json()
        elif response.status_code == 401 or response.status_code == 403:
            raise Exception(f"Unauthorized access to timeseries data: {response.text}")
        elif response.status_code == 400:
            raise Exception(f"Bad request - invalid parameters: {response.text}")
        elif response.status_code == 404:
            raise Exception(f"Data not found: {response.text}")
        else:
            user_log(f"Failed to get timeseries data: {response.text}")
            return None

    def get_latest_timeseries_data(self, group_id, node_id, key, data_type):
        """Get the latest timeseries data point for a specific parameter.

        Args:
            group_id (str): Group ID
            node_id (str): Node ID
            key (str): Data point key
            data_type (str): Data type (int, float, bool, string)

        Returns:
            dict: API response containing latest timeseries data
        """
        response = self.make_api_request('GET', f'/v1/groups/{group_id}/nodes/{node_id}/timeseries',
                                       params={'key': key, 'data_type': data_type, 'type': 'latest'})
        if response.status_code in (200, 201):
            return response.json()
        elif response.status_code == 401 or response.status_code == 403:
            raise Exception(f"Unauthorized access to latest timeseries data: {response.text}")
        else:
            user_log(f"Failed to get latest timeseries data: {response.text}")
            return None

    # Wrapper methods for comprehensive timeseries API testing
    def get_node_timeseries_api_info(self, group_id, node_id):
        """Get timeseries API information by calling the endpoint with no parameters.

        Args:
            group_id (str): Group ID
            node_id (str): Node ID

        Returns:
            dict: API response containing service information and examples
        """
        response = self.make_api_request('GET', f'/v1/groups/{group_id}/nodes/{node_id}/timeseries')
        if response.status_code in (200, 201):
            return response.json()
        else:
            user_log(f"Failed to get timeseries API info: {response.text}")
            return None

    def get_node_timeseries_raw(self, group_id, node_id, key, data_type, page_size=None, start_time=None, end_time=None):
        """Get raw timeseries data for a specific parameter.

        Args:
            group_id (str): Group ID
            node_id (str): Node ID
            key (str): Data point key
            data_type (str): Data type (int, float, bool, string)
            page_size (int): Maximum number of records per page (optional)
            start_time (int): Start timestamp (required)
            end_time (int): End timestamp (optional)

        Returns:
            dict: API response containing raw timeseries data
        """
        if start_time is None:
            raise ValueError("start_time is required for raw timeseries data")

        params = {
            'key': key,
            'data_type': data_type,
            'start_time': start_time
        }
        if end_time is not None:
            params['end_time'] = end_time
        if page_size is not None:
            params['page_size'] = page_size

        response = self.make_api_request('GET', f'/v1/groups/{group_id}/nodes/{node_id}/timeseries/raw', params=params)
        if response.status_code in (200, 201):
            return response.json()
        elif response.status_code == 401 or response.status_code == 403:
            raise Exception(f"Unauthorized access to raw timeseries data: {response.text}")
        elif response.status_code == 400:
            raise Exception(f"Bad request - invalid parameters: {response.text}")
        else:
            user_log(f"Failed to get raw timeseries data: {response.text}")
            return None

    def get_node_timeseries_latest(self, group_id, node_id, key, data_type):
        """Get the latest timeseries data point for a specific parameter.

        Args:
            group_id (str): Group ID
            node_id (str): Node ID
            key (str): Data point key
            data_type (str): Data type (int, float, bool, string)

        Returns:
            dict: API response containing latest timeseries data
        """
        response = self.make_api_request('GET', f'/v1/groups/{group_id}/nodes/{node_id}/timeseries/latest',
                                       params={'key': key, 'data_type': data_type})
        if response.status_code in (200, 201):
            return response.json()
        elif response.status_code == 401 or response.status_code == 403:
            raise Exception(f"Unauthorized access to latest timeseries data: {response.text}")
        else:
            user_log(f"Failed to get latest timeseries data: {response.text}")
            return None

    def get_node_timeseries_current_aggregates(self, group_id, node_id, key, data_type, window=None):
        """Get current aggregates for a specific parameter.

        Args:
            group_id (str): Group ID
            node_id (str): Node ID
            key (str): Data point key
            data_type (str): Data type (int, float, bool, string)
            window (str): Specific window for aggregates (hourly, daily, weekly, monthly) (required)

        Returns:
            dict: API response containing current aggregates
        """
        if window is None:
            raise ValueError("window is required for aggregates timeseries data")

        params = {
            'key': key,
            'data_type': data_type,
            'window': window
        }

        response = self.make_api_request('GET', f'/v1/groups/{group_id}/nodes/{node_id}/timeseries/aggregates', params=params)
        if response.status_code in (200, 201):
            return response.json()
        elif response.status_code == 401 or response.status_code == 403:
            raise Exception(f"Unauthorized access to aggregates timeseries data: {response.text}")
        elif response.status_code == 400:
            raise Exception(f"Bad request - invalid parameters: {response.text}")
        else:
            user_log(f"Failed to get aggregates timeseries data: {response.text}")
            return None

    def get_node_timeseries_historical_aggregates(self, group_id, node_id, key, data_type, window=None, date=None):
        """Get historical aggregates for a specific parameter and date.

        Args:
            group_id (str): Group ID
            node_id (str): Node ID
            key (str): Data point key
            data_type (str): Data type (int, float, bool, string)
            window (str): Specific window for aggregates (hourly, daily, weekly, monthly) (required)
            date (str): Specific date for historical aggregates (YYYY-MM-DD format) (optional)

        Returns:
            dict: API response containing historical aggregates
        """
        if window is None:
            raise ValueError("window is required for aggregates timeseries data")

        params = {
            'key': key,
            'data_type': data_type,
            'window': window
        }
        if date is not None:
            params['date'] = date

        response = self.make_api_request('GET', f'/v1/groups/{group_id}/nodes/{node_id}/timeseries/aggregates', params=params)
        if response.status_code in (200, 201):
            return response.json()
        elif response.status_code == 401 or response.status_code == 403:
            raise Exception(f"Unauthorized access to historical aggregates timeseries data: {response.text}")
        elif response.status_code == 400:
            raise Exception(f"Bad request - invalid parameters: {response.text}")
        else:
            user_log(f"Failed to get historical aggregates timeseries data: {response.text}")
            return None

    def get_node_timeseries_historical_aggregates_range(self, group_id, node_id, key, data_type,
                                                       window=None, start_date=None, end_date=None, page_size=None):
        """Get historical aggregates for a specific parameter and date range.

        Args:
            group_id (str): Group ID
            node_id (str): Node ID
            key (str): Data point key
            data_type (str): Data type (int, float, bool, string)
            window (str): Specific window for aggregates (hourly, daily, weekly, monthly) (required)
            start_date (str): Start date for historical aggregates date range (YYYY-MM-DD format) (optional)
            end_date (str): End date for historical aggregates date range (YYYY-MM-DD format) (optional)
            page_size (int): Maximum number of historical aggregates per page (optional)

        Returns:
            dict: API response containing historical aggregates range
        """
        if window is None:
            raise ValueError("window is required for aggregates timeseries data")

        params = {
            'key': key,
            'data_type': data_type,
            'window': window
        }
        if start_date is not None:
            params['start_date'] = start_date
        if end_date is not None:
            params['end_date'] = end_date
        if page_size is not None:
            params['page_size'] = page_size

        response = self.make_api_request('GET', f'/v1/groups/{group_id}/nodes/{node_id}/timeseries/aggregates', params=params)
        if response.status_code in (200, 201):
            return response.json()
        elif response.status_code == 401 or response.status_code == 403:
            raise Exception(f"Unauthorized access to historical aggregates range timeseries data: {response.text}")
        elif response.status_code == 400:
            raise Exception(f"Bad request - invalid parameters: {response.text}")
        else:
            user_log(f"Failed to get historical aggregates range timeseries data: {response.text}")
            return None

    def make_user_api_request(self, method, path, data=None, params=None, require_token=True, token=None):
        """Make an API request to the user API gateway.

        Args:
            method (str): HTTP method (GET, POST, etc.)
            path (str): API path
            params (dict): Query parameters for GET requests
            data (dict or str): JSON data for POST/PUT requests (dict will be JSON encoded)
            require_token (bool): If True, requires token and will try to get one if missing.
                                 If False, makes request without token (for signin/signup).

        Returns:
            requests.Response: The API response
        """
        if not self.user_api_gateway_url:
            raise ValueError("user_api_gateway_url is not set")

        # Build query string if params are provided
        if params:
            query_string = urlencode(params)
            path = f"{path}?{query_string}"

        # Convert data to JSON string if it's a dict
        json_data = None
        if data is not None:
            if isinstance(data, dict):
                json_data = json.dumps(data)
            else:
                json_data = data

        if require_token:
            response = self.make_api_request_with_token(
                method=method,
                path=path,
                data=json_data,
                api_url=self.user_api_gateway_url,
                token=token
            )
        else:
            # Direct request without token (for signin/signup endpoints)
            url = f"{self.user_api_gateway_url}{path}"
            headers = {}
            if json_data is not None:
                headers["Content-Type"] = "application/json"

            print(f"-----------------------------------------")
            print(f"Request: {method} {url} {json_data}")
            response = self.session.request(
                method=method,
                url=url,
                headers=headers,
                data=json_data if json_data is not None else None
            )
            if "signin" not in path and "creds" not in path:
                print(f"Response: {response.status_code} {response.text}")
            print(f"-----------------------------------------")

        return response

    def otp_initiate(self, username, client_id, scope="openid email"):
        """Start direct-token OTP login via POST /v1/auth/otp/initiate.

        Args:
            username (str): email or phone the code is sent to.
            client_id (str): trusted first-party client (allow_direct_token).
            scope (str): requested scope; None to omit.

        Returns:
            requests.Response: the API response (carries flow_id on success).
        """
        data = {"username": username, "client_id": client_id}
        if scope is not None:
            data["scope"] = scope
        return self.make_user_api_request('POST', '/v1/auth/otp/initiate', data=data, require_token=False)

    def otp_verify(self, flow_id, code):
        """Complete direct-token OTP login via POST /v1/auth/otp/verify.

        Args:
            flow_id (str): the handle returned by otp_initiate.
            code (str): the code delivered out-of-band.

        Returns:
            requests.Response: the API response (carries the token set on success).
        """
        data = {"flow_id": flow_id, "otp": code}
        return self.make_user_api_request('POST', '/v1/auth/otp/verify', data=data, require_token=False)

    def oauth_token_refresh(self, refresh_token, client_id):
        """Redeem a refresh token at the OAuth token endpoint (POST /oauth2/token).

        The token endpoint is form-encoded (RFC 6749), unlike the JSON OTP APIs,
        so this posts application/x-www-form-urlencoded directly.

        Args:
            refresh_token (str): the current (unspent) refresh token.
            client_id (str): the client the token was issued to.

        Returns:
            requests.Response: the API response (carries the rotated token set on success).
        """
        url = f"{self.user_api_gateway_url}/oauth2/token"
        body = urlencode({
            "grant_type": "refresh_token",
            "refresh_token": refresh_token,
            "client_id": client_id,
        })
        response = self.session.post(
            url, headers={"Content-Type": "application/x-www-form-urlencoded"}, data=body,
        )
        return response

    def oauth_authorize(self, client_id, redirect_uri, code_challenge, scope="openid email", state="xyz"):
        """Start the browser authorization-code flow (GET /oauth2/authorize). Does not follow
        the redirect, so the flow_id cookie and Location are returned for inspection."""
        params = {
            "response_type": "code", "client_id": client_id, "redirect_uri": redirect_uri,
            "scope": scope, "state": state,
            "code_challenge": code_challenge, "code_challenge_method": "S256",
        }
        url = f"{self.user_api_gateway_url}/oauth2/authorize?{urlencode(params)}"
        return self.session.get(url, allow_redirects=False)

    def otp_initiate_inflow(self, flow_id, username):
        """Send an in-flow OTP bound to an /oauth2/authorize flow (client_id/scope come from the flow)."""
        data = {"flow_id": flow_id, "username": username}
        return self.make_user_api_request('POST', '/v1/auth/otp/initiate', data=data, require_token=False)

    def oauth_exchange_code(self, code, code_verifier, client_id, redirect_uri):
        """Redeem a browser-login authorization code for tokens (POST /oauth2/token, code grant)."""
        url = f"{self.user_api_gateway_url}/oauth2/token"
        body = urlencode({
            "grant_type": "authorization_code", "code": code, "code_verifier": code_verifier,
            "client_id": client_id, "redirect_uri": redirect_uri,
        })
        return self.session.post(
            url, headers={"Content-Type": "application/x-www-form-urlencoded"}, data=body,
        )

    def oauth_userinfo(self, access_token):
        """Fetch the OIDC UserInfo claims (GET /oauth2/userinfo) for an access token.

        Args:
            access_token (str): the RS256 access token to present as a bearer.

        Returns:
            requests.Response: the API response (carries the scope-gated claims on success).
        """
        url = f"{self.user_api_gateway_url}/oauth2/userinfo"
        return self.session.get(url, headers={"Authorization": f"Bearer {access_token}"})

    def oauth_revoke(self, token, client_id="user-pool-client", client_secret="", token_type_hint=None):
        """Revoke one refresh token (POST /oauth2/revoke, RFC 7009). Client auth is HTTP Basic: a public client passes an empty secret. Always 200 (no token-validity oracle)."""
        url = f"{self.user_api_gateway_url}/oauth2/revoke"
        payload = {"token": token}
        if token_type_hint is not None:
            payload["token_type_hint"] = token_type_hint
        basic = base64.b64encode(f"{client_id}:{client_secret}".encode()).decode()
        return self.session.post(
            url,
            headers={"Content-Type": "application/x-www-form-urlencoded", "Authorization": f"Basic {basic}"},
            data=urlencode(payload),
        )

    # ----- Admin OAuth client registry (/v1/admin/clients, superadmin) -----
    # Uses make_user_api_request: ESP User API + Bearer token (must be a super_admin).

    def create_oauth_client(self, client):
        """POST /v1/admin/clients — register a client. `client` is the request dict."""
        return self.make_user_api_request('POST', '/v1/admin/clients', data=client)

    def list_oauth_clients(self, get_secret=False):
        """GET /v1/admin/clients — list all clients; get_secret=True includes plaintext secrets."""
        params = {"get_secret": "true"} if get_secret else None
        return self.make_user_api_request('GET', '/v1/admin/clients', params=params)

    def put_oauth_client(self, client_id, body):
        """PUT /v1/admin/clients/{client_id} — full replace (client_name required)."""
        return self.make_user_api_request('PUT', f'/v1/admin/clients/{client_id}', data=body)

    def delete_oauth_client(self, client_id):
        """DELETE /v1/admin/clients/{client_id} — permanently deletes the client."""
        return self.make_user_api_request('DELETE', f'/v1/admin/clients/{client_id}')

    # ----- Admin post-deployment credentials (/v1/admin/credentials, superadmin) -----
    # Each stack vends credentials for the account values it owns, so the dashboard reads
    # them from the browser.

    def admin_get_rmng_creds(self):
        """POST /v1/admin/credentials on the rmng API — reads the Lambda concurrency limit."""
        return self.make_api_request('POST', '/v1/admin/credentials', data=json.dumps({}))

    def admin_get_espuser_creds(self):
        """POST /v1/admin/credentials on the ESP User API — SES/SMS sandbox state and SMS numbers."""
        return self.make_user_api_request('POST', '/v1/admin/credentials', data={})

    # TODO: temporary until we plug this in other APIs like GET /user
    def get_otp_user_id_by_email(self, email):
        """Return the user_id of the espuser-user-details row for email, or None.

        OTP login JIT-creates users directly in DynamoDB (not Cognito), so tests
        resolve them via the by-email GSI rather than Cognito.
        """
        dynamodb = boto3.resource('dynamodb', region_name=self.region)
        resp = dynamodb.Table('espuser-user-details').query(
            IndexName='espuser-user-details-by-email',
            KeyConditionExpression=Key('email').eq(email.lower()),
        )
        items = resp.get('Items', [])
        return items[0]['user_id'] if items else None

    # Just to clean up
    def delete_otp_user_by_email(self, email):
        """Best-effort delete of an OTP/JIT user from espuser-user-details by email.

        OTP users live in DynamoDB, not Cognito, so a Cognito admin_delete_user
        does not remove them; reruns must clear the DB row to start clean.
        """
        try:
            user_id = self.get_otp_user_id_by_email(email)
            if user_id:
                dynamodb = boto3.resource('dynamodb', region_name=self.region)
                dynamodb.Table('espuser-user-details').delete_item(Key={'user_id': user_id})
        except Exception:
            pass

    def delete_user_by_email(self, email):
        """Delete a user everywhere for a clean rerun: its account in whichever pool holds it, and
        the espuser-user-details row. Both pools are tried because an email may exist in either, and
        a user built without one of the pool ids must not fail on it."""
        cognito = boto3.client('cognito-idp', region_name=self.region)
        for pool_id in (self.admin_user_pool_id, getattr(self, 'end_user_pool_id', None)):
            if not pool_id:
                continue
            try:
                cognito.admin_delete_user(UserPoolId=pool_id, Username=email)
            except Exception:  # noqa: BLE001 — absent from this pool, or not permitted
                pass
        self.delete_otp_user_by_email(email)

    def signup(self, email=None, phone_number=None, password=None):
        """Provision an end user via the passwordless email OTP login.

        Kept for signature compatibility with the former Cognito signup helper.
        `password` is ignored (end users are passwordless); phone provisioning is
        not supported by the native OTP path, so only email is used. Drives
        initiate -> verify and returns the verify response (which carries the token
        set on success and JIT-creates the user). On success the instance tokens are
        populated exactly like `signin()`.
        """
        if email is None:
            email = self.username if '@' in self.username else None
        if email is None:
            raise ValueError("OTP provisioning requires an email address")
        return self.signin(username=email)

    def verify_signup(self, email=None, phone_number=None, code=None):
        """No-op shim retained for signature compatibility.

        Provisioning now happens atomically inside `signup()`/`signin()` (the OTP
        verify step is driven there against the Mailosaur inbox), so a separate
        verify call is unnecessary. Returns the already-established token set.
        """
        return _CognitoResponse(200, {
            "access_token": self.access_token,
            "id_token": self.token,
            "refresh_token": self.refresh_token,
            "token_type": "Bearer",
        })

    def signin(self, username=None, password=None, is_admin=False):
        """User/Admin signin.

        End users post their credentials to POST /v1/user/auth/token, which verifies
        them against the provider pool and returns OUR token set. Admins have no
        admin-auth API — they authenticate against their Cognito pool directly
        (USER_PASSWORD_AUTH).

        Args:
            username (str): User's email (defaults to self.username).
            password (str): User's password (defaults to self.password).
            is_admin (bool): If True, authenticates as an admin via Cognito.

        Returns:
            requests.Response-compatible object. A 200 carrying only token_type means
            the credentials did not resolve to a user; the response withholds which
            half was wrong so it cannot be used to enumerate accounts.
        """
        if username is None:
            username = self.username
        if password is None:
            password = self.password

        if is_admin:
            response = _admin_cognito_auth(
                self.region, self.admin_client_id, 'USER_PASSWORD_AUTH',
                {"USERNAME": username, "PASSWORD": password},
            )
        else:
            response = self.session.post(
                f"{self.user_api_gateway_url}/v1/user/auth/token",
                json={"username": username, "password": password},
            )

        # If successful, update tokens
        if response.status_code in (200, 201):
            try:
                response_data = response.json()
                # Check if response has actual tokens (not just token_type)
                # If only token_type is present, it means user doesn't exist (security feature)
                has_tokens = 'access_token' in response_data or 'id_token' in response_data or 'refresh_token' in response_data

                if has_tokens:
                    if 'access_token' in response_data:
                        self.access_token = response_data['access_token']
                    if 'refresh_token' in response_data:
                        self.refresh_token = response_data['refresh_token']
                    if 'id_token' in response_data:
                        self.token = response_data['id_token']
                        # Extract sub from token after setting it
                        self._extract_sub_from_token()
                else:
                    # Only token_type present means user doesn't exist (security feature to prevent account enumeration)
                    # Don't set any tokens - let the caller check user.token to determine if signin actually succeeded
                    user_log(f"Signin returned 200 without tokens: {username} is not provisioned or the password is wrong. Response keys: {list(response_data.keys())}")
            except (ValueError, json.JSONDecodeError) as e:
                user_log(f"Warning: Failed to parse signin response: {e}")

        return response

    def _otp_login(self, email, scope="openid email"):
        """Drive a full native/direct-token email OTP login and return the verify response.

        Reads the emailed code from Mailosaur for `email` (or self.mailosaur_email
        if set), so the caller's address must be a Mailosaur inbox in integration
        runs. Returns the /v1/auth/otp/verify response, which carries the token set
        (and JIT-creates the user) on success.
        """
        from test.itest.email_utils import get_verification_code_from_server
        before = time.time()
        init = self.otp_initiate(email, client_id=ESP_USER_CLIENT_ID, scope=scope)
        if init.status_code != 200:
            return init
        flow_id = init.json().get("flow_id")
        if not flow_id:
            return init
        time.sleep(2)
        code = get_verification_code_from_server(since_timestamp=before, recipient_email=email)
        if code is None:
            return _CognitoResponse(401, {"message": "no OTP code delivered"})
        return self.otp_verify(flow_id, code)

    def refresh_tokens(self, refresh_token=None):
        """Rotate end-user tokens at the OAuth token endpoint
        (POST /oauth2/token, grant_type=refresh_token).

        Args:
            refresh_token (str): Refresh token (defaults to self.refresh_token)

        Returns:
            requests.Response: The API response
        """
        if refresh_token is None:
            refresh_token = self.refresh_token

        response = self.oauth_token_refresh(refresh_token, ESP_USER_CLIENT_ID)

        # If successful, update tokens
        if response.status_code in (200, 201):
            try:
                response_data = response.json()
                if 'access_token' in response_data:
                    self.access_token = response_data['access_token']
                if 'refresh_token' in response_data:
                    self.refresh_token = response_data['refresh_token']
                if 'id_token' in response_data:
                    self.token = response_data['id_token']
            except (ValueError, json.JSONDecodeError):
                pass

        return response

    def signout(self, global_signout=False, refresh_token=None):
        """End-user signout via POST /oauth2/signout (family revoke).

        Revokes the whole refresh-token family the presented refresh token belongs
        to, so every device fed by that family is signed out. The `global_signout`
        flag is retained for signature compatibility but no longer changes behaviour
        (family revoke is inherently global for that family).

        Args:
            global_signout (bool): Retained for compatibility; ignored.
            refresh_token (str): Refresh token whose family to revoke
                (defaults to self.refresh_token).

        Returns:
            requests.Response: The API response
        """
        if refresh_token is None:
            refresh_token = self.refresh_token

        body = urlencode({
            "token": refresh_token,
            "client_id": ESP_USER_CLIENT_ID,
        })
        return self.session.post(
            f"{self.user_api_gateway_url}/oauth2/revoke",
            headers={"Content-Type": "application/x-www-form-urlencoded"},
            data=body,
        )

    def forgot_password(self, username=None, is_admin=False):
        """Initiate forgot password flow via POST /v1/user/auth/password-recovery or /v1/admin/auth/password-recovery endpoint.

        Args:
            username (str): User's email or phone number (defaults to self.username)
            is_admin (bool): If True, uses admin endpoint, otherwise uses user endpoint

        Returns:
            requests.Response: The API response
        """
        if username is None:
            username = self.username

        data = {
            "username": username
        }

        path = '/v1/admin/auth/password-recovery' if is_admin else '/v1/user/auth/password-recovery'
        return self.make_user_api_request('POST', path, data=data)

    def confirm_forgot_password(self, username=None, code=None, new_password=None, is_admin=False):
        """Confirm forgot password via POST /v1/user/auth/password-recovery/confirmation or /v1/admin/auth/password-recovery/confirmation endpoint.

        Args:
            username (str): User's email or phone number (defaults to self.username)
            code (str): Verification code received via email or SMS
            new_password (str): New password
            is_admin (bool): If True, uses admin endpoint, otherwise uses user endpoint

        Returns:
            requests.Response: The API response
        """
        if username is None:
            username = self.username

        data = {
            "username": username,
            "code": code,
            "new_password": new_password
        }

        path = '/v1/admin/auth/password-recovery/confirmation' if is_admin else '/v1/user/auth/password-recovery/confirmation'
        return self.make_user_api_request('POST', path, data=data)

    def change_password(self, old_password=None, new_password=None, is_admin=False):
        """Change user/admin password via POST /v1/user/auth/password or /v1/admin/auth/password endpoint.

        The Authorization header carries the access_token (the method declares
        API Gateway authorizationScopes, so the Cognito authorizer validates the
        access token and rejects the ID token). The body separately carries the
        access_token that Cognito's ChangePassword API acts on.

        Args:
            old_password (str): Current password
            new_password (str): New password
            is_admin (bool): If True, uses admin endpoint, otherwise uses user endpoint

        Returns:
            requests.Response: The API response
        """
        # Ensure we have a fresh access token for the body.
        if not self.access_token:
            self.get_cognito_token()

        data = {}
        # Access token is required in body for authenticated password change
        if self.access_token:
            data["access_token"] = self.access_token
        if old_password:
            data["old_password"] = old_password
        if new_password:
            data["new_password"] = new_password

        path = '/v1/admin/auth/password' if is_admin else '/v1/user/auth/password'
        # The admin methods declare no API Gateway authorization scopes, so their
        # authorizer validates the ID token; the end-user methods declare scopes and
        # take the access token (the default). See make_api_request_with_token.
        return self.make_user_api_request(
            'POST', path, data=data, token=self.token if is_admin else None)


    def get_user_details(self, user_id='me'):
        """Fetch a user profile via GET /v1/users/{userId}.

        Named distinctly from `get_user` (which calls Cognito GetUser) to
        avoid a method-name collision in this class.

        Args:
            user_id (str): Path parameter. Defaults to 'me' which resolves to
                the authenticated caller server-side.

        Returns:
            requests.Response: The API response.
        """
        return self.make_user_api_request('GET', f'/v1/users/{user_id}')

    @property
    def user_id(self):
        """The user's ID, as returned by GET /v1/users/me — the value unshare_group / unshare_subgroup take as the target. Sharing goes the other way and takes a user name (this user's `username`, i.e. email or E.164 phone). Fetched once and cached. Distinct from the endpoint_id register_client returns."""
        if self._user_id is None:
            response = self.get_user_details('me')
            assert response.status_code == 200, \
                f"Failed to fetch user profile for user_id: {response.status_code} {response.text}"
            self._user_id = json.loads(response.text).get('user_id')
            assert self._user_id, "GET /v1/users/me returned no user_id"
        return self._user_id

    def generate_csr(self):
        """Generate a CSR, deriving a stable private key from the user's username if needed.

        The private key is derived deterministically from the username so it
        remains consistent across calls and CLI sessions for the same user.
        Once set, the key is reused for subsequent calls.

        Returns:
            str: PEM-encoded CSR
        """
        if not hasattr(self, 'matter_private_key') or self.matter_private_key is None:
            # P-256 curve order
            n = 0xFFFFFFFF00000000FFFFFFFFFFFFFFFFBCE6FAADA7179E84F3B9CAC2FC632551
            key_material = hashlib.sha256(f"matter-user-key:{self.username}".encode()).digest()
            private_int = int.from_bytes(key_material, 'big') % (n - 1) + 1
            self.matter_private_key = ec.derive_private_key(private_int, ec.SECP256R1())
        csr = x509.CertificateSigningRequestBuilder().subject_name(
            x509.Name([])
        ).sign(self.matter_private_key, hashes.SHA256())
        return csr.public_bytes(serialization.Encoding.PEM).decode('utf-8')

    def get_matter_noc(self, group_id, csr=None):
        """Request a Matter NOC (Node Operational Certificate) for the user.

        If no CSR is provided, one is generated automatically and the private
        key is stored as self.matter_private_key.

        Args:
            group_id (str): ID of the Matter-capable group
            csr (str): PEM-encoded Certificate Signing Request (optional, auto-generated if None)

        Returns:
            dict: Response containing 'noc', 'matter_node_id'
            None: If the request fails
        """
        if csr is None:
            csr = self.generate_csr()
        noc_data = json.dumps({"csr": csr})
        response = self.get_matter_noc_raw(group_id, noc_data)
        if response.status_code not in (200, 201):
            user_log(f"Failed to get Matter NOC. Status code: {response.status_code}")
            user_log(f"Response: {response.text}")
            return None

        try:
            return response.json()
        except json.JSONDecodeError:
            user_log("Error: Invalid JSON response")
            return None

    def get_matter_noc_raw(self, group_id, data=None):
        """Request a Matter NOC with raw data (for testing error cases).

        Args:
            group_id (str): ID of the Matter-capable group
            data (str): Raw JSON data to send (can be empty for testing)

        Returns:
            requests.Response: The raw API response
        """
        if data is None:
            data = json.dumps({})
        return self.make_api_request('POST', f'/v1/groups/{group_id}/matter-nocs', data=data)

    def get_user(self, access_token=None,is_admin=False):
        """Get user information from Cognito using access token.

        Identity is Cognito-only (admins have no espuser-user-details row), so the
        attributes returned by Cognito are authoritative — there is no DB to cross-check.

        Args:
            access_token (str): Access token (defaults to self.access_token)

        Returns:
            dict: User information with attributes like email, custom:user_id, etc.
            None: If the API call fails
        """
        if access_token is None:
            access_token = self.access_token

        if not access_token:
            user_log("Failed to get user: No access token available")
            return None

        try:
            cognito_client = boto3.client('cognito-idp', region_name=self.region)
            response = cognito_client.get_user(AccessToken=access_token)

            # Convert attributes list to dict for easier access
            user_attrs = {}
            for attr in response.get('UserAttributes', []):
                user_attrs[attr['Name']] = attr['Value']

            user_id = user_attrs.get('custom:user_id', '')
            if not user_id:
                raise Exception("custom:user_id not found in Cognito user attributes")

            return {
                'username': user_attrs.get('username', ''),
                'email': user_attrs.get('email', ''),
                'custom:user_id': user_id,
                'phone_number': user_attrs.get('phone_number', ''),
                'email_verified': user_attrs.get('email_verified', 'false') == 'true',
                'phone_number_verified': user_attrs.get('phone_number_verified', 'false') == 'true',
                'custom:super_admin': user_attrs.get('custom:super_admin', 'false') == 'true',
            }
        except Exception as e:
            user_log(f"Failed to get user: {str(e)}")
            raise
