#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
cfn_stack_parser.py -- Stackfile parser, validator, and deployment-plan engine.

Reads a cdk/Stackfile.yaml, validates all references, detects dependency cycles
(Kahn's algorithm), and produces a deployment plan grouped into parallel stages.

Group membership is declared top-down in the `groups:` layer (Package -> Group ->
Stack) and derived onto each stack by reverse lookup; the `packages:` layer is
presentation-only and ignored by the deploy engine.

Usage:
    python3 scripts/cfn_stack_parser.py --stackfile cdk/Stackfile.yaml
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import logging
from collections import deque
from dataclasses import dataclass, field
from pathlib import Path
from typing import Dict, List, Optional, Set

import yaml

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
)
log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
CROSS_STACK_REF_PATTERN = re.compile(
    r"\$\{(?P<stack>[A-Za-z0-9_-]+)\.(?P<type>outputs|inputs)\.(?P<key>[A-Za-z0-9_]+)\}"
)
SUPPORTED_SCHEMA_VERSIONS = {"1.0.0"}

# Supported variables for ${VAR} substitution in stack_name.
# To add a new variable, add an entry here and supply its value at resolution time.
SUPPORTED_VARIABLES: Dict[str, str] = {
    "APP_REGION": "The AWS region selected by the user for deployment",
}

VARIABLE_PATTERN = re.compile(r"\$\{(?P<var>[A-Z_]+)\}")

# ---------------------------------------------------------------------------
# Exceptions
# ---------------------------------------------------------------------------

class StackfileError(ValueError):
    """Base exception for any Stackfile validation failure."""


class CyclicDependencyError(StackfileError):
    """Raised when a dependency cycle is detected among stacks."""

    def __init__(self, nodes: List[str]) -> None:
        self.nodes = nodes
        super().__init__(
            "Cyclic dependency detected among stacks: "
            + " -> ".join(nodes)
        )


# ---------------------------------------------------------------------------
# Stack-name variable helpers
# ---------------------------------------------------------------------------

def has_name_variables(name: str) -> bool:
    """Return True if *name* contains any ``${VAR}`` placeholder."""
    return bool(VARIABLE_PATTERN.search(name))


def resolve_stack_name(name: str, variables: Dict[str, str]) -> str:
    """Replace every ``${VAR}`` in *name* with the value from *variables*."""
    def _replace(m: re.Match) -> str:
        var = m.group("var")
        if var not in variables:
            raise ValueError(
                f"Cannot resolve variable '{var}' in stack name '{name}': "
                f"no value provided. Known variables: {list(variables.keys())}"
            )
        return variables[var]

    return VARIABLE_PATTERN.sub(_replace, name)


def _validate_name_variables(stack_name: str, stack_id: str) -> None:
    """Raise StackfileError if *stack_name* references an unknown variable."""
    for m in VARIABLE_PATTERN.finditer(stack_name):
        var = m.group("var")
        if var not in SUPPORTED_VARIABLES:
            raise StackfileError(
                f"Stack '{stack_id}': stack_name '{stack_name}' references "
                f"unknown variable '${{{var}}}'. "
                f"Supported variables: {list(SUPPORTED_VARIABLES.keys())}"
            )


# ---------------------------------------------------------------------------
# Data Classes
# ---------------------------------------------------------------------------

@dataclass
class ParameterDef:
    """A single parameter override defined in the Stackfile."""

    name: str
    value: str = ""
    default: str = ""
    prompt: bool = False
    description: str = ""
    configurable: bool = False
    # Resolved at parse time when value is a single ${stack.(outputs|inputs).Key} ref.
    cross_stack_ref: Optional[Dict[str, str]] = None

    def to_dict(self) -> dict:
        parameter_dict = {
            "name": self.name,
            "value": self.value,
            "default": self.default,
            "prompt": self.prompt,
            "description": self.description,
            "configurable": self.configurable,
        }
        if self.cross_stack_ref:
            parameter_dict["cross_stack_ref"] = self.cross_stack_ref
        return parameter_dict


@dataclass
class RegionConfig:
    """Region deployment configuration for a stack."""

    mode: str = "all"  # "all" | "explicit"
    explicit: Dict[str, str] = field(default_factory=dict)  # region -> mandatory|optional

    def to_dict(self) -> dict:
        region_dict: dict = {"mode": self.mode}
        if self.mode == "explicit":
            region_dict["explicit"] = self.explicit
        return region_dict


