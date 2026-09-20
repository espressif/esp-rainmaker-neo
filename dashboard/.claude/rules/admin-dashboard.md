---
description: Admin Dashboard V2 — React/TypeScript frontend standards and conventions
globs:
  - dashboard/**/*.ts
  - dashboard/**/*.tsx
  - dashboard/**/*.scss
alwaysApply: true
---

# Admin Dashboard V2

## Project Overview
A React admin dashboard built with TanStack Start (SPA mode), using shadcn/ui components, Zustand for UI state, TanStack Query for backend data, and i18next for internationalization.

## Project Structure

### Components Organization
- `src/components/` — Reusable application-specific components used across many pages within the app
- `src/components/error/` — Error-related components
- `src/containers/` — Components that do some processing and render children (e.g. layout components)
- Generic, pure, props-only components should be used from the `@espressif/dashboard-ui-components` library. If a needed component is not available there, temporarily create it in `src/components/ui`, then propose adding it to `@espressif/dashboard-ui-components`. Components in `src/components/ui` must be props-based pure components only.

### Other Directories
- `src/api/` — Per-domain API modules (SDK/HTTP calls + TanStack Query options and mutation hooks)
- `src/pages/` — Page components organized by route
- `src/config/` — Configuration files (routes, sidebar, app config)
- `src/stores/` — Zustand stores for UI/client-only state and persisted server-derived state
- `src/hooks/` — Custom React hooks
- `src/i18n/` — i18next translation files organized by locale and namespace
- `src/lib/` — Utility functions and shared libraries
- `src/aws/`
  - `src/aws/components/` — Pure reusable components that directly interact with AWS services and render UI
  - `src/aws/services/` — Functions that directly interact with the AWS SDK

## Code Style & Conventions

### TypeScript
- Always use TypeScript with strict type checking
- Define props interfaces in separate `.props.ts` files
- Use explicit types; avoid `any` unless absolutely necessary
- Export types/interfaces explicitly

### Component Structure
- Use functional components with TypeScript
- Place components in folders: `component-name/component-name.tsx` and `component-name/component-name.props.ts`
- Use default exports for components
- Use named exports for types/interfaces
- One component per file
- Don't create useless abstractions; always go for the simplest solution

### Styling
- Use Tailwind CSS for styling
- Use SCSS modules (`.module.scss`) for component-specific styles when needed
- Use CSS variables prefixed with `--espd-` for global theme values
- Prefer Tailwind utility classes over custom CSS when possible
- Use the `cn()` utility from `@/lib/utils` for conditional class names
- Keep a mobile-first approach; ensure components render properly on smaller screens as well

### State Management
- **Zustand**: Use for UI/client-only state (sidebar collapsed, dark mode, language) and persisted server-derived state (e.g. user profile)
- **TanStack Query**: Use for **all** backend communication — both reads (`useQuery`/`useSuspenseQuery`) and writes (`useMutation`). Never call SDK or API functions directly from components; always go through TanStack Query hooks.
- Store Zustand stores in `src/stores/` with descriptive names (e.g. `app.store.ts`, `user.store.ts`)
- Use the `persist` middleware for Zustand stores that need localStorage persistence
- For local component state, do not use `useState` for more than 2 state variables. In such cases use the `useSetState` hook from the `react-use` package.

### TanStack Query Conventions
- Each API module (`src/api/<domain>/`) must have a `<domain>.queries.ts` file that exports query options and mutation hooks
- **Query key factories**: Use a `<domain>Keys` object (e.g. `profileKeys`) with `all`, `detail()`, `list()`, etc.
- **Query options factories**: Use a `<domain>Queries` object with `queryOptions()` for each query
- **Mutation hooks**: Export named `use<Action>` functions (e.g. `useLogin`, `useSignup`, `useUpdateProfile`)
- Mutation hooks should type all three generics: `useMutation<TData, TError, TVariables>`
- Components should derive loading/error state from the mutation/query return values (`isPending`, `error`, `data`) — do **not** use manual `useState` for API loading/error tracking
- Pass `onSuccess`/`onError` callbacks at the call site (`mutate(data, { onSuccess })`) for navigation and side effects specific to that call
- Re-export all hooks from the domain's `index.ts` and from `src/api/index.ts`

### Internationalization (i18n)

