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
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/signout"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
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

// logoutParams are the RP-Initiated Logout 1.0 s2 request parameters, the only ones carried from a form POST to the GET and from the GET to the sign-out page.
var logoutParams = []string{"id_token_hint", "logout_hint", "client_id", "post_logout_redirect_uri", "state"}

// handleLogout is RP-Initiated Logout 1.0 §2.
//
// A GET that destroys state, deliberately: only a top-level navigation carries the
// SameSite=Lax session cookie, gets its Set-Cookie honoured, and can hand the browser on to
// the upstream. A fetch() does none of the three.
//
// Any site can navigate a browser here, so only an id_token_hint minted for this very session and person ends it at once; anything else is put to the person on the sign-out page (s2, s6).
func handleLogout(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if request.HTTPMethod == http.MethodPost {
		return logoutPostAsGet(request), nil
	}
	if request.HTTPMethod != http.MethodGet {
		return oidc.OAuthErrorResp(http.StatusMethodNotAllowed, oidc.OAuthErrInvalidRequest, "Method not allowed."), nil
	}
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	// Resolved BEFORE anything is destroyed, so a rejected redirect never leaves a half-ended session behind.
	logout, err := resolveLogout(ctx, rmngCtx, request.QueryStringParameters)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("logout: id_token_hint could not be checked")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	// Find, not Lookup: this response either ends the session or asks about it, and re-issues no cookie.
	sess, err := session.NewService(rmngCtx).Find(rmngrequest.Cookie(request, session.CookieName))

	switch {
	// An unreadable session is not an absent one: answering "signed out" would leave it and its logins alive.
	case err != nil:
		rlog.Error(ctx).Err(err).Msg("logout: session unreadable; nothing ended")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	case sess == nil:
		// Nothing to end; clearing a cookie whose row is gone stops the browser presenting a credential that can never work again.
		return redirect(logout.destination, session.SetCookieHeader("", 0)), nil
	case logout.hint == nil || logout.hint.SID != sess.SID || logout.hint.Subject != sess.UserID:
		// Relative, so whatever stage or base path the browser arrived on carries over.
		return redirect(session.SignOutPage+"?"+logoutQuery(request.QueryStringParameters).Encode(), ""), nil
	}
	next, cookies, err := endSession(ctx, rmngCtx, sess, logout.destination)
	if err != nil {
		// Never back to the app: it would take the redirect as a sign-out that did not happen.
		rlog.Error(ctx).Err(err).Msg("logout: revoking families failed; session left intact")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	return redirectWithCookies(next, cookies), nil
}

// logoutConfirmation is the person's yes on the sign-out page: the token it was served with and the RP-Initiated Logout parameters it was opened with, in the body so id_token_hint stays out of this request's URL.
type logoutConfirmation struct {
	CSRFToken             string `json:"csrf_token"`
	IDTokenHint           string `json:"id_token_hint,omitempty"`
	LogoutHint            string `json:"logout_hint,omitempty"`
	ClientID              string `json:"client_id,omitempty"`
	PostLogoutRedirectURI string `json:"post_logout_redirect_uri,omitempty"`
	State                 string `json:"state,omitempty"`
}

func (a logoutConfirmation) params() map[string]string {
	return map[string]string{
		"id_token_hint": a.IDTokenHint, "logout_hint": a.LogoutHint, "client_id": a.ClientID,
		"post_logout_redirect_uri": a.PostLogoutRedirectURI, "state": a.State,
	}
}

// handleLogoutConfirm ends the session the sign-out page asked about and answers where the browser goes next. Another site's POST carries no SameSite=Lax cookie and ends nothing; csrf_token stops a same-site page, whose POST does carry it.
func handleLogoutConfirm(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if request.HTTPMethod != http.MethodPost {
		return oidc.OAuthErrorResp(http.StatusMethodNotAllowed, oidc.OAuthErrInvalidRequest, "Method not allowed."), nil
	}
	var answer logoutConfirmation
	if err := rmngrequest.ExtractRequestStruct(request, &answer); err != nil {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "The body must carry csrf_token."), nil
	}
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	logout, err := resolveLogout(ctx, rmngCtx, answer.params())
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("logout answer: id_token_hint could not be checked")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	cookieValue := rmngrequest.Cookie(request, session.CookieName)
	sess, err := session.NewService(rmngCtx).Find(cookieValue)

	switch {
	case err != nil:
		rlog.Error(ctx).Err(err).Msg("logout answer: session unreadable; nothing ended")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	case sess == nil:
		// No Set-Cookie: a top-level cross-site POST may still set or clear a SameSite=Lax cookie (RFC 6265bis s5.7), so an answer that ended nothing must not touch one.
		return signOutAnswer(logout.destination, nil), nil
	case !session.IsLogoutCSRFToken(cookieValue, answer.CSRFToken):
		rlog.Warn(ctx).Msg("logout answer: csrf_token is not this session's; ending nothing")
		return oidc.OAuthErrorResp(http.StatusForbidden, oidc.OAuthErrInvalidRequest, "This sign-out was not confirmed on this browser's sign-out page."), nil
	}
	next, cookies, err := endSession(ctx, rmngCtx, sess, logout.destination)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("logout answer: revoking families failed; session left intact")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	return signOutAnswer(next, cookies), nil
}

// signOutAnswer is the sign-out answer's JSON reply, setting whatever cookies the sign-out produced.
func signOutAnswer(next string, cookies []string) events.APIGatewayProxyResponse {
	resp := withCookies(utils.APIGwRespJSON(http.StatusOK, auth.RedirectResponse{RedirectTo: next}), cookies)
	resp.Headers["Cache-Control"] = "no-store"
	return resp
}

