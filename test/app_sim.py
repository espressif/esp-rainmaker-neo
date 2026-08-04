# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import json
import sys
import threading
import os
from scripts.rmng_outputs import REPO_ROOT, TEST_CONFIG_PATH, RmngSettings
from py_sdk.test_user import User
from py_sdk.test_group import Group
from py_sdk.test_util import shadow_to_unstructured
from test import prov_ble
from queue import Queue, Empty
from prompt_toolkit import PromptSession
from prompt_toolkit.history import FileHistory
from prompt_toolkit.auto_suggest import AutoSuggestFromHistory
import pathlib
import os
import time
import asyncio
import datetime

# Repo-anchored so the cache is shared no matter which directory the simulator runs from; a cache
# that looks empty from a new CWD would re-fetch every node config the app already had.
APP_CACHE_DIR = os.path.join(REPO_ROOT, '.sim', 'app')

# ANSI color codes
blue = "\033[94m"
reset = "\033[0m"

class AppSim:
    def __init__(self, user_id, config_path=TEST_CONFIG_PATH, rmng_outputs_path=None):
        """Initialize the app simulator"""
        self.user_id = user_id

        settings = RmngSettings.from_source(rmng_outputs_path)
        self.region = settings.region
        self.identity_pool_id = settings.identity_pool_id
        self.api_gateway_url = settings.api_gateway_url
        self.iot_endpoint = settings.iot_endpoint
        self.admin_user_pool_id = settings.admin_user_pool_id
        self.admin_client_id = settings.admin_client_id
        self.user_api_gateway_url = settings.user_api_gateway_url
        self.end_user_pool_id = settings.end_user_pool_id

        # Read configurations
        with open(config_path, 'r') as f:
            self.config = json.load(f)

        # Get user configuration
        self.user = self._get_user_config(user_id)
        if not self.user:
            raise ValueError(f"Failed to find user configuration for user {user_id}")

        # Initialize state
        self.message_queue = Queue()
        self.group_api = Group(self.user)
        self.selected_group = None

        # Cache for device states
        self.device_states = {}  # Format: {device_id: {"reported": state_dict, "timestamp": timestamp}}

        # Register shadow callback
        self.user_on_named_shadow_updated = self.user.on_named_shadow_updated
        self.user.on_named_shadow_updated = self.on_named_shadow_updated

        # Initialize counters for statistics
        self.shadow_ops_count = 0
        self.http_api_count = 0
        self.mqtt_msg_count = 0

        # Ensure cache directory exists
        os.makedirs(APP_CACHE_DIR, exist_ok=True)

    def _get_user_config(self, user_id):
        users = self.config.get('users', [])
        try:
            index = int(user_id)
            if 0 <= index < len(users):
                user_config = users[index]
            else:
                raise ValueError
        except ValueError:
            for user in users:
                if user.get('name') == user_id:
                    user_config = user
                    break
            else:
                print(f"Error: User '{user_id}' not found")
                return None

        username = user_config.get('name')
        password = user_config.get('password')
        is_super_admin = user_config.get('super_admin', False)

        if not username or not password:
            print(f"Error: Missing required configuration for user {user_id}")
            return None

        if is_super_admin:
            return User(username, password, self.region,
                       self.identity_pool_id, self.api_gateway_url, self.user_api_gateway_url, self.iot_endpoint,
                       admin_user_pool_id=self.admin_user_pool_id, admin_client_id=self.admin_client_id, is_super_admin=True)
        # end_user_pool_id is what lets provisioning reach the pool this user signs in against.
        return User(username, password, self.region,
                    self.identity_pool_id, self.api_gateway_url, self.user_api_gateway_url, self.iot_endpoint,
                    end_user_pool_id=self.end_user_pool_id)

    def _format_timestamp(self, timestamp):
        """Helper to format timestamp for JSON serialization"""
        if hasattr(timestamp, 'isoformat'):  # If it's a datetime object
            return timestamp.isoformat()
        return timestamp

    def _print_stats(self):
        """Print statistics in colored output"""
        # ANSI color codes
        CYAN = '\033[96m'
        RESET = '\033[0m'

        print(f"{CYAN}Stats: Shadow operations: {self.shadow_ops_count}, MQTT messages: {self.mqtt_msg_count} HTTP API calls: {self.http_api_count}{RESET}")

    def on_named_shadow_updated(self, shadow_name, payload):
        """Callback for named shadow updates"""
        try:
            # Receiving shadow updates is not a billable shadow operation
            # but it is a billable MQTT message
            self.mqtt_msg_count += 1
            self._print_stats()

            # Extract the device name from the topic
            # Topic format: $aws/things/{thing_name}/shadow/name/{shadow_name}/update/accepted
            # or: $aws/things/{thing_name}/shadow/name/{shadow_name}/get/accepted
            topic = payload.get('topic')
            if not topic:
                print("Error: No topic found in payload")
                return

            topic_parts = topic.split('/')
            if len(topic_parts) < 4:
                print("Error: Invalid topic format")
                return

            payload = shadow_to_unstructured(payload)

            thing_name = topic_parts[2]  # Device name is the third part (index 2)

            # Determine if this is a response to a GET or an UPDATE
            is_get_response = topic.endswith('/get/accepted')

            # Extract shadow state from the payload
            shadow_state = payload["state"]

            # Get timestamp for our records
            timestamp = self._format_timestamp(payload.get('timestamp'))

            # Update device state cache if there's reported state
            if "state" in shadow_state and "reported" in shadow_state["state"]:
                reported_state = shadow_state["state"]["reported"]
                self.device_states[thing_name] = {
                    "reported": reported_state,
                    "timestamp": timestamp
                }

                # Print the shadow state in the same format as test_user.py
                version = shadow_state.get("version", "?")
                print(f"\nDevice: {thing_name}")
                print(f"[{shadow_name}][v{version}][reported] {blue}updated to{reset} {reported_state}")

                if "desired" in shadow_state["state"]:
                    desired_state = shadow_state["state"]["desired"]
                    print(f"[{shadow_name}][v{version}][desired] {blue}updated to{reset} {desired_state}")

            # call the replaced callback
            self.user_on_named_shadow_updated(shadow_name, payload)

        except Exception as e:
            print(f"Error parsing named shadow update: {str(e)}")
            print("Debug - Payload:", json.dumps(payload, indent=2))
            import traceback
            print(traceback.format_exc())

    def _fetch_node_configs(self, group_id, selected_group, devices_in_subgroups):
        """Fetch node configurations for all devices in a group, using cached values when possible"""
        print("\nFetching node configurations...")

        # Create the cached configurations directory if it doesn't exist
        os.makedirs(APP_CACHE_DIR, exist_ok=True)
        cache_file = os.path.join(APP_CACHE_DIR, f"{self.user.username}-app-config-cache.json")

        # Load the cached configurations if they exist
        config_cache = {}
        try:
            if os.path.exists(cache_file):
                with open(cache_file, 'r') as f:
                    config_cache = json.load(f)
                    print(f"Loaded cached configurations for {len(config_cache)} devices")
        except Exception as e:
            print(f"Error loading cached configurations: {str(e)}")

        # Set to track which device configs we've already fetched
        fetched_configs = set()

        # Flag to track if cache was modified
        cache_modified = False

        # First, try to fetch node configurations for devices in the main group (not in subgroups)
        try:
            for node_id in selected_group.get("node_ids", []):
                if node_id in devices_in_subgroups:
                    continue  # Skip devices in subgroups for now

                # Check if we have this device's shadow state with ncfg_ver
                current_ncfg_ver = None
                if node_id in self.device_states and 'reported' in self.device_states[node_id] and \
                   'ncfg_ver' in self.device_states[node_id]['reported']:
                    current_ncfg_ver = self.device_states[node_id]['reported']['ncfg_ver']
                    print(f"Device {node_id} has ncfg_ver: {current_ncfg_ver}")

                # Check if we have a cached configuration that matches the current ncfg_ver
                cached = False
                if node_id in config_cache and 'ncfg_ver' in config_cache[node_id] and 'config' in config_cache[node_id]:
                    if current_ncfg_ver is not None and config_cache[node_id]['ncfg_ver'] == current_ncfg_ver:
                        print(f"Using cached configuration for device {node_id} with ncfg_ver {current_ncfg_ver}")
                        fetched_configs.add(node_id)
                        cached = True

                # If not cached or ncfg_ver doesn't match, fetch the configuration
                if not cached:
                    print(f"Fetching configuration for device {node_id}")
                    try:
                        node_config = self.user.get_node_config(group_id, "", node_id)
                        self.http_api_count += 1
                        self._print_stats()

                        # Cache the configuration with its ncfg_ver
                        if node_config:
                            print(f"Received configuration for device {node_id}")
                            if node_id not in config_cache:
                                config_cache[node_id] = {}

                            # Check if we need to update the cache
                            cache_needs_update = (
                                node_id not in config_cache or
                                'config' not in config_cache[node_id] or
                                config_cache[node_id]['config'] != node_config or
                                (current_ncfg_ver is not None and
                                 ('ncfg_ver' not in config_cache[node_id] or
                                  config_cache[node_id]['ncfg_ver'] != current_ncfg_ver))
                            )

                            if cache_needs_update:
                                config_cache[node_id]['config'] = node_config
                                if current_ncfg_ver is not None:
                                    config_cache[node_id]['ncfg_ver'] = current_ncfg_ver
                                cache_modified = True
                                print(f"Updated cache for device {node_id}")

                            fetched_configs.add(node_id)
                    except Exception as e:
                        print(f"Error fetching configuration for device {node_id}: {str(e)}")
        except Exception as e:
            print(f"Error fetching configurations for main group devices: {str(e)}")

        # Now, fetch configurations for devices in subgroups
        for subgroup in selected_group.get("subgroups", []):
            subgroup_id = subgroup["subgroup_id"]
            try:
                for node_id in subgroup.get("node_ids", []):
                    if node_id in fetched_configs:
                        continue  # Skip if we already fetched this device's config

                    # Check if we have this device's shadow state with ncfg_ver
                    current_ncfg_ver = None
                    if node_id in self.device_states and 'reported' in self.device_states[node_id] and \
                       'ncfg_ver' in self.device_states[node_id]['reported']:
                        current_ncfg_ver = self.device_states[node_id]['reported']['ncfg_ver']
                        print(f"Device {node_id} in subgroup {subgroup_id} has ncfg_ver: {current_ncfg_ver}")

                    # Check if we have a cached configuration that matches the current ncfg_ver
                    cached = False
                    if node_id in config_cache and 'ncfg_ver' in config_cache[node_id] and 'config' in config_cache[node_id]:
                        if current_ncfg_ver is not None and config_cache[node_id]['ncfg_ver'] == current_ncfg_ver:
                            print(f"Using cached configuration for device {node_id} in subgroup {subgroup_id} with ncfg_ver {current_ncfg_ver}")
                            fetched_configs.add(node_id)
                            cached = True

                    # If not cached or ncfg_ver doesn't match, fetch the configuration
                    if not cached:
                        print(f"Fetching configuration for device {node_id} in subgroup {subgroup_id}")
                        try:
                            node_config = self.user.get_node_config(group_id, subgroup_id, node_id)
                            self.http_api_count += 1
                            self._print_stats()

                            # Cache the configuration with its ncfg_ver
                            if node_config:
                                print(f"Received configuration for device {node_id} in subgroup {subgroup_id}")
                                if node_id not in config_cache:
                                    config_cache[node_id] = {}

                                # Check if we need to update the cache
                                cache_needs_update = (
                                    node_id not in config_cache or
                                    'config' not in config_cache[node_id] or
                                    config_cache[node_id]['config'] != node_config or
                                    (current_ncfg_ver is not None and
                                     ('ncfg_ver' not in config_cache[node_id] or
                                      config_cache[node_id]['ncfg_ver'] != current_ncfg_ver))
                                )

                                if cache_needs_update:
                                    config_cache[node_id]['config'] = node_config
                                    if current_ncfg_ver is not None:
                                        config_cache[node_id]['ncfg_ver'] = current_ncfg_ver
                                    cache_modified = True
                                    print(f"Updated cache for device {node_id} in subgroup {subgroup_id}")

                                fetched_configs.add(node_id)
                        except Exception as e:
                            print(f"Error fetching configuration for device {node_id} in subgroup {subgroup_id}: {str(e)}")
            except Exception as e:
                print(f"Error fetching configurations for subgroup {subgroup_id}: {str(e)}")

        # Save the updated cache only if it was modified
        if cache_modified:
            try:
                with open(cache_file, 'w') as f:
                    json.dump(config_cache, f, indent=2)
                print(f"Saved {len(config_cache)} cached configurations to {cache_file}")
            except Exception as e:
                print(f"Warning: Could not save config cache: {str(e)}")
        else:
            print("No changes to configuration cache, skipping file write")

    def _read_device_shadows(self, group_id, selected_group, devices_in_subgroups, timeout=5):
        """Read all device shadows to get their current state including ncfg_ver values"""
        print("\nReading device shadows to determine configuration status...")

        # Create a mapping of device to its shadow names
        device_shadows = {}
        base_shadow = f"params-{group_id}"

        # First, identify devices that are in subgroups and map each device to its shadow name
        for node_id in selected_group.get("node_ids", []):
            if node_id not in devices_in_subgroups:
                device_shadows[node_id] = base_shadow

        # Process subgroups and add shadow names for devices in subgroups
        for node_id in devices_in_subgroups:
            # Get all subgroups this device belongs to
            device_subgroups = []
            for subgroup in selected_group.get("subgroups", []):
                if node_id in subgroup.get("node_ids", []):
                    device_subgroups.append(subgroup["subgroup_id"])

            # Sort subgroups alphabetically
            device_subgroups.sort()

            # Create shadow name with subgroups
            shadow_with_subgroups = f"{base_shadow}-{'-'.join(device_subgroups)}"
            device_shadows[node_id] = shadow_with_subgroups

        # Read shadows synchronously for each device
        read_count = 0
        success_count = 0

        for node_id, shadow_name in device_shadows.items():
            try:
                print(f"Reading shadow for device {node_id}: {shadow_name}")
                read_count += 1
                if not self.user.read_shadow(node_id, shadow_name):
                    print(f"Failed to read shadow for device {node_id}: {shadow_name}")
                    continue

                shadow_data = self.user.shadow_queue.get(timeout=timeout)

                # This is a billable shadow operation
                self.shadow_ops_count += 1
                self.mqtt_msg_count += 1
                self._print_stats()

                if shadow_data:
                    # Extract the reported state from the response
                    if 'state' in shadow_data and 'reported' in shadow_data['state']:
                        reported_state = shadow_data['state']['reported']
                        timestamp = self._format_timestamp(shadow_data.get('timestamp'))

                        # Store in device_states cache
                        self.device_states[node_id] = {
                            "reported": reported_state,
                            "timestamp": timestamp
                        }
                        print(f"Shadow state for {node_id}: {json.dumps(reported_state, indent=2)}")
                        success_count += 1
                    else:
                        print(f"No reported state found in shadow for device {node_id}")
                else:
                    print(f"Failed to read shadow for device {node_id}")
            except Exception as e:
                print(f"Error reading shadow for device {node_id}: {str(e)}")

        print(f"Successfully read shadow data for {success_count} of {read_count} devices")
        return True

    def list_groups(self):
        """List all groups associated with the user"""
        if not self.user:
            print("Error: User not initialized")
            return False

        try:
            response = self.user.make_api_request('GET', '/v1/groups')
            self.http_api_count += 1
            self._print_stats()
            if not response:
                print("Error: Failed to get groups")
                return False

            groups_data = response.json()
            print("\nGroups:")
            print(json.dumps(groups_data, indent=2))

            # Store groups for later use
            self.groups = groups_data.get('groups', [])

            # Update our API statistics
            self._print_stats()
            return True

        except Exception as e:
            print(f"Error listing groups: {str(e)}")
            return False

    def select_home(self, group_id=None):
        """Select a home (group) and subscribe to all its devices' shadows"""
        # Get the list of groups
        groups_data = None
        try:
            response = self.user.make_api_request('GET', '/v1/groups')
            self.http_api_count += 1
            self._print_stats()
            if not response:
                print("Error: Failed to get groups")
                return False
            groups_data = response.json()
        except Exception as e:
            print(f"Error getting groups: {str(e)}")
            return False

        if not groups_data.get("groups"):
            print("No groups available to select")
            return False

        # If no group_id provided and only one group exists, auto-select it
        if not group_id and len(groups_data["groups"]) == 1:
            group_id = groups_data["groups"][0]["group_id"]
            print(f"Auto-selecting the only available group: {group_id}")
        elif not group_id:
            print("Please specify a group ID. Available groups:")
            for group in groups_data["groups"]:
                print(f"  {group['group_id']}: {group.get('group_name', 'Unnamed Group')}")
            return False

        # Verify the group exists and find the selected group
        selected_group = None
        for group in groups_data["groups"]:
            if group["group_id"] == group_id:
                selected_group = group
                break

        if not selected_group:
            print(f"Group {group_id} not found")
            return False

        # First, identify devices that are in subgroups
        devices_in_subgroups = set()
        for subgroup in selected_group.get("subgroups", []):
            for node_id in subgroup.get("node_ids", []):
                devices_in_subgroups.add(node_id)

        # Subscribe to shadows for all devices
        device_shadows = {}
        all_shadows = set()
        base_shadow = f"params-{group_id}"

        # Add base shadow only for devices not in any subgroup
        for node_id in selected_group.get("node_ids", []):
            if node_id not in devices_in_subgroups:
                device_shadows[node_id] = [base_shadow]
                all_shadows.add(base_shadow)

        # Process subgroups and add shadow names for devices in subgroups
        for node_id in devices_in_subgroups:
            # Get all subgroups this device belongs to
            device_subgroups = []
            for subgroup in selected_group.get("subgroups", []):
                if node_id in subgroup.get("node_ids", []):
                    device_subgroups.append(subgroup["subgroup_id"])
            # Sort subgroups alphabetically
            device_subgroups.sort()
            # Create shadow name with subgroups
            shadow_with_subgroups = f"{base_shadow}-{'-'.join(device_subgroups)}"
            device_shadows[node_id] = [shadow_with_subgroups]
            all_shadows.add(shadow_with_subgroups)

        # Subscribe to shadow topics for updates
        print(f"\nSubscribing to shadows for group {group_id}:")
        for shadow_name in sorted(all_shadows):
            print(f"  {shadow_name}")
            if not self.user.subscribe_to_named_shadows('+', [shadow_name]):
                print(f"Failed to subscribe to shadow {shadow_name}")
                return False
            # This is a shadow subscription (billable shadow operation)
            self.shadow_ops_count += 1
            # MQTT subscription itself is also a message
            self.mqtt_msg_count += 3
            self._print_stats()

        # Read all device shadows to get their current state including ncfg_ver
        self._read_device_shadows(group_id, selected_group, devices_in_subgroups)

        self.selected_group = selected_group
        print(f"\nSelected group: {group_id}")

        # Print device to shadow mapping for debugging
        print("\nDevice shadow mappings:")
        for device, shadows in device_shadows.items():
            print(f"  {device}: {', '.join(shadows)}")

        # Then fetch node configurations for all devices
        self._fetch_node_configs(group_id, selected_group, devices_in_subgroups)

        return True

    def start(self):
        """Start the app simulation"""
        print(f"Starting app simulator for user: {self.user.username}")

        # Authenticate user
        if not self.user.get_aws_credentials():
            print("Failed to authenticate user")
            return False
        print("User authenticated successfully")
        self.http_api_count += 1
        self._print_stats()

        # Connect to MQTT
        credentials = self.user.assume_role()
        if not credentials:
            print("Failed to assume role")
            return False
        print("Role assumed successfully")
        self.http_api_count += 1
        self._print_stats()

        if not self.user.mqtt_connect():  # No need to pass credentials, mqtt_connect will get them if needed
            print("Failed to connect to MQTT")
            return False
        print("Connected to MQTT successfully")
        # MQTT connection establishment counts as a message
        self.mqtt_msg_count += 1
        self.http_api_count += 1
        self._print_stats()

        # List groups
        groups_data = self.group_api.list_groups()
        self.http_api_count += 1
        self._print_stats()

        if "groups" in groups_data:
            print("\nUser's Groups:")
            print(json.dumps(groups_data, indent=2))
            for group in groups_data["groups"]:
                self.user.add_group_id(group["group_id"])
            print(f"Retrieved and stored {len(self.user.get_group_ids())} groups for user {self.user.username}")

            # Auto-select home if only one group exists
            if len(groups_data["groups"]) == 1:
                group_id = groups_data["groups"][0]["group_id"]
                selected_group = groups_data["groups"][0]

                # First, identify devices that are in subgroups
                devices_in_subgroups = set()
                for subgroup in selected_group.get("subgroups", []):
                    for node_id in subgroup.get("node_ids", []):
                        devices_in_subgroups.add(node_id)

                print(f"Auto-selecting the only available group: {group_id}")

                # Setup shadow subscriptions and read current shadow states
                if self.select_home(group_id):
                    print("Home selection completed successfully")
                else:
                    print("Warning: Could not auto-select home")
        else:
            print("No groups found for the user")

        return True

    def stop(self):
        """Stop the app simulation"""
        if self.user:
            self.user.mqtt_disconnect()
        print("Disconnected from MQTT")

    def handle_device_command(self, sub_command, args):
        """Handle device-related commands"""
        if sub_command == "update":
            if len(args) < 2:
                print("Usage: update <device_name> <json_payload>")
                return
            device_name = args[0]
            try:
                payload = json.loads(" ".join(args[1:]))
            except json.JSONDecodeError:
                print("Error: Invalid JSON payload")
                return

            if not self.selected_group:
                print("Error: No group selected. Use 'select' command first.")
                return

            # Find the device in the group and its subgroups
            device_found = False
            device_subgroups = []

            # Check if device is in any subgroup
            for subgroup in self.selected_group.get("subgroups", []):
                if device_name in subgroup.get("node_ids", []):
                    device_found = True
                    device_subgroups.append(subgroup["subgroup_id"])

            # If not in subgroups, check if it's in the main group
            if not device_subgroups and device_name in self.selected_group.get("node_ids", []):
                device_found = True

            if not device_found:
                print(f"Error: Device '{device_name}' not found in the selected group or its subgroups")
                return

            # Construct the topic name based on group and subgroups
            topic_name = f"params-{self.selected_group['group_id']}"
            if device_subgroups:
                # Sort subgroups alphabetically
                device_subgroups.sort()
                topic_name = f"{topic_name}-{'-'.join(device_subgroups)}"

            topic_name = f"{topic_name}/params"

            success = self.user.mqtt_publish_to_topic(device_name, topic_name, payload)
            if success:
                print(f"Published update to device {device_name} on topic {topic_name}:")
                print(json.dumps(payload, indent=2))
                # Increment shadow operations counter (billable update)
                self.shadow_ops_count += 1
                # Increment MQTT message counter (publishing is a billable MQTT message)
                self.mqtt_msg_count += 1
                self._print_stats()
            else:
                print(f"Failed to publish update to device {device_name}")
            return
        else:
            print(f"Error: Unknown device command '{sub_command}'")
            return False

    def handle_update_group_command(self, args):
        """Publish a device-type-addressed payload to the selected group's control topic"""
        if not self.selected_group:
            print("Error: No group selected. Use 'select' command first.")
            return
        if len(args) < 1:
            print('Usage: update_group <json_payload>')
            print('  e.g. update_group {"esp.device.light": {"params": {"esp.param.power": true}}}')
            return

        try:
            payload = json.loads(" ".join(args))
        except json.JSONDecodeError:
            print("Error: Invalid JSON payload")
            return

        group_id = self.selected_group['group_id']
        if self.user.mqtt_publish_to_group_control(group_id, payload):
            print(f"Published update to group {group_id}:")
            print(json.dumps(payload, indent=2))
            self.shadow_ops_count += 1
            self.mqtt_msg_count += 1
            self._print_stats()
        else:
            print(f"Failed to publish update to group {group_id}")

    def handle_update_subgroup_command(self, args):
        """Publish a device-type-addressed payload to a subgroup's control topic"""
        if not self.selected_group:
            print("Error: No group selected. Use 'select' command first.")
            return
        if len(args) < 2:
            print('Usage: update_subgroup <subgroup_id> <json_payload>')
            print('  e.g. update_subgroup sg1 {"esp.device.light": {"params": {"esp.param.power": true}}}')
            subgroups = self.selected_group.get("subgroups", [])
            if subgroups:
                print(f"Available subgroups: {', '.join(s['subgroup_id'] for s in subgroups)}")
            return

        subgroup_id = args[0]
        try:
            payload = json.loads(" ".join(args[1:]))
        except json.JSONDecodeError:
            print("Error: Invalid JSON payload")
            return

        # Verify the subgroup belongs to the selected group
        if not any(s["subgroup_id"] == subgroup_id for s in self.selected_group.get("subgroups", [])):
            print(f"Error: Subgroup {subgroup_id} not found in the selected group")
            return

        group_id = self.selected_group['group_id']
        if self.user.mqtt_publish_to_group_control(group_id, payload, subgroup_id=subgroup_id):
            print(f"Published update to subgroup {subgroup_id} in group {group_id}:")
            print(json.dumps(payload, indent=2))
            self.shadow_ops_count += 1
            self.mqtt_msg_count += 1
            self._print_stats()
        else:
            print(f"Failed to publish update to subgroup {subgroup_id}")

    def _get_next_trigger_id(self, automation_id, device_id, existing_triggers):
        """Generate the next trigger ID for a device

        Args:
            automation_id (str): The ID of the automation
            device_id (str): The ID of the device
            existing_triggers (list): List of existing triggers for the device

        Returns:
            str: The next trigger ID in the format <device_id>~<automation_id>~<3digitID>
        """
        # Find the maximum trigger ID for this automation and device
        max_sequence = -1

        if existing_triggers:
            for trigger in existing_triggers:
                trigger_id = trigger.get("id", "")
                # Check if this trigger belongs to our automation and device
                expected_prefix = f"{device_id}~{automation_id}~"
                if trigger_id.startswith(expected_prefix):
                    # Extract the 3-digit sequence
                    try:
                        sequence = int(trigger_id.split("~")[-1])
                        max_sequence = max(max_sequence, sequence)
                    except (ValueError, IndexError):
                        # Skip invalid trigger IDs
                        continue

        # Next sequence is one more than the maximum found, or 0 if none found
        next_sequence = max_sequence + 1 if max_sequence >= 0 else 0

        # Format with leading zeros to ensure 3 digits
        return f"{device_id}~{automation_id}~{next_sequence:03d}"

    def create_empty_automation(self, group_id, name):
        """Create an empty automation to get an automation ID"""
        if not group_id:
            print("Error: Group ID is required")
            return None

        empty_automation = {
            "name": name,
            "conditions": {"and": []},
            "actions": {"targets": []}
        }

        response = self.user.create_automation(group_id, empty_automation)
        self.http_api_count += 1
        self._print_stats()

        if not response:
            print("Failed to create empty automation")
            return None

        print(f"Created empty automation: {json.dumps(response, indent=2)}")
        # Create returns the new id under 'automation_id'; 'id' kept as a fallback.
        return response.get('automation_id') or response.get('id')

    def add_trigger(self, group_id, node_id, automation_id, device, param, operator, value):
        """Add a trigger to a node for an automation

        Args:
            group_id (str): Group ID
            node_id (str): Node ID (the physical device)
            automation_id (str): Automation ID
            device (str): Device name within the node (e.g., Light1)
            param (str): Parameter to trigger on
            operator (str): Comparison operator
            value: Value to compare with
        """
        if not group_id or not node_id or not automation_id:
            print("Error: Group ID, node ID, and automation ID are required")
            return None

        # Get existing triggers
        existing_triggers = self.user.get_node_trigger(group_id, node_id)
        self.http_api_count += 1
        self._print_stats()

        triggers = []
        if existing_triggers and 'triggers' in existing_triggers:
            triggers = existing_triggers['triggers']

        # Generate trigger ID based on existing triggers
        trigger_id = self._get_next_trigger_id(automation_id, node_id, triggers)

        # Add new trigger
        new_trigger = {
            "id": trigger_id,
            "device": device,
            "param": param,
            "operator": operator,
            "value": value
        }

        triggers.append(new_trigger)

        # Update triggers
        success = self.user.set_node_trigger(
            group_id,
            node_id,
            json.dumps({"triggers": triggers})
        )
        self.http_api_count += 1
        self._print_stats()

        if not success:
            print(f"Failed to add trigger to node {node_id}, device {device}")
            return None

        print(f"Added trigger {trigger_id} to node {node_id}, device {device}")
        return trigger_id

    def update_automation(self, group_id, automation_id, name, trigger_ids, action_targets):
        """Update an automation with triggers and actions"""
        if not group_id or not automation_id:
            print("Error: Group ID and automation ID are required")
            return False

        automation = {
            "name": name,
            "conditions": {"and": trigger_ids},
            "actions": {"targets": action_targets}
        }

        response = self.user.update_automation(group_id, automation_id, automation)
        self.http_api_count += 1
        self._print_stats()

        if not response:
            print(f"Failed to update automation {automation_id}")
            return False

        print(f"Updated automation {automation_id}")
        print(json.dumps(response, indent=2))
        return True

    def handle_automation_command(self, args):
        """Handle automation-related commands and their subcommands"""
        if not self.selected_group:
            print("Error: No group selected. Use 'select' command first.")
            return

        group_id = self.selected_group['group_id']

        if len(args) == 0:
            print("Usage: automation create <name>")
            print("       automation add-trigger <automation_id> <node_id> <device> <param> <operator> <value>")
            print("       automation add-action <automation_id> <node> <path> <value>")
            print("       automation complete <automation_id>")
            print("       automation list")
            print("       automation get <automation_id>")
            print("       automation delete <automation_id>")
            return

        sub_command = args[0].lower()

        if sub_command == "create":
            if len(args) < 2:
                print("Usage: automation create <name>")
                return

            name = " ".join(args[1:])
            print(f"Creating automation: {name}")

            # Step 1: Create empty automation
            automation_id = self.create_empty_automation(group_id, name)
            if not automation_id:
                return

            print("\nAutomation created with ID: " + automation_id)
            print(f"Available nodes in your group: {', '.join(self.selected_group.get('node_ids', []))}")
            print("Next, add triggers with: automation add-trigger " + automation_id + " <node_id> <device> <param> <operator> <value>")
            print("Example: automation add-trigger " + automation_id + " node_multi 'Colour Light' V > 50")

        elif sub_command == "add-trigger":
            if len(args) < 7:
                print("Usage: automation add-trigger <automation_id> <node_id> <device> <param> <operator> <value>")
                print(f"Available nodes in your group: {', '.join(self.selected_group.get('node_ids', []))}")
                return

            automation_id = args[1]
            node_id = args[2]
            device = args[3]
            param = args[4]
            operator = args[5]
            value_str = " ".join(args[6:])

            # Verify the node exists in the group
            if node_id not in self.selected_group.get('node_ids', []):
                print(f"Error: Node {node_id} not found in the current group")
                print(f"Available nodes: {', '.join(self.selected_group.get('node_ids', []))}")
                return

            # Convert value to appropriate type
            try:
                if value_str.lower() == 'true':
                    value = True
                elif value_str.lower() == 'false':
                    value = False
                elif '.' in value_str:
                    value = float(value_str)
                else:
                    value = int(value_str)
            except ValueError:
                value = value_str

            # First get the existing automation to extract its current state
            automation = self.user.get_automation(group_id, automation_id)
            self.http_api_count += 1
            self._print_stats()

            if not automation:
                print(f"Automation {automation_id} not found")
                return

            # get_automation() returns the object flat, so read fields off the top level; a `payload` unwrap here silently reset name/triggers/actions to defaults on every edit.
            name = automation.get("name", "Unnamed Automation")
            trigger_ids = automation.get("conditions", {}).get("and", [])
            action_targets = automation.get("actions", {}).get("targets", [])

            # Add the trigger to the node
            trigger_id = self.add_trigger(group_id, node_id, automation_id, device, param, operator, value)
            if not trigger_id:
                return

            # Add the trigger ID to the automation's conditions
            if trigger_id not in trigger_ids:
                trigger_ids.append(trigger_id)

            # Update the automation with the new trigger
            self.update_automation(group_id, automation_id, name, trigger_ids, action_targets)
            print(f"Trigger added with ID: {trigger_id} and linked to automation {automation_id}")
            print("Add more triggers or add actions with: automation add-action " + automation_id + " <node> <path> <value>")
            print("Example: automation add-action " + automation_id + " node_multi 'On Off Light' Power true")

        elif sub_command == "add-action":
            if len(args) < 5:
                print("Usage: automation add-action <automation_id> <node> <path> <value>")
                print("  <path>: within-node data point, e.g. \"Light.Power\" (default) or \"0x1.0x6.0x0\" (matter)")
                print(f"Available nodes in your group: {', '.join(self.selected_group.get('node_ids', []))}")
                return

            automation_id = args[1]
            node = args[2]
            # Strip quotes a user typed to protect a dotted path (e.g. "Switch1.Power"); otherwise they get stored literally and the action never matches a device.
            path = args[3].strip('"').strip("'")
            value_str = " ".join(args[4:])

            # Verify the node exists in the group
            if node not in self.selected_group.get('node_ids', []):
                print(f"Error: Node {node} not found in the current group")
                print(f"Available nodes: {', '.join(self.selected_group.get('node_ids', []))}")
                return

            # Convert value to appropriate type
            try:
                if value_str.lower() == 'true':
                    value = True
                elif value_str.lower() == 'false':
                    value = False
                elif '.' in value_str:
                    value = float(value_str)
                else:
                    value = int(value_str)
            except ValueError:
                value = value_str

            # First get the existing automation to add the action
            automation = self.user.get_automation(group_id, automation_id)
            self.http_api_count += 1
            self._print_stats()

            if not automation:
                print(f"Automation {automation_id} not found")
                return

            # Read off the top level like add-trigger; a `payload` unwrap here wiped the just-added trigger.
            name = automation.get("name", "Unnamed Automation")
            trigger_ids = automation.get("conditions", {}).get("and", [])
            existing_actions = automation.get("actions", {}).get("targets", [])

            # Add new action
            action = {
                "node": node,
                "path": path,
                "value": value
            }
            existing_actions.append(action)

            # Update the automation
            self.update_automation(group_id, automation_id, name, trigger_ids, existing_actions)
            print(f"Action added to automation {automation_id}")
            print(f"Add more actions or complete with: automation complete {automation_id}")

        elif sub_command == "complete":
            if len(args) < 2:
                print("Usage: automation complete <automation_id>")
                return

            automation_id = args[1]

            # Get the current automation
            automation = self.user.get_automation(group_id, automation_id)
            self.http_api_count += 1
            self._print_stats()

            if not automation:
                print(f"Automation {automation_id} not found")
                return

            # There is no separate "activate" step — an automation is live once created; this just echoes its current state so the user can confirm what was built.
            print(f"Automation {automation_id} (status: {automation.get('status', 'unknown')}):")
            print(json.dumps(automation, indent=2))

        elif sub_command == "list":
            automations = self.user.get_automations(group_id)
            self.http_api_count += 1
            self._print_stats()

            if automations:
                print("\nAutomations:")
                print(json.dumps(automations, indent=2))
            else:
                print("No automations found or error occurred")

        elif sub_command == "get":
            if len(args) < 2:
                print("Usage: automation get <automation_id>")
                return

            automation_id = args[1]
            automation = self.user.get_automation(group_id, automation_id)
            self.http_api_count += 1
            self._print_stats()

            if automation:
                print(f"\nAutomation {automation_id}:")
                print(json.dumps(automation, indent=2))
            else:
                print(f"Automation {automation_id} not found or error occurred")

        elif sub_command == "delete":
            if len(args) < 2:
                print("Usage: automation delete <automation_id>")
                return

            automation_id = args[1]

            # First get the automation details to find its triggers
            automation = self.user.get_automation(group_id, automation_id)
            self.http_api_count += 1
            self._print_stats()

            if not automation:
                print(f"Automation {automation_id} not found")
                return

            # Extract trigger IDs from the automation
            payload = automation.get("payload", {})
            conditions = payload.get("conditions", {})
            trigger_ids = conditions.get("and", [])

            if trigger_ids:
                print(f"Found {len(trigger_ids)} triggers to clean up")

                # Create a map of node_id -> trigger_ids to minimize API calls
                # With the new trigger ID format: <nodeID>~<automationID>~<3-digit-sequence-number>
                # we can directly extract the node ID from each trigger ID
                node_triggers = {}

                for trigger_id in trigger_ids:
                    # Extract node ID from trigger ID format: <nodeID>~<automationID>~<3-digit-sequence-number>
                    try:
                        # Split the trigger ID and extract the node ID (first part)
                        parts = trigger_id.split('~')
                        if len(parts) == 3:
                            node_id = parts[0]  # First part is the node ID

                            if node_id not in node_triggers:
                                node_triggers[node_id] = []
                            node_triggers[node_id].append(trigger_id)
                        else:
                            print(f"Warning: Invalid trigger ID format: {trigger_id}")
                    except Exception as e:
                        print(f"Error parsing trigger ID {trigger_id}: {str(e)}")

                # Now clean up triggers for each node
                for node_id, triggers_to_remove in node_triggers.items():
                    # Get all triggers for this node
                    existing_triggers = self.user.get_node_trigger(group_id, node_id)
                    self.http_api_count += 1
                    self._print_stats()

                    if not existing_triggers or 'triggers' not in existing_triggers:
                        continue

                    # Filter out triggers associated with this automation
                    updated_triggers = []
                    for trigger in existing_triggers.get('triggers', []):
                        if trigger.get('id') not in triggers_to_remove:
                            updated_triggers.append(trigger)

                    # Update the node with filtered triggers
                    success = self.user.set_node_trigger(
                        group_id,
                        node_id,
                        json.dumps({"triggers": updated_triggers})
                    )
                    self.http_api_count += 1
                    self._print_stats()

                    if success:
                        print(f"Removed triggers from node {node_id}")
                    else:
                        print(f"Failed to remove triggers from node {node_id}")

            # Finally delete the automation
            success = self.user.delete_automation(group_id, automation_id)
            self.http_api_count += 1
            self._print_stats()

            if success:
                print(f"Deleted automation {automation_id}")
            else:
                print(f"Failed to delete automation {automation_id}")

        else:
            print(f"Unknown automation command: {sub_command}")
            print("Available commands: create, add-trigger, add-action, complete, list, get, delete")

    def handle_schedule_command(self, args):
        """Handle schedule-related commands and their subcommands"""
        if not self.selected_group:
            print("Error: No group selected. Use 'select' command first.")
            return

        group_id = self.selected_group['group_id']

        if len(args) == 0:
            print("Usage: schedule set <device> <payload>")
            print("       schedule get <device>")
            print("       schedule delete <device>")
            return

        sub_command = args[0].lower()

        if sub_command == "set":
            if len(args) < 3:
                print("Usage: schedule set <device> <payload>")
                return

            device = args[1]
            try:
                payload = json.loads(" ".join(args[2:]))
            except json.JSONDecodeError:
                print("Error: Invalid JSON payload")
                return

            if not isinstance(payload, list):
                print("Error: Invalid JSON payload; expecting a list of schedule objects")
                return

            # API contract uses snake_case "schedules" — cloud translates
            # to firmware "Schedules" before forwarding over MQTT.
            dict_payload = {"schedules": payload}

            success = self.user.set_node_schedule(group_id, None, device, dict_payload)
            self.http_api_count += 1
            self._print_stats()

            if success:
                print(f"Set schedule details for device {device}")
            else:
                print(f"Failed to set schedule details for device {device}")

        elif sub_command == "get":
            if len(args) < 2:
                print("Usage: schedule get <device>")
                return

            device = args[1]
            schedule = self.user.get_node_schedule(group_id, None, device)
            self.http_api_count += 1
            self._print_stats()

            if not schedule is None:
                print(f"Schedule details for device {device}:")
                print(json.dumps(schedule, indent=2))
            else:
                print(f"Failed to get schedule details for device {device}")

        elif sub_command == "delete":
            if len(args) < 2:
                print("Usage: schedule delete <device>")
                return

            device = args[1]
            # Wrap the empty list like the set path; a bare [] clears the schedule but leaves get returning [] instead of {"schedules": []}.
            success = self.user.set_node_schedule(group_id, None, device, {"schedules": []})
            self.http_api_count += 1
            self._print_stats()

            if success:
                print(f"Deleted schedule details for device {device}")
            else:
                print(f"Failed to delete schedule details for device {device}")

    def handle_prov_command(self, args):
        """Provision a BLE device the way the phone app does: discover, open a secure
        session, prove identity via the `ch_resp` challenge, associate to the selected
        group, push Wi-Fi credentials, then confirm online and set the timezone.

        Usage: prov [name_prefix]
        """
        if not self.selected_group:
            print("Error: No group selected. Use 'select' command first.")
            return

        # BLE provisioning is opt-in: bootstrap the heavy esp_prov dependency
        # (idf-extra-components clone + IDF_PATH) only now that `prov` was invoked,
        # so plain app-sim sessions never need IDF. Fail with a clear hint if it's
        # not set up rather than aborting the whole session at startup.
        try:
            prov_ble.ensure_esp_prov()
        except Exception as e:
            print(f"BLE provisioning is not available: {e}")
            return

        group_id = self.selected_group['group_id']
        name_prefix = args[0] if args else None

        # BLE-bound steps (discovery, session, challenge, Wi-Fi) run in one event
        # loop so the transport/session stays open across them. Cloud calls are
        # synchronous and invoked inline between awaits.
        try:
            node_id = asyncio.run(self._prov_ble_phase(group_id, name_prefix))
        except Exception as e:
            print(f"Provisioning failed: {str(e)}")
            return

        if not node_id:
            return

        print(f"\nWaiting for node {node_id} to come online...")
        shadow_name = f"params-{group_id}"
        if not self._wait_node_online(node_id, shadow_name):
            print(f"Node {node_id} did not report online in time.")
            return
        print(f"Node {node_id} is online.")

        if not self._set_node_timezone(group_id, node_id, shadow_name):
            return

        print(f"\nProvisioning complete for node {node_id} in group {group_id}.")
        self._print_stats()

    async def _prov_ble_phase(self, group_id, name_prefix):
        """Run the BLE-dependent portion of provisioning. Returns the node_id on
        success, or None if discovery/selection was aborted. Raises on hard failure."""
        # 1. Discover and select
        print("Discovering BLE devices...")
        devices = await prov_ble.discover_devices(name_prefix)
        if not devices:
            print("No matching BLE devices found.")
            return None

        print("\n==== Discovered devices ====")
        print('{0: >4} {1: <33} {2}'.format('S.N.', 'Name', 'Address'))
        for i, (name, addr) in enumerate(devices):
            print('[{0: >2}] {1: <33} {2}'.format(i + 1, name, addr))

        while True:
            try:
                sel = int(input('Select device by number (0 to cancel): '))
                if sel < 0 or sel > len(devices):
                    raise ValueError
                break
            except ValueError:
                print('Invalid input! Retry')
        if sel == 0:
            print("Cancelled.")
            return None
        devname = devices[sel - 1][0]

        # 2. Connect and establish session at the auto-detected security version
        print(f"\nConnecting to {devname}...")
        tp = await prov_ble.connect(devname)
        try:
            secver, sec_patch_ver = await prov_ble.detect_secver(tp)
            print(f"==== Security Scheme: {secver} ====")
            pop = ''
            if secver in (1, 2):
                pop = await PromptSession().prompt_async('Proof of Possession required: ', is_password=True)
            sec = prov_ble.make_security(secver, sec_patch_ver, pop)

            print("Establishing session...")
            if not await prov_ble.establish_session(tp, sec):
                raise RuntimeError(
                    'Failed to establish session (check security scheme / proof of possession)'
                )

            # 3. Wi-Fi credentials
            ssid = (await PromptSession().prompt_async('Wi-Fi SSID: ')).strip()
            passphrase = await PromptSession().prompt_async('Wi-Fi passphrase: ', is_password=True)

            # 4. Challenge / association
            print("\nInitiating node association...")
            request_id, challenge = self.user.initiate_node_assoc(group_id)
            self.http_api_count += 1
            self._print_stats()
            if not request_id:
                raise RuntimeError(f'Node association initiate failed: {challenge}')

            print("Requesting signed challenge from device (ch_resp)...")
            sig_hex, node_id = await prov_ble.ch_resp_get_signed(tp, sec, challenge)
            if not node_id:
                raise RuntimeError('Device did not return a node_id from ch_resp')

            verify_result = self.user.verify_node_assoc(group_id, request_id, sig_hex, node_id)
            self.http_api_count += 1
            self._print_stats()
            if isinstance(verify_result, str):
                raise RuntimeError(f'Node association verify failed: {verify_result}')
            print(f"Associated node {node_id} with group {group_id}.")

            # 5. Send Wi-Fi credentials and wait for connection
            print("\nSending Wi-Fi credentials...")
            if not await prov_ble.send_wifi(tp, sec, ssid, passphrase):
                raise RuntimeError('Device failed to connect to Wi-Fi')
            print("Device connected to Wi-Fi.")
            return node_id
        finally:
            await prov_ble.disconnect(tp)

    def _wait_node_online(self, node_id, shadow_name, timeout=60, interval=5):
        """Poll the node's named shadow until its reported state shows online: true."""
        self.user.subscribe_to_named_shadows(node_id, [shadow_name])
        self.shadow_ops_count += 1
        self.mqtt_msg_count += 3
        self._print_stats()

        deadline = time.time() + timeout
        while time.time() < deadline:
            if self.user.read_shadow(node_id, shadow_name):
                self.shadow_ops_count += 1
                self.mqtt_msg_count += 1
                try:
                    shadow = self.user.read_shadow_queue(timeout=interval)
                except Empty:
                    shadow = None
                reported = (shadow or {}).get('state', {}).get('reported', {})
                if reported.get('online') is True:
                    self._print_stats()
                    return True
            time.sleep(interval)
        return False

    def _set_node_timezone(self, group_id, node_id, shadow_name):
        """Set the node's timezone to the host IANA zone, resolving the service and
        parameter names from the node config (esp.service.time / esp.param.tz)."""
        cfg = self.user.get_node_config(group_id, "", node_id)
        self.http_api_count += 1
        self._print_stats()
        if not cfg:
            print("Failed to fetch node config for timezone update.")
            return False

        svc = next((s for s in cfg.get('services', [])
                    if s.get('type') == 'esp.service.time'), None)
        if not svc:
            print("Node has no esp.service.time service; skipping timezone update.")
            return False
        tz_param = next((p for p in svc.get('params', [])
                         if p.get('type') == 'esp.param.tz'), None)
        if not tz_param:
            print("Time service has no esp.param.tz parameter; skipping timezone update.")
            return False

        svc_name = svc['id']
        param_name = tz_param['id']
        tz = self._host_timezone()
        payload = {svc_name: {param_name: tz}}

        print(f"\nSetting timezone {tz} ({svc_name}.{param_name})...")
        if not self.user.mqtt_publish_to_topic(node_id, f"params-{group_id}/params", payload):
            print("Failed to publish timezone update.")
            return False
        self.shadow_ops_count += 1
        self.mqtt_msg_count += 1
        self._print_stats()

        # Verify the value lands in the reported shadow state.
        deadline = time.time() + 30
        while time.time() < deadline:
            if self.user.read_shadow(node_id, shadow_name):
                try:
                    shadow = self.user.read_shadow_queue(timeout=5)
                except Empty:
                    shadow = None
                reported = (shadow or {}).get('state', {}).get('reported', {})
                if reported.get(svc_name, {}).get(param_name) == tz:
                    print(f"Timezone confirmed: {svc_name}.{param_name} = {tz}")
                    return True
            time.sleep(5)
        print("Timezone update was not reflected in the shadow in time.")
        return False

    def _host_timezone(self):
        """Best-effort IANA timezone name for the host."""
        tz_env = os.environ.get('TZ')
        if tz_env:
            return tz_env
        localtime = pathlib.Path('/etc/localtime')
        if localtime.is_symlink():
            target = os.readlink(localtime)
            if 'zoneinfo/' in target:
                return target.split('zoneinfo/')[-1]
        # Fall back to the local tzname abbreviation.
        return datetime.datetime.now().astimezone().tzname() or 'UTC'

    def _print_commands(self):
        print("  select <group_id>          - Select a group")
        print("  list                       - List all groups")
        print("  update <device> <payload>  - Update device with JSON payload")
        print("  update_group <payload>     - Publish payload to group control topic")
        print("  update_subgroup <subgroup_id> <payload> - Publish payload to subgroup control topic")
        print("  schedule set <device> <payload> - Set schedule details for device")
        print("  schedule get <device> - Get schedule details for device")
        print("  schedule delete <device> - Delete schedule details for device")
        print("  automation create <name>   - Create new automation")
        print("  automation add-trigger <id> <node_id> <device> <param> <operator> <value>")
        print("  automation add-action <id> <node> <path> <value>")
        print("  automation complete <id>   - Finalize automation")
        print("  automation list            - List all automations")
        print("  automation get <id>        - Get automation details")
        print("  automation delete <id>     - Delete an automation")
        print("  prov [name_prefix]         - Provision a discovered BLE device into the selected group")
        print("  stats                      - Display operation statistics")
        print("  quit                       - Exit the program")

    def run_interactive(self):
        """Run the app simulator in interactive mode"""
        print("Starting interactive mode...")
        print("Available commands:")
        self._print_commands()

        # Display initial statistics
        self._print_stats()

        # Create a session with persistent history
        session = PromptSession(
            history=FileHistory(os.path.join(REPO_ROOT, 'cli', '.app_sim.command_history')),
            auto_suggest=AutoSuggestFromHistory()
        )

        try:
            while True:
                command = session.prompt("> ").strip()
                if not command:
                    continue

                # Normal command processing
                parts = command.split()
                main_command = parts[0].lower()
                args = parts[1:]

                if main_command == 'quit':
                    break
                elif main_command == 'list':
                    self.list_groups()
                elif main_command == 'select':
                    group_id = args[0] if args else None
                    self.select_home(group_id)
                elif main_command == 'update':
                    self.handle_device_command(main_command, args)
                elif main_command == 'update_group':
                    self.handle_update_group_command(args)
                elif main_command == 'update_subgroup':
                    self.handle_update_subgroup_command(args)
                elif main_command == 'stats':
                    self._print_stats()
                elif main_command == 'automation':
                    self.handle_automation_command(args)
                elif main_command == 'schedule':
                    self.handle_schedule_command(args)
                elif main_command == 'prov':
                    self.handle_prov_command(args)
                else:
                    print("Unknown command. Available commands:")
                    self._print_commands()

        except KeyboardInterrupt:
            print("\nExiting...")
        except Exception as e:
            print(f"Error: {str(e)}")
        finally:
            self.stop()

def main():
    import argparse

    parser = argparse.ArgumentParser(description="App Simulator")
    parser.add_argument('--user', required=True, help='User ID from test_config.json')

    args = parser.parse_args()

    try:
        # Create and start the app simulator
        simulator = AppSim(args.user)
        if simulator.start():
            simulator.run_interactive()
        else:
            print("Failed to start simulator")
            sys.exit(1)
    except Exception as e:
        print(f"Error running simulator: {str(e)}")
        import traceback
        print(traceback.format_exc())
        sys.exit(1)

if __name__ == '__main__':
    main()
