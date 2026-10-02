// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"

	"github.com/aws/aws-lambda-go/events"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/refreshtoken"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"
)

const (
	// destinationCookieName carries the caller's validated post-logout URL across the upstream
	// hop, because the provider matches our return URL exactly and nothing may be appended.
	destinationCookieName = "__Host-esp_logout_to"
	// destinationCookieTTL bounds how long that memo is worth anything. A sign-out chain is
	// seconds; five minutes is generous and still forgets a browser abandoned mid-hop.
	destinationCookieTTL = 300

	envLogoutDoneURL = "ESPUSER_LOGOUT_DONE_URL"
)

// handleLogout is RP-Initiated Logout 1.0 §2.
//
// A GET that destroys state, deliberately: only a top-level navigation carries the
// SameSite=Lax session cookie, gets its Set-Cookie honoured, and can hand the browser on to
// the upstream. A fetch() does none of the three.
//
// Families are deleted before the session row. The reverse order, interrupted, leaves someone
// signed out of the login but still signed in to everything it opened.
func handleLogout(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if request.HTTPMethod != http.MethodGet {
		return oidc.OAuthErrorResp(http.StatusMethodNotAllowed, oidc.OAuthErrInvalidRequest, "Method not allowed."), nil
	}
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	q := request.QueryStringParameters

	// Where to send the browser afterwards. Resolved BEFORE anything is destroyed, so a
	// rejected redirect never leaves a half-ended session behind.
	hint := verifyHint(ctx, q["id_token_hint"])
	destination := resolveDestination(ctx, rmngCtx, q, hint)
	// RP-Initiated Logout 1.0 §2: echo the caller's state on the post-logout redirect. It rides
	// the destination itself, so it survives both the direct redirect and the upstream hop, where
	// the destination is carried in a cookie and applied at /oauth2/logout/done.
	destination = withState(destination, q["state"])

	cookieValue := rmngrequest.Cookie(request, session.CookieName)
	sess := session.NewService(rmngCtx).Lookup(cookieValue)

	// An id_token_hint naming a different subject than the cookie means the two disagree
	// about who is signing out. OIDC says the hint identifies the session to end; ending a
	// DIFFERENT person's session on a stranger's say-so is the one outcome that must not
	// happen, so a mismatch ends nothing and simply redirects.
	if sess != nil && hint != nil && hint.subject != sess.UserID {
		rlog.Warn(ctx).Msg("logout: id_token_hint names a different subject; ending nothing")
		sess = nil
	}

	provider := ""
	if sess != nil {
		provider = sess.Provider
		if _, err := refreshtoken.NewService(rmngCtx).DeleteFamiliesBySID(sess.UserID, sess.SID); err != nil {
			rlog.Error(ctx).Err(err).Msg("logout: revoking families failed; session left intact")
			return redirect(destination, ""), nil
		}
		// At logout a missing row is success -- "there was no session" and "it is gone now" are
		// the same outcome.
		if err := session.NewService(rmngCtx).DeleteSessionBySID(sess.UserID, sess.SID); err != nil && !errors.Is(err, sessions_db.ErrSessionNotFound) {
			rlog.Error(ctx).Err(err).Msg("logout: deleting the session row failed")
		}
	}

	// The cookie is cleared whatever happened above. A dangling cookie whose row is gone is
	// harmless -- Lookup fails closed -- but leaving it set makes the browser present a
	// credential that can never work again, and every request pays for the lookup.
	clear := session.SetCookieHeader("", 0)

	// Their session decides whether they re-prompt; skipping this signs the person straight back in.
	//
	// The provider is handed OUR return URL, never the caller's, so it holds one registered
	// sign-out URL however many products exist -- the shape the login leg already has. The
	// caller's destination rides across in a cookie and is applied at /oauth2/logout/done.
	if upstream := upstreamLogoutURL(ctx, rmngCtx, provider, logoutDoneURL()); upstream != "" {
		cookies := []string{clear}
		if destination != "" {
			cookies = append(cookies, destinationCookie(destination, destinationCookieTTL))
		}
		return redirectWithCookies(upstream, cookies), nil
	}
	return redirect(destination, clear), nil
}