- Use i18next for translations, via the `useTranslation('namespace')` hook.
- Translation files live at `src/i18n/locales/{locale}/{namespace}.json` and are registered in `src/i18n/config.ts`.
- Support both `en` and `zh`. The two files of a namespace must hold **exactly** the same key set.
- Always provide a fallback: `t('key', 'Fallback text')`. Never `t('key')`, and never `t('key', { count })` without a `defaultValue` — the fallback is what renders before a translation exists.
- Never use hardcoded user-facing labels — buttons, titles, placeholders, aria-labels, toasts, table headers and zod validation messages all go through `t()`.
- Never leave an unused key behind. Deleting UI means deleting its keys.

#### One namespace per sidebar page or primary route

The namespace set is closed. `npm run check:i18n` fails on any file that is not in this table, and on any entry in the table that has no file.

| Namespace | Owning route |
|---|---|
| `common` | shared across routes (see below) |
| `login`, `forgot-password`, `set-password`, `oauth-preview`, `static` | the matching primary route |
| `nodes`, `node-groups`, `register`, `generate` | `/home/node-management/<page>` |
| `ota-images`, `ota-jobs` | `/home/ota/<page>` |
| `voice-assistants`, `push-notifications`, `post-deployment` | `/home/settings/<page>` |
| `account-settings` | `/home/account-settings` |

Sub-routes and tabs nest **inside** their page's file rather than getting one of their own: `/home/node-management/register/new` keys live in `register.json` under `"new": { … }`, and the Alexa/GVA tabs share `voice-assistants.json` under `"alexa": { … }` / `"gva": { … }`.

Adding a sidebar page means adding its namespace file (both locales), registering it in `src/i18n/config.ts`, and adding a row to `NAMESPACE_OWNERS` in `scripts/check-i18n.mjs`.

**Exception:** `/error`, `/logout` and `/goodbye` keep their handful of keys in `common.json`. They share `errorTitle` / `errorMessage` with `/login`, so separate files would duplicate rather than separate.

#### What belongs in `common.json`

A component under `src/components/`, `src/config/`, `src/lib/` or `src/aws/components/` that is used from **more than one route namespace** puts its keys in `common.json` and binds to `useTranslation('common')`. A shared component used by exactly **one** route keeps its keys in that route's namespace and binds to it explicitly.

Also in `common.json`: UI chrome repeated across pages (`actions.cancel`, `actions.delete`, `actions.retry`, `actions.tryAgain`, …) and neutral table headers (`columns.status`, `columns.name`, `columns.nameId`, `columns.createdAt`, …). Reuse these rather than redefining `"Cancel"` in a route namespace.

**Do not** promote domain-loaded words — `Type`, `Node`, `Group`, `Overview`, `Tags`, `Model`, `Version`. One English word maps to several Chinese terms depending on context, so these stay in the namespace that gives them meaning.

#### Keys carried in config objects

A key stored in a config object is read back through a **variable** — `t(i18nKey)` — so neither TypeScript nor a reviewer can see which namespace it needs. If the stored value is bare, it resolves for consumers bound to the right namespace and renders as the **raw key string** (`otaJobStatus.IN_PROGRESS`) for everyone else.

So: any `*Key` string in `src/config/`, `src/components/`, `src/lib/` or `src/aws/` must be **fully qualified**, and every `t(variableKey)` call must pass a fallback.

```ts
// config
{ id: "eq", label: "=", descriptionKey: "common:searchOperators.eq", description: "equals" }
{ Icon: CheckCircle2, color: "success", i18nKey: "common:nodeStatus.online", labelFallback: "Online" }

// consumer — never `t(i18nKey)` on its own
{t(i18nKey, labelFallback)}
```

`check:i18n` enforces the qualification; the fallback is on you, because a variable key is invisible to the fallback rule.

The `{ labelKey, fallback }` pair is the standard shape — see `src/config/sidebar/*.config.ts`, `src/config/password-policy.config.ts` and `advancedSearchFieldsData`. Zod schemas follow the same idea via a `getXSchemaMessages(t)` builder (`src/api/auth/auth.schemas.ts`).

#### The gate

`npm run check:i18n` (also chained from `npm run lint`) fails on: an unknown namespace file, an `en`/`zh` key-set mismatch, a key no source file references, a referenced key that does not exist, a literal `t()` call with no fallback, and a bare (unqualified) `*Key` value in a shared module. Run it before opening an MR.

