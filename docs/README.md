# ESP RainMaker Neo cloud documentation (Sphinx)

`docs/` is the Sphinx root. Toolchain: **Markdown → MyST → Sphinx**, wrapped by
[esp-docs](https://github.com/espressif/esp-docs).

```
docs/
  conf_common.py requirements.txt Makefile utils.sh
  en/                     <- the only directory Sphinx reads
    index.md
    specs/                <- the cloud specification (markdown)
      admin/              <- the admin plane
```

esp-docs requires the `<source-dir>/<language>` layout and accepts only `en` or
`zh_CN`, so the `en/` level is not optional. `myst-parser` is what parses the
`.md` files; version pins are tight, so read the comments in
`requirements.txt` before bumping anything.

## Build locally

Python 3.8–3.12:

```sh
cd docs
pip install -r requirements.txt
make html          # -> _build/en/generic/html/index.html
make clean
```

To view the result, open the built entry page in a browser:

```sh
open _build/en/generic/html/index.html            # macOS
xdg-open _build/en/generic/html/index.html        # Linux
```

`make` wraps `build-docs`; do not call `sphinx-build` directly. The docs build
with **no target** (`build-docs -l en`, no `-t`), so output lands under
`_build/en/generic/`.

## What is committed vs generated

| | Where |
|---|---|
| Committed | `conf_common.py`, `en/conf.py`, `requirements.txt`, `Makefile`, `utils.sh`, all `*.md` and `*.rst` |
| Generated per build, never committed | `_build/**` |

## Adding a spec page

`en/index.md` owns the whole structure — there is no per-directory index. Its
`{toctree}` blocks are the only navigation, and each `:caption:` becomes a
sidebar heading (one caption per toctree).

1. Drop the `.md` file in `en/specs/`.
2. Add it to **one** toctree in `en/index.md`.
3. Add a one-line description to the matching `##` list in `en/index.md`.

Step 2 is enforced — a page in no toctree fails the build. Steps 1 and 3 are
not: a page can sit in the wrong group, or have no description, and the build
stays green.

Pick the group by *what question the page answers*, not by which subsystem
implements it:

| Group | The question it answers |
|---|---|
| Identity and access | Who is allowed to do what, and what data decides that |
| Admin Dashboard | What an admin or super admin can reach |
| Node lifecycle | How a node progresses from registration to steady-state messaging |
| Features | What an end user can do with a node once it is running |
| Voice assistants | How a third-party assistant platform is integrated |
| Platform | How the deployment itself runs, is operated, and is measured |

## Editing the specs

- **One `#` heading per page.** Sphinx treats each top-level heading as a
  document section, so a second `#` adds a sidebar entry that looks like a
  separate page but is an in-page anchor. Use `##` below the title.
- Cross-page links are ordinary relative Markdown links —
  `[text](other_page.md)`, or `[text](other_page.md#some-heading)` for a
  heading. A broken anchor fails the build.
- Heading anchors are generated down to level 4 (`myst_heading_anchors` in
  `conf_common.py`). A link to a `#####` heading will not resolve.
- **A role inside a directive body must use the MyST form.** A `:ref:` left in
  the RST form inside a ` ```{list-table} ` body degrades silently to plain text
  and the build stays green.
- Sequence diagrams are ordinary ` ```mermaid ` fences, so they also render on
  GitLab. The built page pulls `mermaid.js` from a CDN.
- Payload examples using `<placeholder>` values, `...` elisions or `//` comments
  must be tagged ` ```javascript `, not ` ```json `: pygments' JSON lexer errors
  on them and fails the build. Genuine JSON keeps the `json` tag.
- A file must not end with a `---` horizontal rule — docutils rejects a document
  ending in a transition.
- `html_static_path` is intentionally unset; naming a directory that does not
  exist is a fatal warning. If custom CSS is needed, create `docs/_static/` and
  set the option in the same commit.

## Warnings

`build-docs` treats any Sphinx warning not listed in `sphinx-known-warnings.txt`
as fatal. The tree builds warning-free, so that file does not exist — keep it
that way rather than adding one.

When the check trips the HTML is still produced; read
`_build/en/generic/sphinx-warning-log.txt` (uploaded as a CI artifact).

## API reference

The HTTP and MQTT surfaces are **not** part of this build. They are specified as
OpenAPI and AsyncAPI documents under `docs/api/`, and published as their own
sites:

| Surface | Site | Raw spec |
| --- | --- | --- |
| HTTP APIs | <https://api.docs.neo.rainmaker.espressif.com> | `Api_Swagger.yaml`, `User_Api_Swagger.yaml` |
| MQTT — node | <https://mqtt.docs.neo.rainmaker.espressif.com/node/> | `/MQTT_Node.yaml` |
| MQTT — user | <https://mqtt.docs.neo.rainmaker.espressif.com/user/> | `/MQTT_User.yaml` |
| Push / event payloads | <https://events.docs.neo.rainmaker.espressif.com> | `/Push_User.yaml` |
