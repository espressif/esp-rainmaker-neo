#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Interactive bridge simulator.

Drives a bridge cert against AWS IoT for the bridge feature (spec at
docs/en/specs/bridge.md). Holds the bounded set of MQTT subscriptions
described in bridge.md §3.3.1, aligned with
docs/en/specs/group-control-feature.md:

  1. rainmaker/nodes/<self>/from_cloud                          — own
  2. rainmaker/nodes/<self>/user/+/params                       — own unicast
  3. rainmaker/nodes/groups/<self.group>/control                — group broadcast (dynamic)
  4. rainmaker/nodes/groups/<self.group>/subgroups/+/control    — subgroup wildcard (dynamic)
  5. rainmaker/bridges/<self>/children/+/from_cloud             — children (Rule A rewrite)
  6. rainmaker/bridges/<self>/children/+/user/+/params          — children (Rule B rewrite)

(3) and (4) are subscribed once the bridge learns its own pgrp from a
getGroupInfo. Group *moves* trigger an unsub+sub on (3) and (4);
subgroup changes are absorbed by the `+` wildcard in (4) and cause
no subscription churn.

Tracks getGroupInfo for self and each child so update_child can build
the correct named-shadow topic without the user having to type it.

Commands:
  add_child    <suffix> <child_local_id>
  remove_child <child_node_id>
  update_child <child_node_id> <json_state>
      Publishes `{state: {reported: <json>}}` to the child's
      params-<group>[-<sg>...] shadow via the canonical $aws topic.
  update_self  <json_state>
      Publishes {state: {reported: <json>}} to the bridge's own
      params shadow.
  online       <child_node_id> <true|false>
      Writes the child's iparams shadow online field (spec §3.7).
  state
      Prints self.group / self.subgroups / children table.
  list_children
  getgroupinfo
      Sends {event:["getGroupInfo"]} on own to_cloud — useful after
      restart to refresh state from the cloud.
  get_sched_ver     <child_node_id>
      Publishes {event:["getSchedVer"]} on the child's canonical
      to_cloud topic (rainmaker/nodes/<child>/to_cloud), authorized by
      the bridge IoT policy ChildrenToCloudPublish substitution
      (spec §3.5). The handler replies on the child's from_cloud,
      which Rule A rewrites onto the bridge namespace. On
      version-mismatch with the bridge's locally cached version,
      auto-follows up with getSchedDetails.
  get_sched_details <child_node_id>
      Force-pull full schedule for a child (cache-bust the bridge's
      local copy). Same path as the get_sched_ver auto-followup.
  resync_schedules
      Fan-out getSchedVer to every known child — the operation a
      real bridge would run on reconnect (spec §5.9) to recover any
      schedule changes that landed during an offline window.
  quit

Usage:
  python3 test/bridge_sim.py --bridge my-bridge-001

  # or fully explicit:
  python3 test/bridge_sim.py \\
    --endpoint <iot_endpoint> --thing <bridge_name> \\
    --cert <cert.pem> --key <private.key> --ca <root_ca.pem>
