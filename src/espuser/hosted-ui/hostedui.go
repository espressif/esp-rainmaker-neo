// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package hostedui serves the sign-in app Vite builds into dist/ as one page, its script and stylesheet inlined; it lives beside dist/ because go:embed cannot reach a parent directory.
package hostedui

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"html/template"
	"io/fs"
	"regexp"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
)

// Embedded so the UI and its backend deploy as one artifact; `all:` keeps dist/.gitkeep, all a clean checkout has.
//
//go:embed all:dist
var bundle embed.FS

var bundleFS, _ = fs.Sub(bundle, "dist")

// assetTag is how Vite's index.html loads the built files: "./assets/index-AbC.js" as a module script, "./assets/index-AbC.css" as a stylesheet.
var assetTag = regexp.MustCompile(`<script type="module" crossorigin src="\./assets/([^"/]+\.js)"></script>|<link rel="stylesheet" crossorigin href="\./assets/([^"/]+\.css)">`)

// The inlined files enter the template as values, never as its source, so text in the bundle is never read as template syntax.
const (
	scriptSlot = `<script type="module">{{.Script}}</script>`
	styleSlot  = `<style>{{.Style}}</style>`
)

// page is the built index.html, ready to serve: its template, the inlined files, and the CSP that admits exactly them.
type page struct {
	shell  *template.Template
	script string
	style  string
	csp    string
}

var built, builtErr = loadPage(bundleFS)

// loadPage inlines the bundle's script and stylesheet into index.html. A build that skipped `npm run build` leaves none, and that must fail the page routes, not the Lambda's start.
func loadPage(fsys fs.FS) (*page, error) {
	b, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return nil, rmerror.NewRMError(err, "hosted-ui bundle missing: the Makefile must run `npm run build` before `go build`")
	}
	html := string(b)
	p := &page{}
	for _, m := range assetTag.FindAllStringSubmatch(html, -1) {
		slot, name, closing, dst := scriptSlot, m[1], "</script", &p.script
		if name == "" {
			slot, name, closing, dst = styleSlot, m[2], "</style", &p.style
		}
		if *dst != "" {
			return nil, rmerror.NewRMError(nil, "hosted-ui bundle has more than one "+strings.TrimPrefix(closing, "</")+"; inline it as one file")
		}
		body, err := fs.ReadFile(fsys, "assets/"+name)
		if err != nil {
			return nil, rmerror.NewRMError(err, "hosted-ui bundle is missing assets/"+name)
		}
		// The file goes between tags, so a closing tag inside it would end the element early.
		if strings.Contains(strings.ToLower(string(body)), closing) {
			return nil, rmerror.NewRMError(nil, "hosted-ui assets/"+name+" contains "+closing+" and cannot be inlined")
		}
		*dst = string(body)
		html = strings.Replace(html, m[0], slot, 1)
	}
	if p.script == "" || p.style == "" || strings.Contains(html, "./assets/") {
		return nil, rmerror.NewRMError(nil, "hosted-ui index.html does not load exactly one script and one stylesheet from ./assets/")
	}
	if p.shell, err = template.New("shell").Parse(html); err != nil {
		return nil, rmerror.NewRMError(err, "hosted-ui index.html is not a valid template")
	}
	p.csp = "default-src 'none'; script-src " + sourceHash(p.script) + "; style-src " + sourceHash(p.style) +
		"; img-src 'self' data:; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
	return p, nil
}

// sourceHash is a CSP hash source for an inline element's exact text: "a{}" -> "'sha256-...'".
func sourceHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// CSP admits only the page's own inlined script and stylesheet, by hash: a sign-in page is the last place to let anything else execute.
func CSP() string {
	if builtErr != nil {
		return ""
	}
	return built.csp
}

// Err is why the pages cannot be served, nil when they can.
func Err() error { return builtErr }

// Shell renders the page with its data; pageDataJSON is already marshalled, so it is passed as template.JS to avoid escaping it twice.
//
//nolint:gosec // G203: pageDataJSON is json.Marshal output, which escapes <, > and &, so it cannot close the script element; the script and stylesheet are our own build, checked for closing tags at load
func Shell(pageDataJSON []byte) (string, error) {
	if builtErr != nil {
		return "", builtErr
	}
	var out bytes.Buffer
	if err := built.shell.Execute(&out, struct {
		PageData template.JS
		Script   template.JS
		Style    template.CSS
	}{template.JS(pageDataJSON), template.JS(built.script), template.CSS(built.style)}); err != nil {
		return "", err
	}
	return out.String(), nil
}