@dataclass
class StackDef:
    """Fully parsed definition of a single stack."""

    stack_id: str
    stack_name: str
    template: str
    group: str = ""  # Derived from the `groups:` layer; display/CLI-only, no deploy semantics.
    depends_on: List[str] = field(default_factory=list)
    regions: RegionConfig = field(default_factory=RegionConfig)
    parameters: List[ParameterDef] = field(default_factory=list)

    def to_dict(self) -> dict:
        return {
            "stack_id": self.stack_id,
            "stack_name": self.stack_name,
            "template": self.template,
            "group": self.group,
            "depends_on": self.depends_on,
            "regions": self.regions.to_dict(),
            "parameters": [p.to_dict() for p in self.parameters],
            "has_name_variables": has_name_variables(self.stack_name),
        }


@dataclass
class DeploymentStage:
    """A group of stacks at the same dependency depth (can run in parallel)."""

    stage: int
    stacks: List[StackDef] = field(default_factory=list)

    def to_dict(self) -> dict:
        return {
            "stage": self.stage,
            "stacks": [s.to_dict() for s in self.stacks],
        }


# ---------------------------------------------------------------------------
# YAML Parsing
# ---------------------------------------------------------------------------

def _parse_parameter(name: str, raw: dict) -> ParameterDef:
    """Parse a single parameter entry from the YAML."""
    if not isinstance(raw, dict):
        raise StackfileError(
            f"Parameter '{name}' must be a mapping, got {type(raw).__name__}"
        )

    param = ParameterDef(
        name=name,
        value=str(raw.get("value", "")),
        default=str(raw.get("default", "")),
        prompt=bool(raw.get("prompt", False)),
        description=str(raw.get("description", "")),
        configurable=bool(raw.get("configurable", False)),
    )

    match = CROSS_STACK_REF_PATTERN.fullmatch(param.value)
    if match:
        param.cross_stack_ref = {
            "stack": match.group("stack"),
            "type": match.group("type"),
            "key": match.group("key"),
        }

    return param


def _parse_regions(raw: Optional[dict], default_mode: str) -> RegionConfig:
    """Parse the regions block, falling back to global defaults."""
    if raw is None:
        return RegionConfig(mode=default_mode)

    mode = raw.get("mode", default_mode)
    explicit_raw = raw.get("explicit", {})

    explicit: Dict[str, str] = {}
    if isinstance(explicit_raw, dict):
        explicit = {str(k): str(v) for k, v in explicit_raw.items()}
    elif isinstance(explicit_raw, list):
        # Accept a plain list as all mandatory
        explicit = {str(r): "mandatory" for r in explicit_raw}

    if mode == "explicit" and not explicit:
        raise StackfileError(
            "regions.mode is 'explicit' but no regions listed in regions.explicit"
        )

    return RegionConfig(mode=mode, explicit=explicit)


def _merge_addon_stackfiles(doc: dict, core_stackfile: Path) -> None:
    """Merge each optional module's Stackfile fragment into *doc*, in place.

    Mirrors app.py's discovery-by-presence: any addon_modules/<module>/cdk/Stackfile.yaml
    sitting beside the core repo is merged when present, so core names no private
    module and an OSS checkout (no addon_modules folder) is untouched. A fragment
    carries the same schema, usually just `groups:` and `stacks:`; its packages
    layer may extend an existing package's group list.

    A fragment may reference core stacks (depends_on rmng-base) but may not
    redefine them: a duplicate stack or group id is an error, not an override.
    """
    modules_dir = core_stackfile.resolve().parent.parent / os.environ.get(
        "RMNG_OPTIONAL_MODULES_DIR", "addon_modules")
    if not modules_dir.is_dir():
        return
    for fragment_path in sorted(modules_dir.glob("*/cdk/Stackfile.yaml")):
        fragment = yaml.safe_load(fragment_path.read_text())
        if not isinstance(fragment, dict):
            raise StackfileError(f"{fragment_path}: root must be a YAML mapping")
        for layer in ("groups", "stacks"):
            for key, value in (fragment.get(layer) or {}).items():
                if key in (doc.get(layer) or {}):
                    raise StackfileError(
                        f"{fragment_path}: {layer[:-1]} '{key}' already defined in core Stackfile")
                doc.setdefault(layer, {})[key] = value
        for pkg_id, pkg in (fragment.get("packages") or {}).items():
            existing = (doc.get("packages") or {}).get(pkg_id)
            if existing is None:
                doc.setdefault("packages", {})[pkg_id] = pkg
            else:
                existing.setdefault("groups", []).extend(
                    g for g in pkg.get("groups", []) if g not in existing["groups"])
        log.info("Merged addon Stackfile: %s", fragment_path)


