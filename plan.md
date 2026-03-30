# FlowPanel Implementation Plan

## Goal

Build a Go-based server and website domain management panel with these core technologies:

- Router: `chi`
- Database: `SQLite`
- Sessions: `scs`
- Logging: `zap`
- Jobs: `robfig/cron`
- Reverse proxy / TLS / site serving: `Caddy`
- Admin panel frontend: `React`

## Frontend Stack Decision

The admin panel stack is locked in as:

- Build tool: `Vite`
- App framework: `React + TypeScript`
- UI layer: `shadcn/ui`
- Routing: `TanStack Router`
- Server state: `TanStack Query`
- Data grids: `TanStack Table`

### UI direction

The panel should feel like an infrastructure operator console, not a generic admin template:

- dense left navigation
- table-first screens
- strong status indicators
- fast list/detail workflows
- clean, restrained visual language
- designed for frequent operational use rather than marketing-style presentation

The product should let an operator manage sites and domains from a React-based web panel, persist configuration in SQLite, schedule reconciliation/background jobs, and keep Caddy in sync so domains are served with TLS.

This project will be designed as a **single binary**: one Go executable runs the backend APIs, embedded React admin panel assets, domain management logic, cron jobs, SQLite-backed state, and embedded Caddy-based reverse proxy/TLS handling.

## Working Assumptions

These assumptions keep the first version focused. We can adjust them later if needed.

1. FlowPanel is a single Go binary.
2. The binary embeds Caddy and owns reverse proxy plus TLS responsibilities directly.
3. FlowPanel manages Caddy through in-process Go integration instead of an external Admin API or Caddyfile reload flow.
4. SQLite stores panel state, site/domain mappings, users, sessions metadata if needed, and job history.
5. First release supports one organization and an admin user model; multi-tenant support can come later.
6. Authentication starts with local email/password login.
7. Domains will point to upstream targets such as `http://127.0.0.1:3000`, container endpoints, or internal services.
8. TLS is handled by embedded Caddy automatically; FlowPanel is responsible for expressing the right host-to-upstream configuration.
9. The binary will typically bind the admin panel on an internal/private address and bind public traffic on `:80` and `:443`.
10. The admin panel is a React application built into static assets and embedded into the Go binary for production.
11. The Go backend exposes JSON endpoints for the React panel and can still render a minimal fallback error page if needed.

## High-Level Architecture

### Main components

- `cmd/flowpanel`: application entrypoint
- `internal/http`: chi router, middleware, handlers, API routes, static asset serving
- `internal/config`: environment and runtime config loading
- `internal/db`: SQLite connection, migrations, repositories
- `internal/auth`: login, password hashing, session lifecycle with scs
- `internal/domain`: sites, domains, upstreams, validation, business logic
- `internal/caddy`: embedded Caddy runtime manager and config reconciliation
- `internal/jobs`: cron registration, periodic reconciliation, health checks
- `internal/logging`: zap logger setup
- `web/panel`: React source code for the admin UI
- `web/dist`: compiled frontend assets embedded into the binary

### Data flow

1. Admin creates a site and attaches one or more domains.
2. Admin defines the upstream target for that site.
3. FlowPanel stores desired state in SQLite.
4. The React panel talks to Go JSON endpoints for authentication and CRUD operations.
5. A service layer validates the request and triggers reconciliation.
6. The embedded Caddy runtime is reloaded from desired state inside the same process.
7. Periodic cron jobs verify actual state against desired state and repair drift.

## Delivery Strategy

We will build this in small, testable phases. After each step:

1. I implement the step.
2. You run and test it locally.
3. We fix any issues.
4. Then we move to the next step.

## Step-by-Step Plan

## Step 1: Project bootstrap and application skeleton [Completed]

Status: Completed on March 30, 2026. The current baseline is an empty admin shell with sidebar navigation only. No users, sites, domains, or jobs are seeded yet.

### Objective

Create a clean Go project foundation with the required dependencies, startup flow, router, logger, config loader, and health endpoint.

### Deliverables

- Go module initialized
- Base folder structure
- Dependency setup for:
  - `chi`
  - `scs`
  - `zap`
  - `robfig/cron`
  - SQLite driver
