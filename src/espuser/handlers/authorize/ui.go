// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

// providerLogoDataURI is the logo drawn on every provider button. It is a data URI so the
// page stays self-contained under a CSP that allows no remote origin. One image serves all
// providers until the registry carries a per-provider logo.
const providerLogoDataURI = "data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCIgZmlsbD0ibm9uZSIgc3Ryb2tlPSIjMjMyZjNlIiBzdHJva2Utd2lkdGg9IjEuNiIgc3Ryb2tlLWxpbmVjYXA9InJvdW5kIiBzdHJva2UtbGluZWpvaW49InJvdW5kIj48cGF0aCBkPSJNMTIgMkw0IDZ2NmMwIDUgMy40IDguNCA4IDEwIDQuNi0xLjYgOC01IDgtMTBWNnoiLz48cGF0aCBkPSJNOSAxMmwyIDIgNC00Ii8+PC9zdmc+"

// providerChooserHTML is one provider button: %s = href, %s = logo data URI, %s = display name.
const providerChooserHTML = `    <a class="provider" href="%s"><img src="%s" alt="" width="20" height="20">%s</a>
`

// loginPageHTML is the login UI: a button per federated provider, plus the passwordless
// email/phone -> /v1/auth/otp/initiate -> code -> /v1/auth/otp/verify form, which navigates to the
// redirect_to it returns. %s order: the CSP nonce, the provider buttons block, the injected flow id.
const loginPageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sign in</title>
<style>
  body { font-family: system-ui, sans-serif; max-width: 22rem; margin: 4rem auto; padding: 0 1rem; }
  h1 { font-size: 1.25rem; }
  input, button { width: 100%%; padding: .6rem; margin: .35rem 0; font-size: 1rem; box-sizing: border-box; }
  button { cursor: pointer; }
  .hidden { display: none; }
  .err { color: #b00020; min-height: 1.2rem; font-size: .9rem; }
  .muted { color: #666; font-size: .85rem; }
  .provider { display: flex; align-items: center; justify-content: center; gap: .5rem;
              padding: .6rem; margin: .35rem 0; border: 1px solid #ccc; border-radius: .25rem;
              color: inherit; text-decoration: none; font-size: 1rem; }
  .provider:hover { background: #f4f4f4; }
  .sep { display: flex; align-items: center; gap: .5rem; color: #888; font-size: .8rem; margin: 1rem 0 .25rem; }
  .sep::before, .sep::after { content: ""; flex: 1; border-top: 1px solid #ddd; }
</style>
</head>
<body>
  <h1>Sign in</h1>
%s  <form id="identifier-form">
    <label for="username">Email or phone</label>
    <input id="username" name="username" type="text" autocomplete="username" autofocus required>
    <button type="submit">Continue</button>
  </form>
  <form id="otp-form" class="hidden">
    <p class="muted">Enter the 6-digit code we sent you.</p>
    <input id="code" name="code" inputmode="numeric" pattern="[0-9]*" maxlength="6" autocomplete="one-time-code" required>
    <button type="submit">Verify</button>
  </form>
  <p id="error" class="err"></p>
<script nonce="%s">
(function () {
  // Injected server-side from the HttpOnly flow cookie (not readable via document.cookie).
  var flowId = %s;
  var idForm = document.getElementById('identifier-form');
  var otpForm = document.getElementById('otp-form');
  var errEl = document.getElementById('error');

  // The page is served at <base>/oauth2/login (base = the API Gateway stage, e.g. /prod, or
  // empty on a custom domain). Prefix API calls with that base so a stage prefix is preserved.
  var base = window.location.pathname.replace(/\/oauth2\/login\/?$/, '');

  function fail(msg) { errEl.textContent = msg || 'Something went wrong. Please try again.'; }

  if (!flowId) { fail('Your session has expired. Please start again.'); idForm.classList.add('hidden'); }

  idForm.addEventListener('submit', function (e) {
    e.preventDefault(); errEl.textContent = '';
    var username = document.getElementById('username').value.trim();
    fetch(base + '/v1/auth/otp/initiate', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ flow_id: flowId, username: username })
    }).then(function (r) {
      if (!r.ok) throw new Error();
      idForm.classList.add('hidden'); otpForm.classList.remove('hidden');
      document.getElementById('code').focus();
    }).catch(function () { fail('Could not send a code. Check the address and try again.'); });
  });

  otpForm.addEventListener('submit', function (e) {
    e.preventDefault(); errEl.textContent = '';
    var code = document.getElementById('code').value.trim();
    fetch(base + '/v1/auth/otp/verify', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ flow_id: flowId, otp: code })
    }).then(function (r) {
      if (!r.ok) throw new Error();
      return r.json();
    }).then(function (data) {
      if (data && data.redirect_to) { window.location.href = data.redirect_to; }
      else { fail(); }
    }).catch(function () { fail('Invalid or expired code.'); });
  });
})();
</script>
</body>
</html>`

// errorPageHTML is a non-leaking terminal error (%s = code, %s = description).
const errorPageHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Sign-in error</title>
<style>body{font-family:system-ui,sans-serif;max-width:22rem;margin:4rem auto;padding:0 1rem}h1{font-size:1.15rem}.muted{color:#666}</style>
</head>
<body>
  <h1>We couldn't sign you in</h1>
  <p class="muted">%s: %s</p>
</body>
</html>`
