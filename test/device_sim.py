# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import argparse
import json
import sys
from scripts.rmng_outputs import REPO_ROOT, TEST_CONFIG_PATH, RmngSettings
from py_sdk.test_device import Device, generate_key_and_cert
import time
from prompt_toolkit import PromptSession
from prompt_toolkit.history import FileHistory
from prompt_toolkit.auto_suggest import AutoSuggestFromHistory
from queue import Queue, Empty
import threading
import traceback
import hashlib
import os
import pathlib

class DeviceSim:
    def __init__(self, device_id, config_path=TEST_CONFIG_PATH, rmng_outputs_path=None):
        self.device_id = device_id

        settings = RmngSettings.from_source(rmng_outputs_path)
        self.iot_endpoint = settings.iot_endpoint
        self.region = settings.region

        # Initialize state
        self.message_queue = Queue()
        self.shadow_name = None
        self.current_params_topic = None
        # Group/subgroup device-type-control topics the node is currently subscribed to; re-derived on every group move.
        self.current_group_control_topic = None
        self.current_subgroup_control_topics = []
        self.ishadow_name = "iparams"
        
        # Alexa, GVA and SmartThings notification support
        self.alexa_enabled = False
        self.gva_enabled = False
        self.st_enabled = False
        self.notification_version = 1

        # Read configurations
        self.config = self._read_json_file(config_path)

        # Get node configuration and tags from config
        node = self._get_node_config(device_id)
        if not node:
            raise ValueError(f"Failed to find node configuration for device {device_id}")

        # Check if node_cfg and node_tags are provided
        node_cfg_path = node.get('node_cfg')
        node_tags_path = node.get('node_tags')
        
        if not node_cfg_path or not node_tags_path:
            raise ValueError(f"Device {device_id} requires 'node_cfg' and 'node_tags' fields in test_config.json. "
                           f"Only devices with these fields can be simulated. "
                           f"Available devices: node_multi, node_switch")

        self.node_config = self._read_json_file(node_cfg_path)
        self.indexed_params = self.get_indexed_params_from_node_cfg()
        self.node_tags = self._read_json_file(node_tags_path)
        if self.node_tags == {} or self.node_config == {}:
            raise ValueError(f"Failed to find node configuration (node_cfg) or tags (node_tags) for device {device_id}")

        # Initialize device
        self.device = self._get_device(node)
        if not self.device:
            raise ValueError(f"Failed to initialize device {device_id}")

        # Message processing thread
        self.message_thread = None
        self.should_stop = False
        self.group_info_received = threading.Event()

        # Repo-anchored so the cache is shared no matter which directory the simulator runs from;
        # a cache that looks empty from a new CWD would re-push config the node already has.
        self.cache_dir = pathlib.Path(REPO_ROOT) / '.sim' / 'device'
        self.cache_dir.mkdir(parents=True, exist_ok=True)
        self.cache_file = self.cache_dir / f"{device_id}-config-cache"

        # Store the last known ncfg_ver value to avoid reading shadow
        self.last_known_ncfg_ver = None

        # Per-device light-mode bookkeeping. Mirrors the SDK firmware in
        # examples/common/app_node_setups/src/light.c (lines 105-116): when
        # only HSV params change, switch the device to HSV mode (1); when only
        # CCT changes, switch to CCT (2); when both change in one message,
        # keep the current mode. _light_mode_devices maps device_name -> dict
        # with keys {hue, sat, cct, mode_param} holding the actual param IDs
        # for that device. _current_light_mode tracks the device's current
        # Light Mode value (driven by writes the simulator publishes, since
        # there's no local shadow cache).
        self._light_mode_devices = self._build_light_mode_devices()
        self._current_light_mode = {
            dev: meta["default_mode"]
            for dev, meta in self._light_mode_devices.items()
        }

    def _read_json_file(self, file_path):
        # node_cfg/node_tags in test_config.json are stored repo-relative (e.g. "cli/cli_data/..."),
        # so anchor them rather than resolving against whatever directory the CLI was started in.
        if not os.path.isabs(file_path):
            file_path = os.path.join(REPO_ROOT, file_path)
        with open(file_path, 'r') as config_file:
            return json.load(config_file)

    def _get_node_config(self, device_id):
        nodes = self.config.get('nodes', [])
        for node in nodes:
            if node.get('thing_name') == device_id:
                return node
        return None

    def _get_device(self, node):
        return Device(
            node['thing_name'],
            node['key'],
            node['cert'],
            self.config['ca_cert'],
            self.iot_endpoint,
            self.region,
            self.config.get('debug', False)
        )

    def _tags_to_update_payload(self, tags):
        """Convert tags to update payload. Expects {tag1: value1, tag2: value2, ...}"""
        
        # Convert it to the format of {"data": {"device": { "t": {"tag1": "value1", "tag2": "value2"}}}}
        payload = {"data": {"device": { "t": {}}}}
        for tag in tags:
            payload["data"]["device"]["t"][tag] = tags[tag]
        
        return payload

    def calculate_checksum(self, data):
        """Calculate SHA-256 checksum of the data"""
        data_str = json.dumps(data, sort_keys=True)
        return hashlib.sha256(data_str.encode()).hexdigest()

    def read_cache_checksum(self):
        """Read the cached checksum from the cache file"""
        try:
            if self.cache_file.exists():
                with open(self.cache_file, 'r') as f:
                    return f.read().strip()
            return None
        except Exception as e:
            print(f"Error reading cache file: {str(e)}")
            return None

    def write_cache_checksum(self, checksum):
        """Write the checksum to the cache file"""
        try:
            with open(self.cache_file, 'w') as f:
                f.write(checksum)
            return True
        except Exception as e:
            print(f"Error writing cache file: {str(e)}")
            return False

    def on_params_message(self, topic, payload, **kwargs):
        print(f"Received message on params topic:")
        print(f"Topic: {topic}")
        try:
            # Handle both string and dict payloads
            if isinstance(payload, dict):
                message = payload
            elif isinstance(payload, bytes):
                message = json.loads(payload.decode())
            else:
                message = json.loads(payload)

            print(f"Payload: {json.dumps(message, indent=2)}")

            # Group/subgroup control broadcasts share this callback (the Device layer routes them here) but carry a device-type-addressed payload that must be translated before it can be applied.
            if "/groups/" in topic and topic.endswith("control"):
                self.message_queue.put(('group_control', message))
                return

            # Check if ncfg_ver is present in the reported state
            if "state" in message and "reported" in message["state"] and "params" in message["state"]["reported"] and "ncfg_ver" in message["state"]["reported"]["params"]:
                # Update our cached value
                self.last_known_ncfg_ver = message["state"]["reported"]["params"]["ncfg_ver"]

            self.message_queue.put(('params', message))
        except (json.JSONDecodeError, AttributeError) as e:
            print(f"Error decoding payload: {str(e)}")
            if isinstance(payload, bytes):
                print(f"Raw payload: {payload.decode()}")
            else:
                print(f"Raw payload: {payload}")

    def on_from_cloud_message(self, topic, message):
        print(f"Received message from cloud:")
        print(f"Payload: {json.dumps(message, indent=2)}")
        self.message_queue.put(('from_cloud', message))

    def handle_group_info_update(self, message):
        new_group_id = message["getGroupInfo"].get("pgrp")
        new_shadow_name = None

        if new_group_id:
            # Construct new shadow name
            new_shadow_name = f"params-{new_group_id}"
            if "subgrps" in message["getGroupInfo"]:
                subgroup_ids = message["getGroupInfo"].get("subgrps", [])
                for subgroup_id in sorted(subgroup_ids):
                    new_shadow_name += f"-{subgroup_id}"

        # If group has changed
        if new_shadow_name != self.shadow_name:
            # Unsubscribe from old params topic if it exists
            if self.current_params_topic:
                print(f"Unsubscribing from old params topic: {self.current_params_topic}")
                self.device.unsubscribe(self.current_params_topic)
                self.current_params_topic = None

            # Update shadow name
            self.shadow_name = new_shadow_name

            if self.shadow_name:
                # Subscribe to new params topic with full path
                new_params_topic = f"rainmaker/nodes/{self.device.node_thing_name}/user/{self.shadow_name}/params"
                print(f"Subscribing to new params topic: {new_params_topic}")
                if self.device.subscribe(topic=new_params_topic, callback=self. on_params_message):
                    print(f"Successfully subscribed to topic: {new_params_topic}")
                    self.current_params_topic = new_params_topic

                    # Connect to the named shadow
                    if self.device.shadow_connect([self.shadow_name]):
                        print(f"Successfully connected to named shadow: {self.shadow_name}")

                        # First set online status
                        online_status = {"online": True}
                        if self.update_shadows(online_status):
                            pass
                        else:
                            print("Failed to set online status in shadow")
                    else:
                        print(f"Failed to connect to named shadow: {self.shadow_name}")
                else:
                    print(f"Failed to subscribe to topic: {new_params_topic}")

            # Re-subscribe the group/subgroup device-type-control topics for the new group.
            self._resubscribe_group_control(new_group_id, message["getGroupInfo"].get("subgrps", []))

        # Signal that group info has been received
        self.group_info_received.set()

    def _resubscribe_group_control(self, group_id, subgroup_ids):
        """(Re)subscribe the device-type-addressed control topics for the node's group and subgroups, mirroring how the app sim's update_group / update_subgroup publish. The Device layer classifies these topics as 'params', so they arrive at on_params_message and are distinguished there by topic."""
        # Drop stale subscriptions from the previous group.
        for topic in filter(None, [self.current_group_control_topic, *self.current_subgroup_control_topics]):
            self.device.unsubscribe(topic)
        self.current_group_control_topic = None
        self.current_subgroup_control_topics = []

        if not group_id:
            return

        group_topic = f"rainmaker/nodes/groups/{group_id}/control"
        if self.device.subscribe(topic=group_topic, callback=self.on_params_message):
            print(f"Subscribed to group-control topic: {group_topic}")
            self.current_group_control_topic = group_topic
        else:
            print(f"Warning: failed to subscribe to group-control topic: {group_topic}")

        for subgroup_id in subgroup_ids:
            sg_topic = f"rainmaker/nodes/groups/{group_id}/subgroups/{subgroup_id}/control"
            if self.device.subscribe(topic=sg_topic, callback=self.on_params_message):
                print(f"Subscribed to subgroup-control topic: {sg_topic}")
                self.current_subgroup_control_topics.append(sg_topic)
            else:
                print(f"Warning: failed to subscribe to subgroup-control topic: {sg_topic}")

    def handle_alexa_enabled_response(self, message):
        """Handle the getAlexaEn response from the cloud"""
        if "getAlexaEn" in message and isinstance(message["getAlexaEn"], dict):
            self.alexa_enabled = message["getAlexaEn"].get("enabled", False)
            print(f"Alexa enabled status updated: {self.alexa_enabled}")

    def handle_gva_enabled_response(self, message):
        """Handle the getGVAEn response from the cloud"""
        if "getGVAEn" in message and isinstance(message["getGVAEn"], dict):
            self.gva_enabled = message["getGVAEn"].get("enabled", False)
            print(f"GVA enabled status updated: {self.gva_enabled}")

    def handle_st_enabled_response(self, message):
        """Handle the getSTEn response from the cloud"""
        if "getSTEn" in message and isinstance(message["getSTEn"], dict):
            self.st_enabled = message["getSTEn"].get("enabled", False)
            print(f"SmartThings enabled status updated: {self.st_enabled}")

    def process_messages(self):
        while not self.should_stop:
            try:
                message_type, message = self.message_queue.get(timeout=1)  # Wait for 1 second

                if message_type == 'from_cloud':
                    # Handle group info updates
                    if "getGroupInfo" in message:
                        self.handle_group_info_update(message)
                    # Handle Alexa enabled status
                    if "getAlexaEn" in message:
                        self.handle_alexa_enabled_response(message)
                    # Handle GVA enabled status
                    if "getGVAEn" in message:
                        self.handle_gva_enabled_response(message)
                    # Handle SmartThings enabled status
                    if "getSTEn" in message:
                        self.handle_st_enabled_response(message)
                elif message_type == 'params' and self.shadow_name:
                    # Apply the SDK's auto-mode-switch logic before mirroring
                    # the desired-params write back into the reported shadow,
                    # so the cloud (and Alexa) see a coherent Light Mode.
                    self._apply_light_mode_auto_switch(message)
                    # Process params topic messages
                    if self.update_shadows(message):
                        print(f"Successfully updated named shadow '{self.shadow_name}' with received message")
                    else:
                        print(f"Failed to update named shadow '{self.shadow_name}'")
                elif message_type == 'group_control' and self.shadow_name:
                    # Translate the device-type-addressed broadcast to this node's params, then apply it exactly like a per-node params write.
                    translated = self._translate_group_control(message)
                    if not translated:
                        print("Group-control broadcast matched no device on this node; ignoring")
                    else:
                        print(f"Group-control applies to: {', '.join(translated)}")
                        self._apply_light_mode_auto_switch(translated)
                        if self.update_shadows(translated):
                            print(f"Successfully applied group-control broadcast to '{self.shadow_name}'")
                        else:
                            print(f"Failed to apply group-control broadcast to '{self.shadow_name}'")
            except Empty:
                pass  # No message in the queue, continue the loop
            except Exception as e:
                print(f"Error processing message: {str(e)}")

    def get_indexed_params_from_node_cfg(self):
        """Return a list of indexed parameters from node_config
        """
        indexed_params = {}
        devices = self.node_config.get('devices', [])
        for device in devices:
            device_id = device.get('id', device.get('name'))
            indexed_params[device_id] = []
            for param in device.get('params', []):
                if 'indexed' in param.get('properties', []):
                    indexed_params[device_id].append(param.get('id', param.get('name')))
        return indexed_params

    def get_default_params_from_node_cfg(self):
        """Process all parameters from node_config and return organized parameter sets

        Returns:
            tuple: (node_tags, device_params)
                - node_tags: dict of parameters marked as indexed
                - device_params: dict of parameters organized by device name
        """
        device_params = {}
        device_indexed_params = {}
        # Get all devices from node_config
        devices = self.node_config.get('devices', [])

        for device in devices:
            device_name = device.get('id', device.get('name'))
            device_params[device_name] = {}
            device_indexed_params[device_name] = {}

            device_params_list = device.get('params', [])
            for param in device_params_list:
                param_name = param.get('id', param.get('name'))

                # Set default values based on data type
                data_type = param.get('data_type')
                if data_type == 'bool':
                    default_value = False
                elif data_type == 'int':
                    bounds = param.get('bounds')
                    default_value = bounds.get('min', 0) if bounds else 0
                elif data_type == 'string':
                    if param.get('type') == 'esp.param.name':
                        default_value = device_name
                    else:
                        default_value = "demo"
                elif data_type == 'float':
                    bounds = param.get('bounds')
                    default_value = float(bounds.get('min', 0.0) if bounds else 0.0)
                else:
                    # Unknown/unsupported type — degrade to "" rather than crash the whole default-param setup with UnboundLocalError.
                    print(f"Warning: param {device_name}.{param_name} has unsupported data_type {data_type!r}; defaulting to \"\"")
                    default_value = ""

                # Store in device_params
                device_params[device_name][param_name] = default_value

                # Check if parameter should be indexed
                if 'indexed' in param.get('properties', []):
                    device_indexed_params[device_name][param_name] = default_value

            # Remove device from node_tags if it has no indexed parameters
            if device_indexed_params[device_name] == {}:
                del device_indexed_params[device_name]

        return device_indexed_params, device_params

    def _translate_group_control(self, payload):
        """Map a device-type-addressed broadcast onto this node's own devices.

        Input is keyed by device type and esp.param.* type, e.g.
        {"esp.device.lightbulb": {"params": {"esp.param.power": true}}}.
        Returns the friendly-id shape update_shadows() consumes, e.g.
        {"Colour Light": {"Power": true}, "CCT Light": {"Power": true}}, applying to every
        device of a matching type. Device types the node lacks are silently dropped — that is
        how a real node treats a broadcast for hardware it doesn't have.
        """
        translated = {}
        for device in self.node_config.get('devices', []):
            dev_type = device.get('type')
            type_block = payload.get(dev_type)
            if not isinstance(type_block, dict):
                continue
            wanted_params = type_block.get('params', {})
            if not isinstance(wanted_params, dict):
                continue

            device_id = device.get('id', device.get('name'))
            resolved = {}
            for param in device.get('params', []):
                if param.get('type') in wanted_params:
                    resolved[param.get('id', param.get('name'))] = wanted_params[param['type']]
            if resolved:
                translated[device_id] = resolved
        return translated

    # Light-mode constants — must match
    # rmng-sdk components/rmng/include/data_model/default/public/esp_rmaker_standard_params.h
    # (ESP_RMAKER_LIGHT_MODE_INVALID/HSV/CCT)
    LIGHT_MODE_INVALID = 0
    LIGHT_MODE_HSV = 1
    LIGHT_MODE_CCT = 2

    def _build_light_mode_devices(self):
        """For each device in node_config that has an esp.param.light-mode
        param, record the param IDs for hue/saturation/cct/light-mode plus
        the default mode. Used by _apply_light_mode_auto_switch.
        """
        out = {}
        for device in self.node_config.get('devices', []):
            device_name = device.get('id', device.get('name'))
            meta = {"hue": None, "sat": None, "cct": None, "mode_param": None, "default_mode": self.LIGHT_MODE_INVALID}
            for param in device.get('params', []):
                ptype = param.get('type')
                pid = param.get('id', param.get('name'))
                if ptype == 'esp.param.hue':
                    meta["hue"] = pid
                elif ptype == 'esp.param.saturation':
                    meta["sat"] = pid
                elif ptype == 'esp.param.cct':
                    meta["cct"] = pid
                elif ptype == 'esp.param.light-mode':
                    meta["mode_param"] = pid
                    bounds = param.get('bounds') or {}
                    meta["default_mode"] = bounds.get('min', self.LIGHT_MODE_INVALID)
            if meta["mode_param"] and (meta["hue"] or meta["sat"] or meta["cct"]):
                out[device_name] = meta
        return out

    def _apply_light_mode_auto_switch(self, message):
        """Mirror SDK behaviour from light.c:105-116.

        For each tracked device whose params changed in this message, detect
        HSV-change (hue or saturation in the message) vs CCT-change (cct in
        the message). If exactly one occurred and the device isn't already in
        that mode, inject a Light Mode update into the same message so the
        shadow reflects the new mode atomically. If both changed in the same
        message, keep the current mode (matches the `^` exclusive-or in the
        SDK).
        """
        for device_name, meta in self._light_mode_devices.items():
            dev_params = message.get(device_name)
            if not isinstance(dev_params, dict):
                continue
            hsv_change = (meta["hue"] in dev_params) or (meta["sat"] in dev_params)
            cct_change = meta["cct"] in dev_params
            if hsv_change == cct_change:  # both or neither
                continue
            current_mode = self._current_light_mode.get(device_name, meta["default_mode"])
            new_mode = self.LIGHT_MODE_HSV if hsv_change else self.LIGHT_MODE_CCT
            if current_mode == new_mode:
                continue
            print(f"Auto-switching {device_name} light mode {current_mode} -> {new_mode}")
            dev_params[meta["mode_param"]] = new_mode
            self._current_light_mode[device_name] = new_mode

    def update_shadows(self, message):
        """Update device shadows and (indexed shadow if applicable) with parameters

        Args:
            message: dict of parameters to update
        """
        print(f"Updating shadows with message: {message}")
        # If Alexa, GVA or SmartThings is enabled, add the notification field
        if self.alexa_enabled or self.gva_enabled or self.st_enabled:
            shadow_message = message.copy()
            notify = {"version": self.notification_version}
            if self.alexa_enabled:
                notify["alexa"] = True
            if self.gva_enabled:
                notify["gva"] = True
            if self.st_enabled:
                notify["smartthings"] = True
            shadow_message["notify"] = notify
            self.notification_version += 1
            print(f"Adding notification to shadow update (version: {self.notification_version-1}, alexa: {self.alexa_enabled}, gva: {self.gva_enabled}, smartthings: {self.st_enabled})")
        else:
            shadow_message = message

        # First update the device shadow
        if not self.device.update_named_shadow(self.shadow_name, shadow_message):
            return False

        # Now check which parameters are indexed and update the indexed shadow
        indexed = {}
        if "online" in message:
            indexed["online"] = message["online"]

        indexed_params = {}
        for device in message.keys():
            if device in ["online", "ncfg_ver", "notify"]:
                # These are special cases not related to device names
                continue
            indexed_params[device] = {}
            for param in message[device].keys():
                if self.indexed_params.get(device, []).count(param) > 0:
                    indexed_params[device][param] = message[device][param]
            if indexed_params[device] == {}:
                del indexed_params[device]
        if indexed_params:
            indexed["params"] = indexed_params

        # Update the indexed shadow if there are any indexed parameters that need updating
        if indexed:
            if not self.device.update_named_shadow(self.ishadow_name, indexed):
                return False
        return True

    def write_default_params(self, include_ncfg_ver=True):
        """Update both indexed and device shadows with parameters

        Args:
            include_ncfg_ver (bool): Whether to include the ncfg_ver timestamp. Set to False when
                                    only updating other parameters without changing node_config.

        Returns:
            bool: True if both shadows updated successfully, False otherwise
        """

        device_indexed_params, device_params = self.get_default_params_from_node_cfg()

        # Add timestamp for node_config version only if explicitly requested
        if include_ncfg_ver:
            timestamp = int(time.time())
            device_params["ncfg_ver"] = timestamp
            # Update our cached value
            self.last_known_ncfg_ver = timestamp
        elif self.last_known_ncfg_ver is not None:
            # Include the cached ncfg_ver if we have it
            device_params["ncfg_ver"] = self.last_known_ncfg_ver

        if not self.device.update_named_shadow(self.shadow_name, device_params):
            return False

        node_tags = self.node_tags
        # Set indexable parameters in indexed shadow
        if node_tags:
            node_tags["params"] = device_indexed_params
        else:
            node_tags = {"params": device_indexed_params}

        if not self.device.update_named_shadow(self.ishadow_name, node_tags):
            return False

        return True

    def start(self):
        """Start the device simulation"""
        print(f"Starting simulator for device: {self.device.node_thing_name}")

        # Connect to MQTT and register callbacks
        if self.device.connect():
            print("Successfully connected to MQTT")
            self.device.register_callback('from_cloud', self.on_from_cloud_message)
        else:
            print("Failed to connect to MQTT")
            return False

        # Start the message processing thread
        self.should_stop = False
        self.message_thread = threading.Thread(target=self.process_messages)
        self.message_thread.daemon = True
        self.message_thread.start()

        # Get initial group info
        self.device.get_group_info()
        print("Waiting for group info...")
        if not self.group_info_received.wait(timeout=10):
            print("Timed out waiting for group info")
            return False

        # Calculate the checksum of the current node_config
        current_checksum = self.calculate_checksum(self.node_config)
        cached_checksum = self.read_cache_checksum()

        # Only send node_config if the checksum has changed or cache doesn't exist
        config_changed = current_checksum != cached_checksum

        if config_changed:
            print("Node configuration has changed, sending to cloud...")

            # Set node config and update the cache
            if self.device.set_node_config(self.node_config):
                print("Successfully set node configuration")
                # Update the cache with the new checksum
                self.write_cache_checksum(current_checksum)
            else:
                print("Failed to set node configuration")
                return False

            # Update shadows with processed parameters and new ncfg_ver timestamp
            # Only update with ncfg_ver when configuration has changed
            if self.device.group_id:
                if self.write_default_params(include_ncfg_ver=True):
                    print(f"Successfully updated device shadows with default parameters and new ncfg_ver")
                else:
                    print("Failed to write default parameters to shadow")
                    return False
            else:
                print("Failed to set-up device")
                return False
        else:
            print("Node configuration unchanged, using cached version")

            # For unchanged config, still update shadows but don't update the ncfg_ver
            if self.device.group_id:
                if self.write_default_params(include_ncfg_ver=False):
                    print(f"Successfully updated device shadows with default parameters (preserved ncfg_ver)")
                else:
                    print("Failed to write default parameters to shadow")
                    return False
            else:
                print("Failed to set-up device")
                return False

        return True

    def stop(self):
        """Stop the device simulation"""
        self.should_stop = True
        if self.message_thread:
            self.message_thread.join()

        # Clean up subscriptions and connections
        for topic in filter(None, [self.current_params_topic, self.current_group_control_topic, *self.current_subgroup_control_topics]):
            self.device.unsubscribe(topic)
        self.device.disconnect()
        print("Disconnected from MQTT")

    def run_interactive(self):
        """Run the simulator in interactive mode"""
        # Create a session with persistent history
        session = PromptSession(
            history=FileHistory(os.path.join(REPO_ROOT, 'cli', '.device_sim.command_history')),
            auto_suggest=AutoSuggestFromHistory()
        )

        # Main simulation loop
        try:
            while True:
                user_input = session.prompt("Enter command (q|quit for exit): ")
                try:
                    command = user_input.split(maxsplit=1)
                    match command[0]:
                        case "update_params":
                            if len(command) < 2:
                                print("Please provide JSON data. Usage: update_params {\"device1\": {\"param1\": value}}")
                                continue
                            try:
                                params_data = json.loads(command[1])
                                if not isinstance(params_data, dict):
                                    print(f"Warning: Expected dict but got {type(params_data)}")
                                    continue

                                if self.update_shadows(params_data):
                                    print("Successfully updated shadows")
                                else:
                                    print("Failed to update shadows")
                            except json.JSONDecodeError as e:
                                print(f"Invalid JSON format: {e}")

                        case "update_tags":
                            if len(command) < 2:
                                print("Please provide JSON data. Usage: update_tags {\"tag1\": value}")
                                continue
                            try:
                                tags_data = json.loads(command[1])
                                payload = self._tags_to_update_payload(tags_data)
                                if self.device.update_named_shadow(self.ishadow_name, payload):
                                    print("Successfully updated tags")
                                else:
                                    print("Failed to update tags")
                            except json.JSONDecodeError:
                                print("Invalid JSON format")

                        case "q" | "quit":
                            break

                        case _:
                            print("Unknown command. Available commands:")
                            print("  update_params {json_data}  - Update device parameters")
                            print("  update_tags {json_data}    - Update device tags")
                            print("  quit                       - Exit simulator")
                except IndexError:
                    print("Invalid command format")
        except KeyboardInterrupt:
            print("Simulator stopped by user")
        finally:
            self.stop()

def main():
    parser = argparse.ArgumentParser(description="Device Behavior Simulator")
    parser.add_argument('--device', required=True, help='Device ID from test_config.json')

    args = parser.parse_args()

    try:
        # Create and start the device simulator
        simulator = DeviceSim(args.device)
        if simulator.start():
            simulator.run_interactive()
        else:
            print("Failed to start simulator")
            sys.exit(1)
    except Exception as e:
        print(f"Error running simulator: {str(e)}")
        print(traceback.format_exc())
        sys.exit(1)

if __name__ == "__main__":
    main()

# python3 device_sim.py --device <device_id>