- Central config loader using env vars
- Zap logger initialization
- HTTP server with chi router
- Frontend asset serving strategy
- Basic middleware stack
- `/healthz` endpoint
- Graceful shutdown handling

### Detailed tasks

- Define application config structure:
  - app env
  - admin listen address
  - public HTTP address
  - public HTTPS address
  - SQLite database path
  - session secret
  - cron enabled flag
- Build a minimal app container struct to hold shared dependencies.
- Reserve route layout early:
  - `/api/*` for React-facing backend endpoints
  - `/healthz` for health checks
  - `/admin` or `/` for serving the React app shell
- Add middleware for:
  - request ID
  - real IP
  - recovery
  - structured request logging
- Wire startup and shutdown sequence carefully:
  - load config
  - create logger
  - open DB
  - initialize session manager
  - build admin router
  - initialize embedded Caddy runtime
  - start admin HTTP server
  - trap SIGINT/SIGTERM

### Manual test

- Start the app
- Open `/healthz`
- Confirm HTTP 200 response
- Confirm startup/shutdown logs are structured and readable
- Confirm invalid env config fails fast with clear errors

### Done when

- App starts reliably
- Health endpoint works
- Logging and shutdown are in place
- Runtime layout supports the future embedded Caddy integration cleanly
- Route layout supports the React panel cleanly

## Step 2: SQLite setup and migrations

### Objective

Create durable data storage with migration support and a minimal schema foundation.

### Deliverables

- SQLite connection layer
- DB configuration pragmas
- Migration runner
- Initial schema

### Detailed tasks

- Choose migration approach:
  - SQL migration files stored in repo
  - simple migration runner executed at startup
- Configure SQLite pragmas appropriate for a web app:
  - foreign keys on
  - busy timeout
  - WAL mode
- Create initial tables:
  - `users`
  - `sites`
  - `domains`
  - `domain_routes` or `site_domains` if many-to-many is needed
  - `job_runs`
  - `audit_logs` optional in phase 1, but recommended
- Add timestamps and status columns where useful.
- Add basic repository helpers for inserts/selects.

### Suggested schema direction

- `users`
  - id
  - email
  - password_hash
  - role
  - created_at
  - updated_at
- `sites`
  - id
  - name
  - upstream_url
  - status
  - created_at
  - updated_at
- `domains`
  - id
  - site_id
  - hostname
  - is_primary
  - tls_mode
  - status
  - last_synced_at
  - created_at
  - updated_at
- `job_runs`
  - id
  - job_name
  - status
  - started_at
  - finished_at
  - message

### Manual test

- Run app against empty database path
- Confirm DB file is created
- Confirm migrations run automatically
- Inspect tables with `sqlite3`

### Done when

- Fresh startup creates a working DB schema
- Re-running startup is idempotent

## Step 3: Authentication and sessions

### Objective

Secure the panel with admin login using `scs`.

### Deliverables

- Login API
- Logout API
- Password hashing and verification
- Protected API routes
- Session middleware
- Bootstrap command or seed for first admin user

### Detailed tasks

- Use a strong password hashing strategy such as `bcrypt` or `argon2id`.
- Configure `scs` with secure cookie defaults.
- Add auth middleware:
  - require authenticated user for protected `/api/*` routes
  - expose current session/user endpoint for the React app
- Build first-user bootstrap path:
  - env-driven seed
  - CLI command
  - one-time setup page
- Store only necessary session data:
  - user ID
  - role
- Keep auth cookie-based so the React panel can use the same session securely.

### Manual test

- Create first admin user
- Login from API or UI with correct credentials
- Reject wrong password
- Verify protected API rejects when not logged in
- Logout invalidates access

### Done when

- Session-based auth is working end-to-end

## Step 4: React admin panel foundation

### Objective

Create the React-based admin UI structure used to manage the entire system.

### Deliverables

- React app scaffold
- Vite and TypeScript setup
- Shared layout and navigation
- Login page
- Dashboard page
- Global API client and auth handling
- Error boundaries and loading states
- Embedded static asset serving from the Go binary
- `shadcn/ui` component setup
- `TanStack Router` route foundation
- `TanStack Query` provider setup

### Detailed tasks

- Create the React app under `web/panel`.
- Build the panel with:
  - `Vite`
  - `React + TypeScript`
  - `shadcn/ui`
  - `TanStack Router`
  - `TanStack Query`
  - `TanStack Table`
