# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import boto3
import json
import hashlib
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.asymmetric import ec, rsa, padding
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.utils import Prehashed
from cryptography.hazmat.primitives.serialization import load_pem_private_key
from cryptography.hazmat.primitives.asymmetric import utils as crypto_utils
from cryptography import x509
from cryptography.x509.oid import NameOID
from cryptography.hazmat.primitives import hashes
import datetime

import time
import tempfile
import os
import sys
import subprocess
import logging
import ssl
import socket
import requests
from botocore.exceptions import ClientError
from awscrt import io, mqtt
from awsiot import iotshadow, mqtt_connection_builder
from queue import Queue, Empty

from py_sdk.test_util import shadow_to_unstructured

blue = "\033[94m"
green = "\033[92m"
red = "\033[91m"
reset = "\033[0m"

# Define colored logging function for Device
def device_log(message):
    """Print message with Device prefix in green color."""
    print(f"{green}[Device]{reset} {message}")

def setup_logging(debug):
    if debug:
        logging.basicConfig(level=logging.DEBUG)
        logger = logging.getLogger("awsiot")
        logger.setLevel(logging.DEBUG)
        streamHandler = logging.StreamHandler()
        formatter = logging.Formatter('%(asctime)s - %(name)s - %(levelname)s - %(message)s')
        streamHandler.setFormatter(formatter)
        logger.addHandler(streamHandler)
    else:
        logging.getLogger("awsiot").setLevel(logging.CRITICAL)

