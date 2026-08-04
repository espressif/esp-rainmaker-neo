# CustomIcon

Brand/product marks for third-party integrations the dashboard talks to (Alexa, Google Assistant, Matter, APNS, GCM, …). Looked up by a kebab-case id through a static registry, so an unknown id is a compile error rather than a blank space.

```tsx
import { CustomIcon } from "@/components/custom-icon";

<CustomIcon type="amazon-alexa" size={24} />
```

## Provenance

Vendored from `@espressif/dashboard-ui-components` — branch `main`, v0.10.0, commit `31c824b` — where it lived at `src/components/custom-icon/`. The library removed it in 0.10.1 rather than keep redistributing third-party marks inside a published npm package, so the app owns this copy now; it will not come back from the package.

The only change from the original is the `cn` import, repointed to `@/utils/utils`.

The marks belong to their respective owners and appear here solely to identify the integration they label. They are not Espressif marks. Anything added to `./icons/` inherits that constraint — keep it to marks we're entitled to display for identification, and don't restyle a brand's colours.

## Adding an icon

1. New `.tsx` in `./icons/`, inlining the SVG. Match the existing signature exactly — the registry depends on it:

   ```tsx
   export function FooIcon({ size = 18 }: { size?: number }) {
     return <svg width={size} height={size} viewBox="…" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden>…</svg>
   }
   ```

2. Export it from [`./icons/index.ts`](./icons/index.ts).
3. Add a registry entry in [`./custom-icon.config.ts`](./custom-icon.config.ts). `CustomIconId` derives from the registry keys, so that's all the wiring.

## Colour

Three groups, and they behave differently when the theme flips:

- **Brand hex** (most icons) — fixed by the brand. Renders identically in light and dark; leave it alone.
- **`currentColor`** — `matter`, `apple`. Inherit the surrounding text colour.
- **`var(--color-primary)`** — `security` only. The token comes from the library's `colors.css`, pulled in by [`src/styles/globals.css`](../../styles/globals.css); it renders transparent if that import ever goes away.

## Not vendored

The library also kept 10 `.svg` files under `src/assets/custom-icons/` — the source art the `.tsx` files were hand-ported from, imported by nothing (and absent for `oauth`, `security`, `wechat`). Left behind rather than carried in as dead weight. Still recoverable if a mark ever needs re-porting:

```bash
git -C <esp-ui-component-library> show main:src/assets/custom-icons/aws.svg
```