- Add the first screens:
  - login
  - dashboard
  - sites list
  - domains list
- Add app-level concerns:
  - route protection
  - session bootstrap on app load
  - API error handling
  - loading and empty states
- Serve the compiled app from Go using embedded files for production.
- Keep the visual system clean and operator-focused because the UI is now the main control surface.
- Avoid generic template dashboards; favor a compact operations console feel.

### Manual test

- Navigate panel pages
- Confirm auth-protected layout works
- Confirm loading/error states render correctly
- Confirm embedded frontend assets load from the binary

### Done when

- Admin can navigate the panel comfortably
- The React panel is a reliable shell for the remaining CRUD work

## Step 5: Sites CRUD

### Objective

Manage upstream applications that will receive proxied traffic.

### Deliverables

- Site CRUD API endpoints
- React pages/forms for create/edit/delete/list
- Site validation
- Status handling

### Detailed tasks

- Define site rules:
  - unique name
  - valid upstream URL
  - optional description
  - active/inactive status
- Add repository and service methods for CRUD.
- Add JSON API handlers for the React panel.
- Prevent deletion if domains are still attached unless explicitly forced.
- Show last sync state or proxy state later once embedded Caddy integration lands.

### Manual test

- Create a site with valid upstream
- Reject invalid upstream URL
- Edit site
- Delete unused site
- Confirm persistence after restart
- Confirm the React form and list flows work cleanly

### Done when

- Sites can be managed reliably from the panel

## Step 6: Domains CRUD and validation

### Objective

Attach domains to sites and validate hostnames before syncing to Caddy.

### Deliverables

- Domain CRUD API endpoints
- React pages/forms for create/edit/delete/list
- Associate domains with sites
- Hostname normalization and validation
- Primary domain support

### Detailed tasks

- Normalize hostnames:
  - lowercase
  - trim whitespace
  - remove trailing dot if appropriate
- Enforce uniqueness of hostname globally.
- Validate allowed hostnames:
  - no scheme
  - no path
  - valid DNS label structure
- Track domain states such as:
  - pending
  - active
  - error
  - disabled
- Support one primary domain per site if needed for UI clarity.
- Expose domain operations through JSON endpoints consumed by React pages.

### Manual test

- Add multiple domains to a site
- Reject duplicate hostnames
- Reject malformed hostnames
- Delete a domain
- Confirm the React flows handle validation errors properly

### Done when

- Desired host-to-site mappings are stored correctly

## Step 7: Embedded Caddy runtime and configuration reconciliation

### Objective

Push desired domain routing state from FlowPanel into the embedded Caddy runtime.

### Deliverables

- Embedded Caddy runtime manager
- Config builder for host-to-upstream routes
- Reconciliation service
- Sync status reporting

### Detailed tasks

- Decide runtime ownership strategy:
  - FlowPanel owns the full embedded Caddy config
  - no external Caddy process is assumed
- Implement embedded Caddy operations:
  - build JSON config from desired state
  - load or reload config in-process
  - validate startup and reload errors
- Build routes for:
  - host matcher by domain
  - reverse proxy upstream target
  - automatic HTTPS
- Persist sync metadata in SQLite:
  - last sync time
  - last sync result
  - last error message
- Add a manual “sync now” action in the admin panel.
- Expose sync status and manual sync actions through API endpoints for the React dashboard.

### Important design note

This step is simpler under the single-binary model because FlowPanel owns the embedded Caddy instance completely. That means FlowPanel can safely build and reload the full proxy config from SQLite without coordinating with unrelated Caddy-managed routes.

### Manual test

- Start the FlowPanel binary with public ports available
- Create a site and domain in FlowPanel
- Trigger sync
- Confirm embedded Caddy serves the hostname and proxies to the upstream
- Confirm TLS provisions successfully after DNS points correctly
- Confirm sync status and errors are visible in the React UI

### Done when

- FlowPanel can create working live routes through embedded Caddy

## Step 8: Background jobs with cron

### Objective

Run periodic maintenance and reconciliation jobs.

### Deliverables

- Cron scheduler bootstrap
- Job registration
- Job execution logging
- Job run persistence

### Detailed tasks

- Add recurring jobs such as:
  - config reconciliation
  - stale domain health check
  - sync retry for failed items
  - optional certificate/status audit
