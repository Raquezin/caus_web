const pageQuery = [["limit", "integer", "1..100; default 20"], ["offset", "integer", "0 or greater; default 0"]];
const talkQuery = [["period", "string", "all (default), upcoming or past; exactly one value"], ["limit", "integer", "1..100; default 20"], ["offset", "integer", "0 or greater; default 0"]];
const pageErr = ["400 invalid_pagination"];
const d = window.API_DATA;
d.requests.push({
  name: "AccountPatch",
  summary: "Patch the signed-in person's own names. At least one field is required.",
  fields: [["first_name", "string", "optional", "Nonblank valid Unicode, at most 200 characters."], ["last_name", "string", "optional", "Valid Unicode, at most 200 characters; an empty string is accepted."]],
  rules: ["Omitted fields retain their current values.", "Neither field is nullable.", "An empty {} patch fails with 422 and fields.patch."]
});
d.endpoints.push(
  window.API_ENDPOINT("GET", "/api/healthz", "public", "Health", "Liveness probe. Confirms that the process is serving without querying the database.", {
    res: [[200, "Service is alive"]],
    shape: '{ "status": "ok" }',
    note: "Rate-limit exempt."
  }),
  window.API_ENDPOINT("GET", "/api/readyz", "public", "Health", "Readiness probe. Pings Postgres with a 2 second timeout and reports whether the service can serve requests.", {
    res: [[200, "Database ping succeeded"], [503, "Database ping failed or timed out"]],
    err: ["503 unavailable"],
    shape: '{ "status": "ok" }',
    note: "Rate-limit exempt."
  }),
  window.API_ENDPOINT("GET", "/api/people", "admin", "People", "Lists people for admins, ordered by id ascending.", {
    q: pageQuery,
    res: [[200, "Paginated person list"]],
    err: ["401 unauthenticated", "403 forbidden", ...pageErr],
    shape: "data: Person[]; pagination: { limit, offset, has_more }",
    ex: "/api/people?limit=20&offset=0"
  }),
  window.API_ENDPOINT("GET", "/api/people/{id}", "admin", "People", "Returns one person from the legacy admin-only people route. Only id, first_name and last_name are exposed.", {
    res: [[200, "Person"]],
    err: ["401 unauthenticated", "403 forbidden", "400 invalid_id", "404 person_not_found"],
    shape: "Person",
    ex: "/api/people/11111111-2222-3333-4444-555555555555",
    note: "A parsed zero UUID is accepted by this legacy route and follows the normal lookup path; it returns 404 person_not_found only when no person with that id is stored."
  }),
  window.API_ENDPOINT("GET", "/api/talks", "public", "Talks", "Lists published talks. A talk counts as upcoming until it ends (start plus duration). Upcoming dates sort ascending, past dates sort descending; all returns upcoming first, then past, with id as tie-breaker.", {
    q: talkQuery,
    res: [[200, "Paginated published talk list"]],
    err: ["400 invalid_filter", "400 invalid_pagination"],
    shape: "data: Talk[]; pagination: { limit, offset, has_more }",
    ex: "/api/talks?period=upcoming&limit=20&offset=0"
  }),
  window.API_ENDPOINT("GET", "/api/talks/{id}", "public", "Talks", "Returns one published talk. Drafts and missing ids both return 404 not_found.", {
    res: [[200, "Talk"]],
    err: ["400 invalid_id", "404 not_found"],
    shape: "Talk",
    ex: "/api/talks/22222222-3333-4444-5555-666666666666"
  }),
  window.API_ENDPOINT("GET", "/api/talks.ics", "public", "Talks", "Downloads every published talk as an iCalendar feed. The period query is not applied and there is no pagination.", {
    res: [[200, "iCalendar feed"]],
    err: ["500 internal_error"],
    shape: "text/calendar; charset=utf-8; DTSTART and DTEND in UTC",
    ex: "/api/talks.ics",
    note: "Sorted by date then id. UID format is uuid@caus. Madrid wall-clock dates are converted to UTC instants when encoded."
  }),
  window.API_ENDPOINT("GET", "/api/competitions", "public", "Competitions", "Lists competitions ordered by date descending, then id ascending.", {
    q: pageQuery,
    res: [[200, "Paginated competition list"]],
    err: pageErr,
    shape: "data: Competition[]; pagination: { limit, offset, has_more }",
    ex: "/api/competitions?limit=20&offset=0"
  }),
  window.API_ENDPOINT("GET", "/api/competitions/{id}", "public", "Competitions", "Returns a competition with its teams. Public team members expose id, first_name and last_name only, never emails or role.", {
    res: [[200, "Competition detail"]],
    err: ["400 invalid_id", "404 not_found"],
    shape: "CompetitionDetail: { competition: Competition, teams: Team[] }",
    ex: "/api/competitions/33333333-4444-5555-6666-777777777777"
  }),
  window.API_ENDPOINT("GET", "/api/auth/google/login", "public", "Auth", "Starts Google OIDC authorization-code login with PKCE S256, state and nonce, then redirects to Google.", {
    q: [["return_to", "string", "Optional single root-relative safe path; default /. Maximum 2048 UTF-8 bytes as measured by Go len, not characters. Repeated values are rejected. Absolute URLs, protocol-relative paths, backslashes, control characters and encoded unsafe paths are rejected."]],
    res: [[302, "Redirect to Google; sets the OAuth flow cookie"]],
    err: ["400 invalid_return_to", "503 auth_unavailable"],
    shape: "Location: Google authorization URL; Set-Cookie: __Host-caus_oauth=<opaque> over HTTPS (caus_oauth on HTTP localhost)",
    ex: "/api/auth/google/login?return_to=%2F",
    note: "Use a top-level browser navigation. curl only starts the redirect and cannot complete Google login. Very long paths can also fail because the OAuth state cookie must stay within the roughly 3800-byte cookie limit."
  }),
  window.API_ENDPOINT("GET", "/api/auth/google/callback", "public", "Auth", "Google OIDC redirect target. Validates state, exchanges the authorization code, validates the identity and creates a server-side session.", {
    q: [["code", "string", "Authorization code managed by Google."], ["state", "string", "Provider-managed state; must match the OAuth flow cookie."], ["error", "string", "Provider error when the user refuses consent."]],
    res: [[302, "Redirect to the saved return_to. Success sets the session cookie; a sign-in failure adds auth_error and sets no session cookie. The flow cookie is always expired."]],
    err: ["400 oauth_callback_invalid", "400 invalid_oauth_state", "500 internal_error"],
    shape: "Location: validated return_to; Set-Cookie: __Host-caus_session=<opaque> over HTTPS (caus_session on HTTP localhost); flow cookie expired",
    ex: "/api/auth/google/callback?code=PROVIDER_CODE&state=PROVIDER_STATE",
    cookie: "caus_oauth=YOUR_OAUTH_COOKIE",
    note: "This is a browser redirect endpoint, not a client token-submission endpoint. It requires one matching, unexpired OAuth flow cookie (__Host-caus_oauth over HTTPS, caus_oauth on HTTP localhost); duplicated query values are invalid. code and state are provider-managed illustrative placeholders and must not be replayed manually. A missing Google family name is accepted and stored as an empty last name; later logins never refresh stored names. Sign-in failures (oauth_denied, invalid_identity, identity_conflict, auth_unavailable) are not JSON errors: the callback redirects to the saved return_to with that code in the auth_error query parameter, replacing any existing auth_error, and the site shows a localized message."
  }),
  window.API_ENDPOINT("GET", "/api/me", "user", "Account", "Returns the current account as a bare Account object.", {
    res: [[200, "Account"]],
    err: ["401 unauthenticated"],
    shape: "Account",
    note: "Roles are read per request. Missing, empty, duplicate, malformed or expired session cookies return 401 unauthenticated."
  }),
  window.API_ENDPOINT("PATCH", "/api/me", "user", "Account", "Changes the signed-in person's own names and returns the updated Account object.", {
    b: "AccountPatch",
    payload: {first_name: "Ada Maria", last_name: "Lovelace"},
    res: [[200, "Updated Account"]],
    err: ["400 invalid_body", "401 unauthenticated", "403 origin_denied", "404 not_found", "415 unsupported_media_type", "422 validation_failed", "429 rate_limited", "500 internal_error"],
    shape: "Account",
    note: "Requires the Origin header to equal the configured PUBLIC_URL exactly. first_name must be nonblank and last_name may be empty; both are trimmed and limited to 200 characters. An empty {} patch fails with 422 and fields.patch, and a missing or deleted account answers 404 not_found. Login never overwrites these names: only the person changes them after the first sign-in."
  }),
  window.API_ENDPOINT("GET", "/api/me/export", "user", "Account", "Exports the current account as a downloadable JSON object.", {
    res: [[200, "Account export"]],
    err: ["401 unauthenticated", "404 not_found", "500 internal_error"],
    shape: 'AccountExport; Content-Disposition: attachment; filename="caus-account.json"',
    note: "Includes profile and google_sub, session metadata, associated talks including own unpublished talks, teams and competitions. Session token hashes and cookies are never included."
  }),
  window.API_ENDPOINT("DELETE", "/api/me", "user", "Account", "Deactivates authentication for the current account. This is not full erasure or anonymization.", {
    res: [[204, "Authentication deactivated; session cookie cleared"]],
    err: ["401 unauthenticated", "403 origin_denied", "404 not_found", "409 conflict", "500 internal_error"],
    shape: "No response body.",
    note: "Requires the Origin header to equal the configured PUBLIC_URL exactly. Deletes all sessions, clears email and google_sub, and sets role to member. Consumed hashed-email admin bootstrap markers are retained and cannot retrigger promotion. The person row, names, talks, speaker links, team memberships and competition history are retained."
  }),
  window.API_ENDPOINT("POST", "/api/auth/logout", "user", "Auth", "Revokes only the current session and clears the session cookie.", {
    res: [[204, "Current session revoked"]],
    err: ["401 unauthenticated", "403 origin_denied", "500 internal_error"],
    shape: "No response body.",
    note: "Requires the Origin header to equal the configured PUBLIC_URL exactly. The handler ignores any request body, so no body is required; a body is neither read nor validated."
  }),
  window.API_ENDPOINT("GET", "/api/admin/talks", "admin", "Admin Talks", "Lists talks for admins, including unpublished talks, using the same period filter and ordering as the public list.", {
    q: talkQuery,
    res: [[200, "Paginated talk list including drafts"]],
    err: ["400 invalid_filter", "400 invalid_pagination"],
    shape: "data: Talk[]; pagination: { limit, offset, has_more }",
    ex: "/api/admin/talks?period=all&limit=20&offset=0"
  }),
  window.API_ENDPOINT("POST", "/api/admin/talks", "admin", "Admin Talks", "Creates a talk and returns it with a Location header.", {
    b: "TalkCreate",
    payload: {title: "Constraint solving in practice", description: "A practical tour of constraint models and search.", date: "2026-03-12T18:30:00", duration: 3600, location: "Aula 3", speaker_ids: ["11111111-2222-3333-4444-555555555555"]},
    res: [[201, "Talk created"]],
    err: ["409 conflict (duplicate title, date, location)", "422 validation_failed"],
    shape: "Talk; Location: /api/admin/talks/{id}",
    note: "Unknown speaker ids and invalid fields fail with 422 and an error.fields map."
  }),
  window.API_ENDPOINT("GET", "/api/admin/talks/{id}", "admin", "Admin Talks", "Returns one talk including unpublished content.", {
    res: [[200, "Talk"]],
    err: ["400 invalid_id", "404 not_found"],
    shape: "Talk",
    ex: "/api/admin/talks/22222222-3333-4444-5555-666666666666"
  }),
  window.API_ENDPOINT("PATCH", "/api/admin/talks/{id}", "admin", "Admin Talks", "Updates selected talk fields and returns the complete talk.", {
    b: "TalkPatch",
    payload: {published: true},
    res: [[200, "Updated Talk"]],
    err: ["400 invalid_id", "400 invalid_body", "404 not_found", "409 conflict (duplicate title, date, location)", "422 validation_failed"],
    shape: "Talk",
    ex: "/api/admin/talks/22222222-3333-4444-5555-666666666666",
    note: "At least one field is required. Omitted fields retain their values. Null clears only stream_url and poster_url. speaker_ids replaces the entire speaker set. Speakers added by the patch must be registered accounts, while speakers already on the talk may be kept after deleting their account."
  }),
  window.API_ENDPOINT("DELETE", "/api/admin/talks/{id}", "admin", "Admin Talks", "Deletes a talk and its speaker associations. People are retained.", {
    res: [[204, "Talk deleted"]],
    err: ["400 invalid_id", "404 not_found", "500 internal_error"],
    shape: "No response body.",
    ex: "/api/admin/talks/22222222-3333-4444-5555-666666666666"
  }),
  window.API_ENDPOINT("GET", "/api/admin/competitions", "admin", "Admin Competitions", "Lists competitions for admins using the same ordering as the public list.", {
    q: pageQuery,
    res: [[200, "Paginated competition list"]],
    err: pageErr,
    shape: "data: Competition[]; pagination: { limit, offset, has_more }",
    ex: "/api/admin/competitions?limit=20&offset=0"
  }),
  window.API_ENDPOINT("POST", "/api/admin/competitions", "admin", "Admin Competitions", "Creates a competition and returns it with a Location header.", {
    b: "CompetitionCreate",
    payload: {name: "Spring Cup", description: "A one-day algorithm competition for student teams.", date: "2026-05-09T10:00:00", location: "Campus Norte", team_size: 3},
    res: [[201, "Competition created"]],
    err: ["409 conflict (duplicate name, date)", "422 validation_failed"],
    shape: "Competition; Location: /api/admin/competitions/{id}"
  }),
  window.API_ENDPOINT("GET", "/api/admin/competitions/{id}", "admin", "Admin Competitions", "Returns a competition detail object with its teams. This route is not a bare Competition response.", {
    res: [[200, "Competition detail"]],
    err: ["400 invalid_id", "404 not_found"],
    shape: "CompetitionDetail: { competition: Competition, teams: Team[] }",
    ex: "/api/admin/competitions/33333333-4444-5555-6666-777777777777"
  }),
  window.API_ENDPOINT("PATCH", "/api/admin/competitions/{id}", "admin", "Admin Competitions", "Updates selected competition fields and returns a bare Competition.", {
    b: "CompetitionPatch",
    payload: {team_size: 4},
    res: [[200, "Updated Competition"]],
    err: ["400 invalid_id", "400 invalid_body", "404 not_found", "409 conflict (duplicate name, date)", "422 validation_failed"],
    shape: "Competition",
    ex: "/api/admin/competitions/33333333-4444-5555-6666-777777777777",
    note: "Lowering team_size below an existing team's member count fails with 422."
  }),
  window.API_ENDPOINT("DELETE", "/api/admin/competitions/{id}", "admin", "Admin Competitions", "Deletes a competition and cascades its teams and memberships. People are retained.", {
    res: [[204, "Competition deleted"]],
    err: ["400 invalid_id", "404 not_found", "500 internal_error"],
    shape: "No response body.",
    ex: "/api/admin/competitions/33333333-4444-5555-6666-777777777777"
  }),
  window.API_ENDPOINT("GET", "/api/admin/competitions/{id}/teams", "admin", "Admin Teams", "Lists teams for a competition ordered by position ascending with null positions last, then id ascending.", {
    q: pageQuery,
    res: [[200, "Paginated team list"]],
    err: ["400 invalid_id", "400 invalid_pagination", "404 not_found"],
    shape: "data: Team[]; pagination: { limit, offset, has_more }",
    ex: "/api/admin/competitions/33333333-4444-5555-6666-777777777777/teams?limit=20&offset=0",
    note: "The {id} path value is the competition id. A missing competition returns 404."
  }),
  window.API_ENDPOINT("POST", "/api/admin/competitions/{id}/teams", "admin", "Admin Teams", "Creates a team in a competition and returns it with a Location header.", {
    b: "TeamCreate",
    payload: {member_ids: ["11111111-2222-3333-4444-555555555555"], name: "Team Turing", photo_url: null, position: 1},
    res: [[201, "Team created"]],
    err: ["400 invalid_id", "404 not_found", "409 conflict (duplicate team name or person already on another team in this competition)", "422 validation_failed"],
    shape: "Team; Location: /api/admin/teams/{newTeamId}",
    ex: "/api/admin/competitions/33333333-4444-5555-6666-777777777777/teams",
    note: "Missing, invalid or too many members, or a nonexistent person, fail with 422."
  }),
  window.API_ENDPOINT("GET", "/api/admin/teams/{id}", "admin", "Admin Teams", "Returns one team with its members.", {
    res: [[200, "Team"]],
    err: ["400 invalid_id", "404 not_found"],
    shape: "Team",
    ex: "/api/admin/teams/44444444-5555-6666-7777-888888888888"
  }),
  window.API_ENDPOINT("PATCH", "/api/admin/teams/{id}", "admin", "Admin Teams", "Updates selected team fields and returns the complete team.", {
    b: "TeamPatch",
    payload: {name: "Team Hopper", member_ids: ["11111111-2222-3333-4444-555555555555"]},
    res: [[200, "Updated Team"]],
    err: ["400 invalid_id", "400 invalid_body", "404 not_found", "409 conflict (duplicate team name or person already on another team in this competition)", "422 validation_failed"],
    shape: "Team",
    ex: "/api/admin/teams/44444444-5555-6666-7777-888888888888",
    note: "At least one field is required. Null clears name, photo_url and position. member_ids is nonnullable and replaces the entire member set."
  }),
  window.API_ENDPOINT("DELETE", "/api/admin/teams/{id}", "admin", "Admin Teams", "Deletes a team and its member associations. People are retained.", {
    res: [[204, "Team deleted"]],
    err: ["400 invalid_id", "404 not_found", "500 internal_error"],
    shape: "No response body.",
    ex: "/api/admin/teams/44444444-5555-6666-7777-888888888888"
  }),
  window.API_ENDPOINT("GET", "/api/admin/users", "admin", "Admin Users", "Lists users for admins, ordered by id, including people without a login.", {
    q: [["q", "string", "Optional case-insensitive literal substring of first_name, last_name, full name or email. Trimmed valid UTF-8, at most 200 bytes, one value."], ...pageQuery],
    res: [[200, "Paginated user list"]],
    err: ["400 invalid_query", "400 invalid_pagination"],
    shape: "data: User[]; pagination: { limit, offset, has_more }",
    ex: "/api/admin/users?q=ada&limit=20&offset=0",
    note: "q uses literal substring matching, not SQL wildcards. Users without a login have email null."
  }),
  window.API_ENDPOINT("PATCH", "/api/admin/users/{id}", "admin", "Admin Users", "Updates only the role of a user; identity fields are unchanged.", {
    b: "RoleUpdate",
    payload: {role: "admin"},
    res: [[200, "Updated User"]],
    err: ["400 invalid_id", "404 not_found", "409 conflict", "422 validation_failed"],
    shape: "User",
    ex: "/api/admin/users/55555555-6666-7777-8888-999999999999",
    note: "The body must contain role set to member or admin. This is how admin is granted normally: authorization checks only the stored account role, so an admin can grant admin to any existing user even if that email is not on ADMIN_EMAILS. ADMIN_EMAILS is an optional allowlist applied only at login as a one-time bootstrap: the first login with a listed, authoritative Google email (@gmail.com or a Workspace hosted domain) consumes a hashed-email marker and becomes admin, with no re-promotion after the marker is consumed or after a demotion. Removing an entry demotes nobody. The last remaining admin cannot be demoted and cannot delete their account; both requests answer 409 conflict. Only an account that can sign in can be admin: promoting a deleted or never-registered person answers 422 validation_failed with field role."
  })
);