"""

import argparse
import json
import sys
import threading
import time
import uuid
from pathlib import Path
from queue import Empty, Queue

from awscrt import io, mqtt
from awsiot import mqtt_connection_builder


DEFAULT_CONFIG = Path(".bridges/bridges_config.json")


# ---------------------------------------------------------------------------
# MQTT client
# ---------------------------------------------------------------------------


class BridgeMQTT:
    """Thin mTLS MQTT wrapper. All inbound payloads land in `inbox` so the
    REPL thread can drain them between commands."""

    def __init__(self, *, endpoint: str, cert_path: str, key_path: str,
                 ca_path: str, thing_name: str):
        self.endpoint = endpoint
        self.thing_name = thing_name
        self.inbox: Queue = Queue()
        self._cert_bytes = Path(cert_path).read_bytes()
        self._key_bytes = Path(key_path).read_bytes()
        self._ca_bytes = Path(ca_path).read_bytes()
        self._loop_group = io.EventLoopGroup(1)
        self._host_resolver = io.DefaultHostResolver(self._loop_group)
        self._bootstrap = io.ClientBootstrap(self._loop_group, self._host_resolver)
        self.connection = None

    def _interrupted(self, connection, error, **_):
        print(f"  [mqtt] interrupted: {error}", file=sys.stderr)

    def _resumed(self, connection, return_code, session_present, **_):
        print(f"  [mqtt] resumed (rc={return_code})", file=sys.stderr)

    def connect(self) -> None:
        self.connection = mqtt_connection_builder.mtls_from_bytes(
            endpoint=self.endpoint,
            cert_bytes=self._cert_bytes,
            pri_key_bytes=self._key_bytes,
            ca_bytes=self._ca_bytes,
            client_bootstrap=self._bootstrap,
            client_id=self.thing_name,
            clean_session=True,
            keep_alive_secs=30,
            on_connection_interrupted=self._interrupted,
            on_connection_resumed=self._resumed,
        )
        self.connection.connect().result(timeout=10)

    def subscribe(self, topic_filter: str) -> None:
        def _cb(topic, payload, **_):
            try:
                msg = json.loads(payload.decode())
            except (json.JSONDecodeError, UnicodeDecodeError):
                msg = {"_raw": payload.decode(errors="replace")}
            self.inbox.put({"topic": topic, "payload": msg, "ts": time.time()})

        future, _ = self.connection.subscribe(
            topic=topic_filter,
            qos=mqtt.QoS.AT_LEAST_ONCE,
            callback=_cb,
        )
        future.result(timeout=10)

    def unsubscribe(self, topic_filter: str) -> None:
        """Best-effort unsubscribe. awscrt returns a future per
        unsubscribe; we wait briefly so a follow-up subscribe doesn't
        race with the broker's view of subscription state."""
        try:
            future, _ = self.connection.unsubscribe(topic=topic_filter)
            future.result(timeout=10)
        except Exception as e:
            print(f"  [mqtt] unsubscribe {topic_filter!r} failed: {e}",
                  file=sys.stderr)

    def publish(self, topic: str, payload: dict) -> None:
        future, _ = self.connection.publish(
            topic=topic,
            payload=json.dumps(payload),
            qos=mqtt.QoS.AT_LEAST_ONCE,
        )
        future.result(timeout=10)

    def disconnect(self) -> None:
        if self.connection:
            try:
                self.connection.disconnect().result(timeout=5)
            except Exception:
                pass
            self.connection = None


# ---------------------------------------------------------------------------
# Sim state and REPL
# ---------------------------------------------------------------------------


def _shadow_name(group: str, subgroups: list[str]) -> str:
    """Compute the params shadow name from a group + sorted subgroups.
    Mirrors `device_sim.py.handle_group_info_update`."""
    name = f"params-{group}"
    for sg in sorted(subgroups):
        name += f"-{sg}"
    return name


class ChildState:
    __slots__ = ("name", "group", "subgroups", "sched_version", "sched_details")

    def __init__(self, name: str, group: str = "", subgroups: list[str] | None = None):
        self.name = name
        self.group = group
        self.subgroups = list(subgroups or [])
        # Locally cached cloud schedule version + last full schedule
        # payload. The version is what the bridge compares against the
        # `getSchedVer` reply on reconnect; details are cached so the
        # bridge can replay the schedule to the protocol-side device
        # without another round-trip.
        self.sched_version: int | None = None
        self.sched_details: dict | None = None

    def shadow_name(self) -> str:
        return _shadow_name(self.group, self.subgroups)