- Wrap jobs with structured logging and panic recovery.
- Record job start/end status in `job_runs`.
- Ensure jobs do not overlap destructively:
  - use mutex or DB-backed lock if needed

### Manual test

- Enable cron with short intervals locally
- Confirm jobs run and are logged
- Confirm job history appears in DB or panel

### Done when

- Periodic jobs work safely and observably

## Step 9: Observability and auditability

### Objective

Make the system debuggable and operationally safe.

### Deliverables

- Structured logs across all layers
- Audit trail for panel actions
- Better error reporting
- Optional request metrics hooks

### Detailed tasks

- Standardize log fields:
  - request_id
  - user_id
  - site_id
  - domain
  - job_name
- Add audit entries for:
  - login
  - site create/update/delete
  - domain create/update/delete
  - manual sync
- Add admin-visible sync errors and job failures.
- Ensure the React panel surfaces these operational events clearly.

### Manual test

- Perform a few admin actions
- Confirm logs and audit records are useful
- Trigger a failed sync and confirm the error is visible

### Done when

- Operators can understand what happened and why

## Step 10: Domain health, diagnostics, and UX polish

### Objective

Help the operator understand why a domain is or is not working.

### Deliverables

- React diagnostics page
- DNS guidance
- Last known proxy/TLS sync status
- Health indicators in UI

### Detailed tasks

- Add checks for:
  - DNS resolution
  - HTTP reachability of upstream
  - embedded Caddy sync status
- Show actionable hints:
  - DNS not pointed yet
  - upstream unavailable
  - embedded proxy runtime failed to reload
- Surface timestamps and last errors near each domain/site row.

### Manual test

- Test both healthy and broken domains
- Confirm the UI shows useful diagnostics

### Done when

- Troubleshooting is practical without reading raw logs only

## Step 11: Hardening and production readiness

### Objective

Reduce operational risk before broader use.

### Deliverables

- Secure cookie/session settings
- CSRF protection
- Input validation review
- Deployment config examples
- Backup guidance for SQLite

### Detailed tasks

- Add CSRF protection for session-authenticated API mutation requests.
- Review auth/session security:
  - secure cookies
  - httpOnly
  - sameSite
  - session lifetime
- Add rate limiting for login attempts if needed.
- Provide example systemd service or container setup.
- Document SQLite backup and restore approach.
- Document recommended single-binary deployment topology.
- Document binding to `:80` and `:443`, including privilege requirements on Linux.

### Manual test

- Verify forms reject invalid CSRF tokens
- Verify production env settings work as expected

### Done when

- App is safe enough for controlled production use

## Step 12: Documentation and handoff

### Objective

Make setup and future development straightforward.

### Deliverables

- `README.md` setup guide
- `.env.example`
- architecture notes
- runbook for common tasks

### Detailed tasks

- Document:
  - local development startup
  - frontend build and embed flow
  - migration behavior
  - first admin creation
  - how FlowPanel embeds and configures Caddy
  - how to troubleshoot failed sync
- Add example single-binary deployment setup.
- Add example production environment variables.

### Manual test

- Follow docs from a clean environment
- Confirm another person could bootstrap the app without hidden steps

### Done when

- The project is understandable and runnable from docs

## Recommended Build Order

We should implement in this exact order:

1. Step 1: bootstrap and skeleton
2. Step 2: SQLite and migrations
3. Step 3: authentication and sessions
4. Step 4: React admin panel foundation
5. Step 5: sites CRUD
6. Step 6: domains CRUD
7. Step 7: embedded Caddy reconciliation
8. Step 8: cron jobs
9. Step 9: observability and audit
10. Step 10: diagnostics UX
11. Step 11: hardening
12. Step 12: documentation

## Risks and Design Choices To Revisit

These are the main decisions we may need to revisit during implementation:

1. Whether embedding full Caddy in-process stays preferable versus splitting proxy duties later.
2. Whether SQLite remains enough if concurrent write volume grows.
3. Whether the React panel should be a SPA or a route-based app with lighter client state.
4. Whether one admin role is enough or we need finer permissions.
5. Whether the admin panel should be exposed separately from the public listeners in production.

## Immediate Next Step

Start with **Step 1: Project bootstrap and application skeleton**.