// handleLogoutDone is where the upstream hands the browser back. It takes no query parameters
// -- a provider matches the return URL exactly, so nothing may be appended -- and reads the
// destination from the cookie the sign-out set. The value was already validated against the
// client registry; the check below is belt-and-braces, because anything that forwards a
// browser is one mistake from being an open redirect wearing the issuer's hostname.
func handleLogoutDone(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Written escaped (a URL carries characters a cookie value may not), so read unescaped.
	to, unescapeErr := url.QueryUnescape(rmngrequest.Cookie(request, destinationCookieName))
	if unescapeErr != nil {
		to = ""
	}
	clear := destinationCookie("", 0)
	if !isForwardableDestination(to) {
		if to != "" {
			rlog.Warn(ctx).Msg("logout done: destination cookie is not a forwardable URL; landing on the issuer")
		}
		// No memo, or nothing we will forward to. Say plainly that the sign-out worked rather
		// than sending the browser somewhere it did not ask for.
		return redirect("", clear), nil
	}
	return redirect(to, clear), nil
}

// isForwardableDestination admits an absolute URI whose scheme names a place, not code. Custom
// schemes are allowed because a native app's post-logout URI is one (RFC 8252 s7.1).
func isForwardableDestination(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() {
		return false
	}
	return !clients.IsDangerousRedirectScheme(u.Scheme)
}

// destinationCookie renders the memo carrying the caller's post-logout URL across the upstream
// hop. __Host- for the same reasons the session cookie carries it, and SameSite=Lax because
// the provider returns the browser by top-level navigation -- Strict would suppress it and the
// person would land on the issuer's page instead of the product they signed out of.
func destinationCookie(value string, maxAge int64) string {
	return fmt.Sprintf("%s=%s; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=%d",
		destinationCookieName, url.QueryEscape(value), maxAge)
}

// logoutDoneURL is our own return URL, handed to the upstream. Empty when unset, which
// disables the upstream hop entirely rather than sending a provider somewhere wrong.
func logoutDoneURL() string { return os.Getenv(envLogoutDoneURL) }

// resolveDestination exact-matches post_logout_redirect_uri against the client's registered
// list, like redirect_uri and for the same reason: an unvalidated forward is an open redirect
// wearing the issuer's hostname. Anything unvalidated falls back to the issuer's own page and
// never to an error -- refusing to SIGN OUT over a cosmetic problem would leave the session alive.
func resolveDestination(ctx context.Context, rmngCtx *rmngctx.RmngContext, q map[string]string, hint *idTokenHint) string {
	want, clientID := q["post_logout_redirect_uri"], q["client_id"]
	if hint != nil {
		// RP-Initiated Logout 1.0 §2: client_id must match the hint's audience, and the hint may stand in for an absent one.
		if clientID == "" && len(hint.audience) == 1 {
			clientID = hint.audience[0]
		} else if clientID != "" && !slices.Contains(hint.audience, clientID) {
			rlog.Info(ctx).Str("client_id", clientID).Msg("logout: client_id is not the id_token_hint's audience; ignoring post_logout_redirect_uri")
			return ""
		}
	}
	if want == "" || clientID == "" {
		return ""
	}
	client, err := clients.NewService(rmngCtx).Get(clientID)
	if err != nil || client == nil {
		rlog.Info(ctx).Str("client_id", clientID).Msg("logout: unknown client; ignoring post_logout_redirect_uri")
		return ""
	}
	if !client.AllowsPostLogoutRedirectURI(want) {
		rlog.Info(ctx).Str("client_id", clientID).Msg("logout: unregistered post_logout_redirect_uri; ignoring")
		return ""
	}
	return want
}