def load_stackfile(path: str | Path) -> List[StackDef]:
    """
    Parse a cdk/Stackfile.yaml and return a list of StackDef objects.
    Applies global defaults where per-stack values are absent, and derives each
    stack's group from the top-down `groups:` layer. Optional-module Stackfile
    fragments (addon_modules/*/cdk/Stackfile.yaml) are merged in when present.
    """
    filepath = Path(path)
    if not filepath.exists():
        raise FileNotFoundError(f"Stackfile not found: {filepath}")

    with open(filepath) as f:
        doc = yaml.safe_load(f)

    if not isinstance(doc, dict):
        raise StackfileError("Stackfile root must be a YAML mapping")

    _merge_addon_stackfiles(doc, filepath)

    # -- Schema version check ------------------------------------------------
    version = str(doc.get("version", ""))
    if version not in SUPPORTED_SCHEMA_VERSIONS:
        raise StackfileError(
            f"Unsupported Stackfile version '{version}'. "
            f"Supported: {', '.join(sorted(SUPPORTED_SCHEMA_VERSIONS))}"
        )

    # -- Global defaults -----------------------------------------------------
    defaults = doc.get("defaults", {}) or {}
    default_region_mode = defaults.get("region_mode", "all")

    # -- Stacks --------------------------------------------------------------
    stacks_raw = doc.get("stacks")
    if not stacks_raw or not isinstance(stacks_raw, dict):
        raise StackfileError("Stackfile must contain a non-empty 'stacks' mapping")

    stacks: List[StackDef] = []

    for stack_id, stack_raw in stacks_raw.items():
        if not isinstance(stack_raw, dict):
            raise StackfileError(
                f"Stack '{stack_id}' must be a mapping, got {type(stack_raw).__name__}"
            )

        stack_name = stack_raw.get("stack_name")
        template = stack_raw.get("template")

        if not stack_name:
            raise StackfileError(f"Stack '{stack_id}' is missing 'stack_name'")
        if not template:
            raise StackfileError(f"Stack '{stack_id}' is missing 'template'")

        _validate_name_variables(str(stack_name), str(stack_id))

        depends_on = stack_raw.get("depends_on", []) or []
        if not isinstance(depends_on, list):
            raise StackfileError(
                f"Stack '{stack_id}' depends_on must be a list"
            )

        regions = _parse_regions(stack_raw.get("regions"), default_region_mode)

        params_raw = stack_raw.get("parameters", {}) or {}
        parameters = [
            _parse_parameter(pname, pval)
            for pname, pval in params_raw.items()
        ]

        stacks.append(
            StackDef(
                stack_id=str(stack_id),
                stack_name=str(stack_name),
                template=str(template),
                depends_on=[str(d) for d in depends_on],
                regions=regions,
                parameters=parameters,
            )
        )

    _assign_groups(stacks, doc.get("groups", {}) or {})

    return stacks


def _assign_groups(stacks: List[StackDef], groups_raw: dict) -> None:
    """Derive each stack's group by reverse lookup from the `groups:` layer.

    Group membership is declared top-down under `groups.<id>.stacks`; a stack
    listed in no group keeps its empty default.
    """
    stack_by_id = {s.stack_id: s for s in stacks}
    for group_id, group_def in groups_raw.items():
        for sid in (group_def or {}).get("stacks", []) or []:
            target = stack_by_id.get(str(sid))
            if target is not None:
                target.group = str(group_id)


# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

def validate_references(stacks: List[StackDef]) -> None:
    """
    Verify that every depends_on entry and every ${stack.(outputs|inputs).X}
    reference names a stack that actually exists in the Stackfile.

    Raises StackfileError with a descriptive message on failure.
    """
    known_ids: Set[str] = {s.stack_id for s in stacks}
    errors: List[str] = []

    for stack in stacks:
        # Check depends_on targets
        for dep in stack.depends_on:
            if dep not in known_ids:
                errors.append(
                    f"Stack '{stack.stack_id}' depends_on unknown stack '{dep}'"
                )

        # Check cross-stack parameter references
        for param in stack.parameters:
            if param.cross_stack_ref:
                ref_stack = param.cross_stack_ref["stack"]
                if ref_stack not in known_ids:
                    errors.append(
                        f"Stack '{stack.stack_id}', parameter '{param.name}' "
                        f"references unknown stack '{ref_stack}' "
                        f"in value '{param.value}'"
                    )

    if errors:
        raise StackfileError(
            "Reference validation failed:\n  - " + "\n  - ".join(errors)
        )


