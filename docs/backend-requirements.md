# CAUS Platform — Backend requirements (`api/`)

Extracted from [REQUIREMENTS.md](../REQUIREMENTS.md). The Go API is the only auth and
business-logic backend; it is served same-origin under `/api/` behind the reverse proxy.
`schema.sql` (repo root) is the source of truth for the data model.

## 5.1 Configuration (env vars)

| Var                     | Purpose                                                   |
| ----------------------- | --------------------------------------------------------- |
| `DATABASE_URL`          | Postgres DSN                                              |
| `PUBLIC_URL`            | e.g. `https://clubalgoritmiaus.es` (redirects, Origin check) |
| `GOOGLE_CLIENT_ID`      | OAuth client                                              |
| `GOOGLE_CLIENT_SECRET`  | OAuth secret (never logged, never committed)              |
| `ADMIN_EMAILS`          | comma-separated bootstrap admins, promoted on login       |
| `SESSION_TTL`           | e.g. `720h`                                               |
| `PORT`                  | default `8080`                                            |
| `LOG_LEVEL`             | `debug` \| `info` \| `warn` \| `error`                    |

Startup fails fast if any required var is missing.

## 5.2 Authentication — Sign in with Google (everyone)

The only login is a "Sign in with Google" button; no passwords, no other providers.
Scopes: `openid email profile`, no offline access (Google is never called after the callback).

- OIDC authorization code flow with **PKCE** and **state** + **nonce**, stored in a short-lived
  HttpOnly cookie.
- `GET /api/auth/google/login?return_to=` → redirect to Google. `return_to` must be a
  relative path (open-redirect protection).
- `GET /api/auth/google/callback` → verify state, exchange code, verify ID token (issuer,
  audience, nonce, `email_verified`), find the person by `google_sub` (a conflicting
  email is rejected with `identity_conflict`, never auto-linked); create session, redirect.
- A sign-in failure the user can meet (consent refused, identity rejected, `identity_conflict`, provider unavailable) redirects to `return_to` with `?auth_error=<code>` and no session; malformed callbacks and state errors stay JSON 400.
- Session: random 32-byte token in cookie `__Host-caus_session` on HTTPS (`caus_session`
  on HTTP localhost); HttpOnly, SameSite=Lax, Path=/, `Secure` only on HTTPS; DB stores `sha256(token)`; expiry via `SESSION_TTL`.
- `POST /api/auth/logout` deletes the session row and clears the cookie.
- `GET /api/me` → current user or 401.
- Roles: `member` (default), `admin`. Each email in `ADMIN_EMAILS` bootstraps admin
  exactly once (a `sha256(email)` marker), and only for an authoritative Google
  address (`@gmail.com` or hosted domain); admins can grant roles from the panel.
- CSRF: all non-GET requests require `Origin` == `PUBLIC_URL` (plus SameSite=Lax cookie).
- Optional: restrict nothing by domain (students use personal Gmail too); store
  `is_us_email` derived from `@alum.us.es` / `@us.es` for stats/filters.

## 5.3 Data model (PostgreSQL)

`schema.sql` (repo root) is the source of truth for the data model. Current tables:

- `people` — members, speakers and competitors. Email is optional: deleting an account
  clears it while the row and name stay as history. Roles: `member`, `admin`.
  `google_sub` links the Google account.
- `sessions` — `sha256(token)`, person, expiry.
- `talks` — title, `description` (Markdown), Seville-local `date` + `duration`, `location`,
  `language` (`es` default, `en`), `stream_url`, `poster_url`, `published`.
- `talk_speakers` — talk ↔ person; every talk must have at least one speaker.
- `competitions` — name, `description` (Markdown: announcement, prizes), `date`, `location`,
  `team_size` (default 3; 1 = individual competition).
- `teams` — a participation in a competition; optional `name` (individual entries need
  none), `position` and `photo_url` filled in after the event.
- `team_members` — team ↔ person; 1..`team_size` members, one team per person per
  competition.

Pending (not in the schema yet):