class BridgeSim:
    def __init__(self, mqtt_client: BridgeMQTT):
        self.mqtt = mqtt_client
        self.thing = mqtt_client.thing_name

        # Own group state, learned from getGroupInfo replies on
        # subscription #1 (own from_cloud).
        self.group: str = ""
        self.subgroups: list[str] = []

        # child_local_id -> child_node_id (populated from bridgeAck)
        self.local_id_to_child: dict[str, str] = {}

        # child_node_id -> ChildState (populated from getGroupInfo
        # rewrites on subscription #4 / Rule A).
        self.children: dict[str, ChildState] = {}

        # Group-control reconciliation state — tracks which group
        # we currently have GC subscriptions for. Empty string means
        # no GC subs are active yet.
        self._gc_subscribed_for_group: str = ""

        self._stop = False
        self._printer_thread: threading.Thread | None = None

    # ---- background printer / state updater ----------------------------

    def _drain_inbox(self):
        while not self._stop:
            try:
                msg = self.mqtt.inbox.get(timeout=0.2)
            except Empty:
                continue
            self._handle(msg["topic"], msg["payload"])

    def _handle(self, topic: str, payload: dict) -> None:
        body = json.dumps(payload, indent=2)
        print(f"\n← {topic}\n{body}\n> ", end="", flush=True)

        # Group-control messages have no `event` field — payload is a
        # device-type-keyed envelope per group-control-feature.md §3.2.
        # Route them before the event-based dispatch.
        if topic.startswith("rainmaker/nodes/groups/"):
            self._handle_group_control(topic, payload)
            return

        events = payload.get("event") or []
        if not isinstance(events, list):
            return

        if "bridgeAck" in events:
            self._handle_bridge_ack(payload)
        if "getGroupInfo" in events:
            self._handle_get_group_info(topic, payload)
        if "getSchedVer" in events or "getSchedDetails" in events:
            self._handle_sched_response(topic, payload, events)

    def _handle_bridge_ack(self, payload: dict) -> None:
        ack = payload.get("bridgeAck", {})
        if ack.get("status") != "success":
            return
        child = ack.get("child_node_id")
        if not child:
            return
        # The Lambda doesn't echo child_local_id in the ack; we have
        # to remember it from the request side. cmd_add_child does
        # that pre-emptively, so by the time we get here we just
        # ensure the ChildState exists.
        entry = self.children.setdefault(child, ChildState(name=child))
        # Pre-populate the child's group from self.group. Spec §3.2:
        # children are *tethered* to the bridge's main group, so we
        # already know the answer without waiting for a getGroupInfo
        # rewrite. (Production rmng's group.AddNode currently doesn't
        # publish getGroupInfo after assoc — backend gap, but the
        # invariant holds either way.) Subgroups still come from the
        # Rule A rewrite if/when a subgroup change is requested.
        if not entry.group and self.group:
            entry.group = self.group

    def _handle_get_group_info(self, topic: str, payload: dict) -> None:
        info = payload.get("getGroupInfo") or {}
        group = info.get("pgrp", "")
        subgrps = info.get("subgrps") or []

        # Origin: the bridge's own from_cloud, or a child's rewritten
        # from_cloud (`rainmaker/bridges/<self>/children/<child>/from_cloud`).
        parts = topic.split("/")
        if len(parts) >= 3 and parts[0] == "rainmaker" and parts[1] == "nodes" \
                and parts[2] == self.thing and len(parts) == 4 \
                and parts[3] == "from_cloud":
            # Own
            self.group = group
            self.subgroups = list(subgrps)
            # Backfill any children that were added before we knew our
            # own group (their group is the same as ours by spec §3.2).
            for entry in self.children.values():
                if not entry.group and self.group:
                    entry.group = self.group
            # Dynamic group-control subscriptions reconcile whenever
            # our group changes (initial pgrp + future group moves).
            # Subgroup changes alone don't churn subs — the subgroup
            # wildcard absorbs them.
            self._reconcile_group_control_subs()
        elif len(parts) >= 6 and parts[0] == "rainmaker" \
                and parts[1] == "bridges" and parts[2] == self.thing \
                and parts[3] == "children" and parts[5] == "from_cloud":
            # Child via Rule A rewrite
            child = parts[4]
            entry = self.children.setdefault(child, ChildState(name=child))
            entry.group = group
            entry.subgroups = list(subgrps)

    # ---- schedule version sync (spec §5.9) ----------------------------
    #
    # The bridge publishes `getSchedVer` / `getSchedDetails` on the
    # child's *canonical* to_cloud topic (rainmaker/nodes/<child>/to_cloud),
    # not on the bridge namespace. Authorized by the
    # ChildrenToCloudPublish statement in the bridge IoT policy. The
    # publish_input_event_handler Lambda extracts thing_name=topic(3)
    # and replies on the child's from_cloud, which Rule A rewrites
    # onto the bridge namespace and lands on subscription #5.

    def _handle_sched_response(self, topic: str, payload: dict, events: list) -> None:
        """Parse a getSchedVer / getSchedDetails reply arriving on
        either subscription #1 (bridge's own from_cloud — possible only
        if someone publishes on the bridge's own to_cloud with these
        events) or subscription #5 (Rule A rewrite of a child's
        from_cloud reply)."""
        parts = topic.split("/")
        child: str | None = None
        if len(parts) == 6 and parts[0] == "rainmaker" \
                and parts[1] == "bridges" and parts[2] == self.thing \
                and parts[3] == "children" and parts[5] == "from_cloud":
            child = parts[4]
        else:
            # Not a child reply we care about. Ignore.
            return
        entry = self.children.setdefault(child, ChildState(name=child))

        if "getSchedVer" in events:
            ver_obj = payload.get("getSchedVer") or {}
            version = ver_obj.get("version")
            if version is None:
                return
            cached = entry.sched_version
            entry.sched_version = int(version)
            stale = cached is None or int(cached) != int(version)
            print(
                f"  [sched] {child} version cloud={version} cached={cached} "
                f"{'STALE — pulling details' if stale else 'in sync'}"
            )
            if stale:
                # Auto-follow up with getSchedDetails so a single
                # `get_sched_ver` invocation also pulls fresh data
                # when needed. This is the same pattern firmware
                # would use.
                self._publish_child_to_cloud(child, {"event": ["getSchedDetails"]})

        if "getSchedDetails" in events:
            details = payload.get("getSchedDetails") or {}
            # The handler embeds version inside the details object
            # (node.go:897 — AppendGetSchedDetails). Pull it out so
            # we keep sched_version aligned with sched_details.
            version = details.get("version")
            entry.sched_details = dict(details)
            if version is not None:
                entry.sched_version = int(version)
            print(
                f"  [sched] {child} got full schedule (version={version}): "
                f"{json.dumps(details)[:120]}{'…' if len(json.dumps(details)) > 120 else ''}"
            )

    def _publish_child_to_cloud(self, child_node_id: str, payload: dict) -> None:
        """Publish on a child's canonical to_cloud topic. Authorized
        by the bridge IoT policy ChildrenToCloudPublish substitution."""
        topic = f"rainmaker/nodes/{child_node_id}/to_cloud"
        print(f"→ {topic}  payload={json.dumps(payload)}")
        self.mqtt.publish(topic, payload)

    # ---- group-control --------------------------------------
    #
    # Subscribes to two topics per spec / repo group-control-feature.md:
    #   rainmaker/nodes/groups/<self.group>/control                — group broadcast
    #   rainmaker/nodes/groups/<self.group>/subgroups/+/control    — any subgroup
    # The single-segment `+` wildcard absorbs every subgroup so subscribing
    # never churns on a subgroup-membership change. Only group-level
    # *moves* trigger an unsubscribe/resubscribe cycle.

    def _gc_topics_for(self, group_id: str) -> list[str]:
        return [
            f"rainmaker/nodes/groups/{group_id}/control",
            f"rainmaker/nodes/groups/{group_id}/subgroups/+/control",
        ]

    def _reconcile_group_control_subs(self) -> None:
        """Idempotent: subscribes for self.group if not already; if
        self.group changed, unsubscribes from the old group's filters
        first. Called every time we observe a getGroupInfo for self."""
        target = self.group
        current = self._gc_subscribed_for_group
        if current == target:
            return  # no-op

        if current:
            for f in self._gc_topics_for(current):
                self.mqtt.unsubscribe(f)
            print(f"  [gc] unsubscribed from group {current!r}")
        if target:
            for f in self._gc_topics_for(target):
                self.mqtt.subscribe(f)
            print(f"  [gc] subscribed to group {target!r}")
        self._gc_subscribed_for_group = target

    @staticmethod
    def _parse_gc_topic(topic: str) -> tuple[str | None, str | None]:
        """Returns (group_id, subgroup_id_or_None) or (None, None) if
        the topic isn't a recognised group-control shape."""
        parts = topic.split("/")
        # rainmaker / nodes / groups / <g> / control
        if (len(parts) == 5 and parts[:3] == ["rainmaker", "nodes", "groups"]
                and parts[4] == "control"):
            return parts[3], None
        # rainmaker / nodes / groups / <g> / subgroups / <sg> / control
        if (len(parts) == 7 and parts[:3] == ["rainmaker", "nodes", "groups"]
                and parts[4] == "subgroups" and parts[6] == "control"):
            return parts[3], parts[5]
        return None, None

    def _dispatch_targets(self, subgroup_id: str | None) -> tuple[bool, list[str]]:
        """Per spec §5.4: returns (should_dispatch_to_self, [child_names]).
        - subgroup_id=None  → group broadcast → everyone
        - subgroup_id set    → only members of that subgroup"""
        if subgroup_id is None:
            self_target = True
            child_targets = list(self.children.keys())
        else:
            self_target = subgroup_id in self.subgroups
            child_targets = [
                name for name, c in self.children.items()
                if subgroup_id in c.subgroups
            ]
        return self_target, sorted(child_targets)

    def _handle_group_control(self, topic: str, payload: dict) -> None:
        group_id, subgroup_id = self._parse_gc_topic(topic)
        if group_id is None:
            print(f"  [gc] ignoring unrecognised group-control topic: {topic}")
            return
        if group_id != self.group:
            # Defensive — we shouldn't receive other groups' broadcasts
            # because the device policy substitution pins us to our own
            # group_id attribute.
            print(f"  [gc] WARN: received cross-group broadcast for {group_id!r}, "
                  f"self.group={self.group!r}")
            return

        scope = "group-broadcast" if subgroup_id is None else f"subgroup={subgroup_id!r}"
        self_target, child_targets = self._dispatch_targets(subgroup_id)

        # The payload is device-type-keyed per group-control-feature.md §3.2.
        # In a real bridge firmware, each receiving device filters by
        # device-type from its own registry. The sim doesn't model
        # device types — we just log the targets.
        dev_types = list(payload.keys()) if isinstance(payload, dict) else []
        print(f"  [gc] {scope}; targets self={self_target} children={child_targets}; "
              f"device_types={dev_types}")

    def start(self) -> None:
        self._printer_thread = threading.Thread(target=self._drain_inbox, daemon=True)
        self._printer_thread.start()

    def stop(self) -> None:
        self._stop = True
        if self._printer_thread:
            self._printer_thread.join(timeout=2)

    # ---- helpers ------------------------------------------------------

    def _publish_to_cloud(self, payload: dict) -> None:
        topic = f"rainmaker/nodes/{self.thing}/to_cloud"
        self.mqtt.publish(topic, payload)

    def _publish_bridge_to_cloud(self, payload: dict) -> None:
        # Rule D source per spec §3.4. Sent via basic ingest so we exercise
        # the same path real bridge firmware uses; Rule D is the sole rule
        # on this topic so no broker fanout is needed.
        topic = (
            f"$aws/rules/bridge_to_cloud_rule/"
            f"rainmaker/bridges/{self.thing}/to_cloud"
        )
        self.mqtt.publish(topic, payload)

    def _require_child(self, child_node_id: str) -> ChildState | None:
        entry = self.children.get(child_node_id)
        if entry is None:
            print(
                f"  unknown child {child_node_id!r}. "
                f"Known: {list(self.children.keys())}"
            )
            return None
        if not entry.group:
            print(
                f"  child {child_node_id!r} has no group on file yet. "
                f"Wait for the getGroupInfo from add_child to arrive "
                f"(usually <2s), then retry. Run `state` to inspect."
            )
            return None
        return entry

    # ---- commands -----------------------------------------------------

    def cmd_add_child(self, suffix: str, child_local_id: str) -> None:
        req_id = str(uuid.uuid4())[:8]
        child_name = f"{self.thing}--{suffix}"
        # Remember the local_id → child mapping pre-emptively so it's
        # visible in list_children even before the ack arrives.
        self.local_id_to_child[child_local_id] = child_name
        payload = {
            "event": ["addChild"],
            "addChild": {
                "request_id": req_id,
                "child_suffix": suffix,
                "child_local_id": child_local_id,
            },
        }
        print(f"→ rainmaker/bridges/{self.thing}/to_cloud  (request_id={req_id})")
        self._publish_bridge_to_cloud(payload)

    def cmd_remove_child(self, child_node_id: str) -> None:
        req_id = str(uuid.uuid4())[:8]
        payload = {
            "event": ["removeChild"],
            "removeChild": {
                "request_id": req_id,
                "child_node_id": child_node_id,
            },
        }
        print(f"→ rainmaker/bridges/{self.thing}/to_cloud  (request_id={req_id})")
        self._publish_bridge_to_cloud(payload)
        # Drop local state — the cloud is authoritative, but keeping
        # the entry around after a successful removal is confusing.
        self.children.pop(child_node_id, None)
        for k, v in list(self.local_id_to_child.items()):
            if v == child_node_id:
                self.local_id_to_child.pop(k, None)

    def cmd_update_child(self, child_node_id: str, json_state_str: str) -> None:
        entry = self._require_child(child_node_id)
        if entry is None:
            return
        try:
            state = json.loads(json_state_str)
        except json.JSONDecodeError as e:
            print(f"  invalid JSON: {e}")
            return
        shadow = entry.shadow_name()
        topic = f"$aws/things/{entry.name}/shadow/name/{shadow}/update"
        payload = {"state": {"reported": state}}
        print(f"→ {topic}")
        self.mqtt.publish(topic, payload)

    def cmd_update_self(self, json_state_str: str) -> None:
        if not self.group:
            print(
                "  bridge has no group on file yet. Run `getgroupinfo` "
                "and wait for the reply, then retry."
            )
            return
        try:
            state = json.loads(json_state_str)
        except json.JSONDecodeError as e:
            print(f"  invalid JSON: {e}")
            return
        shadow = _shadow_name(self.group, self.subgroups)
        topic = f"$aws/things/{self.thing}/shadow/name/{shadow}/update"
        payload = {"state": {"reported": state}}
        print(f"→ {topic}")
        self.mqtt.publish(topic, payload)

    def cmd_online(self, child_node_id: str, value: str) -> None:
        entry = self._require_child(child_node_id)
        if entry is None:
            return
        val = value.lower() in ("true", "1", "yes", "on")
        # iparams shadow is fixed (no group/subgroup suffix).
        topic = f"$aws/things/{entry.name}/shadow/name/iparams/update"
        payload = {"state": {"reported": {"online": val}}}
        print(f"→ {topic}")
        self.mqtt.publish(topic, payload)

    def cmd_getgroupinfo(self) -> None:
        req_id = str(uuid.uuid4())[:8]
        payload = {"event": ["getGroupInfo"]}
        print(f"→ rainmaker/nodes/{self.thing}/to_cloud  (getGroupInfo, request_id={req_id})")
        self._publish_to_cloud(payload)

    def cmd_get_sched_ver(self, child_node_id: str) -> None:
        """Publish {event:[getSchedVer]} on the child's to_cloud. The
        reply lands on subscription #5 (Rule A rewrite) and is parsed
        by _handle_sched_response, which auto-follows up with
        getSchedDetails on a version mismatch."""
        if child_node_id not in self.children:
            print(
                f"  unknown child {child_node_id!r}. "
                f"Known: {list(self.children.keys())}"
            )
            return
        self._publish_child_to_cloud(child_node_id, {"event": ["getSchedVer"]})

    def cmd_get_sched_details(self, child_node_id: str) -> None:
        """Force-pull full schedule details for a child. Same path as
        get_sched_ver's auto-followup, but useful for cache-busting
        the local copy."""
        if child_node_id not in self.children:
            print(
                f"  unknown child {child_node_id!r}. "
                f"Known: {list(self.children.keys())}"
            )
            return
        self._publish_child_to_cloud(child_node_id, {"event": ["getSchedDetails"]})

    def cmd_resync_schedules(self) -> None:
        """Fan-out getSchedVer to every known child. This is the
        operation a bridge runs on reconnect (spec §5.9) to discover
        which children's schedules changed during the offline window.
        Each reply triggers a getSchedDetails pull only on
        version-mismatch."""
        if not self.children:
            print("  no children to resync")
            return
        for name in sorted(self.children):
            self._publish_child_to_cloud(name, {"event": ["getSchedVer"]})
        print(f"  [sched] sent getSchedVer to {len(self.children)} children")

    def cmd_state(self) -> None:
        print(f"  thing:     {self.thing}")
        print(f"  group:     {self.group or '(unknown — try `getgroupinfo`)'}")
        print(f"  subgroups: {self.subgroups or '(none)'}")
        print(f"  shadow:    {_shadow_name(self.group, self.subgroups) if self.group else '(unknown)'}")
        if self.children:
            print(f"  children:")
            for name, c in sorted(self.children.items()):
                print(f"    {name}  group={c.group!r}  subgroups={c.subgroups}  shadow={c.shadow_name() if c.group else '?'}")
        else:
            print(f"  children:  (none)")
        if self.local_id_to_child:
            print(f"  local_id → child:")
            for lid, ch in self.local_id_to_child.items():
                print(f"    {lid} -> {ch}")

    def cmd_list_children(self) -> None:
        if not self.children and not self.local_id_to_child:
            print("  (no children seen yet)")
            return
        for name, c in sorted(self.children.items()):
            print(f"  {name}  group={c.group}  subgroups={c.subgroups}")