# ---------------------------------------------------------------------------
# Cycle Detection (Kahn's Algorithm -- BFS Topological Sort)
# ---------------------------------------------------------------------------

def detect_cycles(stacks: List[StackDef]) -> List[str]:
    """
    Perform a topological sort using Kahn's algorithm (BFS).

    Returns the sorted list of stack IDs.
    Raises CyclicDependencyError listing the offending nodes if a cycle exists.
    """
    id_set: Set[str] = {s.stack_id for s in stacks}

    # Build adjacency list and in-degree map
    adjacency: Dict[str, List[str]] = {s.stack_id: [] for s in stacks}
    in_degree: Dict[str, int] = {s.stack_id: 0 for s in stacks}

    for stack in stacks:
        for dep in stack.depends_on:
            if dep in id_set:
                adjacency[dep].append(stack.stack_id)
                in_degree[stack.stack_id] += 1

    # Seed the queue with zero-in-degree nodes
    queue: deque[str] = deque()
    for sid in in_degree:
        if in_degree[sid] == 0:
            queue.append(sid)

    sorted_order: List[str] = []

    while queue:
        node = queue.popleft()
        sorted_order.append(node)
        for neighbour in adjacency[node]:
            in_degree[neighbour] -= 1
            if in_degree[neighbour] == 0:
                queue.append(neighbour)

    if len(sorted_order) != len(stacks):
        cycle_nodes = [
            sid for sid in in_degree if sid not in set(sorted_order)
        ]
        raise CyclicDependencyError(cycle_nodes)

    return sorted_order


# ---------------------------------------------------------------------------
# Stack Resolution (filter by requested stacks + transitive dependencies)
# ---------------------------------------------------------------------------

def resolve_stacks(
    all_stacks: List[StackDef],
    requested: List[str],
) -> List[StackDef]:
    """
    Given the full list of stacks from the Stackfile and a list of requested
    stack names (stack_name, not stack_id), resolve the minimal subset that
    includes every requested stack plus all of its transitive dependencies.

    Accepts stack_name values (e.g. 'rmng-core').  Raises StackfileError if
    a requested name does not match any stack in the Stackfile.
    """
    name_to_id: Dict[str, str] = {s.stack_name: s.stack_id for s in all_stacks}
    id_to_stack: Dict[str, StackDef] = {s.stack_id: s for s in all_stacks}

    # Validate that every requested name exists
    unknown = [n for n in requested if n not in name_to_id]
    if unknown:
        raise StackfileError(
            "Unknown stack name(s): " + ", ".join(unknown)
        )

    # BFS backwards through depends_on to collect all required stack IDs
    needed: Set[str] = set()
    queue: deque[str] = deque(name_to_id[n] for n in requested)

    while queue:
        sid = queue.popleft()
        if sid in needed:
            continue
        needed.add(sid)
        stack = id_to_stack[sid]
        for dep in stack.depends_on:
            if dep not in needed:
                queue.append(dep)

    # Return in original declaration order (preserves Stackfile ordering)
    return [s for s in all_stacks if s.stack_id in needed]


# ---------------------------------------------------------------------------
# Deployment Plan
# ---------------------------------------------------------------------------

def deployment_plan(stacks: List[StackDef]) -> List[DeploymentStage]:
    """
    Group stacks by dependency depth into DeploymentStage objects.
    Stacks at the same depth can be deployed in parallel.

    Internally runs validation and cycle detection before building the plan.
    """
    validate_references(stacks)
    detect_cycles(stacks)

    id_to_stack: Dict[str, StackDef] = {s.stack_id: s for s in stacks}

    # Compute depth for each stack via BFS from roots
    depth: Dict[str, int] = {}
    in_degree: Dict[str, int] = {s.stack_id: 0 for s in stacks}
    adjacency: Dict[str, List[str]] = {s.stack_id: [] for s in stacks}

    for stack in stacks:
        for dep in stack.depends_on:
            adjacency[dep].append(stack.stack_id)
            in_degree[stack.stack_id] += 1

    queue: deque[str] = deque()
    for sid, deg in in_degree.items():
        if deg == 0:
            depth[sid] = 0
            queue.append(sid)

    while queue:
        node = queue.popleft()
        for neighbour in adjacency[node]:
            candidate_depth = depth[node] + 1
            depth[neighbour] = max(depth.get(neighbour, 0), candidate_depth)
            in_degree[neighbour] -= 1
            if in_degree[neighbour] == 0:
                queue.append(neighbour)

    # Group by depth
    max_depth = max(depth.values()) if depth else 0
    stages: List[DeploymentStage] = []

    for d in range(max_depth + 1):
        stage_stacks = [
            id_to_stack[sid]
            for sid, sd in sorted(depth.items())
            if sd == d
        ]
        if stage_stacks:
            stages.append(DeploymentStage(stage=d, stacks=stage_stacks))

    return stages


