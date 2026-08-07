import fs from 'node:fs'
import path from 'node:path'
import type { HtmlTagDescriptor, Plugin } from 'vite'
import appConfig from '../app.config.ts'
import urlParams from '../src/config/url-params.config.json' with { type: 'json' }

/** Vite escapes tag attributes itself, but not element text (`children`). */
function escapeText(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

/**
 * Node-side mirror of `resolveAssetPath` (src/lib/asset-resolver.ts), which
 * can't be reused here because it relies on `import.meta.glob`.
 *
 * Returns a root-relative `/src/...` URL; Vite serves that verbatim in dev and
 * rewrites it to a content-hashed `/assets/<name>-<hash>.<ext>` at build. Throws
 * on a bad path so a typo in app.config.ts fails the build rather than shipping
 * a broken icon.
 */
function resolveConfigAsset(root: string, assetPath: string): string {
  if (/^https?:\/\//.test(assetPath)) {
    return assetPath
  }

  const normalized = assetPath.replace(/^\/+/, '').replace(/^src\//, '')
  const candidates = [
    ...new Set([`src/${normalized}`, `src/assets/${normalized.replace(/^assets\//, '')}`]),
  ]

  for (const candidate of candidates) {
    if (fs.existsSync(path.resolve(root, candidate))) {
      return `/${candidate}`
    }
  }

  throw new Error(
    `[app-head] asset from app.config.ts not found: "${assetPath}" (tried ${candidates.join(', ')})`,
  )
}

/**
 * Parser-blocking bootstrap that resolves the theme before first paint.
 *
 * Mirrors the precedence the app settles on at runtime: URL param (applied in
 * __root.tsx) > persisted zustand state > OS preference. Without this the
 * stylesheet paints the light background for the whole SPA boot, because the
 * `dark` variant is class-only with no media fallback.
 *
 * The OS-preference fallback mirrors `defaults.darkMode` in app.config.ts. That
 * field can't be read here — it evaluates to `false` in Node, where there is no
 * `window` — so if it ever becomes a hardcoded literal, update this to match.
 *
 * Kept dependency-free and wrapped in try/catch: a throw here would block
 * parsing of the rest of the document.
 */
function themeBootstrapScript(storageKey: string, darkParam: string): string {
  return `(function(){try{
var p=new URLSearchParams(location.search).get(${JSON.stringify(darkParam)}),d;
if(p!==null){d=p==="true"}else{var s=localStorage.getItem(${JSON.stringify(storageKey)});if(s){d=JSON.parse(s).state.darkMode}}
if(d===undefined){d=matchMedia("(prefers-color-scheme: dark)").matches}
if(d){document.documentElement.classList.add("dark")}
document.documentElement.style.colorScheme=d?"dark":"light";
}catch(e){}})();`
}

/**
 * Writes the document head into index.html from app.config.ts, so the served
 * HTML is already branded and correctly themed with no post-mount flash.
 *
 * `order: 'pre'` is load-bearing: pre-hooks run before vite:build-html walks the
 * document, so the `/src/...` hrefs below go through Vite's own asset pipeline
 * and come out content-hashed.
 */
export function appHead(): Plugin {
  let root = process.cwd()
  let warn: (msg: string) => void = console.warn

  return {
    name: 'app-head',

    configResolved(config) {
      root = config.root
      warn = (msg) => config.logger.warn(msg)
    },

    transformIndexHtml: {
      order: 'pre',
      handler(html) {
        const icons: { ico?: string; svg?: string } =
          typeof appConfig.favicon === 'string' ? { ico: appConfig.favicon } : appConfig.favicon

        // The type union already rules this out; the guard covers a config
        // authored in plain JS, where a page would otherwise ship no icon at
        // all and fall back to the implicit /favicon.ico request.
        if (!icons.ico && !icons.svg) {
          throw new Error(
            '[app-head] app.config.ts `favicon` must set at least one of `ico` or `svg`',
          )
        }
        if (icons.svg && !icons.ico) {
          warn(
            '[app-head] `favicon.svg` is set without `favicon.ico`. Safari ignores SVG icons ' +
              'and will fall back to the implicit /favicon.ico request, which CloudFront ' +
              'answers with index.html.',
          )
        }

        const tags: HtmlTagDescriptor[] = [
          {
            tag: 'script',
            children: themeBootstrapScript(
              `${appConfig.storagePrefix}-app-storage`,
              urlParams.DARK_MODE,
            ),
            injectTo: 'head',
          },
          { tag: 'title', children: escapeText(appConfig.title), injectTo: 'head' },
          {
            // Admin console on an open CloudFront domain that answers every
            // path with a 200 — keep it out of search indexes.
            tag: 'meta',
            attrs: { name: 'robots', content: 'noindex, nofollow' },
            injectTo: 'head',
          },
          {
            tag: 'meta',
            attrs: { name: 'color-scheme', content: 'light dark' },
            injectTo: 'head',
          },
        ]

        // Safari ignores SVG icons, so the .ico stays as the fallback.
        if (icons.ico) {
          tags.push({
            tag: 'link',
            attrs: { rel: 'icon', sizes: '32x32', href: resolveConfigAsset(root, icons.ico) },
            injectTo: 'head',
          })
        }
        if (icons.svg) {
          tags.push({
            tag: 'link',
            attrs: {
              rel: 'icon',
              type: 'image/svg+xml',
              href: resolveConfigAsset(root, icons.svg),
            },
            injectTo: 'head',
          })
        }

        if (appConfig.description) {
          tags.push({
            tag: 'meta',
            attrs: { name: 'description', content: appConfig.description },
            injectTo: 'head',
          })
        }

        if (appConfig.themeColor) {
          tags.push(
            {
              tag: 'meta',
              attrs: {
                name: 'theme-color',
                media: '(prefers-color-scheme: light)',
                content: appConfig.themeColor.light,
              },
              injectTo: 'head',
            },
            {
              tag: 'meta',
              attrs: {
                name: 'theme-color',
                media: '(prefers-color-scheme: dark)',
                content: appConfig.themeColor.dark,
              },
              injectTo: 'head',
            },
          )
        }

        // `lang` is an attribute on <html>, so it can't be a tag descriptor.
        return {
          html: html.replace(/(<html\b[^>]*\blang=")[^"]*(")/, `$1${appConfig.defaults.language}$2`),
          tags,
        }
      },
    },
  }
}
