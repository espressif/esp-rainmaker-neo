#!/usr/bin/env node
/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * i18n structure gate.
 *
 * Enforces the rules in `.cursor/rules/admin-dashboard.mdc`:
 *   1. namespace files map 1:1 to a sidebar page or primary route
 *   2. `en` and `zh` hold exactly the same key set
 *   3. every key in a locale file is referenced by the source
 *   4. every key referenced by the source exists in its namespace
 *   5. every literal `t("key", …)` call carries a string fallback
 *
 * Run with `npm run check:i18n`. Exits non-zero on any violation.
 */
import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const LOCALES = join(ROOT, "src/i18n/locales");
const SRC = join(ROOT, "src");
const LOCALE_CODES = ["en", "zh"];

/**
 * The only namespaces allowed to exist, each owned by one sidebar page or
 * primary route. `common` holds cross-route text only. `/error`, `/logout` and
 * `/goodbye` deliberately share `common`: their handful of strings are also used
 * by `/login`, so dedicated files would duplicate rather than separate.
 */
const NAMESPACE_OWNERS = {
  common: "shared across routes",
  login: "/login",
  "forgot-password": "/forgot-password",
  "set-password": "/set-password",
  "oauth-preview": "/oauth-preview",
  static: "/static",
  nodes: "/home/node-management/nodes",
  "node-groups": "/home/node-management/node-groups",
  register: "/home/node-management/register",
  generate: "/home/node-management/generate",
  "ota-images": "/home/ota/images",
  "ota-jobs": "/home/ota/jobs",
  "voice-assistants": "/home/settings/voice-assistants",
  "push-notifications": "/home/settings/push-notifications",
  "post-deployment": "/home/settings/post-deployment",
  "account-settings": "/home/account-settings",
};

/** Fields whose string value is an i18n key. `statusKey` is NOT one — it carries a raw AWS status. */
const KEY_FIELDS =
  "labelKey|titleKey|placeholderKey|noteKey|descriptionKey|emptyHeadingKey|emptyDescriptionKey|bodyKey|primaryKey|badgeKey|i18nKey";
const KEY_FIELD_RE = new RegExp(`(?:${KEY_FIELDS})\\s*[:=]\\s*(["'])([^"'\`]+)\\1`, "g");
const T_CALL_RE = /\bt(?:\?\.)?\(\s*(["'`])([^"'`]+)\1\s*(,?)([^)]*)/g;
const USE_TR_RE = /useTranslation\(\s*(\[[^\]]*\]|["'][^"']*["'])\s*\)/g;

const errors = [];
const fail = (rule, detail) => errors.push(`[${rule}] ${detail}`);

// ---------------------------------------------------------------- locale files
const flatten = (obj, prefix = "", out = new Map()) => {
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === "object" && !Array.isArray(v)) {flatten(v, key, out);}
    else {out.set(key, v);}
  }
  return out;
};

const namespaces = readdirSync(join(LOCALES, "en"))
  .filter((f) => f.endsWith(".json"))
  .map((f) => f.replace(/\.json$/, ""));

for (const ns of namespaces) {
  if (!(ns in NAMESPACE_OWNERS)) {
    fail("namespace", `${ns}.json has no owning route — add the route or fold it into an existing namespace`);
  }
}
for (const ns of Object.keys(NAMESPACE_OWNERS)) {
  if (!namespaces.includes(ns)) {fail("namespace", `${ns}.json is declared for ${NAMESPACE_OWNERS[ns]} but missing`);}
}

const keys = {};
for (const code of LOCALE_CODES) {
  keys[code] = {};
  for (const ns of namespaces) {
    keys[code][ns] = flatten(JSON.parse(readFileSync(join(LOCALES, code, `${ns}.json`), "utf8")));
  }
}

for (const ns of namespaces) {
  for (const [a, b] of [["en", "zh"], ["zh", "en"]]) {
    for (const key of keys[a][ns].keys()) {
      if (!keys[b][ns].has(key)) {fail("parity", `${ns}:${key} exists in ${a} but not ${b}`);}
    }
  }
}

// --------------------------------------------------------------- source files
const walk = (dir, acc = []) => {
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) {
      if (entry !== "i18n") {walk(p, acc);}
    } else if (/\.(ts|tsx)$/.test(entry)) {acc.push(p);}
  }
  return acc;
};
const files = walk(SRC);
const sources = new Map(files.map((f) => [f, readFileSync(f, "utf8")]));

// Inline `const X = "literal"` so `${X}.suffix` template keys resolve to real paths.
let corpus = [...sources.values()].join("\n");
for (const [, name, value] of corpus.matchAll(/\bconst\s+([A-Z][A-Z0-9_]*)\s*=\s*["']([^"'\n]+)["']/g)) {
  corpus = corpus.split("${" + name + "}").join(value);
}

/**
 * Template literals become key patterns, so dynamically built keys
 * (`sections.${id}.title`) still count as referenced. Patterns without a
 * meaningful static part would match every key and are discarded.
 */
