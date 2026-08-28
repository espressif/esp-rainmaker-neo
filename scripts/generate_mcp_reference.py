#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Render docs/mcp/rainmaker-mcp.json as a browsable HTML reference.

The tool catalogue is not an OpenAPI or AsyncAPI document, so neither Swagger UI
nor the AsyncAPI html-template can render it -- hence this generator. It is the
MCP equivalent of the `asyncapi generate fromTemplate` calls in sync_mqtt: one
static page per surface, published beside its raw spec.

The catalogue is generated from the Go tool registry and pinned by
TestToolCatalogMatchesSnapshot, so this script only ever reads it. Styling is
copied from docs/api/landing_index.html to keep the site reading as one product;
like that page it fetches no external asset and has no build step of its own.

Usage:
    python3 scripts/generate_mcp_reference.py [--catalog PATH] [--out PATH]
"""

from __future__ import annotations

import argparse
import html
import json
import pathlib
import sys

REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent
DEFAULT_CATALOG = REPO_ROOT / "docs" / "mcp" / "rainmaker-mcp.json"
DEFAULT_OUT = REPO_ROOT / "build" / "docs" / "mcp" / "index.html"

# Read tools come first: they turn names into the ids the write tools need.
READ_PREFIXES = ("list_", "get_")


def esc(value: object) -> str:
    return html.escape(str(value), quote=True)


def split_paragraphs(text: str) -> list[str]:
    """Split a description into its blank-line-separated paragraphs, escaped.

    Descriptions are prose written for models and contain `backtick` spans and
    blank-line breaks. Both survive here; everything else is escaped, since the
    text is data and must never reach the page as markup.
    """
    out = []
    for block in text.split("\n\n"):
        block = block.strip()
        if not block:
            continue
        # Escape first, then re-introduce only <code> -- never the reverse.
        safe = esc(block)
        parts = safe.split("`")
        # Odd indices sit between a matched pair of backticks.
        rendered = "".join(
            f"<code>{part}</code>" if i % 2 else part for i, part in enumerate(parts)
        )
        out.append(f"<p>{rendered}</p>")
    return out


def paragraphs(text: str) -> str:
    """Render every paragraph of a description, uncollapsed."""
    return "\n".join(split_paragraphs(text))


def collapsible_description(text: str) -> str:
    """Render a description with everything after the lead paragraph hidden.

    Two levels of disclosure, doing different jobs: the card collapse answers
    "which tool", this one answers "how much prose". Without it an expanded card
    drops five paragraphs of model-steering guidance plus the parameter table at
    once, which is the wall of text the card collapse exists to avoid.

    <details>/<summary> again, so it needs no script and stays keyboard- and
    screen-reader-accessible.
    """
    paras = split_paragraphs(text)
    if len(paras) <= 1:
        return f'<div class="desc">{paragraphs(text)}</div>'

    lead, rest = paras[0], paras[1:]
    label = f"{len(rest)} more paragraph" + ("s" if len(rest) != 1 else "")
    body = chr(10).join("                " + p for p in rest)
    return f"""<div class="desc">
            {lead}
            <details class="more">
              <summary>
                <span class="more-show">Show {label}</span>
                <span class="more-hide">Hide guidance</span>
              </summary>
              <div class="more-body">
{body}
              </div>
            </details>
          </div>"""


def lead_sentence(text: str) -> str:
    """First sentence of a description, for the collapsed card's summary line.

    Closed, a card shows only its header, so that line has to say what the tool
    is for. The first sentence of the first paragraph does that in every
    description in the catalogue; the whole lead paragraph is too long to sit on
    one line beside the name.
    """
    first = text.strip().split("\n\n")[0].strip()
    # A period followed by a space ends the sentence; requiring the space avoids
    # cutting inside a quoted example.
    for i in range(len(first) - 1):
        if first[i] == "." and first[i + 1] == " ":
            first = first[: i + 1]
            break
    return esc(first)


def type_label(prop: dict) -> str:
    """Human-readable type for one input property, including array item types."""
    kind = prop.get("type", "any")
    if kind == "array":
        item_type = (prop.get("items") or {}).get("type")
        return f"array of {item_type}" if item_type else "array"
    return str(kind)


def render_param(name: str, prop: dict, required: bool) -> str:
    badges = [f'<span class="type">{esc(type_label(prop))}</span>']
    if required:
        badges.append('<span class="req">required</span>')
    else:
        badges.append('<span class="opt">optional</span>')

    enum = prop.get("enum")
    enum_html = ""
    if enum:
        values = " ".join(f"<code>{esc(v)}</code>" for v in enum)
        enum_html = f'<div class="enum">One of: {values}</div>'

    desc = prop.get("description", "")
    return f"""          <div class="param" role="row">
            <div class="pname-cell" role="cell">
              <code class="pname">{esc(name)}</code>
              {" ".join(badges)}
            </div>
            <div class="param-body" role="cell">{paragraphs(desc)}{enum_html}</div>
          </div>"""


def render_tool(tool: dict) -> str:
    schema = tool.get("inputSchema") or {}
    props = schema.get("properties") or {}
    required = set(schema.get("required") or [])

    if props:
        # Required first, then alphabetical.
        ordered = sorted(props.items(), key=lambda kv: (kv[0] not in required, kv[0]))
        params = "\n".join(render_param(n, p, n in required) for n, p in ordered)
        n_req = sum(1 for n, _ in ordered if n in required)
        detail = f"{n_req} required" if n_req else "all optional"
        tally = f"{len(ordered)} &middot; {detail}"
        params_html = f"""<div class="params-wrap">
            <div class="params-head">
              <span class="params-title">Parameters</span>
              <span class="count">{tally}</span>
            </div>
            <div class="params" role="table">
{params}
            </div>
          </div>"""
    else:
        params_html = (
            '<div class="params-wrap">\n'
            '            <div class="params-head">'
            '<span class="params-title">Parameters</span>'
            '<span class="count">none</span></div>\n'
            "          </div>"
        )

    name = tool["name"]
    # Drives the card's colour and pill, as SEND/RECEIVE does on the MQTT page.
    kind = "read" if name.startswith(READ_PREFIXES) else "write"

    # Cards are closed by default so the page reads as an index of the surface.
    # 5fr/7fr, not 1fr/1fr: prose needs less width than the parameter table, and
    # an even split left a collapsed description short beside a long table.
    summary_line = lead_sentence(tool.get("description", ""))
    return f"""      <details class="tool tool--{kind}" id="{esc(name)}">
        <summary class="tool-head">
          <span class="kind">{kind}</span>
          <code class="tool-name">{esc(name)}</code>
          <span class="tool-lead">{summary_line}</span>
        </summary>
        <div class="tool-body">
          <div class="cols">
            <div class="col-desc">{collapsible_description(tool.get("description", ""))}</div>
            <div class="col-params">{params_html}</div>
          </div>
        </div>
      </details>"""


def render(catalog: dict) -> str:
    tools = catalog.get("tools") or []
    reads = [t for t in tools if t["name"].startswith(READ_PREFIXES)]
    writes = [t for t in tools if not t["name"].startswith(READ_PREFIXES)]

    def nav_group(label: str, members: list) -> str:
        if not members:
            return ""
        links = "\n          ".join(
            f'<a href="#{esc(t["name"])}">{esc(t["name"])}</a>' for t in members
        )
        return f"""        <div class="toc-group">
          <span class="toc-label">{label}</span>
          {links}
        </div>"""

    nav = "\n".join(g for g in (nav_group("Read", reads), nav_group("Write", writes)) if g)

    groups = []
    # Titled more fully than the nav's "Read"/"Write" so it is not a verbatim
    # repeat of the link group above.
    for title, members in (("Read tools", reads), ("Write tools", writes)):
        if not members:
            continue
        body = "\n".join(render_tool(t) for t in members)
        groups.append(f"      <h2>{title}</h2>\n{body}")

    name = catalog.get("name", "mcp")
    version = catalog.get("version", "")

    return f"""<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>ESP RainMaker Neo MCP Tool Reference</title>
    <link rel="icon" type="image/png" href="../favicon-neo-32x32.png" sizes="32x32" />
    <link rel="icon" type="image/png" href="../favicon-neo-16x16.png" sizes="16x16" />
    <!--
      GENERATED FILE -- do not edit.

      Produced by scripts/generate_mcp_reference.py from
      docs/mcp/rainmaker-mcp.json, which is itself generated from the Go tool
      registry (`make update-mcp-schema`). Edit a tool's description in the
      registry, not here.

      Palette and font stack are copied from docs/api/landing_index.html for the
      reasons documented there.
    -->
    <style>
      :root {{
        --font-sans:
          system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto,
          Oxygen, Ubuntu, Cantarell, "Open Sans", "Helvetica Neue", Helvetica,
          Arial, "PingFang SC", "Microsoft YaHei", 黑体, sans-serif;
        --font-mono:
          ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas,
          "Liberation Mono", monospace;

        --background: #fafcff;
        --foreground: #121212;
        --foreground-secondary: #12121299;
        --muted-foreground: #666666;
        --card: #ffffff;
        --border: #e1e1e1;
        --secondary: #2a5a9a;
        --primary: #0071e3;
        --sidebar-foreground: #183251;

        --brand-red: #e54c3c;
        --brand-purple: #9472c8;
        --brand-blue: #3283f2;

        /* Semantic colour, as SEND/RECEIVE and GET/POST are on the other
           references: read is safe to call, write changes device state. Pill
           colour, header tint, border tone. */
        --read: #2f6fd0;
        --read-tint: #eef4fd;
        --read-edge: #cfe0f8;

        --write: #c8452f;
        --write-tint: #fdf1ee;
        --write-edge: #f7d5cd;

        --type-accent: #1a5fb4;
      }}

      *,
      *::before,
      *::after {{
        box-sizing: border-box;
      }}

      body {{
        margin: 0;
        padding: 0 20px 64px;
        background: var(--background);
        color: var(--foreground);
        font-family: var(--font-sans);
        line-height: 1.55;
        -webkit-font-smoothing: antialiased;
      }}

      /* 1040px puts the side margins ~20% narrower than the previous 940px on a
         1440px screen. The prose blocks cap their own measure, so the extra width
         goes to the parameter table rather than to long lines of text. */
      .wrap {{
        max-width: 1040px;
        margin: 0 auto;
      }}

      /* Matches the banner sync_mqtt injects into the AsyncAPI renderings, so
         every reference page has the same way back to the index. */
      .rmng-home {{
        font: 14px/1.4 var(--font-sans);
        margin: 0 -20px 40px;
        padding: 12px 16px;
        background: var(--background);
        border-bottom: 1px solid var(--border);
      }}

      .rmng-home a {{
        color: var(--secondary);
        text-decoration: none;
      }}

      .rmng-home a:hover {{
        text-decoration: underline;
      }}

      h1 {{
        font-size: 1.75rem;
        font-weight: 600;
        letter-spacing: -0.02em;
        margin: 0 0 0.375rem;
        color: var(--sidebar-foreground);
      }}

      .subtitle {{
        margin: 0;
        color: var(--muted-foreground);
        font-size: 0.9375rem;
        max-width: 46rem;
      }}

      h2 {{
        font-size: 0.75rem;
        font-weight: 600;
        text-transform: uppercase;
        letter-spacing: 0.06em;
        color: var(--foreground-secondary);
        margin: 2.5rem 0 0.375rem;
      }}

      .group-blurb {{
        margin: 0 0 1rem;
        font-size: 0.875rem;
        color: var(--muted-foreground);
      }}

      .intro {{
        margin-top: 2rem;
        max-width: 68ch;
      }}

      .intro p {{
        margin: 0 0 0.875rem;
        font-size: 0.9375rem;
        color: var(--foreground-secondary);
      }}

      .intro strong {{
        font-weight: 600;
        color: var(--foreground);
      }}

      /* Same gradient accent bar as the landing page cards, for the same
         reason -- a border cannot carry a gradient. */
      /* Padding lives on the inner regions so the header band spans full width. */
      .tool {{
        overflow: hidden;
        background: var(--card);
        border: 1px solid var(--border);
        border-radius: 0.5rem;
        margin-bottom: 1rem;
      }}

      .tool--read {{
        --kind: var(--read);
        --kind-tint: var(--read-tint);
        --kind-edge: var(--read-edge);
      }}

      .tool--write {{
        --kind: var(--write);
        --kind-tint: var(--write-tint);
        --kind-edge: var(--write-edge);
      }}

      .tool-head {{
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 0.625rem;
        padding: 12px 20px;
        background: var(--kind-tint);
        cursor: pointer;
        list-style: none;
      }}

      .tool-head::-webkit-details-marker {{
        display: none;
      }}

      .tool[open] > .tool-head {{
        border-bottom: 1px solid var(--kind-edge);
      }}

      .tool-head:hover {{
        /* Avoids a second hardcoded hex per kind. */
        background: color-mix(in srgb, var(--kind-tint) 88%, var(--kind));
      }}

      .tool-head:focus-visible {{
        outline: 2px solid var(--primary);
        outline-offset: -2px;
      }}

      .tool-head::before {{
        content: "";
        flex: none;
        width: 0;
        height: 0;
        border-top: 4.5px solid transparent;
        border-bottom: 4.5px solid transparent;
        border-left: 6px solid var(--kind);
        transition: transform 120ms ease;
      }}

      .tool[open] > .tool-head::before {{
        transform: rotate(90deg);
      }}

      .tool-name {{
        flex: none;
        font-family: var(--font-mono);
        font-size: 1rem;
        font-weight: 600;
        background: none;
        padding: 0;
        color: var(--sidebar-foreground);
      }}

      /* Truncates rather than wrapping the band onto a second line. */
      .tool-lead {{
        flex: 1 1 16rem;
        min-width: 0;
        font-size: 0.8125rem;
        color: var(--muted-foreground);
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
      }}

      @media (max-width: 620px) {{
        .tool-lead {{
          display: none;
        }}
      }}

      .kind {{
        flex: none;
        min-width: 4.25rem;
        padding: 3px 8px;
        border-radius: 0.25rem;
        background: var(--kind);
        color: #fff;
        font-family: var(--font-mono);
        font-size: 0.6875rem;
        font-weight: 700;
        letter-spacing: 0.06em;
        text-transform: uppercase;
        text-align: center;
      }}

      .tool-body {{
        padding: 20px 20px 24px;
      }}

      /* minmax(0, 1fr) stops a long token widening the grid past the card. */
      .params {{
        display: grid;
        grid-template-columns: minmax(0, 1fr);
      }}

      .param {{
        padding: 0.75rem 0;
        border-top: 1px solid var(--border);
      }}

      .param .param-body {{
        margin-top: 0.25rem;
      }}

      .desc {{
        max-width: 62ch;
      }}

      .desc p {{
        margin: 0 0 0.625rem;
        font-size: 0.9375rem;
        color: var(--foreground);
      }}

      .cols {{
        display: grid;
        grid-template-columns: minmax(0, 5fr) minmax(0, 7fr);
        gap: 0 2rem;
        align-items: start;
      }}

      .col-params {{
        padding-left: 2rem;
        border-left: 1px solid var(--border);
      }}

      /* Below this the two columns are too narrow to set prose and a parameter
         table side by side, so they stack -- description first. */
      @media (max-width: 900px) {{
        .cols {{
          grid-template-columns: minmax(0, 1fr);
        }}

        .col-params {{
          padding-left: 0;
          border-left: 0;
          margin-top: 0.5rem;
        }}
      }}

      /* Lead paragraph always shown; the guidance sits behind this toggle. */
      .more {{
        margin-top: 0.25rem;
      }}

      .more > summary {{
        display: inline-flex;
        align-items: center;
        gap: 0.3rem;
        cursor: pointer;
        font-size: 0.8125rem;
        color: var(--kind, var(--secondary));
        list-style: none;
      }}

      .more > summary::-webkit-details-marker {{
        display: none;
      }}

      .more > summary::before {{
        content: "";
        width: 0;
        height: 0;
        border-top: 4px solid transparent;
        border-bottom: 4px solid transparent;
        border-left: 5px solid currentcolor;
        transition: transform 120ms ease;
      }}

      .more[open] > summary::before {{
        transform: rotate(90deg);
      }}

      .more > summary:hover {{
        text-decoration: underline;
      }}

      .more > summary:focus-visible {{
        outline: 2px solid var(--primary);
        outline-offset: 2px;
        border-radius: 2px;
      }}

      .more[open] .more-show,
      .more:not([open]) .more-hide {{
        display: none;
      }}

      .more-body {{
        margin-top: 0.75rem;
      }}

      .more-body p:last-child {{
        margin-bottom: 0;
      }}

      h4 {{
        font-size: 0.75rem;
        font-weight: 600;
        text-transform: uppercase;
        letter-spacing: 0.06em;
        color: var(--foreground-secondary);
        margin: 0 0 0.5rem;
      }}

      .params-wrap {{
        margin-top: 1.5rem;
        border-top: 1px solid var(--border);
      }}

      .params-head {{
        display: flex;
        align-items: center;
        gap: 0.5rem;
        padding: 0.875rem 0 0.25rem;
      }}

      .params-title {{
        font-size: 0.75rem;
        font-weight: 600;
        text-transform: uppercase;
        letter-spacing: 0.06em;
        color: var(--foreground-secondary);
      }}

      .count {{
        font-weight: 400;
        text-transform: none;
        letter-spacing: 0;
        font-size: 0.8125rem;
        color: var(--muted-foreground);
      }}

      .pname-cell {{
        display: flex;
        flex-wrap: wrap;
        align-items: baseline;
        gap: 0.375rem;
        align-content: start;
      }}

      .pname {{
        font-weight: 600;
        background: none;
        padding: 0;
        color: var(--sidebar-foreground);
        /* Break rather than push the column wider. */
        overflow-wrap: anywhere;
      }}

      .type {{
        font-family: var(--font-mono);
        font-size: 0.6875rem;
        line-height: 1.5;
        color: var(--type-accent);
        white-space: nowrap;
      }}

      /* required is a warning and gets a filled chip; optional is the default and
         gets plain grey text. Boxing both made the table a wall of chips. */
      /* Same treatment as .opt; colour alone carries the distinction. */
      .req {{
        font-size: 0.6875rem;
        color: var(--write);
        white-space: nowrap;
      }}

      .opt {{
        font-size: 0.6875rem;
        color: var(--muted-foreground);
        white-space: nowrap;
      }}

      .param-body p {{
        margin: 0;
        font-size: 0.875rem;
        color: var(--foreground-secondary);
      }}

      .param-body p + p {{
        margin-top: 0.375rem;
      }}

      .enum {{
        margin-top: 0.5rem;
        font-size: 0.8125rem;
        color: var(--muted-foreground);
      }}

      .enum code {{
        font-size: 0.75rem;
      }}


      code {{
        font-family: var(--font-mono);
        font-size: 0.8125em;
        background: #f2f5f9;
        padding: 0.1em 0.35em;
        border-radius: 3px;
      }}

      .masthead {{
        display: flex;
        flex-wrap: wrap;
        align-items: flex-start;
        justify-content: space-between;
        gap: 1rem 2rem;
      }}

      .masthead-main {{
        flex: 1 1 22rem;
      }}

      .actions {{
        display: flex;
        flex-wrap: wrap;
        gap: 0.5rem;
        /* Sits optically level with the two-line title block. */
        padding-top: 0.25rem;
      }}

      .btn {{
        display: inline-block;
        padding: 6px 12px;
        background: var(--card);
        border: 1px solid var(--border);
        border-radius: 0.375rem;
        font-size: 0.8125rem;
        font-weight: 500;
        color: var(--secondary);
        text-decoration: none;
        transition:
          border-color 150ms ease,
          box-shadow 150ms ease;
      }}

      .btn:hover {{
        border-color: #cfd6e0;
        box-shadow: 0 1px 3px rgb(18 18 18 / 8%);
      }}

      .btn:focus-visible {{
        outline: 2px solid var(--primary);
        outline-offset: 2px;
      }}

      .facts {{
        display: flex;
        flex-wrap: wrap;
        gap: 0 2.5rem;
        margin: 1.75rem 0 0;
        padding: 0.875rem 0;
        border-top: 1px solid var(--border);
        border-bottom: 1px solid var(--border);
      }}

      .fact dt {{
        font-size: 0.6875rem;
        font-weight: 600;
        text-transform: uppercase;
        letter-spacing: 0.06em;
        color: var(--muted-foreground);
        margin-bottom: 0.1875rem;
      }}

      .fact dd {{
        margin: 0;
        font-size: 0.875rem;
      }}

      nav.toc {{
        display: flex;
        flex-wrap: wrap;
        gap: 0.75rem 2rem;
        margin: 1.25rem 0 0;
      }}

      .toc-group {{
        display: flex;
        flex-wrap: wrap;
        align-items: baseline;
        gap: 0.5rem;
      }}

      .toc-label {{
        font-size: 0.6875rem;
        font-weight: 600;
        text-transform: uppercase;
        letter-spacing: 0.06em;
        color: var(--muted-foreground);
      }}

      nav.toc a {{
        font-family: var(--font-mono);
        font-size: 0.8125rem;
        color: var(--secondary);
        text-decoration: none;
        border-bottom: 1px solid transparent;
      }}

      nav.toc a:hover {{
        border-bottom-color: currentcolor;
      }}

      nav.toc a:focus-visible {{
        outline: 2px solid var(--primary);
        outline-offset: 2px;
        border-radius: 2px;
      }}

      footer {{
        margin-top: 3rem;
        padding-top: 1.25rem;
        border-top: 1px solid var(--border);
        font-size: 0.8125rem;
        color: var(--muted-foreground);
      }}

      footer a {{
        color: var(--secondary);
      }}
    </style>
  </head>
  <body>
    <div class="rmng-home"><a href="../">&larr; API Reference Home</a></div>
    <div class="wrap">
      <header class="masthead">
        <div class="masthead-main">
          <h1>ESP RainMaker Neo MCP Tool Reference</h1>
          <p class="subtitle">
            Tools exposed by the ESP RainMaker Neo MCP server, as an MCP client
            sees them from <code>tools/list</code>.
          </p>
        </div>
        <div class="actions">
          <a class="btn" href="../http/?urls.primaryName=MCP%20API"
            >HTTP &amp; OAuth surface</a
          >
        </div>
      </header>

      <!-- Labelled pairs rather than a &middot;-separated run: the facts are of
           different kinds (identity, version, count) and read better named. -->
      <dl class="facts">
        <div class="fact">
          <dt>Server</dt>
          <dd><code>{esc(name)}</code></dd>
        </div>
        <div class="fact">
          <dt>Version</dt>
          <dd><code>{esc(version)}</code></dd>
        </div>
        <div class="fact">
          <dt>Tools</dt>
          <dd>{len(tools)}</dd>
        </div>
      </dl>

      <!-- A page-level intro, in the spirit of the description block on the
           Swagger pages: what this surface is, how it is reached, and how it is
           organised, before any individual tool. -->
      <section class="intro">
        <p>
          These are the tools an MCP client receives from <code>tools/list</code>
          after authenticating against the ESP RainMaker Neo MCP endpoint. They read and
          control devices that already exist on a signed-in user's account, and
          every call is authorised as that user &mdash; the same RBAC checks the
          REST API applies, so a client only ever sees the devices its user can
          reach.
        </p>
        <p>
          Reaching them takes a Bearer token from the OAuth 2.0 proxy in front of
          the endpoint. That HTTP surface
          &mdash; the endpoint, the discovery documents and the token exchange
          &mdash; is specified separately in the
          <a href="../http/?urls.primaryName=MCP%20API">HTTP & OAuth surface</a> reference; this page covers only what the tools themselves do.
        </p>
        <p>
          The tools divide in two, <strong>Read</strong> and <strong>Write</strong>.
      </section>

{chr(10).join(groups)}

      <footer>
        Generated from the server's tool registry &mdash; descriptions here are
        verbatim what the model receives.
        <a href="https://docs.neo.rainmaker.espressif.com/"
          >ESP RainMaker Neo documentation</a
        >
      </footer>
    </div>
  </body>
</html>
"""


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--catalog", type=pathlib.Path, default=DEFAULT_CATALOG)
    parser.add_argument("--out", type=pathlib.Path, default=DEFAULT_OUT)
    args = parser.parse_args()

    if not args.catalog.is_file():
        print(f"catalogue not found: {args.catalog}", file=sys.stderr)
        return 1

    catalog = json.loads(args.catalog.read_text())
    if not catalog.get("tools"):
        print(f"catalogue has no tools: {args.catalog}", file=sys.stderr)
        return 1

    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(render(catalog))
    print(f"wrote {args.out} ({len(catalog['tools'])} tools)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