// withState appends the RP's state query parameter to the validated post-logout destination so
// it is echoed back to the RP (RP-Initiated Logout 1.0 §2). An absent state, or a destination we
// will not forward to, is returned unchanged.
func withState(destination, state string) string {
	if destination == "" || state == "" {
		return destination
	}
	u, err := url.Parse(destination)
	if err != nil {
		return destination
	}
	q := u.Query()
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String()
}

// idTokenHint is what a verified id_token_hint says; nil when the hint is absent or does not verify.
type idTokenHint struct {
	subject  string
	audience []string
}

// verifyHint treats an unverifiable hint as absent: refusing on one bought nothing -- a caller can simply omit the parameter -- and cost every sign-out whose hint had aged past its hour. The cookie stays the credential; the hint only ever narrows.
func verifyHint(ctx context.Context, hint string) *idTokenHint {
	if hint == "" {
		return nil
	}
	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("logout: cannot verify id_token_hint; ignoring it")
		return nil
	}
	sub, aud, err := svc.VerifyIDTokenHint(hint)
	if err != nil {
		rlog.Warn(ctx).Err(err).Msg("logout: unverifiable id_token_hint; ignoring it")
		return nil
	}
	return &idTokenHint{subject: sub, audience: aud}
}

// upstreamLogoutURL builds the provider's end-session URL, carrying our final destination in
// whichever query parameter that provider names. Empty when the provider row configures
// none, which is the standalone default: a deployment that never fills the field behaves
// exactly as it does today.
func upstreamLogoutURL(ctx context.Context, rmngCtx *rmngctx.RmngContext, providerName, destination string) string {
	if providerName == "" {
		return ""
	}
	row, err := identity_providers_db.NewIdentityProvidersDB(rmngCtx).GetProvider(providerName)
	if err != nil || row == nil || row.EndSessionURL == "" {
		return ""
	}
	parsed, err := url.Parse(row.EndSessionURL)
	if err != nil {
		rlog.Warn(ctx).Str("provider", providerName).Msg("logout: provider end_session_url is unparseable; skipping the upstream hop")
		return ""
	}
	if destination != "" {
		param := row.EndSessionRedirectParam
		if param == "" {
			param = "post_logout_redirect_uri"
		}
		query := parsed.Query()
		query.Set(param, destination)
		parsed.RawQuery = query.Encode()
	}
	return parsed.String()
}

// redirect sends the browser on, clearing the session cookie when one was set. no-store
// because a cached logout response is a logout that stops happening.
func redirect(location, setCookie string) events.APIGatewayProxyResponse {
	if location == "" {
		// Nowhere validated to send them. Say plainly that it worked rather than 302 to a
		// URL a caller merely asked for.
		resp := utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("Signed out."))
		resp.Headers["Cache-Control"] = "no-store"
		if setCookie != "" {
			resp.Headers["Set-Cookie"] = setCookie
		}
		return resp
	}
	headers := map[string]string{
		"Location":      location,
		"Cache-Control": "no-store",
		// A logout URL can carry an id_token_hint; never leak it to the destination.
		"Referrer-Policy": "no-referrer",
	}
	if setCookie != "" {
		headers["Set-Cookie"] = setCookie
	}
	return events.APIGatewayProxyResponse{StatusCode: http.StatusFound, Headers: headers}
}

// redirectWithCookies is redirect() for the one response that has to set more than one cookie:
// the hop to the upstream clears the session and stores the destination memo at the same time.
//
// APIGatewayProxyResponse.Headers is map[string]string, so it can carry exactly one Set-Cookie
// and the second would be silently dropped -- the browser would keep a session cookie whose row
// is gone, or lose the memo and land on the issuer's page instead of the product. MultiValueHeaders
// is the only way to emit both, and the two must never both carry Set-Cookie.
func redirectWithCookies(location string, setCookies []string) events.APIGatewayProxyResponse {
	resp := redirect(location, "")
	if len(setCookies) > 0 {
		resp.MultiValueHeaders = map[string][]string{"Set-Cookie": setCookies}
	}
	return resp
}
