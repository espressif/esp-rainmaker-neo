# SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

# Common (non-language-specific) Sphinx configuration, imported by en/conf.py.

# type: ignore
# pylint: disable=wildcard-import
# pylint: disable=undefined-variable


import os
import sys

from esp_docs.conf_docs import *

sys.path.insert(0, os.path.abspath("."))

extensions += [
    "sphinx_copybutton",
    # Parses the .md spec pages. esp-docs lists '.md' in source_suffix but wires
    # it to recommonmark through the pre-Sphinx-3 'source_parsers' setting,
    # which modern Sphinx ignores.
    "myst_parser",
    # esp-docs bundles blockdiag/seqdiag/nwdiag but not mermaid.
    "sphinxcontrib.mermaid",
]

# The specs use plain ```mermaid fences so they also render on GitLab; without
# this MyST would treat them as an unhighlightable code block.
myst_fence_as_directive = ["mermaid"]

# Anchors for cross-page links that target a heading. Level 4 is the shallowest
# that covers every heading currently linked to.
myst_heading_anchors = 4

# Do not add sphinx.ext.autosectionlabel: myst_heading_anchors already provides
# heading targets, and enabling both gives two competing label schemes.

# No GitHub mirror to edit against; drop the theme's 'Edit on GitHub' link.
html_context["display_github"] = False

html_copy_source = False
html_show_sourcelink = False

# html_static_path is deliberately unset: naming a directory that does not exist
# is a fatal warning here. Create docs/_static/ and set it in the same commit if
# custom CSS is ever needed.

# Used by sphinx_idf_theme for the version/doc switcher URLs.
project_slug = "esp-rainmaker-neo-cloud"

# idf_targets is unset: a cloud backend has no ESP target, so build-docs runs
# without -t and output lands in _build/<lang>/generic/.

languages = ["en"]

_DOCS_DIR = os.path.dirname(os.path.abspath(__file__))
_ESPUSER_SPECS = os.path.join(
    os.path.dirname(_DOCS_DIR), "src", "espuser", "docs", "specs"
)
_STUB_TO_ROOT = "../../../../"

for _lang in languages:
    _stub_dir = os.path.join(_DOCS_DIR, _lang, "specs", "espuser")
    os.makedirs(_stub_dir, exist_ok=True)

    _specs = sorted(f for f in os.listdir(_ESPUSER_SPECS) if f.endswith(".md"))
    for _name in _specs:
        _target = "{}src/espuser/docs/specs/{}".format(_STUB_TO_ROOT, _name)
        _stub = "```{{include}} {}\n```\n".format(_target)
        _stub_path = os.path.join(_stub_dir, _name)
        if not os.path.exists(_stub_path) or open(_stub_path).read() != _stub:
            with open(_stub_path, "w") as _fh:
                _fh.write(_stub)

    for _stale in set(os.listdir(_stub_dir)) - set(_specs):
        os.remove(os.path.join(_stub_dir, _stale))