class Device:
    def __init__(self, node_thing_name, node_key, node_combined_cert, ca_cert, iot_endpoint, region, debug=False):
        self.node_thing_name = node_thing_name
        self.node_key = node_key
        self.ca_cert = ca_cert
        self.iot_endpoint = iot_endpoint
        self.region = region
        self.debug = debug
        # MQTT connection
        self.mqtt_connection = None
        self.shadow_client = None

        self.node_cert, self.node_ca_cert = split_combined_cert_pem(node_combined_cert)

        # Message queues for different types of messages
        self.from_cloud_queue = Queue()
        self.params_queue = Queue()
        self.shadow_queue = Queue()

        # Callback registry for different message types
        self.callbacks = {
            'from_cloud': None,
            'params': None,
            'shadow': None
        }

        setup_logging(self.debug)
        self.iot_data_client = None
        self.group_id = None

    def register_test_node(self, admin_group_names=None, capabilities=None, caller_identity=None):
        """Register this device as a node via a direct invoke of the rmng-admin-node-reg Lambda.

        caller_identity is the API-Gateway CognitoAuthenticationProvider string of a REAL
        superadmin"""
        node_thing_name = self.node_thing_name

        if not node_thing_name or not self.node_cert:
            device_log("Error: node_thing_name or node_cert not found in test_config.json")
            return False

        tags = ["created_by:test"]

        # Create boto3 client
        iot_client = boto3.client('iot', region_name=self.region)
        lambda_client = boto3.client('lambda', region_name=self.region)
        try:
            try:
                # Check if the thing already exists
                iot_client.describe_thing(thingName=node_thing_name)
                device_log(f"Thing '{node_thing_name}' already exists")
                return True
            except iot_client.exceptions.ResourceNotFoundException:
                # Thing doesn't exist, proceed with registration
                pass

            # Prepare request body for Lambda function
            request_body = {
                "cert": self.node_cert,
                "ca_cert": self.node_ca_cert,
                "tags": tags,
                "admin_group_names": admin_group_names,
            }
            if capabilities:
                request_body["capabilities"] = capabilities

            # Invoke Lambda function directly
            try:
                payload = {
                    "httpMethod": "POST",
                    "path": "/v1/admin/nodes",
                    "body": json.dumps(request_body),
                }
                if caller_identity:
                    payload["requestContext"] = {
                        "identity": {"cognitoAuthenticationProvider": caller_identity},
                    }
                response = lambda_client.invoke(
                    FunctionName='rmng-admin-node-reg',
                    InvocationType='RequestResponse',
                    Payload=json.dumps(payload)
                )

                # Parse the response
                response_data = json.loads(response['Payload'].read().decode())
                if 'body' in response_data:
                    response_body = json.loads(response_data['body'])
                    if not response_body.get("node_id"):
                        raise Exception(f"Node registration failed: {response_body.get('message', 'Unknown error')}")

                    device_log(f"Node '{node_thing_name}' registered successfully")
                    device_log(f"Node ID: {response_body['node_id']}")
                else:
                    raise Exception("Invalid response format from Lambda")

            except Exception as e:
                device_log(f"Error invoking Lambda function: {str(e)}")
                raise

            return True
        except ClientError as e:
            device_log(f"An error occurred: {e}")
            return False

    def sign_challenge(self, challenge):
        if not self.node_key:
            device_log("Error: node_key not set")
            return None

        try:
            # Load the private key
            private_key = load_pem_private_key(
                self.node_key.encode(),
                password=None,
            )

            # Determine the key type and choose the appropriate signing algorithm
            if isinstance(private_key, rsa.RSAPrivateKey):
                # For RSA keys
                signature = private_key.sign(
                    challenge.encode(),
                    padding.PSS(
                        mgf=padding.MGF1(hashes.SHA256()),
                        salt_length=padding.PSS.MAX_LENGTH
                    ),
                    hashes.SHA256()
                )

            elif isinstance(private_key, ec.EllipticCurvePrivateKey):
                # For EC keys
                signature = private_key.sign(
                    challenge.encode(),
                    ec.ECDSA(hashes.SHA256())
                )
                # Convert signature to DER format
                der_signature = crypto_utils.encode_dss_signature(
                    *crypto_utils.decode_dss_signature(signature)
                )
                signature = der_signature

            else:
                raise ValueError("Unsupported key type")

            return signature.hex()
        except Exception as e:
            device_log(f"Error signing challenge: {str(e)}")
            return None

    def sign_matter_attestation(self, nocsr_elements: bytes, attestation_challenge: bytes) -> bytes:
        """Sign Matter attestation data (NOCSRElements || AttestationChallenge) with device's private key.

        Args:
            nocsr_elements: TLV-encoded NOCSRElements
            attestation_challenge: 16-byte attestation challenge

        Returns:
            bytes: 64-byte raw r||s ECDSA signature, or None on failure
        """
        if not self.node_key:
            device_log("Error: node_key not set")
            return None

        try:
            import hashlib

            # Load the private key
            private_key = load_pem_private_key(
                self.node_key.encode(),
                password=None,
            )

            if not isinstance(private_key, ec.EllipticCurvePrivateKey):
                device_log("Error: Matter attestation requires EC key")
                return None

            # Build TBS data
            tbs = nocsr_elements + attestation_challenge

            # Compute SHA256 hash
            hash_digest = hashlib.sha256(tbs).digest()

            # Sign with ECDSA using prehashed data
            der_sig = private_key.sign(hash_digest, ec.ECDSA(Prehashed(hashes.SHA256())))

            # Convert DER signature to raw r||s format (64 bytes)
            r, s = crypto_utils.decode_dss_signature(der_sig)

            # Pad r and s to 32 bytes each
            r_bytes = r.to_bytes(32, byteorder='big')
            s_bytes = s.to_bytes(32, byteorder='big')

            return r_bytes + s_bytes
        except Exception as e:
            device_log(f"Error signing Matter attestation: {str(e)}")
            return None

    def generate_csr(self):
        """Generate a CSR, deriving a stable private key from the device's node_key if needed.

        The private key is derived deterministically from node_key so it
        remains consistent across calls and CLI sessions for the same device.
        Once set, the key is reused for subsequent calls.

        Returns:
            tuple: (csr_pem: str, csr_der: bytes)
        """
        if not hasattr(self, 'matter_private_key') or self.matter_private_key is None:
            # P-256 curve order
            n = 0xFFFFFFFF00000000FFFFFFFFFFFFFFFFBCE6FAADA7179E84F3B9CAC2FC632551
            key_material = hashlib.sha256(f"matter-device-key:{self.node_key}".encode()).digest()
            private_int = int.from_bytes(key_material, 'big') % (n - 1) + 1
            self.matter_private_key = ec.derive_private_key(private_int, ec.SECP256R1())
        csr = x509.CertificateSigningRequestBuilder().subject_name(
            x509.Name([])
        ).sign(self.matter_private_key, hashes.SHA256())
        csr_pem = csr.public_bytes(serialization.Encoding.PEM).decode('utf-8')
        csr_der = csr.public_bytes(serialization.Encoding.DER)
        return csr_pem, csr_der

    def mqtt_connect(self):
        try:
            device_log("Creating MQTT connection...")
            event_loop_group = io.EventLoopGroup(1)
            host_resolver = io.DefaultHostResolver(event_loop_group)
            client_bootstrap = io.ClientBootstrap(event_loop_group, host_resolver)

            self.mqtt_connection = mqtt_connection_builder.mtls_from_bytes(
                endpoint=self.iot_endpoint,
                cert_bytes=self.node_cert.encode('utf-8'),
                pri_key_bytes=self.node_key.encode('utf-8'),
                client_bootstrap=client_bootstrap,
                ca_bytes=self.ca_cert.encode('utf-8'),
                client_id=self.node_thing_name,
                clean_session=True,
                keep_alive_secs=30
            )

            device_log("Connecting to MQTT...")
            connect_future = self.mqtt_connection.connect()
            connect_future.result()
            device_log("Connected!")

            return True
        except Exception as e:
            device_log(f"Error connecting to AWS IoT Core MQTT: {str(e)}")
            device_log(f"Error type: {type(e).__name__}")
            import traceback
            traceback.print_exc()
            self.mqtt_connection = None
            return False

    def connect(self):
        # Check if already connected to avoid duplicate connections
        if self.mqtt_connection:
            device_log("Already connected to MQTT")
            return True

        if not self.mqtt_connect():
            device_log("Failed to establish MQTT connection")
            return False

        try:
            # Subscribe to the from_cloud topic with default callback
            topic = f"rainmaker/nodes/{self.node_thing_name}/from_cloud"
            device_log(f"Subscribing to topic: {topic}")

            future, _ = self.mqtt_connection.subscribe(
                topic=topic,
                qos=mqtt.QoS.AT_LEAST_ONCE,
                callback=self.on_message_received
            )

            # Wait for subscription to succeed
            future.result()
            device_log(f"Subscribed to topic: {topic}")

            return True
        except Exception as e:
            device_log(f"Error in device_connect: {str(e)}")
            device_log(f"Error type: {type(e).__name__}")
            import traceback
            traceback.print_exc()
            return False

    def on_message_received(self, topic, payload, **kwargs):
        """Central message handler that routes messages to appropriate queues and callbacks"""
        try:
            message = json.loads(payload.decode())

            topic_parts = topic.split('/')

            if len(topic_parts) < 4:
                device_log(f"Invalid topic format: {topic}")
                return

            # Determine message type based on topic.
            # Unicast params:  rainmaker/nodes/<nodeID>/user/params-*/params  → parts[4] contains 'params-'
            # Group control:   rainmaker/nodes/groups/<groupID>/[subgroups/<sgID>/]control
            is_group_control = (
                len(topic_parts) >= 5
                and topic_parts[1] == 'nodes'
                and topic_parts[2] == 'groups'
                and 'control' in topic_parts[4:]
            )
            if any('params-' in part for part in topic_parts) or is_group_control:
                device_log(f"Received message on topic: {topic}")
                device_log(f"Received params message: {message}")
                self.params_queue.put(message)
                if self.callbacks['params']:
                    self.callbacks['params'](topic, message)
            elif topic_parts[3] == 'from_cloud':
                device_log(f"Received message on topic: {topic}")
                device_log(f"Received from_cloud message: {message}")
                self.from_cloud_queue.put(message)
                if self.callbacks['from_cloud']:
                    self.callbacks['from_cloud'](topic, message)
            elif 'shadow' in topic_parts:
                device_log(f"Received message on topic: {topic}")
                device_log(f"Received shadow message: {message}")
                self.shadow_queue.put(message)
                if self.callbacks['shadow']:
                    self.callbacks['shadow'](topic, message)
            else:
                device_log(f"Unknown topic type: {topic}")

        except json.JSONDecodeError:
            device_log(f"Failed to decode message payload: {payload}")
        except Exception as e:
            device_log(f"Error processing message: {str(e)}")

    def update_named_shadow(self, shadow_name, state):
        state = reported_or_desired_shadow_to_structured(state)

        if not self.shadow_client:
            device_log("Error: Shadow client not initialized. Call connect() first.")
            return False

        try:
            device_log(f"[{shadow_name}] {blue}updating to{reset} {state}")
            request = iotshadow.UpdateNamedShadowRequest(
                thing_name=self.node_thing_name,
                shadow_name=shadow_name,
                state=iotshadow.ShadowState(
                    reported=state
                )
            )
            future = self.shadow_client.publish_update_named_shadow(request, mqtt.QoS.AT_LEAST_ONCE)
            future.result()
            return True
        except Exception as e:
            device_log(f"Error updating named shadow: {str(e)}")
            return False

    def shadow_connect(self, named_shadows=None):
        if self.shadow_client:
            return True

        if not self.mqtt_connection:
            if not self.mqtt_connect():
                return False

        try:
            device_log("Creating IoT Shadow Client...")
            self.shadow_client = iotshadow.IotShadowClient(self.mqtt_connection)

            # Subscribe to classic shadow events
            self.subscribe_to_shadow_events()

            # Subscribe to named shadow events if specified
            if named_shadows:
                for shadow_name in named_shadows:
                    self.subscribe_to_named_shadow_events(shadow_name)

            device_log(f"Connected to AWS IoT Core Shadow for thing '{self.node_thing_name}'")
            return True
        except Exception as e:
            device_log(f"Error connecting to AWS IoT Core Shadow: {str(e)}")
            device_log(f"Error type: {type(e).__name__}")
            import traceback
            traceback.print_exc()
            return False

    def subscribe_to_shadow_events(self):
        device_log("Subscribing to shadow events...")

        # Shadow Update Accepted
        accepted_subscribed_future, _ = self.shadow_client.subscribe_to_update_shadow_accepted(
            request=iotshadow.UpdateShadowSubscriptionRequest(thing_name=self.node_thing_name),
            qos=mqtt.QoS.AT_LEAST_ONCE,
            callback=self.on_update_shadow_accepted)

        # Shadow Update Rejected
        rejected_subscribed_future, _ = self.shadow_client.subscribe_to_update_shadow_rejected(
            request=iotshadow.UpdateShadowSubscriptionRequest(thing_name=self.node_thing_name),
            qos=mqtt.QoS.AT_LEAST_ONCE,
            callback=self.on_update_shadow_rejected)

        # Shadow Delta
        delta_subscribed_future, _ = self.shadow_client.subscribe_to_shadow_delta_updated_events(
            request=iotshadow.ShadowDeltaUpdatedSubscriptionRequest(thing_name=self.node_thing_name),
            qos=mqtt.QoS.AT_LEAST_ONCE,
            callback=self.on_shadow_delta_updated)

        # Wait for all subscriptions to succeed
        accepted_subscribed_future.result()
        rejected_subscribed_future.result()
        delta_subscribed_future.result()

        device_log("Subscribed to shadow events successfully")

    def subscribe_to_named_shadow_events(self, shadow_name, callback=None):
        device_log(f"Subscribing to named shadow events for '{shadow_name}'...")

        # Named Shadow Update Accepted
        update_accepted_future, _ = self.shadow_client.subscribe_to_named_shadow_updated_events(
            request=iotshadow.NamedShadowUpdatedSubscriptionRequest(thing_name=self.node_thing_name, shadow_name=shadow_name),
            qos=mqtt.QoS.AT_LEAST_ONCE,
            callback=callback if callback else lambda response: self.on_named_shadow_updated(shadow_name, response)
        )

        # Named Shadow Delta
        delta_subscribed_future, _ = self.shadow_client.subscribe_to_named_shadow_delta_updated_events(
            request=iotshadow.NamedShadowDeltaUpdatedSubscriptionRequest(thing_name=self.node_thing_name, shadow_name=shadow_name),
            qos=mqtt.QoS.AT_LEAST_ONCE,
            callback=callback if callback else lambda delta: self.on_named_shadow_delta_updated(shadow_name, delta)
        )

        # Named Shadow Update Accepted
        update_accepted_future, _ = self.shadow_client.subscribe_to_update_named_shadow_accepted(
            request=iotshadow.UpdateNamedShadowSubscriptionRequest(thing_name=self.node_thing_name, shadow_name=shadow_name),
            qos=mqtt.QoS.AT_LEAST_ONCE,
            callback=callback if callback else lambda response: self.on_update_named_shadow_accepted(shadow_name, response)
        )

        # Wait for all subscriptions to succeed
        update_accepted_future.result()
        delta_subscribed_future.result()
        update_accepted_future.result()

        device_log(f"Subscribed to named shadow events for '{shadow_name}' successfully")

    def on_update_shadow_accepted(self, response):
        device_log(f"Shadow update accepted: Version: {response.version}")
        if response.state:
            if response.state.reported:
                device_log(f"Reported State: {response.state.reported}")
            if response.state.desired:
                device_log(f"Desired State: {response.state.desired}")

    def on_update_shadow_rejected(self, error):
        device_log(f"Shadow update rejected:")
        device_log(f"Error code: {error.code}")
        device_log(f"Error message: {error.message}")

    def on_shadow_delta_updated(self, delta):
        device_log(f"Shadow delta updated: Version: {delta.version}")
        if delta.state:
            device_log(f"Delta State: {delta.state}")
        if delta.metadata:
            device_log(f"Delta Metadata: {delta.metadata}")

    def on_named_shadow_updated(self, shadow_name, response):
        if response.current.state:
            if response.current.state.reported:
                device_log(f"[{shadow_name}][v{response.current.version}][reported] {blue}updated to{reset} {response.current.state.reported}")
            if response.current.state.desired:
                device_log(f"[{shadow_name}][v{response.current.version}][desired] {blue}updated to{reset} {response.current.state.desired}")

    def on_named_shadow_delta_updated(self, shadow_name, delta):
        if delta.state:
            device_log(f"[{shadow_name}][v{delta.version}][delta] {blue}updated to{reset} {delta.state}")
        if delta.metadata:
            device_log(f"[{shadow_name}][v{delta.version}][metadata] {blue}updated to{reset} {delta.metadata}")

    def on_update_named_shadow_accepted(self, shadow_name, response):
        if response.state:
            if response.state.reported:
                device_log(f"[{shadow_name}][v{response.version}][reported] {blue}update accepted{reset} {response.state.reported}")
            if response.state.desired:
                device_log(f"[{shadow_name}][v{response.version}][desired] {blue}update accepted{reset} {response.state.desired}")

    def __shadow_parse_input(self, input_str):
        try:
            # First, check if the string is already a dict
            if isinstance(input_str, dict):
                return input_str

            parsed = json.loads(input_str)
            if isinstance(parsed, dict):
                return parsed
            elif isinstance(parsed, str):
                return json.loads(parsed)
            else:
                return None
        except json.JSONDecodeError as e:
            raise ValueError(f"Invalid JSON data: {e}")

    def update_shadow(self, state_json, shadow_name=None):
        if not self.shadow_client:
            device_log("Error: Shadow not connected. Call shadow_connect() first.")
            return False

        try:
            # Parse the JSON string into a dictionary
            state = self.__shadow_parse_input(state_json)
            device_log(f"Updating {'named ' if shadow_name else ''}shadow state: {state}")

            if shadow_name:
                request = iotshadow.UpdateNamedShadowRequest(
                    thing_name=self.node_thing_name,
                    shadow_name=shadow_name,
                    state=iotshadow.ShadowState(
                        reported=state
                    )
                )
                future = self.shadow_client.publish_update_named_shadow(request, mqtt.QoS.AT_LEAST_ONCE)
            else:
                request = iotshadow.UpdateShadowRequest(
                    thing_name=self.node_thing_name,
                    state=iotshadow.ShadowState(
                        reported=state
                    )
                )
                future = self.shadow_client.publish_update_shadow(request, mqtt.QoS.AT_LEAST_ONCE)

            future.result()
            device_log(f"{'Named shadow' if shadow_name else 'Shadow'} state updated successfully")
            return True
        except json.JSONDecodeError:
            device_log("Error: Invalid JSON data")
            return False
        except Exception as e:
            device_log(f"Error updating shadow: {str(e)}")
            return False

    def disconnect(self):
        if self.mqtt_connection:
            device_log("Disconnecting from MQTT...")
            disconnect_future = self.mqtt_connection.disconnect()
            disconnect_future.result()
            self.mqtt_connection = None
            self.shadow_client = None
            device_log("Disconnected!")

    def _remove_leaked_keychain_cert(self):
        """Remove the client cert that awscrt imports into the macOS login keychain on mtls connect.

        aws-c-io's Secure Transport backend imports the mTLS client identity into the login keychain on every connect and never removes it, so long-lived test machines accumulate thousands of certs. Left unchecked, any process that walks the keychain per TLS handshake (e.g. Cursor/VS Code's @vscode/proxy-agent) ends up pinning a CPU core. The keychain label equals this device's thing_name. Deletion is by SHA-1 hash matched on the label, because delete-certificate -c matches the cert common name (not the label) and does not match these certs. No-op off macOS.
        """
        if sys.platform != "darwin" or not self.node_thing_name:
            return
        keychain = os.path.expanduser("~/Library/Keychains/login.keychain-db")
        try:
            dump = subprocess.run(
                ["security", "find-certificate", "-a", "-Z", keychain],
                capture_output=True, text=True, timeout=60,
            ).stdout
        except Exception:
            return

        # Pair each SHA-1 hash with the label that follows it, collect hashes whose label is this device's thing_name.
        current_hash = None
        hashes_to_delete = []
        for line in dump.splitlines():
            if line.startswith("SHA-1 hash:"):
                current_hash = line.split(":", 1)[1].strip()
            elif '"labl"' in line and current_hash:
                label = line.split("<blob>=", 1)[-1].strip().strip('"')
                if label == self.node_thing_name:
                    hashes_to_delete.append(current_hash)
                current_hash = None

        for cert_hash in hashes_to_delete:
            subprocess.run(
                ["security", "delete-certificate", "-Z", cert_hash, keychain],
                capture_output=True, timeout=30,
            )

    def destroy_test_node(self):
        node_thing_name = self.node_thing_name

        if not node_thing_name:
            device_log("Error: node_thing_name not found in test_config.json")
            return False

        # Create boto3 client
        iot_client = boto3.client('iot', region_name=self.region)
        iot_data_client = boto3.client('iot-data', region_name=self.region, endpoint_url=f'https://{self.iot_endpoint}')

        try:
            # Connect to MQTT and get group info if not already available
            if not self.group_id:
                if not self.mqtt_connection:
                    if not self.connect():
                        device_log("Failed to connect to MQTT")
                        error_response = {
                            'Error': {
                                'Code': 'AccessDeniedException',
                                'Message': 'You are not authorized to perform this operation.'
                            }
                        }

                        # Couldn't connect to MQTT, but proceed with other steps
                        raise ClientError(error_response, operation_name="connect")
                if not self.get_group_info():
                    device_log("Failed to get group info, but continuing with deletion")

            # Delete all shadows associated with the thing
            try:
                # Delete the indexed shadow
                iot_data_client.delete_thing_shadow(
                    thingName=node_thing_name,
                    shadowName='iparams'
                )
                device_log(f"Deleted indexed shadow for thing '{node_thing_name}'")
            except iot_data_client.exceptions.ResourceNotFoundException:
                pass  # Shadow doesn't exist, which is fine

            # Delete any group-based shadows
            if self.group_id:
                shadow_name = f"params-{self.group_id}"
                if hasattr(self, 'subgroup_ids') and self.subgroup_ids:
                    for subgroup_id in sorted(self.subgroup_ids):
                        shadow_name += f"-{subgroup_id}"
                try:
                    iot_data_client.delete_thing_shadow(
                        thingName=node_thing_name,
                        shadowName=shadow_name
                    )
                    device_log(f"Deleted shadow '{shadow_name}' for thing '{node_thing_name}'")
                except iot_data_client.exceptions.ResourceNotFoundException:
                    pass  # Shadow doesn't exist, which is fine

            # Disconnect MQTT if we connected it
            if self.mqtt_connection:
                self.disconnect()

        except ClientError as e:
            device_log(f"An error occurred while deleting shadows: {e}")

        # Remove the node's backend rows so destroyed test nodes don't leave
        # stale entries in rmng-nodes / rmng-nodes-online. For self-registered
        # test nodes node_id == clientId == node_thing_name. Best-effort:
        # never let cleanup block teardown.
        try:
            dynamodb = boto3.client('dynamodb', region_name=self.region)
            dynamodb.delete_item(
                TableName='rmng-nodes',
                Key={'node_id': {'S': node_thing_name}},
            )
            dynamodb.delete_item(
                TableName='rmng-nodes-online',
                Key={'clientId': {'S': node_thing_name}},
            )
            device_log(f"Deleted node_details and nodes-online rows for '{node_thing_name}'")
        except Exception as e:
            device_log(f"Warning: failed to delete node rows for '{node_thing_name}': {e}")

        try:
            # Get the principals (certificates) attached to the thing
            principals = iot_client.list_thing_principals(thingName=node_thing_name)['principals']

            for principal in principals:
                # Detach the principal from the thing
                iot_client.detach_thing_principal(thingName=node_thing_name, principal=principal)
                device_log(f"Detached principal {principal} from thing '{node_thing_name}'")

                # List and detach policies from the certificate
                policies = iot_client.list_attached_policies(target=principal)['policies']
                for policy in policies:
                    iot_client.detach_policy(policyName=policy['policyName'], target=principal)
                    device_log(f"Detached policy {policy['policyName']} from principal {principal}")

                # Update the certificate status to INACTIVE
                certificate_id = principal.split('/')[-1]
                iot_client.update_certificate(certificateId=certificate_id, newStatus='INACTIVE')
                device_log(f"Set certificate {certificate_id} to INACTIVE")

                # Delete the certificate
                iot_client.delete_certificate(certificateId=certificate_id, forceDelete=True)
                device_log(f"Deleted certificate {certificate_id}")

            # Delete the thing
            iot_client.delete_thing(thingName=node_thing_name)
            device_log(f"Deleted thing '{node_thing_name}'")

            # Now that the device is fully gone (real IoT cert + thing deleted), remove the local keychain copy awscrt left behind on connect.
            self._remove_leaked_keychain_cert()

            return True
        except ClientError as e:
            device_log(f"An error occurred: {e}")
            return False

    def get_shadow(self, shadow_name):
        if not self.iot_data_client:
            self.iot_data_client = boto3.client('iot-data',
                                                region_name=self.region,
                                                endpoint_url=f'https://{self.iot_endpoint}')
        try:
            response = self.iot_data_client.get_thing_shadow(thingName=self.node_thing_name, shadowName=shadow_name)
            shadow_state = json.loads(response['payload'].read())
            return shadow_to_unstructured(shadow_state)
        except self.iot_data_client.exceptions.ResourceNotFoundException:
            device_log(f"Shadow '{shadow_name}' not found for thing '{self.node_thing_name}'")
            return None
        except Exception as e:
            device_log(f"Failed to get thing shadow: {str(e)}")
            return None

    def _publish_to_topic(self, topic, data, description="message"):
        """Internal method to publish data to a specific MQTT topic.

        Args:
            topic (str): The MQTT topic to publish to
            data (dict): The data to publish
            description (str): Description for logging

        Returns:
            bool: True if successful, False otherwise
        """
        if not self.mqtt_connection:
            device_log("Error: MQTT not connected. Call connect() first.")
            return False

        try:
            device_log(f"Publishing {description} to topic: {topic}")

            message_json = json.dumps(data)
            message = message_json.encode()

            future, _ = self.mqtt_connection.publish(
                topic=topic,
                payload=message,
                qos=mqtt.QoS.AT_LEAST_ONCE
            )
            future.result()
            device_log(f"Published {description} to topic '{topic}': {message_json}")
            return True
        except Exception as e:
            device_log(f"Error publishing {description}: {str(e)}")
            device_log(f"Error type: {type(e).__name__}")
            import traceback
            traceback.print_exc()
            return False

    def publish_to_cloud(self, data, basic_ingest=True):
        """Publish data to the to_cloud topic.

        Args:
            data (dict): The data to publish
            basic_ingest (bool): When True (default), publish via the Basic
                Ingest form `$aws/rules/node_to_cloud_rule/<topic>` to save
                messaging costs. Pass False to publish on the direct topic.

        Returns:
            bool: True if successful, False otherwise
        """
        topic = f"rainmaker/nodes/{self.node_thing_name}/to_cloud"
        if basic_ingest:
            topic = f"$aws/rules/node_to_cloud_rule/{topic}"
        return self._publish_to_topic(topic, data, "message")

    def send_direct_notification(self, data, basic_ingest=True):
        """Send a direct notification using the device's own group and subgroup information.

        The device must have called get_group_info() first to populate group_id and subgroup_ids.
        The topic will be: rainmaker/nodes/{thingName}/notify/{groupID}-[{subgroupIDs}]
        The IoT rule will extract notify_topic_name as: {groupID}-[{subgroupIDs}]

        The payload format is: {"notify": {...}} to be consistent with shadow updates.

        Args:
            data (dict): The notification data to send (will be wrapped in {"notify": {...}})
            basic_ingest (bool): When True (default), publish via the Basic
                Ingest form `$aws/rules/node_notify_rule/<topic>` to
                save messaging costs. Pass False to publish on the direct
                topic.

        Returns:
            bool: True if successful, False otherwise
        """
        if not self.mqtt_connection:
            device_log("Error: MQTT not connected. Call connect() first.")
            return False

        # Check if we have group information
        if not hasattr(self, 'group_id') or not self.group_id:
            device_log("Error: Device group information not available. Call get_group_info() first.")
            return False

        # Build the topic suffix with group/subgroup information
        topic_suffix = f"{self.group_id}"
        if hasattr(self, 'subgroup_ids') and self.subgroup_ids:
            # Join subgroup IDs with commas
            sub_group_str = ",".join(self.subgroup_ids)
            topic_suffix += f"-{sub_group_str}"

        topic = f"rainmaker/nodes/{self.node_thing_name}/notify/{topic_suffix}"
        if basic_ingest:
            topic = f"$aws/rules/node_notify_rule/{topic}"

        # Wrap the data in the notify field for consistency with shadow updates
        payload = {"notify": data}

        return self._publish_to_topic(topic, payload, "direct notification")

    def wait_for_group_info(self, timeout=15):
        # Wait for the response
        start_time = time.time()
        while time.time() - start_time < timeout:
            try:
                message = self.from_cloud_queue.get(timeout=1)
                if "getGroupInfo" in message:
                    self.group_id = message["getGroupInfo"].get("pgrp")
                    if "subgrps" in message["getGroupInfo"]:
                        self.subgroup_ids = message["getGroupInfo"].get("subgrps")
                    return True
            except Empty:
                pass

        device_log(f"Timeout waiting for getGroupInfo response after {timeout} seconds")
        return False


    def get_group_info(self, timeout=15):
        if not self.mqtt_connection:
            device_log("Error: MQTT not connected. Call connect() first.")
            return None

        # Clear the queue before sending the request
        while not self.from_cloud_queue.empty():
            self.from_cloud_queue.get_nowait()

        # Send the getGroupInfo request
        request = {
            "event": ["getGroupInfo", "getAlexaEn", "getGVAEn", "getSTEn", "getSchedVer", "getTriggerVer"]
        }
        if not self.publish_to_cloud(request):
            device_log("Failed to publish getGroupInfo request")
            return None

        # Wait for the response
        if not self.wait_for_group_info(timeout):
            device_log(f"Timeout waiting for getGroupInfo response after {timeout} seconds")
            return None

        return None

    def get_schedule_version(self, timeout=5):
        """Get the current schedule version from the device.

        Returns:
            int: Schedule version if successful, None if failed
        """
        if not self.mqtt_connection:
            device_log("Error: MQTT not connected. Call connect() first.")
            return None

        # Clear the queue before sending the request
        while not self.from_cloud_queue.empty():
            self.from_cloud_queue.get_nowait()

        # Send the getSchedVer request
        request = {
            "event": ["getSchedVer"]
        }
        if not self.publish_to_cloud(request):
            device_log("Failed to publish getSchedVer request")
            return None

        # Wait for the response
        start_time = time.time()
        while time.time() - start_time < timeout:
            try:
                message = self.from_cloud_queue.get(timeout=1)
                if "getSchedVer" in message:
                    version = message["getSchedVer"].get("version", 0)
                    device_log(f"Received schedule version: {version}")
                    return version
            except Empty:
                continue

        device_log(f"Timeout waiting for getSchedVer response after {timeout} seconds")
        return None

    def get_schedule_details(self, timeout=10):
        """Get the current schedule details from the device.

        Returns:
            dict: Schedule details if successful, None if failed
        """
        if not self.mqtt_connection:
            device_log("Error: MQTT not connected. Call connect() first.")
            return None

        # Clear the queue before sending the request
        while not self.from_cloud_queue.empty():
            self.from_cloud_queue.get_nowait()

        # Send the getSchedDetails request
        request = {
            "event": ["getSchedDetails"]
        }
        if not self.publish_to_cloud(request):
            device_log("Failed to publish getSchedDetails request")
            return None

        # Wait for the response
        start_time = time.time()
        while time.time() - start_time < timeout:
            try:
                message = self.from_cloud_queue.get(timeout=1)
                if "getSchedDetails" in message:
                    schedule = message["getSchedDetails"]
                    device_log(f"Received schedule details: {schedule}")
                    return schedule
            except Empty:
                continue

        device_log(f"Timeout waiting for getSchedDetails response after {timeout} seconds")
        return None

    def wait_for_schedule_update(self, timeout=10):
        """Wait for a schedule update message.

        Returns:
            dict: Schedule data if received, None if timeout
        """
        start_time = time.time()
        while time.time() - start_time < timeout:
            try:
                message = self.from_cloud_queue.get(timeout=1)
                if "getSchedDetails" in message:
                    schedule = message["getSchedDetails"]
                    return schedule
            except Empty:
                continue

        return None

    def publish_timeseries_data(self, k, data_type, value, cumulative=False, timezone="UTC", timestamp=None, basic_ingest=True):
        """Publish timeseries data to the device's timeseries topic.

        Args:
            k (str): Data point key (e.g., "temperature")
            data_type (str): Data type ("int", "float", "bool", "string")
            value: The value to publish
            cumulative (bool): Whether this is cumulative data
            timezone (str): Timezone identifier (IANA name, e.g. "UTC", "Asia/Kolkata")
            timestamp (int): Optional Unix timestamp in seconds (will be converted to milliseconds)
            basic_ingest (bool): When True (default), publish via the Basic
                Ingest form `$aws/rules/node_ts_rule/<topic>` to save
                messaging costs. Pass False to publish on the direct topic.

        Returns:
            bool: True if successful, False otherwise
        """
        if not self.mqtt_connection:
            device_log("Error: MQTT not connected. Call connect() first.")
            return False

        # Check if we have group information
        if not hasattr(self, 'group_id') or not self.group_id:
            device_log("Error: Device group information not available. Call get_group_info() first.")
            return False

        # Build the topic suffix with group information
        topic_suffix = f"{self.group_id}"
        if hasattr(self, 'subgroup_ids') and self.subgroup_ids:
            # Join subgroup IDs with commas
            sub_group_str = ",".join(self.subgroup_ids)
            topic_suffix += f"-{sub_group_str}"

        topic = f"rainmaker/nodes/{self.node_thing_name}/ts/{topic_suffix}"
        if basic_ingest:
            topic = f"$aws/rules/node_ts_rule/{topic}"

        # Build the timeseries data payload
        import time
        if timestamp is not None:
            # Use provided timestamp (convert seconds to milliseconds)
            current_timestamp = int(timestamp * 1000)
        else:
            # Use milliseconds for unique timestamps to avoid overwriting
            current_timestamp = int(time.time() * 1000)

        payload = {
            "k": k,
            "dt": data_type,
            "tz": timezone,
            "t": current_timestamp,
            "v": value,
            "cumulative": cumulative
        }

        return self._publish_to_topic(topic, payload, "timeseries data")

    def publish_timeseries_batch(self, data_points, basic_ingest=True):
        """Publish multiple independently queryable timeseries points in one MQTT message.

        Each item must contain ``k``, ``dt``, ``t`` (Unix seconds), and
        ``v``. Device-side publishing is limited to 100 items per message.
        """
        if not self.mqtt_connection:
            device_log("Error: MQTT not connected. Call connect() first.")
            return False
        if not hasattr(self, 'group_id') or not self.group_id:
            device_log("Error: Device group information not available. Call get_group_info() first.")
            return False
        if not isinstance(data_points, list) or not data_points:
            device_log("Error: Timeseries batch must contain at least one data point.")
            return False
        if len(data_points) > 100:
            device_log("Error: Timeseries batch cannot contain more than 100 data points.")
            return False

        topic_suffix = f"{self.group_id}"
        if hasattr(self, 'subgroup_ids') and self.subgroup_ids:
            topic_suffix += f"-{','.join(self.subgroup_ids)}"

        topic = f"rainmaker/nodes/{self.node_thing_name}/ts/{topic_suffix}/batch"
        if basic_ingest:
            topic = f"$aws/rules/node_ts_batch_rule/{topic}"

        return self._publish_to_topic(topic, {"data": data_points}, "timeseries batch data")

    def set_node_config(self, config_data):
        if not self.mqtt_connection:
            device_log("Error: MQTT not connected. Call connect() first.")
            return False

        # Clear the queue before sending the request
        while not self.from_cloud_queue.empty():
            self.from_cloud_queue.get_nowait()

        event_data = {
            "event": ["setNodeConfig"],
            "setNodeConfig": config_data
        }

        try:
            topic = f"$aws/rules/node_to_cloud_rule/rainmaker/nodes/{self.node_thing_name}/to_cloud"
            message_json = json.dumps(event_data)
            message = message_json.encode()

            future, _ = self.mqtt_connection.publish(
                topic=topic,
                payload=message,
                qos=mqtt.QoS.AT_LEAST_ONCE
            )
            future.result()
            device_log(f"Published setNodeConfig message to topic '{topic}': {message_json}")

            # Wait for the response. The ack round-trips IoT rule -> SQS -> lambda ->
            # from_cloud publish; 5s was routinely missed under load, and a caller that
            # times out and re-publishes clears the queue, discarding a late ack.
            start_time = time.time()
            while time.time() - start_time < 30:
                try:
                    message = self.from_cloud_queue.get(timeout=1)
                    if "setNodeConfig" in message:
                        status = message["setNodeConfig"].get("status")
                        if status == "success":
                            device_log("Node configuration set successfully")
                            return True
                        else:
                            device_log(f"Failed to set node configuration: {message['setNodeConfig'].get('message', 'Unknown error')}")
                            return False
                except Empty:
                    continue  # Keep waiting until timeout

            device_log("Timeout waiting for setNodeConfig response")
            return False

        except Exception as e:
            device_log(f"Error setting node config: {str(e)}")
            return False

    def subscribe(self, topic=None, callback=None, shadow_name=None):
        """Subscribe to a topic or shadow events.

        Args:
            topic (str): Full topic path to subscribe to
            callback (callable): Optional callback function for messages
            shadow_name (str): Optional shadow name for shadow subscriptions
        """
        try:
            if not self.mqtt_connection:
                device_log("Not connected to MQTT")
                return False

            if shadow_name:
                # Handle shadow subscriptions
                if not self.shadow_client:
                    device_log("Shadow client not initialized")
                    return False

                shadow_topics = [
                    f"$aws/things/{self.node_thing_name}/shadow/name/{shadow_name}/update/accepted",
                    f"$aws/things/{self.node_thing_name}/shadow/name/{shadow_name}/update/rejected",
                    f"$aws/things/{self.node_thing_name}/shadow/name/{shadow_name}/update/delta",
                    f"$aws/things/{self.node_thing_name}/shadow/name/{shadow_name}/get/accepted"
                ]

                for shadow_topic in shadow_topics:
                    device_log(f"Subscribing to shadow topic: {shadow_topic}")
                    future, _ = self.mqtt_connection.subscribe(
                        topic=shadow_topic,
                        qos=mqtt.QoS.AT_LEAST_ONCE,
                        callback=self.on_message_received
                    )
                    future.result()

                if callback:
                    self.register_callback('shadow', callback)
                return True

            if topic:
                # Handle regular topic subscriptions
                # The topic should already be a full path
                device_log(f"Subscribing to topic: {topic}")
                future, _ = self.mqtt_connection.subscribe(
                    topic=topic,
                    qos=mqtt.QoS.AT_LEAST_ONCE,
                    callback=self.on_message_received
                )
                future.result()

                # Determine message type based on topic parts.
                # Group control topics (.../groups/<g>/[subgroups/<sg>/]control) route to
                # `params` like the per-device params topic does (see on_message_received).
                topic_parts = topic.split('/')
                is_group_control = (
                    len(topic_parts) >= 5
                    and topic_parts[1] == 'nodes'
                    and topic_parts[2] == 'groups'
                    and 'control' in topic_parts[4:]
                )
                if 'params-' in topic or is_group_control:
                    message_type = 'params'
                elif 'from_cloud' in topic:
                    message_type = 'from_cloud'
                else:
                    device_log(f"Unknown topic type: {topic}")
                    return False

                if callback:
                    self.register_callback(message_type, callback)
                return True

            device_log("No topic or shadow_name provided")
            return False

        except Exception as e:
            device_log(f"Error in subscribe: {str(e)}")
            return False

    def unsubscribe(self, full_topic):
        if not self.mqtt_connection:
            device_log("Error: MQTT not connected.")
            return False

        try:
            # Unsubscribe from topic
            future, _ = self.mqtt_connection.unsubscribe(
                topic=full_topic
            )
            future.result()
            device_log(f"Unsubscribed from topic: {full_topic}")
            return True
        except Exception as e:
            device_log(f"Error unsubscribing: {str(e)}")
            return False

    def register_callback(self, message_type, callback):
        """Register a callback for a specific message type"""
        if message_type not in self.callbacks:
            device_log(f"Unknown message type: {message_type}")
            return False
        self.callbacks[message_type] = callback
        return True

    def wait_for_message(self, queue, timeout=5):
        """Generic message waiting function"""
        try:
            start_time = time.time()
            while time.time() - start_time < timeout:
                try:
                    return queue.get(timeout=0.1)
                except Empty:
                    continue
            device_log(f"Timeout waiting for message after {timeout} seconds")
            return None
        except Exception as e:
            device_log(f"Error waiting for message: {str(e)}")
            return None

    def wait_for_params_message(self, timeout=5):
        """Wait for a parameter change message"""
        return self.wait_for_message(self.params_queue, timeout)

    def wait_for_cloud_message(self, timeout=5):
        """Wait for a from_cloud message"""
        return self.wait_for_message(self.from_cloud_queue, timeout)

    def wait_for_shadow_message(self, timeout=5):
        """Wait for a shadow message"""
        return self.wait_for_message(self.shadow_queue, timeout)

    def get_trigger_version(self, timeout=30):
        """Get the current trigger version from the device.

        Returns:
            int: Trigger version if successful, None if failed
        """
        if not self.mqtt_connection:
            device_log("Error: MQTT not connected. Call connect() first.")
            return None

        # Clear the queue before sending the request
        while not self.from_cloud_queue.empty():
            self.from_cloud_queue.get_nowait()

        # Send the getTriggerVer request
        request = {
            "event": ["getTriggerVer"]
        }
        if not self.publish_to_cloud(request):
            device_log("Failed to publish getTriggerVer request")
            return None

        # Wait for response
        start_time = time.time()
        while time.time() - start_time < timeout:
            try:
                message = self.from_cloud_queue.get(timeout=1)
                if "getTriggerVer" in message:
                    version = message["getTriggerVer"].get("version", 0)
                    device_log(f"Received trigger version: {version}")
                    return version
            except Empty:
                continue

        device_log(f"Timeout waiting for trigger version after {timeout} seconds")
        return None

    def wait_for_trigger_update(self, timeout=5):
        """Wait for a trigger update notification from the cloud.

        Returns:
            dict: Trigger data if received, None if timeout
        """
        start_time = time.time()
        while time.time() - start_time < timeout:
            try:
                message = self.from_cloud_queue.get(timeout=1)
                if "getTriggerDetails" in message:
                    triggers = message["getTriggerDetails"]
                    device_log(f"Received trigger update: {triggers}")
                    return triggers
            except Empty:
                continue

        device_log(f"Timeout waiting for trigger update after {timeout} seconds")
        return None

    def get_trigger_details(self, timeout=5):
        """Get the current trigger details from the device.

        Returns:
            dict: Trigger details if successful, None if failed
        """
        if not self.mqtt_connection:
            device_log("Error: MQTT not connected. Call connect() first.")
            return None

        # Clear the queue before sending the request
        while not self.from_cloud_queue.empty():
            self.from_cloud_queue.get_nowait()

        # Send the getTriggerDetails request
        request = {
            "event": ["getTriggerDetails"]
        }
        if not self.publish_to_cloud(request):
            device_log("Failed to publish getTriggerDetails request")
            return None

        # Wait for the response using wait_for_trigger_update
        return self.wait_for_trigger_update(timeout)

    def clear_queues(self):
        """
        Clear all internal message queues to ensure a clean state.

        This is useful in tests to remove any stale messages before starting a new verification step.
        """
        for q in [self.from_cloud_queue, self.params_queue, self.shadow_queue]:
            try:
                while True:
                    q.get_nowait()
            except Empty:
                pass

    def clear_callbacks(self):
        """
        Clear all registered callbacks to ensure a clean state.

        This is useful in tests to remove any stale callbacks before starting a new verification step.
        """
        self.callbacks = {
            'from_cloud': None,
            'params': None,
            'shadow': None
        }

    def get_s3_client(self, credential_provider_endpoint, role_alias, region):
        """Get a boto3 S3 client using IoT Credential Provider credentials.

        Uses the device's X.509 cert and private key to make an mTLS request to the
        IoT Credential Provider endpoint, then returns an S3 client configured with
        the temporary credentials.

        Also ensures the rmng-node-file-policy is attached to the device's certificate.

        Args:
            credential_provider_endpoint (str): The IoT Credential Provider endpoint hostname
            role_alias (str): The IAM role alias name for device file access
            region (str): AWS region for the S3 client

        Returns:
            boto3.client: An S3 client configured with temporary credentials
        """
        # Ensure the rmng-node-file-policy is attached to this device's certificate
        iot_client = boto3.client("iot", region_name=region)
        principals = iot_client.list_thing_principals(thingName=self.node_thing_name)
        for principal_arn in principals.get("principals", []):
            try:
                iot_client.attach_policy(
                    policyName="rmng-node-file-policy",
                    target=principal_arn,
                )
            except iot_client.exceptions.ResourceAlreadyExistsException:
                pass  # Already attached

        # Write cert and key to temp files for the mTLS request
        with tempfile.NamedTemporaryFile(mode='w', suffix='.pem', delete=False) as cert_file:
            cert_file.write(self.node_cert)
            cert_path = cert_file.name

        with tempfile.NamedTemporaryFile(mode='w', suffix='.pem', delete=False) as key_file:
            key_file.write(self.node_key)
            key_path = key_file.name

        try:
            url = f"https://{credential_provider_endpoint}/role-aliases/{role_alias}/credentials"
            headers = {"x-amzn-iot-thingname": self.node_thing_name}

            response = requests.get(
                url,
                headers=headers,
                cert=(cert_path, key_path),
                verify=True,
            )
            if response.status_code != 200:
                device_log(f"[S3 Cred Provider] Status: {response.status_code}, Body: {response.text}")
            response.raise_for_status()
            creds = response.json()["credentials"]

            return boto3.client(
                "s3",
                region_name=region,
                aws_access_key_id=creds["accessKeyId"],
                aws_secret_access_key=creds["secretAccessKey"],
                aws_session_token=creds["sessionToken"],
            )
        finally:
            os.unlink(cert_path)
            os.unlink(key_path)

