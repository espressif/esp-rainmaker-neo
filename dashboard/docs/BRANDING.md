# Branding the dashboard

Everything visual — name, logo, colours, favicon, sign-in screen — is driven from
[`app.config.ts`](../app.config.ts) and one optional stylesheet. No component needs editing.

This covers the *appearance* of the app. The Terms of Use and Privacy Policy served at
`/static/*` are a separate job with legal consequences: please consult your legal advisor and fill up accordingly.

## Checklist

Everything in steps 1–6 is in [`app.config.ts`](../app.config.ts).

1. Drop `projectName` — see [Logo](#logo). Leaving it set keeps the Espressif logo on every screen except `/static/*`.
2. Point `logo` and `favicon` at your own files under `src/assets/`.
3. Set `title` and `description` — both currently name ESP RainMaker.
4. Set `themeColor` to match your background in each colour scheme.
5. Replace `customAuth.onboardingHeading` and `onboardingLayoutBackgroundImage` — the heading is rendered large on the sign-in screen and still reads "ESP RainMaker NEO".
6. Change `storagePrefix` so your build doesn't share `localStorage` keys with another deployment on the same origin.
7. Add `src/styles/theme.css` **and import it after the library stylesheet** in `globals.css` — see [Colours](#colours). Creating the file alone does nothing; the import order is what makes it work.
8. Replace the `{{TOKEN}}` placeholders in the public legal documents under `src/pages/static/docs/`, with your own counsel. Unreplaced tokens render literally to your users.
9. Run the [verification](#verification) steps.

## `app.config.ts`

| Field | Effect |
|---|---|
| `title` | `<title>`, and the browser tab / PWA name |
| `description` | `<meta name="description">` |
| `themeColor` | `<meta name="theme-color">` per colour scheme — tints mobile browser chrome and the PWA title bar. Should match `--color-background` in each theme |
| `projectName` | Library logo preset key. **Omit when forking** — see below |
| `storagePrefix` | Namespace for `localStorage` keys |
| `defaults` | Initial sidebar state, dark mode and language before the user chooses |
| `logo` | `minimal` (icon) and `full` (with wordmark), each with a `light` and `dark` variant |
| `favicon` | A bare string is shorthand for `{ ico }`; otherwise `{ ico, svg }` with at least one set |
| `i18n.supportedLanguages` | Which locales appear in the language switcher |
| `hideFooter` | Hides the app footer |
| `oAuth` / `customAuth` | Auth mode, and the sign-in screen's copy and artwork |

`title`, `description`, `themeColor` and `favicon` are read at **build time** by
[`vite-plugins/app-head.ts`](../vite-plugins/app-head.ts), which generates the document
head. Changing them needs a rebuild — they are not runtime-swappable.

## Logo

`projectName` names a logo preset baked into `@espressif/dashboard-ui-components`. The type
only admits Espressif's own products, so **a fork has no valid value to put there.** Leaving
it out is the supported path, and TypeScript enforces it — setting `projectName: "acme"`
fails to compile.

| | `projectName` set | `projectName` omitted |
|---|---|---|
| Workspace (`/home/*`) | library preset | your `logo` assets |
| Sign-in / onboarding | library preset | your `logo` assets |
| Public static shell (`/static/*`) | **your `logo` assets** | your `logo` assets |
| Expired-session screen | library preset | your `logo` assets |

The static shell always shows your own mark — it is the public face of the deployment even
on a first-party build. Everything else treats the preset as the default and your assets as
the fallback. The rule lives in one place,
[`components/brand-logo`](../src/components/brand-logo/brand-logo.tsx).

Both variants are used: `full` in the expanded sidebar and on the sign-in panel, `minimal`
when the sidebar collapses and on mobile. Supply both, or the collapsed rail shows a
wordmark squeezed into an icon slot.

## Colours

The palette comes from design tokens in `@espressif/dashboard-ui-components`. Override them
in `src/styles/theme.css` rather than forking the library — every component reads these
variables through Tailwind utilities (`bg-primary`, `text-sidebar-foreground`, …), so one
file retints the whole app.

Create the file and import it **after** the library stylesheet in
[`src/styles/globals.css`](../src/styles/globals.css):

```css
@import "@espressif/dashboard-ui-components/styles";
@import "./theme.css";
```

Two rules govern that file, and both bite silently:

- **Import order is load-bearing.** The library's `.dark` block is unlayered and has
  identical specificity to yours, so only source order decides dark mode.
- **Every override needs a twin.** Change a token in `:root` without mirroring it in
  `.dark` and the app reverts to the library palette the moment the user flips the theme.

A minimal `src/styles/theme.css`. Override only what you need — anything you leave out
keeps the library's value:

```css
/*
 * Application colour theme.
 *
 * Overrides the design tokens from `@espressif/dashboard-ui-components`. Must be imported
 * after the library stylesheet, and every `:root` value needs a `.dark` twin.
 */

:root {
  /* Brand */
  --color-primary: #1b4dd8;
  --color-primary-light: #93b0ff;
  --color-primary-dark: #0a2a86;
  --color-primary-foreground: #ffffff;

  /* Base canvas — keep `themeColor.light` in app.config.ts equal to this */
  --color-background: #fafcff;
  --color-foreground: #121212;

  /* Sidebar */
  --color-sidebar: #dfdfdf;
  --color-sidebar-foreground: #333333;
  --color-sidebar-border: transparent;

  /* Status */
  --color-error: #b3000b;
  --color-error-foreground: #ffffff;
}

.dark {
  /* Brand */
  --color-primary: #a8c1ff;
  --color-primary-light: #d6e2ff;
  --color-primary-dark: #002a86;
  --color-primary-foreground: #00205e;

  /* Base canvas — matches `themeColor.dark` */
  --color-background: #030303;
  --color-foreground: #f2f2f2;

  /* Sidebar */
  --color-sidebar: #161616;
  --color-sidebar-foreground: #e6e6e6;
  --color-sidebar-border: transparent;

  /* Status — dark mode needs lighter fills on a dark canvas */
  --color-error: #ff978f;
  --color-error-foreground: #5a0000;
}
```

A token may reference another, which is useful where a dark-mode value has no fixed
counterpart:

```css
.dark {
  /* On a dark canvas the tooltip can no longer invert, so it borrows secondary. */
  --color-tooltip: var(--color-secondary);
  --color-tooltip-foreground: var(--color-secondary-foreground);
}
```

The full token list — brand, status, neutrals, surfaces, card, popover, border, sidebar and
header — is the library's own palette:

```bash
less node_modules/@espressif/dashboard-ui-components/src/styles/colors.css
```

**Leave these alone:**

- **Chart tokens** (`--color-chart-*`) — `--color-chart-primary` and `-secondary` are
  already declared as `var(--color-primary)` / `var(--color-secondary)`, so they follow your
  brand automatically. The categorical series colours are tuned for contrast against each
  other; retint them only deliberately.
- **Font stack and radius** (`--font-sans`, `--radius-*`) — Tailwind reads these at build
  time to generate the utility classes themselves, so they belong in an `@theme` block, not
  in a `:root` override where they would have no effect on the generated utilities.

Keep `themeColor` in `app.config.ts` in step with `--color-background`, or mobile browser
chrome will clash with the page.

## Sign-in screen

Under `customAuth`:

- `onboardingLayoutBackgroundImage` — artwork for the right-hand panel.
- `onboardingHeading` — supports text-effect tags: `<gradient-text>`, `<shimmer-text>`,
  `<highlight-text>`, `<glitch-text>`. Pass JSX instead of a string to skip parsing.

```ts
onboardingHeading: "Acme <gradient-text>Cloud</gradient-text> Console",
```

`allowSignups` and `allowKeepMeSignedIn` control the form itself. These apply only when
`oAuth` is `false`.

## Assets

Put files under `src/assets/` and reference them relative to it:

```ts
logo: {
  full: { light: { src: "assets/img/logo/acme.svg", width: 320, height: 48 }, dark: { … } },
},
```

[`resolveAssetPath`](../src/lib/asset-resolver.ts) resolves these through a Vite glob, so
they are hashed and bundled. `width` and `height` must match the real intrinsic size —
they are emitted on the `<img>` to reserve space and avoid layout shift. Absolute
`https://` URLs are passed through untouched and skip bundling.

SVG is preferred for logos: it stays crisp on any display and Vite inlines small files as
data URIs.

## Verification

```bash
npm run typecheck && npm run lint && npm run build
```

Then run the app and confirm, in **both** light and dark mode:

- Sidebar logo expanded *and* collapsed, plus the mobile top bar
- The sign-in screen, including its background and heading
- `/static/terms-of-use` — the shell that always uses your own mark
- Browser tab title and favicon
- No stray library colours after a theme toggle — the most common symptom of a missing
  `.dark` twin

Before shipping, don't forget the legal documents under `src/pages/static/docs/` — no
`{{TOKEN}}` may reach your users:

```bash
grep -rn '{{[A-Z_]*}}' src/pages/static/docs/
```