### Routing
- Routes are configured in `src/config/app-routes.config.ts`
- Page components live in `src/pages/`, matching the route structure
- Use TanStack Router for navigation
- Use the `useNavigate()` and `useLocation()` hooks from `@tanstack/react-router`

### Icons
- Use `lucide-react` for icons
- Import icons as: `import { IconName } from "lucide-react"`
- Use consistent icon sizes (typically `h-4 w-4` or `h-5 w-5`)

### SVG Assets
- Import SVGs as URLs: `import logoUrl from "@/assets/img/logo.svg?url"`
- Use the `?url` suffix for URL imports
- Place SVG assets in `src/assets/img/`

## Best Practices

### Component Design
- Keep components small and focused. A component should not exceed 300 lines.
- Extract reusable logic into custom hooks
- Use composition over configuration
- Prefer props over context when possible
- Use `React.memo()` for expensive components when needed
- Always prefer early returns over ternary operators. Ternaries (especially complex nested ones) make code hard to read.
- One React component per file (even for small components)
- Do **not** use `let body: ReactNode` (or any `let`-assigned JSX) with `if / else if / else` chains to pick what to render. This hides branching inside the component body and makes the JSX return misleading. Instead, extract a dedicated child component (e.g. `*-main-content.tsx` under `_components/`) that receives all required data as props and uses **early returns** for each branch (loading, error, empty, success). The parent renders this child once in its JSX. This keeps each render branch isolated, typed, and easy to read.
- All buttons must have a start icon unless specified otherwise
- Destructive buttons like "Delete" must always show a confirmation modal

### Event Handlers and JSX Callbacks
- Do not pack non-trivial logic into inline arrow functions in JSX (e.g. multi-branch `setValue`, loops, or several sequential side effects). Extract a named function in the same file, a `*.helpers.ts` / `*.utils.ts` module next to the component, or a `useCallback` with a stable body that delegates to a named helper.
- One-liner callbacks (single call, trivial mapping) are fine inline.

### Performance
- Use `React.lazy()` for code splitting (already configured in routes)
- Use Suspense boundaries for loading states
- Memoize expensive computations with `useMemo()`
- Use `useCallback()` for event handlers passed to child components

### Error Handling
- Use `ErrorBoundary` for component error boundaries
- Handle errors gracefully with user-friendly messages
- Use i18next for error messages

### Accessibility
- Use semantic HTML elements
- Include ARIA labels where appropriate
- Ensure keyboard navigation works
- Use the `sr-only` class for screen-reader-only text

## File Naming Conventions
- Components: `kebab-case` for folders and files (e.g. `my-account-menu/my-account-menu.tsx`)
- Props files: `component-name.props.ts`
- Config files: `kebab-case.config.ts` or `kebab-case.config.tsx`
- Store files: `kebab-case.store.ts`
- Hook files: `use-kebab-case.tsx`

## Import Paths
- Use the `@/` alias for the `src/` directory
- Example: `import { Button } from "@/components/ui/button"`

## Code Quality

### General
- Avoid deeply nested code (if / loops / callbacks > 3 levels). Extract functions instead.
- Prefer guard clauses / early returns.
- Functions should do one thing only.
- A function body should generally not exceed ~40 lines.
- Avoid magic strings/numbers. Extract constants into `constants.ts`.
- Avoid duplicate logic. Extract helpers/hooks.
- Do not comment obvious code. Write self-explanatory code.
- Add comments only for non-obvious business rules or technical constraints.

### Variables
- Prefer `const` over `let`
- Never use `var`
- Avoid reassignment where possible
- Use descriptive variable names; avoid `data`, `item`, `temp`, `res`

```ts
// Bad
const d = data?.x;
// Good
const userDevices = data?.devices;
```

## Promise Safety

`eslint.config.js` enables type-aware promise rules (`no-floating-promises`,
`no-misused-promises`). `npm run lint` runs at `--max-warnings 0`, so these gate
CI. Four conventions cover every case in the app — follow them and the rules
never fire.

**1. `navigate()` is fire-and-forget — mark it `void`.** TanStack's `navigate`
returns a promise that resolves when the transition settles; nothing awaits it.

```tsx
void navigate({ to: "/home/ota/jobs" });                       // statement
onRowClick={(row) => void navigate({ to: "...", params })}     // concise arrow body
```