# ---------------------------------------------------------------------------
# CLI / config loading
# ---------------------------------------------------------------------------


def _load_from_config(config_path: Path, bridge_name: str) -> dict:
    if not config_path.exists():
        sys.exit(
            f"ERROR: {config_path} not found. Either run "
            f"tools/bridge_register.py first, or pass --endpoint / "
            f"--cert / --key / --ca explicitly."
        )
    cfg = json.loads(config_path.read_text())
    certs = cfg.get("certs", {})
    if bridge_name not in certs:
        sys.exit(
            f"ERROR: {bridge_name!r} not in {config_path}. Known: "
            f"{list(certs.keys())}"
        )
    entry = certs[bridge_name]
    return {
        "endpoint": cfg["iot_endpoint"],
        "thing": bridge_name,
        "cert": entry["cert_pem_path"],
        "key": entry["private_key_path"],
        "ca": cfg["root_ca"],
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--bridge",
        help="Bridge thing name; cert paths resolved from --config "
             "(default .bridges/bridges_config.json)",
    )
    parser.add_argument(
        "--config",
        default=str(DEFAULT_CONFIG),
        help=f"Bridges config file (default {DEFAULT_CONFIG})",
    )
    parser.add_argument("--endpoint", help="IoT data endpoint (overrides --config)")
    parser.add_argument("--thing", help="Bridge thing name (overrides --bridge)")
    parser.add_argument("--cert", help="Path to certificate PEM")
    parser.add_argument("--key", help="Path to private key")
    parser.add_argument("--ca", help="Path to Amazon root CA")
    args = parser.parse_args()

    if args.endpoint and args.thing and args.cert and args.key and args.ca:
        params = {
            "endpoint": args.endpoint,
            "thing": args.thing,
            "cert": args.cert,
            "key": args.key,
            "ca": args.ca,
        }
    elif args.bridge:
        params = _load_from_config(Path(args.config), args.bridge)
    else:
        parser.error("Pass either --bridge <name> or all of --endpoint/--thing/--cert/--key/--ca")

    print(f"Connecting as {params['thing']} → {params['endpoint']} ...")
    client = BridgeMQTT(
        endpoint=params["endpoint"],
        cert_path=params["cert"],
        key_path=params["key"],
        ca_path=params["ca"],
        thing_name=params["thing"],
    )
    client.connect()
    print("Connected.")

    # Static §3.3.1 subscriptions. The two group-control filters (3)
    # and (4) are added dynamically via _reconcile_group_control_subs
    # once we learn our group from getGroupInfo.
    bridge = params["thing"]
    for f in (
        f"rainmaker/nodes/{bridge}/from_cloud",                     # §3.3.1 #1
        f"rainmaker/nodes/{bridge}/user/+/params",                  # §3.3.1 #2
        f"rainmaker/bridges/{bridge}/children/+/from_cloud",        # #5 (Rule A target)
        f"rainmaker/bridges/{bridge}/children/+/user/+/params",     # #6 (Rule B target)
    ):
        client.subscribe(f)
    print("Subscribed to 4 static filters; group-control filters (3,4) added on first getGroupInfo.")
    print()
    print("Commands:")
    print("  add_child    <suffix> <child_local_id>")
    print("  remove_child <child_node_id>")
    print("  update_child <child_node_id> <json>")
    print("  update_self  <json>")
    print("  online       <child_node_id> <true|false>")
    print("  state")
    print("  list_children")
    print("  getgroupinfo")
    print("  get_sched_ver     <child_node_id>")
    print("  get_sched_details <child_node_id>")
    print("  resync_schedules")
    print("  quit")
    print()

    sim = BridgeSim(client)
    sim.start()

    try:
        while True:
            try:
                line = input("> ").strip()
            except EOFError:
                print()
                break
            if not line:
                continue
            # split: first 1-2 tokens are command + first arg; the
            # remainder may be a JSON literal with embedded spaces.
            parts = line.split(maxsplit=2)
            cmd = parts[0].lower()
            try:
                if cmd in ("q", "quit", "exit"):
                    break
                elif cmd == "add_child":
                    if len(parts) < 3:
                        print("usage: add_child <suffix> <child_local_id>")
                        continue
                    sim.cmd_add_child(parts[1], parts[2])
                elif cmd == "remove_child":
                    if len(parts) < 2:
                        print("usage: remove_child <child_node_id>")
                        continue
                    sim.cmd_remove_child(parts[1])
                elif cmd == "update_child":
                    if len(parts) < 3:
                        print("usage: update_child <child_node_id> <json>")
                        continue
                    sim.cmd_update_child(parts[1], parts[2])
                elif cmd == "update_self":
                    if len(parts) < 2:
                        print("usage: update_self <json>")
                        continue
                    # update_self takes the whole rest as JSON
                    rest = line.split(maxsplit=1)[1]
                    sim.cmd_update_self(rest)
                elif cmd == "online":
                    if len(parts) < 3:
                        print("usage: online <child_node_id> <true|false>")
                        continue
                    sim.cmd_online(parts[1], parts[2])
                elif cmd == "state":
                    sim.cmd_state()
                elif cmd == "list_children":
                    sim.cmd_list_children()
                elif cmd == "getgroupinfo":
                    sim.cmd_getgroupinfo()
                elif cmd == "get_sched_ver":
                    if len(parts) < 2:
                        print("usage: get_sched_ver <child_node_id>")
                        continue
                    sim.cmd_get_sched_ver(parts[1])
                elif cmd == "get_sched_details":
                    if len(parts) < 2:
                        print("usage: get_sched_details <child_node_id>")
                        continue
                    sim.cmd_get_sched_details(parts[1])
                elif cmd == "resync_schedules":
                    sim.cmd_resync_schedules()
                else:
                    print(f"unknown command: {cmd!r}")
            except Exception as e:
                print(f"command failed: {e}")
    finally:
        sim.stop()
        client.disconnect()
        print("Disconnected.")

    return 0


if __name__ == "__main__":
    sys.exit(main())