# ---------------------------------------------------------------------------
# Serialisation (Frontend-Consumable)
# ---------------------------------------------------------------------------

def plan_to_dict(stages: List[DeploymentStage]) -> dict:
    """
    Convert the deployment plan into a plain dict ready for JSON serialisation.
    Designed to be returned directly from an API Gateway / Lambda response.
    """
    return {
        "total_stages": len(stages),
        "total_stacks": sum(len(s.stacks) for s in stages),
        "stages": [stage.to_dict() for stage in stages],
    }


# ---------------------------------------------------------------------------
# CLI Entry Point
# ---------------------------------------------------------------------------

def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Parse a Stackfile and produce a deployment plan."
    )
    parser.add_argument(
        "--stackfile",
        default="cdk/Stackfile.yaml",
        help="Path to the Stackfile (default: cdk/Stackfile.yaml)",
    )
    parser.add_argument(
        "--stacks",
        nargs="*",
        default=None,
        metavar="STACK_NAME",
        help=(
            "Optional list of stack names to deploy. "
            "Transitive dependencies are included automatically. "
            "If omitted, all stacks in the Stackfile are planned."
        ),
    )
    parser.add_argument(
        "--format",
        choices=["json", "text", "groups"],
        default="json",
        help=(
            "Output format (default: json). "
            "'groups' outputs space-separated CDK group names in "
            "deployment order (for Makefile consumption)."
        ),
    )
    return parser.parse_args()


def _print_text_plan(stages: List[DeploymentStage]) -> None:
    """Pretty-print the deployment plan in human-readable form."""
    print(f"\nDeployment Plan ({len(stages)} stages)")
    print("=" * 50)
    for stage in stages:
        stack_ids = [s.stack_id for s in stage.stacks]
        parallel_label = " (parallel)" if len(stack_ids) > 1 else ""
        print(f"\n  Stage {stage.stage}{parallel_label}:")
        for s in stage.stacks:
            deps = f" (after: {', '.join(s.depends_on)})" if s.depends_on else ""
            prompts = [p.name for p in s.parameters if p.prompt]
            prompt_label = f"  [prompts: {', '.join(prompts)}]" if prompts else ""
            print(f"    - {s.stack_id} ({s.stack_name}){deps}{prompt_label}")
    print()


def _print_groups(stages: List[DeploymentStage]) -> None:
    """Print unique CDK group names in deployment order (space-separated).

    Feeds the Makefile's default all-groups sweep.
    """
    seen: Set[str] = set()
    ordered: List[str] = []
    for stage in stages:
        for stack in stage.stacks:
            if stack.group and stack.group not in seen:
                seen.add(stack.group)
                ordered.append(stack.group)
    print(" ".join(ordered))


def main() -> None:
    args = parse_args()

    try:
        all_stacks = load_stackfile(args.stackfile)
        log.info("Loaded %d stack(s) from %s", len(all_stacks), args.stackfile)

        if args.stacks:
            stacks = resolve_stacks(all_stacks, args.stacks)
            log.info(
                "Resolved %d stack(s) for requested: %s",
                len(stacks),
                ", ".join(args.stacks),
            )
        else:
            stacks = all_stacks

        stages = deployment_plan(stacks)
        log.info("Deployment plan: %d stage(s)", len(stages))

        if args.format == "json":
            print(json.dumps(plan_to_dict(stages), indent=2))
        elif args.format == "text":
            _print_text_plan(stages)
        elif args.format == "groups":
            _print_groups(stages)

    except CyclicDependencyError as exc:
        log.error("Cycle detected: %s", exc)
        sys.exit(1)
    except StackfileError as exc:
        log.error("Validation error: %s", exc)
        sys.exit(1)
    except FileNotFoundError as exc:
        log.error("%s", exc)
        sys.exit(1)


if __name__ == "__main__":
    main()