def _generate_key_and_cert(thing_name, key_type='ec', node_key=None, node_validity_days=365):
    """Internal implementation that supports passing an existing node_key and custom validity."""
    # Generate CA key
    if key_type == 'ec':
        ca_key = ec.generate_private_key(ec.SECP256R1())
    elif key_type == 'rsa':
        ca_key = rsa.generate_private_key(
            public_exponent=65537,
            key_size=2048
        )
    else:
        raise ValueError("Invalid key type. Use 'ec' or 'rsa'.")

    # Create self-signed CA certificate
    ca_subject = issuer = x509.Name([
        x509.NameAttribute(NameOID.COUNTRY_NAME, u"US"),
        x509.NameAttribute(NameOID.STATE_OR_PROVINCE_NAME, u"California"),
        x509.NameAttribute(NameOID.LOCALITY_NAME, u"San Francisco"),
        x509.NameAttribute(NameOID.ORGANIZATION_NAME, u"Test CA"),
        x509.NameAttribute(NameOID.COMMON_NAME, u"Test CA"),
    ])
    ca_cert = x509.CertificateBuilder().subject_name(
        ca_subject
    ).issuer_name(
        issuer
    ).public_key(
        ca_key.public_key()
    ).serial_number(
        x509.random_serial_number()
    ).not_valid_before(
        datetime.datetime.now(datetime.UTC)
    ).not_valid_after(
        datetime.datetime.now(datetime.UTC) + datetime.timedelta(days=3650)  # 10 years
    ).add_extension(
        x509.BasicConstraints(ca=True, path_length=None), critical=True
    ).sign(ca_key, hashes.SHA256())

    # Generate node key (or use provided one)
    if node_key is None:
        if key_type == 'ec':
            node_key = ec.generate_private_key(ec.SECP256R1())
        elif key_type == 'rsa':
            node_key = rsa.generate_private_key(
                public_exponent=65537,
                key_size=2048
            )

    # Create node certificate signed by CA
    node_subject = x509.Name([
        x509.NameAttribute(NameOID.COUNTRY_NAME, u"US"),
        x509.NameAttribute(NameOID.STATE_OR_PROVINCE_NAME, u"California"),
        x509.NameAttribute(NameOID.LOCALITY_NAME, u"San Francisco"),
        x509.NameAttribute(NameOID.ORGANIZATION_NAME, u"Test Organization"),
        x509.NameAttribute(NameOID.COMMON_NAME, thing_name),
    ])
    node_cert = x509.CertificateBuilder().subject_name(
        node_subject
    ).issuer_name(
        ca_subject
    ).public_key(
        node_key.public_key()
    ).serial_number(
        x509.random_serial_number()
    ).not_valid_before(
        datetime.datetime.now(datetime.UTC)
    ).not_valid_after(
        datetime.datetime.now(datetime.UTC) + datetime.timedelta(days=node_validity_days)
    ).add_extension(
        x509.SubjectAlternativeName([x509.DNSName(thing_name)]),
        critical=False,
    ).sign(ca_key, hashes.SHA256())

    # Serialize private key and certificates to PEM format
    node_key_pem = node_key.private_bytes(
        encoding=serialization.Encoding.PEM,
        # For some reason, the mqtt_connection_builder requires only the EC keys in this format
        format=serialization.PrivateFormat.TraditionalOpenSSL if key_type == 'ec' else serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption()
    ).decode('utf-8')

    node_cert_pem = node_cert.public_bytes(serialization.Encoding.PEM).decode('utf-8')
    ca_cert_pem = ca_cert.public_bytes(serialization.Encoding.PEM).decode('utf-8')

    # Combine node certificate and CA certificate
    combined_cert_pem = node_cert_pem + ca_cert_pem

    return node_key_pem, combined_cert_pem

