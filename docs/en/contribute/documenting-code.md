# Documenting Code

Three documentation surfaces exist, and a change is only complete when the
relevant ones are updated. CI validates the API specs.

## 1. The cloud specification (this site)

Behavioral documentation lives in `docs/en/specs/` — Markdown, built with
Sphinx via [esp-docs](https://github.com/espressif/esp-docs) and
`myst-parser`. Each page is self-contained and covers one feature area.

If your change alters how the backend *behaves* — data models, flows, access
control, contracts with nodes or apps — update the corresponding spec page,
or add a new one and list it in the appropriate `toctree` in
`docs/en/index.md`.

Build locally:

```shell
cd docs
pip install -r requirements.txt
make html     # -> _build/en/generic/html/index.html
```

`make` wraps `build-docs`; do not call `sphinx-build` directly. See
`docs/README.md` for the layout rules and version pins.

## 2. API references

- **HTTP APIs** — OpenAPI documents in `misc/swagger/`. Any change to a
  public HTTP API must be reflected there; a Swagger UI ships alongside for
  viewing.
- **MQTT** — AsyncAPI specs, same directory. Device↔cloud topic or payload
  changes go here.

## 3. In-code documentation

- Go: doc comments on exported symbols, following standard `godoc`
  conventions.
- Comment the *why*, not the *what*, and only when non-obvious — see the
  [Style Guide](style-guide.md).

## Glossary

Domain terms are defined once, in `misc/GLOSSARY.md`. If your change
introduces a new concept, add it there rather than redefining it per-page.
