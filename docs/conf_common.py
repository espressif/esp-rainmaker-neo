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

# html_static_path is deliberately unset: naming a directory that does not exist
# is a fatal warning here. Create docs/_static/ and set it in the same commit if
# custom CSS is ever needed.

# Used by sphinx_idf_theme for the version/doc switcher URLs.
project_slug = "esp-rainmaker-neo-cloud"

# idf_targets is unset: a cloud backend has no ESP target, so build-docs runs
# without -t and output lands in _build/<lang>/generic/.

languages = ["en"]