def generate_key_and_cert(thing_name, key_type='ec'):
    return _generate_key_and_cert(thing_name, key_type=key_type)

def reported_or_desired_shadow_to_structured(target_state):
    """
    Restructures shadow state by moving all fields except data and online into params.
    Assumes shadow structure defined in shadow_node.go.
    """
    if not target_state:
        return target_state
    structured_state = {}
    params = {}
    # Keep data and online fields at top level
    if "data" in target_state:
        structured_state["data"] = target_state["data"]
    if "online" in target_state:
        structured_state["online"] = target_state["online"]
    # Move all other fields into params
    for key, value in target_state.items():
        if key not in ["data", "online", "params"]:
            params[key] = value
    # Add existing params if any
    if "params" in target_state:
        params.update(target_state["params"])
    if params:
        structured_state["params"] = params
    return structured_state

def split_combined_cert_pem(combined_cert_pem):
    # Initialize with empty strings to handle invalid formats
    node_cert = ""
    node_ca_cert = ""
    # node_cert_pem + ca_cert_pem
    certs = combined_cert_pem.split("-----BEGIN CERTIFICATE-----", 2)
    if len(certs) >= 2:
        node_cert = "-----BEGIN CERTIFICATE-----\n" + certs[1].strip()
    else:
        print("Node cert not found")

    if len(certs) == 3:
        # First element will be empty since the cert starts with the delimiter
        node_ca_cert = "-----BEGIN CERTIFICATE-----\n" + certs[2].strip()
    else:
        print("CA not found")

    return node_cert, node_ca_cert