const dynamicPatterns = [...corpus.matchAll(/`([^`\n]*\$\{[^`\n]*)`/g)]
  .map((m) => m[1])
  .filter((s) => /^[\w.:$}{ -]+$/.test(s))
  .map((s) => {
    const body = s
      .split(/\$\{[^}]*\}/)
      .map((part) => part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
      .join("[\\w.-]*");
    return new RegExp(`^${body}$`);
  })
  .filter((re) => !re.test("zzqq") && !re.test("zzqq.wwvv.uutt") && !re.test("zzqq:wwvv.uutt"));

const referencedLiterally = (ns, key) =>
  corpus.includes(`"${key}"`) ||
  corpus.includes(`'${key}'`) ||
  corpus.includes(`\`${key}\``) ||
  corpus.includes(`${ns}:${key}`) ||
  dynamicPatterns.some((re) => re.test(key) || re.test(`${ns}:${key}`));

// ------------------------------------------------- missing keys + fallbacks
const declaredNamespaces = (text) => {
  const found = [];
  for (const m of text.matchAll(USE_TR_RE)) {
    const list = m[1].startsWith("[")
      ? [...m[1].matchAll(/["']([^"']+)["']/g)].map((x) => x[1])
      : [m[1].slice(1, -1)];
    found.push(...list);
  }
  return [...new Set(found)];
};

const isDynamic = (key) => key.includes("${");

/**
 * Modules whose i18n keys are consumed by components in other namespaces, so the
 * stored key must be fully qualified. Page-local `*.config.*` files are exempt:
 * their keys are only ever read by the page that owns them.
 */
const SHARED_KEY_HOLDER = /^src\/(config|components|lib|aws)\//;

for (const [file, text] of sources) {
  const rel = relative(ROOT, file);
  const declared = declaredNamespaces(text);
  const scope = declared.length ? declared : ["common"];

  /**
   * A file that declares its namespaces is checked strictly against them. Helpers
   * that merely receive a `t: TFunction` (columns, configs, schemas) have no
   * statically knowable namespace, so their bare keys are resolved against all of
   * them — enough to catch a key that exists nowhere.
   */
  const resolveKey = (key) => {
    if (key.includes(":")) {
      const ns = key.slice(0, key.indexOf(":"));
      return namespaces.includes(ns) ? [ns, key.slice(ns.length + 1)] : null;
    }
    const pool = declared.length ? scope : namespaces;
    const owner = pool.find((ns) => keys.en[ns]?.has(key));
    return owner ? [owner, key] : [pool[0], key];
  };

  const check = (key, where) => {
    if (isDynamic(key)) {return;}
    const resolved = resolveKey(key);
    if (!resolved) {return;}
    const [ns, bare] = resolved;
    if (!keys.en[ns]) {return;}
    if (!keys.en[ns].has(bare)) {fail("missing-key", `${rel} — ${where} "${ns}:${bare}" is not defined`);}
  };

  for (const m of text.matchAll(T_CALL_RE)) {
    const [, , key, comma, rest] = m;
    check(key, "t()");
    if (isDynamic(key)) {continue;}
    const hasStringFallback = comma === "," && /^\s*(["'`]|[^,]*defaultValue\s*:)/.test(rest);
    if (!hasStringFallback) {
      fail("fallback", `${rel} — t("${key}") has no string fallback`);
    }
  }
  for (const m of text.matchAll(KEY_FIELD_RE)) {check(m[2], "key field");}

  /*
   * Keys stored in a config object are read back through a variable — `t(i18nKey)` —
   * which no static check can attribute to a namespace. They are only safe when the
   * stored value is fully qualified, because the consuming `t` may be bound to any
   * namespace. A bare key here resolves for some consumers and renders as the raw
   * key string for the rest.
   */
  if (SHARED_KEY_HOLDER.test(rel)) {
    for (const m of text.matchAll(KEY_FIELD_RE)) {
      const key = m[2];
      if (!key || isDynamic(key) || key.includes(":")) {continue;}
      fail(
        "unqualified-key",
        `${rel} — "${key}" must be namespace-qualified (e.g. "common:${key}") because it is resolved through a variable`,
      );
    }
  }
}

// ------------------------------------------------------------- unused keys
for (const ns of namespaces) {
  for (const key of keys.en[ns].keys()) {
    if (!referencedLiterally(ns, key)) {fail("unused-key", `${ns}:${key} is never referenced`);}
  }
}

// ------------------------------------------------------------------- report
if (errors.length === 0) {
  const total = namespaces.reduce((n, ns) => n + keys.en[ns].size, 0);
  console.log(`i18n OK — ${namespaces.length} namespaces, ${total} keys, en/zh in sync.`);
  process.exit(0);
}

const grouped = errors.reduce((acc, e) => {
  const rule = e.slice(1, e.indexOf("]"));
  (acc[rule] ??= []).push(e);
  return acc;
}, {});
for (const [rule, list] of Object.entries(grouped)) {
  console.error(`\n${rule} (${list.length}):`);
  for (const e of list) {console.error("  " + e.slice(e.indexOf("]") + 2));}
}
console.error(`\n${errors.length} i18n problem(s).`);
process.exit(1);
