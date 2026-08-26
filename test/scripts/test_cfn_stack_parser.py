# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Unit tests for the Stackfile parser's wave planning.

The deploy sweep runs every group in a wave concurrently, so a wrong wave is not a slow
deploy — it is a group deploying before the group whose outputs it reads. Run with
`pytest test/scripts/`; no AWS credentials and no deployed stack needed.
"""
import os
import sys

import pytest
import yaml

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "../../scripts"))

from cfn_stack_parser import (  # noqa: E402
    CyclicDependencyError,
    _ordered_groups,
    deployment_plan,
    group_waves,
    load_stackfile,
)

REPO_ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "../..")
STACKFILE = os.path.join(REPO_ROOT, "cdk/Stackfile.yaml")


def _write(tmp_path, stacks, groups):
    path = tmp_path / "Stackfile.yaml"
    path.write_text(yaml.safe_dump({
        "version": "1.0.0",
        "defaults": {"region_mode": "all"},
        "groups": groups,
        "stacks": stacks,
    }))
    return str(path)


def _stack(name, depends_on=None):
    return {
        "friendly_name": name,
        "stack_name": name,
        "template": f"{name}.template.json",
        "regions": {"mode": "all", "explicit": []},
        "parameters": {},
        **({"depends_on": depends_on} if depends_on else {}),
    }


class TestRealStackfile:
    """The wave plan has to agree with the flat plan it replaces."""

    @pytest.fixture(scope="class")
    @classmethod
    def stacks(cls):
        return load_stackfile(STACKFILE)

    def test_waves_cover_exactly_the_same_groups(self, stacks):
        flat = _ordered_groups(deployment_plan(stacks))
        assert sorted(g for wave in group_waves(stacks) for g in wave) == sorted(flat)

    def test_no_group_appears_twice(self, stacks):
        flattened = [g for wave in group_waves(stacks) for g in wave]
        assert len(flattened) == len(set(flattened))

    def test_every_cross_group_edge_points_backwards(self, stacks):
        """The property that matters: a group's prerequisites are in *earlier* waves."""
        waves = group_waves(stacks)
        wave_of = {g: i for i, wave in enumerate(waves) for g in wave}
        group_of = {s.stack_id: s.group for s in stacks if s.group}

        for stack in stacks:
            if not stack.group:
                continue
            for dep in stack.depends_on:
                dep_group = group_of.get(dep)
                if dep_group and dep_group != stack.group:
                    assert wave_of[dep_group] < wave_of[stack.group], (
                        f"{stack.stack_id} needs {dep} but {dep_group} is not in an "
                        f"earlier wave than {stack.group}"
                    )

    def test_espuser_leads_and_the_integrations_share_the_last_wave(self, stacks):
        waves = group_waves(stacks)
        assert waves[0] == ["espuser"]
        assert {"alexa", "smartthings", "gva"} <= set(waves[-1])


class TestSynthetic:
    def test_a_group_waits_for_every_group_it_draws_from(self, tmp_path):
        """b's *second* stack is what depends on c, so b must still land after c."""
        path = _write(
            tmp_path,
            stacks={
                "a1": _stack("a1"),
                "c1": _stack("c1", ["a1"]),
                "b1": _stack("b1", ["a1"]),
                "b2": _stack("b2", ["c1"]),
            },
            groups={
                "a": {"friendly_name": "a", "mandatory": True, "stacks": ["a1"]},
                "b": {"friendly_name": "b", "mandatory": True, "stacks": ["b1", "b2"]},
                "c": {"friendly_name": "c", "mandatory": True, "stacks": ["c1"]},
            },
        )
        assert group_waves(load_stackfile(path)) == [["a"], ["c"], ["b"]]

    def test_independent_groups_share_a_wave(self, tmp_path):
        path = _write(
            tmp_path,
            stacks={"a1": _stack("a1"), "b1": _stack("b1", ["a1"]), "c1": _stack("c1", ["a1"])},
            groups={
                "a": {"friendly_name": "a", "mandatory": True, "stacks": ["a1"]},
                "b": {"friendly_name": "b", "mandatory": True, "stacks": ["b1"]},
                "c": {"friendly_name": "c", "mandatory": True, "stacks": ["c1"]},
            },
        )
        waves = group_waves(load_stackfile(path))
        assert waves[0] == ["a"]
        assert sorted(waves[1]) == ["b", "c"]

    def test_group_cycle_over_acyclic_stacks_is_rejected(self, tmp_path):
        """a1 -> b1 -> a2 is a fine stack graph but makes groups a and b mutually dependent.

        Kahn leaves both undepthed rather than failing, which would silently drop them from
        the sweep — so group_waves has to raise instead.
        """
        path = _write(
            tmp_path,
            stacks={"a1": _stack("a1"), "b1": _stack("b1", ["a1"]), "a2": _stack("a2", ["b1"])},
            groups={
                "a": {"friendly_name": "a", "mandatory": True, "stacks": ["a1", "a2"]},
                "b": {"friendly_name": "b", "mandatory": True, "stacks": ["b1"]},
            },
        )
        with pytest.raises(CyclicDependencyError, match="Group-level dependency cycle"):
            group_waves(load_stackfile(path))