def validate_tags(shadow, expected_tags, tags_type="admin"):
    """
    Validates that the shadow contains the expected tags.

    Args:
        shadow (dict): The shadow data structure to validate
        expected_tags (dict): Dictionary of expected tag key-value pairs
        tags_type (str): The type of tags to validate ('user' or 'admin')

    Returns:
        bool: True if all expected tags are present with correct values, False otherwise
    """
    if not shadow or "state" not in shadow:
        device_log("Invalid shadow data structure")
        return False

    if "reported" not in shadow["state"]:
        device_log("No reported state in shadow")
        return False

    reported = shadow["state"]["reported"]

    if "data" not in reported:
        device_log("No data field in reported state")
        return False

    if tags_type not in reported["data"]:
        device_log(f"No {tags_type} field in data")
        return False

    if "t" not in reported["data"][tags_type]:
        device_log(f"No tags ('t') field in {tags_type} data")
        return False

    tags = reported["data"][tags_type]["t"]

    # Check that all expected tags exist with correct values
    missing_tags = []
    incorrect_tags = []

    for key, expected_value in expected_tags.items():
        if key not in tags:
            missing_tags.append(key)
        elif tags[key] != expected_value:
            incorrect_tags.append(f"{key} (expected: {expected_value}, got: {tags[key]})")

    if missing_tags:
        device_log(f"Missing expected tags: {', '.join(missing_tags)}")
        return False

    if incorrect_tags:
        device_log(f"Tags with incorrect values: {', '.join(incorrect_tags)}")
        return False

    return True
