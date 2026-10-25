# Admin Dashboard v2

IoT Management Dashboard for Administrators.

Built with React, TypeScript, Vite, and Tailwind CSS.

## Prerequisites

You need **Node.js 24** (the current Active LTS, codename Krypton — supported until
2028-04-30). This is the version CI builds with, set via `NODE_VERSION` in
`.gitlab-ci.yml`; match it locally so your builds behave the same as the pipeline.

Node.js 20 and earlier are end-of-life and no longer receive security patches.

**npm** comes bundled with Node.js — do not install it separately. Node.js 24 ships
**npm 11.x**, which is what the committed `package-lock.json` was resolved with. A
different npm major can rewrite the lockfile on `npm install`, so if `npm -v` shows
anything other than 11.x, you are not on the expected toolchain.

### Install Node.js

- **Any platform** (recommended, using [nvm](https://github.com/nvm-sh/nvm)):
  ```bash
  nvm install 24
  nvm use 24
  ```
  `nvm use` applies per shell. Run `nvm alias default 24` if you want it in every
  new terminal.
- **macOS** (using Homebrew):
  ```bash
  brew install node@24
  ```
- **Windows / Linux**: Download the Node.js 24 LTS installer from https://nodejs.org.

Verify the installation by running:

```bash
node -v
npm -v
```

`node -v` should print `v24.x.x` and `npm -v` should print `11.x.x`.

## Setup

All commands below must be run from the `dashboard` directory.

```bash
cd dashboard
```

### 1. Install dependencies

```bash
npm install
```

This reads `package.json` and downloads every library the project needs into a local `node_modules` folder. You only need to run this once, or again after someone adds a new dependency.

### 2. Environment variables

There is a **single** build-time variable, `VITE_SERVER_URL` — the URL of a published `rmng-client-outputs.json`. Everything the app needs at runtime (API Gateway URL, ESP-User API URL, AWS region, IoT endpoint, Cognito admin pool + client, hosted-UI domain, OTA bucket/role, credential-provider endpoint) is fetched from that document at startup. No other `VITE_*` variables are read.

`npm run dev` sets it for you: its `predev` step, [`scripts/derive-server-url.mjs`](scripts/derive-server-url.mjs), builds the URL of the deployment's `rmng-client-outputs.json` (in the `rmng-public-assets-<ACCOUNT_ID>` S3 bucket, partitioned by `<REGION>/`) and writes it to `.env.development.local`. It takes the account and region from the repo-root `rmng-outputs.json` that `make deploy` writes, and falls back to your current AWS CLI identity and region when that file is absent.

To point at a different deployment, export `VITE_SERVER_URL` before `npm run dev` — the script leaves an exported value alone — or edit `.env.development.local` by hand. Both `.env.development.local` and `.env.development` are gitignored, since the URL carries an account id; do not commit either.

### 3. How configuration is loaded (dev vs deployed production)

| Where you run | What happens |
|---------------|----------------|
| **`npm run dev`** | Fetches the `rmng-client-outputs.json` at `VITE_SERVER_URL` and maps the `rmng-base` / `espuser-base` outputs into runtime config. If `VITE_SERVER_URL` is unset, the app shows an error instead of loading. |
| **Deployed behind CloudFront** (ESP RainMaker Neo CDK `AdminDashboardStack`) | If `VITE_SERVER_URL` was set when the bundle was built, it is fetched at startup. Otherwise the app fetches **`/config.json`** from its own origin — written by the CDK stack, it holds only `SERVER_URL`, the client-outputs URL — and then fetches that. |
| **`npm run preview`** | Serves a production build (`DEV` is false), so it behaves like the deployed app: `VITE_SERVER_URL` if it was set at build time, otherwise `/config.json` from the preview origin (absent from a bare `dist/`, so the app fails to load its config). |

**API Gateway usage:** calls that go to the main ESP RainMaker Neo API Gateway (for example temporary credentials via `POST …/v1/user/credentials`) use **`getApiGatewayUrl()`**, resolved from the fetched client-outputs (`rmng-base.ApiGatewayUrl`) or `config.json`. The shared axios client uses **`getEspUserApiUrl()`** (`espuser-base.EspUserApiUrl`). IoT/OTA helpers use **`getIotEndpoint()`**, **`getOtaS3Bucket()`**, and **`getOtaServiceRoleArn()`** with the same source.

## Running the app

### Local development (recommended for day-to-day work)

From `dashboard`:

```bash
npm run dev
```

This starts Vite at **http://localhost:5174** (see `vite.config.ts`) after `predev` derives `VITE_SERVER_URL` into `.env.development.local` (see [Environment variables](#2-environment-variables)). Hot reload is enabled.

## Building and production

### Production build (artifact for CDK / static hosting)

```bash
npm run build
```

`VITE_SERVER_URL` is baked into the bundle and fetched at startup. When it is unset the deployed app falls back to **`/config.json`** served from its own origin.

### Deploy with ESP RainMaker Neo (from the repo root)

The Makefile builds this app before `make deploy` / `make deploy-rmng` so `dist/` exists for the CDK stack. You do not need to run `npm run build` manually unless you are testing the bundle alone.

### Preview a build locally (`vite preview`)

After `npm run build`:

```bash
npm run preview
```

`.env.development.local` is not read by a production build, so export `VITE_SERVER_URL` before `npm run build`, or place a `config.json` (`{ "SERVER_URL": "<client-outputs URL>" }`) in `dist/`. Without either, the preview cannot load its runtime config.

## White-labeling

**[docs/BRANDING.md](docs/BRANDING.md)** — name, logo, colours, favicon and sign-in screen.
All driven from [`app.config.ts`](app.config.ts) plus one optional stylesheet; no component
edits.

The public Terms of Use and Privacy Policy served at `/static/*` are placeholder documents
carrying `{{TOKEN}}` markers for every company, product, contact and deployment detail. Since these are legal documents, please consult your legal advisor and fill up accordingly.

## Type checking

Run the TypeScript compiler without emitting files to catch type errors:

```bash
npm run typecheck
```

## Project structure

```
src/
  api/            API clients and TanStack Query hooks
  components/
    common/       Reusable app-specific components
    layout/       Header, footer, sidebar, page layouts
    ui-library/   Pure, prop-driven UI primitives (built on shadcn/ui)
    error/        Error boundary components
  config/         Route definitions, sidebar config, app config
  hooks/          Custom React hooks
  i18n/           Translation files (en, zh) organized by namespace
  lib/            Shared utilities
  pages/          Page components organized by route
  stores/         Zustand stores for client-only state
```

## Key technologies

| What               | Library                           |
|---------------------|----------------------------------|
| Framework           | React 19 + TypeScript            |
| Bundler             | Vite 8                           |
| Routing             | TanStack Router                  |
| Server state        | TanStack Query                   |
| Client state        | Zustand                          |
| UI primitives       | shadcn/ui (Radix UI)             |
| Styling             | Tailwind CSS 4                   |
| Animations          | Framer Motion                    |
| Internationalization| i18next                          |
| Icons               | lucide-react                     |
| Forms               | React Hook Form + Zod            |