func endSession(ctx context.Context, rmngCtx *rmngctx.RmngContext, sess *session.Session, destination string) (next string, cookies []string, err error) {
	// At logout a missing row is success: "there was no session" and "it is gone now" are the same outcome.
	if err := signout.NewService(rmngCtx).EndSession(sess.UserID, sess.SID); err != nil && !errors.Is(err, sessions_db.ErrSessionNotFound) {
		return "", nil, err
	}
	cookies = []string{session.SetCookieHeader("", 0)}

	// Their session decides whether they re-prompt; skipping this signs the person straight back in.
	//
	// The provider is handed OUR return URL, never the caller's, so it holds one registered
	// sign-out URL however many products exist -- the shape the login leg already has. The
	// caller's destination rides across in a cookie and is applied at /oauth2/logout/done.
	upstream := upstreamLogoutURL(ctx, rmngCtx, sess.Provider, logoutDoneURL())
	if upstream == "" {
		return destination, cookies, nil
	}
	if destination != "" {
		cookies = append(cookies, destinationCookie(destination, destinationCookieTTL))
	}
	return upstream, cookies, nil
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
		// No memo, or nothing we will forward to: our own sign-out page says the sign-out worked, never somewhere the browser did not ask for. Relative from /oauth2/logout/done.
		return redirect("../"+session.SignOutPage, clear), nil
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

// logoutRequest is what the s2 parameters establish before any session is touched.
type logoutRequest struct {
	// hint is nil when id_token_hint is absent or does not verify.
	hint *auth.IDTokenHint
	// destination is the validated post_logout_redirect_uri with state; empty when there is nowhere to forward to.
	destination string
}

// resolveLogout reads the s2 parameters. An id_token_hint that does not verify is an error in the request, so it forfeits the redirect back to the RP (RP-Initiated Logout 1.0 s4); one that cannot be checked at all is an error.
func resolveLogout(ctx context.Context, rmngCtx *rmngctx.RmngContext, q map[string]string) (logoutRequest, error) {
	var hint *auth.IDTokenHint
	if raw := q["id_token_hint"]; raw != "" {
		svc, err := auth.NewOAuthUserAuthService(ctx)
		if err != nil {
			return logoutRequest{}, rmerror.NewRMError(err, "failed to build the service that verifies id_token_hint")
		}
		if hint, err = svc.VerifyIDTokenHint(raw); err != nil {
			rlog.Warn(ctx).Err(err).Msg("logout: id_token_hint does not verify")
			return logoutRequest{}, nil
		}
	}
	// The state rides the destination itself, so it survives the upstream hop too (RP-Initiated Logout 1.0 s2).
	return logoutRequest{hint: hint, destination: withState(resolveDestination(ctx, rmngCtx, q, hint), q["state"])}, nil
}

// resolveDestination exact-matches post_logout_redirect_uri against the client's registered
// list, like redirect_uri and for the same reason: an unvalidated forward is an open redirect
// wearing the issuer's hostname. Anything unvalidated falls back to the issuer's own page and
// never to an error -- refusing to SIGN OUT over a cosmetic problem would leave the session alive.
func resolveDestination(ctx context.Context, rmngCtx *rmngctx.RmngContext, q map[string]string, hint *auth.IDTokenHint) string {
	want, clientID := q["post_logout_redirect_uri"], q["client_id"]
	if hint != nil {
		// RP-Initiated Logout 1.0 §2: client_id must match the hint's audience, and the hint may stand in for an absent one.
		if clientID == "" && len(hint.Audience) == 1 {
			clientID = hint.Audience[0]
		} else if clientID != "" && !slices.Contains(hint.Audience, clientID) {
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

// logoutPostAsGet answers a form POST with a 303 to the same RP-Initiated Logout 1.0 s2 parameters as a GET, ending nothing itself: RFC 6265bis s5.6.7.1 withholds a SameSite=Lax cookie from a cross-site POST but sends it on the top-level GET that follows.
func logoutPostAsGet(request events.APIGatewayProxyRequest) events.APIGatewayProxyResponse {
	if !rmngrequest.IsFormRequest(request) {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "The body must be application/x-www-form-urlencoded.")
	}
	params := url.Values{}
	for _, name := range logoutParams {
		if values := rmngrequest.FormValues(request, name); len(values) > 0 {
			params.Set(name, values[0])
		}
	}
	resp := redirect("?"+params.Encode(), "")
	resp.StatusCode = http.StatusSeeOther
	return resp
}

// logoutQuery keeps only the s2 parameters of a GET's query.
func logoutQuery(q map[string]string) url.Values {
	params := url.Values{}
	for _, name := range logoutParams {
		if v := q[name]; v != "" {
			params.Set(name, v)
		}
	}
	return params
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
		// Nowhere validated to send them, so our own sign-out page; bare, so it cannot hop back here, and relative from /oauth2/logout.
		location = session.SignOutPage
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

// redirectWithCookies is redirect() for a response that sets any number of cookies.
func redirectWithCookies(location string, setCookies []string) events.APIGatewayProxyResponse {
	return withCookies(redirect(location, ""), setCookies)
}

// withCookies sets each cookie on resp: Headers carries exactly one Set-Cookie and would silently drop a second (the memo beside the cleared session, on the upstream hop), so two or more go in MultiValueHeaders, and never in both maps.
func withCookies(resp events.APIGatewayProxyResponse, setCookies []string) events.APIGatewayProxyResponse {
	switch {
	case len(setCookies) == 1:
		resp.Headers["Set-Cookie"] = setCookies[0]
	case len(setCookies) > 1:
		resp.MultiValueHeaders = map[string][]string{"Set-Cookie": setCookies}
	}
	return resp
}
