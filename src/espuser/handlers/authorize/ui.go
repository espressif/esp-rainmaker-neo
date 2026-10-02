// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"html/template"
)

// The page is embedded rather than fetched: one self-contained response under a CSP that allows no remote origin keeps a third-party outage or a compromised CDN out of the page where a sign-in credential is entered.
//
//go:embed templates/login.html
var loginTemplateSrc string

//go:embed templates/login.css
var loginCSS string

// The wordmark is inlined so it inherits currentColor and works on either theme.
//
//go:embed templates/espressif.svg
var espressifWordmark string

//go:embed templates/error.html
var errorTemplateSrc string

// loginTemplate is parsed once per cold start. html/template escapes by context, so a display
// name or a href reaching the page cannot break out of its element -- escaping is the
// compiler's job here rather than something each call site has to remember.
var loginTemplate = template.Must(template.New("login").Parse(loginTemplateSrc))

// errorTemplate is the terminal page shown when we cannot safely redirect -- an unknown
// client, an unregistered redirect_uri. It shares the login page's stylesheet so a person
// who lands here does not appear to have left the product.
var errorTemplate = template.Must(template.New("error").Parse(errorTemplateSrc))

// providerView is one button. LogoSVG and DefaultMark are template.HTML because they are SVG
// markup we control: the logo comes from the provider registry, which only an operator with
// DynamoDB access can write, and the default is a constant in this file.
type providerView struct {
	Label   string
	Href    string
	LogoSVG template.HTML
}

// loginView is everything the page renders from.
type loginView struct {
	Wordmark    template.HTML
	Providers   []providerView
	CSS         template.CSS
	DefaultMark template.HTML
	Nonce       string
	// FlowID is a JS string literal, marshalled rather than interpolated so it cannot
	// terminate the script element.
	FlowID template.JS
}

// renderLoginPage produces the sign-in page.
func renderLoginPage(providers []providerView, nonce, flowID string) (string, error) {
	flowIDLit, err := json.Marshal(flowID)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = loginTemplate.Execute(&out, loginView{
		Wordmark:    template.HTML(espressifWordmark),
		Providers:   providers,
		CSS:         template.CSS(loginCSS),
		DefaultMark: template.HTML(defaultMarkSVG),
		Nonce:       nonce,
		FlowID:      template.JS(flowIDLit),
	})
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

// renderErrorPage produces the terminal error page. It carries the OAuth code and the
// description and nothing else: no stack, no client id, no state, because this page is
// reachable by anyone who can construct a URL.
func renderErrorPage(code, description string) (string, error) {
	var out bytes.Buffer
	err := errorTemplate.Execute(&out, struct {
		Wordmark    template.HTML
		CSS         template.CSS
		Code        string
		Description string
	}{template.HTML(espressifWordmark), template.CSS(loginCSS), code, description})
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

// defaultMarkSVG is the mark for a provider row that has no logo of its own.
const defaultMarkSVG = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 2L4 6v6c0 5 3.4 8.4 8 10 4.6-1.6 8-5 8-10V6z"/><path d="M9 12l2 2 4-4"/></svg>`