- Competition registration (window, capacity, per-competition fields, answers) — see
  [§5.4](#54-per-competition-fields-pending-design).
- Localized public content values (translated titles, descriptions, field/option labels) —
  schema/API contract design pending; see
  [frontend i18n](frontend-requirements.md#internationalization-i18n).

## 5.4 Per-competition fields (pending design)

The text below is the current proposal.

`competitions.fields` is an ordered array:

```json
[
  { "key": "degree", "label": { "es": "Grado", "en": "Degree" }, "type": "select",
    "required": true, "options": ["GII-IS", "GII-TI", "GII-IC", "Matemáticas", "Otro"] },
  { "key": "year", "label": { "es": "Curso", "en": "Year" }, "type": "number",
    "required": true, "min": 1, "max": 6 },
  { "key": "codeforces", "label": { "es": "Usuario de Codeforces", "en": "Codeforces handle" },
    "type": "text", "required": false, "max_length": 64 },
  { "key": "photo_consent", "label": { "es": "Acepto aparecer en fotos", "en": "I accept photos" },
    "type": "checkbox", "required": true }
]
```

- Types: `text`, `textarea`, `email`, `number`, `select`, `checkbox`.
- `key`: unique per competition, `^[a-z0-9_]+$`.
- Server validates on save (admin) that the definition is well-formed, and on registration
  that `answers` has exactly the defined keys, correct types, required present, options valid,
  length/range limits respected. Errors returned per field (422).
- Editing fields after registrations exist: allowed; old answers are kept as-is, CSV export
  includes the union of keys.

## 5.5 Business rules

Talks:

- Public list returns published talks: upcoming (asc) and past (desc).
- Every talk has at least one speaker; speakers are registered people.

Competitions:

- Every participation is a team; a team has between 1 and `team_size` members, and a
  person can be in only one team per competition.
- Registration rules (window, capacity, per-competition fields): pending design — see
  [§5.4](#54-per-competition-fields-pending-design) and
  [REQUIREMENTS.md §10](../REQUIREMENTS.md#10-open-decisions).

## 5.6 HTTP API

Public / member (`/api`):

| Method | Path                                         | Auth   |
| ------ | -------------------------------------------- | ------ |
| GET    | `/healthz` (process up), `/readyz` (DB ping) | —      |
| GET    | `/auth/google/login`, `/auth/google/callback`| —      |
| POST   | `/auth/logout`                               | user   |
| GET    | `/me`                                        | user   |
| PATCH  | `/me` (change own first/last name)           | user   |
| GET    | `/me/export` (GDPR data export, JSON)        | user   |
| DELETE | `/me` (GDPR account deletion — see [§6](#6-privacy--gdpr-personal-data-of-students-eu)) | user |
| GET    | `/talks`, `/talks/{id}`                      | —      |
| GET    | `/talks.ics` (calendar feed, replaces Google Calendar embed) | — |
| GET    | `/competitions`, `/competitions/{id}`        | —      |

Pending (competition registration design — see [§5.4](#54-per-competition-fields-pending-design)):

| Method | Path                                                          | Auth |
| ------ | ------------------------------------------------------------- | ---- |
| GET    | `/me/registrations`                                           | user |
| PUT    | `/competitions/{id}/registration` (create/update answers)     | user |
| DELETE | `/competitions/{id}/registration`                             | user |

Admin (`/api/admin`, role `admin`):

| Method          | Path                                                        |
| --------------- | ----------------------------------------------------------- |
| GET/POST        | `/talks`                                                    |
| GET/PATCH/DELETE| `/talks/{id}`                                               |
| GET/POST        | `/competitions`                                             |
| GET/PATCH/DELETE| `/competitions/{id}`                                        |
| GET/POST        | `/competitions/{id}/teams`                                  |
| GET/PATCH/DELETE| `/teams/{id}`                                               |
| GET             | `/users` (search by name/email)                             |
| PATCH           | `/users/{id}` (role)                                        |

Pending (competition registration design — see [§5.4](#54-per-competition-fields-pending-design)):

| Method          | Path                                                        |
| --------------- | ----------------------------------------------------------- |
| GET             | `/competitions/{id}/registrations` (`?format=csv`)          |
| DELETE          | `/competitions/{id}/registrations/{registrationId}`         |

Conventions:

- JSON in/out, `snake_case`. Timestamps: event dates (`talks.date`, `competitions.date`)
  are `Europe/Madrid` local time; `created_at`/`updated_at` are RFC 3339 UTC.
- Error body: `{ "error": { "code": "validation_failed", "message": "...", "fields": { "degree": "required" } } }`.
  `message` is diagnostic only; clients translate `code`/`fields` values to localized text.
- Status codes: 400 malformed, 401 not signed in, 403 not admin / bad Origin, 404, 409
  duplicate / full / window closed (distinct `code`), 422 validation, 500.
- Request body size limit (e.g. 64 KB), `DisallowUnknownFields` on JSON decode.
- CSV export: UTF-8 with BOM (Excel), columns `name, email, registered_at, <field keys…>`.

## 5.7 Cross-cutting

- **Logging**: `slog` JSON; one access log line per request (method, path pattern, status,
  duration, request id, user id if any). Never log tokens, cookies, secrets or answers.
- **Middleware chain**: request id → recover (500 + log) → access log → security headers →
  session loader → `requireUser` / `requireAdmin` / `requireSameOrigin` where needed.
- **Security headers** (set at proxy or API): HSTS, `X-Content-Type-Options`,
  `Referrer-Policy`, CSP allowing Google sign-in redirect and own origin.
- **Rate limiting**: per-IP limit on auth and write endpoints (in-memory is fine for 1 instance).
- **Graceful shutdown** on SIGTERM; HTTP server timeouts set.
- **Expired session cleanup**: periodic delete or on-login purge.

---

## 6. Privacy / GDPR (personal data of students, EU)

- Privacy policy page (`/privacidad/`) — also required for the Google OAuth consent screen.
- Purpose, legal basis (consent), controller (CAUS), retention period, contact email.
- Collect only what each competition needs; competition fields are reviewed by admins.
- User can see, export (`/me/export`) and delete (`DELETE /me`) their data. Deleting an
  account clears `email` and `google_sub` and deletes its sessions; the person
  row and name stay as history in talks and teams — nothing cascades away from them.
- Retention policy: e.g. delete registrations N months after the event (job or manual admin
  action) — decide N.
- Admin access to registrant data limited to `admin` role; CSV exports handled carefully.
- Public build artifacts must never contain private data: no admin credentials, user
  sessions, private profiles or registration answers — only what the published public
  API endpoints expose.
