# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""MkDocs hook for docs/cli/reference.md.

mkdocs-click 0.9 predates click 8.2's UNSET sentinel and prints it as the default of every option
that has none; this drops it. The mkdocs-click block is expanded during Markdown conversion, so
the hook runs on the HTML.
"""


def on_page_content(html, page, config, files):
    if page.file.src_uri == "reference.md":
        return html.replace("<code>Sentinel.UNSET</code>", "")
    return html