**2. `invalidateQueries` in a mutation's `onSuccess` — `return` it.** Returning
the promise keeps the mutation `isPending` until the refetch settles, so submit
buttons stay in their loading state until the list actually shows fresh data.
This removes the stale-data flash; do not `void` it.

```ts
onSuccess: () => {
  return queryClient.invalidateQueries({ queryKey: otaJobsKeys.all });
},
```

Exception: a fire-and-forget invalidation (inside a `setTimeout`, or any
void-returning callback) takes `void` — returning a promise there would just
trade one rule violation for the other.

**3. A form's `submit` must return `void`, not a promise.** `handleSubmit(fn)`
returns `(e?) => Promise<void>`; passing it straight to `<form onSubmit>` drops
any rejection escaping `fn`. Wrap with
[`voidFormSubmit`](../../src/lib/void-form-submit.ts) — inside the `use-*-form`
hook when there is one, so the form component needs no wrapper of its own:

```ts
const submit = voidFormSubmit(form.handleSubmit((values) => { /* ... */ }));
```

For a form nested inside another form's React tree, use
`isolateNestedFormSubmit` instead — it voids the promise *and* stops the event
reaching the ancestor form.

**4. Async handlers in `void`-typed props get wrapped at the call site.** Never
widen a library prop to accept `Promise<void>` — that pushes the unhandled
rejection into the component.

```tsx
onClick={() => void handleCopyJson()}                    // bare async reference
onFilesChange={(files) => void handleFileChange(files)}  // keep the arguments
```

### Route params

Routes are built at runtime from `routesConfig`, so `useParams` cannot be typed
from a static route tree. Use
[`useRouteParams`](../../src/lib/navigation/use-route-params.ts) — never
`useParams({ strict: false }) as { … }` in a page:

```ts
const { groupName } = useRouteParams<{ groupName?: string }>();
```

## React Hooks Rules
- Always follow the Rules of Hooks
- Do not call hooks conditionally
- Custom hooks should encapsulate business logic, not UI rendering
- Prefer extracting reusable state logic into hooks before creating contexts
- Split hooks longer than 150 lines

### `useEffect` Rules
- Avoid `useEffect` unless necessary
- Never use `useEffect` to derive state from props/data — prefer `useMemo`
- Every effect must have a clear purpose: subscriptions, DOM sync, timers, external systems

```tsx
// Bad
useEffect(() => {
  setFiltered(data.filter(/* ... */));
}, [data]);

// Good
const filteredData = useMemo(() => data.filter(/* ... */), [data]);
```

## Forms
- Use `react-hook-form` for all forms
- Never use an individual `useState` per input
- Use schema validation with `zod`
- Validation schemas belong in dedicated files
- Form submission/loading/error state should come from TanStack mutation state
- Avoid manual form state tracking

### Form / Data-Display Container Separation
- A form component (e.g. `*-form.tsx`) MUST be self-contained and decoupled from any data-display container (`Sheet`, `Dialog`, `Drawer`, `Modal`, `Popover`, full page layout, etc.). The same form should be droppable into a sheet, a dialog, or rendered directly inside a page without modification.
- The form owns:
  - `useForm` setup, schema, and default values
  - All related TanStack mutations (create/update/delete) and their loading/error state
  - Field rendering (via a dedicated `*-form-fields.tsx` sub-component when needed)
  - Action buttons (submit, cancel, delete) and their disabled/loading wiring
  - Its own error alert / inline messages
- The form MUST NOT:
  - Import or render `Sheet`, `Dialog`, `Drawer`, `Modal`, `TableRowDetailSheet`, page section cards, or other container components
  - Own the title/header of the container it happens to be rendered in
  - Call `onOpenChange`-style container callbacks directly; instead it exposes semantic callbacks (`onCancel`, `onSuccess`, optional `onDelete`) that the container maps to its own lifecycle
- The container wrapper (e.g. `*-form-sheet.tsx`, `*-form-dialog.tsx`) is a thin component that:
  - Imports the container primitive (`Sheet`, `Dialog`, etc.) and the form component
  - Computes the container title/header
  - Renders `<FormComponent onCancel={close} onSuccess={close} ... />` inside the container
  - Adds no business logic beyond container concerns (open/close, title, sizing)
