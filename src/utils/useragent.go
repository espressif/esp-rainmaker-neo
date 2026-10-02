// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"strings"

	"github.com/aws/aws-lambda-go/events"
)

// UserAgent is the caller's User-Agent as API Gateway parsed it from the request.
func UserAgent(request events.APIGatewayProxyRequest) string {
	return request.RequestContext.Identity.UserAgent
}

// UserAgentType renders a stored User-Agent as something a person recognises: "Chrome on macOS" for a browser, "Android app" for a native client. A hint for a human, never a security control -- it is self-reported, and it runs at render time so the parser can improve without a migration.
//
// A native client's User-Agent names an SDK, not a browser, so only the OS is worth reporting for an app row; "App" is the honest answer when even that cannot be told.
//
// Order matters in the tables below: every Chromium browser also says "Safari", Edge and Opera also say "Chrome", and iPadOS Safari says "Macintosh". The specific claim is tested first.
func UserAgentType(userAgent, origin string) string {
	ua := strings.TrimSpace(userAgent)
	if origin == "app" {
		if os := osName(ua); os != "" {
			return os + " app"
		}
		return "App"
	}
	browser, os := browserName(ua), osName(ua)
	switch {
	case browser != "" && os != "":
		return browser + " on " + os
	case browser != "":
		return browser
	case os != "":
		return os
	default:
		return "Unknown"
	}
}

func browserName(ua string) string {
	for _, candidate := range []struct{ token, name string }{
		// Edge and Opera embed the whole Chrome UA, so they must be tested before it.
		{"Edg/", "Edge"},
		{"EdgiOS/", "Edge"},
		{"OPR/", "Opera"},
		{"Firefox/", "Firefox"},
		{"FxiOS/", "Firefox"},
		{"CriOS/", "Chrome"},
		{"Chrome/", "Chrome"},
		// Only reached when nothing Chromium matched, which is what makes it really Safari.
		{"Safari/", "Safari"},
	} {
		if strings.Contains(ua, candidate.token) {
			return candidate.name
		}
	}
	return ""
}

func osName(ua string) string {
	for _, candidate := range []struct{ token, name string }{
		// iPhone and iPad before Macintosh: iPadOS Safari deliberately claims to be a Mac,
		// and the more specific token is the truthful one.
		{"iPhone", "iPhone"},
		{"iPad", "iPad"},
		{"Android", "Android"},
		{"Macintosh", "macOS"},
		{"Mac OS X", "macOS"},
		{"Windows", "Windows"},
		{"CrOS", "ChromeOS"},
		{"Linux", "Linux"},
	} {
		if strings.Contains(ua, candidate.token) {
			return candidate.name
		}
	}
	return ""
}