- Wrapper file names should reflect the container, not the form contents: `platform-form-sheet.tsx`, `platform-form-dialog.tsx`, `platform-form-page.tsx`, etc. Each lives in its own folder alongside its `.props.ts`.

### Structure

```text
platform-form/
    platform-form.tsx          # standalone form (state + mutations + actions)
    platform-form.props.ts
    platform-form.schema.ts
    _components/               # form-internal sub-components (fields, action buttons, etc.)

platform-form-sheet/           # thin container wrapper
    platform-form-sheet.tsx
    platform-form-sheet.props.ts
```

## Tables & Lists
- Lists with more than 15 items should support pagination, virtualization, or infinite loading
- Always provide loading, empty, and error states
- Tables should support sorting if the backend permits
- Avoid rendering large arrays directly

## Loading States
- Every async view must support: loading, error, empty, and success
- Avoid blank screens
- Prefer skeletons over spinners for content loading

## Error Handling Standards
- Never silently swallow errors

```ts
// Bad
catch (e) {}

// Good
catch (error) {
  logger.error(error);
  toast.error(/* ... */);
}
```

- Create a reusable error normalization utility, e.g. `normalizeApiError(error)`
- User-facing errors should be translated

## Logging
- Never use `console.log`
- Remove debugging logs before merge
- Use the centralized logger utility, e.g. `logger.info()`, `logger.error()`
- Logging must not expose tokens, passwords, IDs, or sensitive data

## Performance Guardrails
- Avoid unnecessary rerenders
- Memoize derived objects passed as props

```tsx
// Bad
<Component config={{ a: 1 }} />
// Good
const config = useMemo(() => ({ a: 1 }), []);
<Component config={config} />
```

- Avoid inline arrays/objects in JSX props
- Prefer virtualization for large tables
- Debounce expensive search/filter operations

## Query Standards
- Cancel queries when navigating away from the page

### Query Keys
- Always include identifiers:

```ts
// Bad
["users"]
// Good
["users", organizationId]
```

### Cache
- Set stale times intentionally; do not rely on defaults
- Invalidate affected queries after mutations
- Use optimistic updates only where the UX benefit is meaningful

## Security
- Never render backend HTML via `dangerouslySetInnerHTML` unless sanitized
- Never store tokens in localStorage if the SDK handles auth
- Do not expose internal backend errors to users
- Never hardcode secrets, URLs, or API keys — use `import.meta.env`

## File Organization
If a folder grows beyond ~8 files, split it:

```text
_components/
_hooks/
_utils/
_constants/
_types/
```

- Avoid dumping unrelated files into one directory.

## Accessibility Standards
- Buttons must have accessible labels
- Inputs require labels
- Interactive elements must be keyboard accessible
- Modals should trap focus
- Images require alt text

## Date Handling
- Use `date-fns`
- Always check for the date format pattern in `app.config.json`
- Never manually format dates
- Store dates in UTC; convert for display only

```ts
// Bad
new Date().toLocaleString();
// Good
format(date, "dd MMM yyyy");
```

## Testing (Future)
- Write unit tests for utilities and hooks
- Write integration tests for components
- Write e2e tests for critical user flows

## Dependencies
- **UI Framework**: TanStack Start (SPA mode)
- **UI Components**: `@espressif/dashboard-ui-components` (built on shadcn/ui / Radix UI primitives)
- **Styling**: Tailwind CSS + SCSS modules
- **State**: Zustand (UI state + persisted server state) + TanStack Query (server-state reactivity)
- **Routing**: TanStack Router
- **i18n**: i18next + react-i18next
- **Animations**: Framer Motion
- **Icons**: lucide-react

## Notes
- Theme color: `#0071E3`

## Planning (Implementation Plans)
When writing or updating an implementation plan for this project (including plans produced in Cursor), include a **File Changes** section near the top of the plan body (after the title/overview). That section must contain a **markdown table** listing every repository file the work touches.

Suggested columns:

| Column | Description |
|--------|-------------|
| **Name** | File name only (e.g. `node-groups.api.ts`) |
| **Path** | Repo-relative path from the project root (e.g. `src/api/node-groups/node-groups.api.ts`) |
| **Action** | One of: `create`, `modify`, `delete` |
| **Notes** | Optional short note (purpose of change, or "—" if obvious) |

Update the **File Changes** table when the plan scope changes so it stays the single inventory of touched files.
