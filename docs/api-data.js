window.API_DATA = {
  product: "caus API",
  subtitle: "HTTP reference for the caus platform backend.",
  baseUrl: "http://localhost:8080",
  publicUrl: "http://localhost:8080",
  endpointCount: 33,
  categories: ["Health", "People", "Talks", "Competitions", "Auth", "Account", "Admin Talks", "Admin Competitions", "Admin Teams", "Admin Users"],
  accessLevels: [["all", "All"], ["public", "Public"], ["user", "Signed in"], ["admin", "Admin"]],
  endpoints: [],
  curlNote: "Example values only. A local frontend's PUBLIC_URL may be another localhost port and must match the actual configured value or proxy. Replace illustrative UUIDs and cookie placeholders with real values."
};
const SAMPLE_PERSON = {id: "11111111-2222-3333-4444-555555555555", first_name: "Ada", last_name: "Lovelace"};
const SAMPLE_TALK = {id: "22222222-3333-4444-5555-666666666666", title: "Constraint solving in practice", description: "A practical tour of constraint models and search.", date: "2026-03-12T18:30:00", duration: 3600, location: "Aula 3", language: "es", stream_url: null, poster_url: "https://example.com/poster.jpg", published: true, speakers: [SAMPLE_PERSON], created_at: "2026-01-10T09:00:00Z", updated_at: "2026-02-01T12:15:00Z"};
const SAMPLE_COMPETITION = {id: "33333333-4444-5555-6666-777777777777", name: "Spring Cup", description: "A one-day algorithm competition for student teams.", date: "2026-05-09T10:00:00", location: "Campus Norte", team_size: 3, created_at: "2026-01-15T10:00:00Z", updated_at: "2026-02-02T11:30:00Z"};
const SAMPLE_TEAM = {id: "44444444-5555-6666-7777-888888888888", competition_id: SAMPLE_COMPETITION.id, name: "Team Turing", photo_url: null, position: 1, members: [SAMPLE_PERSON], created_at: "2026-02-03T09:30:00Z", updated_at: "2026-02-03T09:30:00Z"};
const SAMPLE_ACCOUNT = {id: "55555555-6666-7777-8888-999999999999", first_name: "Ada", last_name: "Lovelace", email: "ada@example.com", role: "member", created_at: "2026-01-05T08:00:00Z", updated_at: "2026-02-04T16:45:00Z"};
const SAMPLE_USER = {id: SAMPLE_ACCOUNT.id, first_name: "Ada", last_name: "Lovelace", email: null, role: "member", created_at: "2026-01-05T08:00:00Z", updated_at: "2026-02-04T16:45:00Z"};
window.API_DATA.models = [
  {name: "Person", summary: "Public person identity. No private account fields are exposed. A missing family name is stored as an empty last name, and login never refreshes stored names.", fields: [["id", "uuid", "Nonzero UUID."], ["first_name", "string", "Given name."], ["last_name", "string", "Family name; an empty string when the person has none. Names are copied from Google only at the first sign-in and afterwards only the person changes them."]], sample: SAMPLE_PERSON},
  {name: "Talk", summary: "A published or draft talk. Dates are Europe/Madrid wall-clock values.", fields: [["id", "uuid", "Nonzero UUID."], ["title", "string", "Talk title."], ["description", "string", "Talk description."], ["date", "string", "Exactly YYYY-MM-DDTHH:MM:SS, Europe/Madrid wall clock."], ["duration", "integer", "Whole seconds, 1..9223372036."], ["location", "string", "Room or venue."], ["language", "string", "es or en."], ["stream_url", "string|null", "Null when absent."], ["poster_url", "string|null", "Null when absent."], ["published", "boolean", "Drafts are hidden from public reads."], ["speakers", "Person[]", "Ordered speaker list."], ["created_at", "string", "UTC RFC3339 timestamp."], ["updated_at", "string", "UTC RFC3339 timestamp."]], sample: SAMPLE_TALK},
  {name: "Competition", summary: "A competition with a team size limit.", fields: [["id", "uuid", "Nonzero UUID."], ["name", "string", "Competition name."], ["description", "string", "Competition description."], ["date", "string", "Exactly YYYY-MM-DDTHH:MM:SS, Europe/Madrid wall clock."], ["location", "string", "Venue."], ["team_size", "integer", "1..32767."], ["created_at", "string", "UTC RFC3339 timestamp."], ["updated_at", "string", "UTC RFC3339 timestamp."]], sample: SAMPLE_COMPETITION},
  {name: "Team", summary: "A team registered in one competition.", fields: [["id", "uuid", "Nonzero UUID."], ["competition_id", "uuid", "Owning competition."], ["name", "string|null", "Null when unnamed."], ["photo_url", "string|null", "Null when absent."], ["position", "integer|null", "Whole integer 1..32767, or null."], ["members", "Person[]", "Public member identities."], ["created_at", "string", "UTC RFC3339 timestamp."], ["updated_at", "string", "UTC RFC3339 timestamp."]], sample: SAMPLE_TEAM},
  {name: "Account", summary: "The signed-in account.", fields: [["id", "uuid", "Nonzero UUID."], ["first_name", "string", "Given name."], ["last_name", "string", "Family name; an empty string when the person has none. Names are copied from Google only at the first sign-in and afterwards only the person changes them."], ["email", "string", "Account email; never null."], ["role", "string", "member or admin."], ["created_at", "string", "UTC RFC3339 timestamp."], ["updated_at", "string", "UTC RFC3339 timestamp."]], sample: SAMPLE_ACCOUNT},
  {name: "User", summary: "An admin-facing user record. People without a login have a null email.", fields: [["id", "uuid", "Nonzero UUID."], ["first_name", "string", "Given name."], ["last_name", "string", "Family name; an empty string when the person has none. Names are copied from Google only at the first sign-in and afterwards only the person changes them."], ["email", "string|null", "Null for people without a login."], ["role", "string", "member or admin."], ["created_at", "string", "UTC RFC3339 timestamp."], ["updated_at", "string", "UTC RFC3339 timestamp."]], sample: SAMPLE_USER},
  {name: "CompetitionDetail", summary: "Competition detail response with its teams.", fields: [["competition", "Competition", "Competition object."], ["teams", "Team[]", "Teams in this competition."]], sample: {competition: SAMPLE_COMPETITION, teams: [SAMPLE_TEAM]}},
  {name: "AccountExport", summary: "Account data export. Session token hashes and cookies are never included.", fields: [["account", "Account", "Account object."], ["google_sub", "string", "Durable Google subject identifier."], ["sessions", "{created_at,expires_at}[]", "Session metadata only."], ["talks", "Talk[]", "Associated talks, including own unpublished talks."], ["teams", "Team[]", "Associated teams."], ["competitions", "Competition[]", "Associated competitions."]], sample: {account: SAMPLE_ACCOUNT, google_sub: "123456789012345678901", sessions: [{created_at: "2026-02-04T16:40:00Z", expires_at: "2026-03-06T16:40:00Z"}], talks: [SAMPLE_TALK], teams: [SAMPLE_TEAM], competitions: [SAMPLE_COMPETITION]}},
  {name: "Pagination", summary: "Page metadata returned beside every paginated data array.", fields: [["limit", "integer", "1..100; default 20."], ["offset", "integer", "0 or greater; default 0."], ["has_more", "boolean", "True when another page exists."]], sample: {limit: 20, offset: 0, has_more: false}}
];
window.API_DATA.requests = [
  {name: "TalkCreate", summary: "Create a talk. All required strings must be nonblank valid Unicode; U+FEFF is rejected anywhere in text.", fields: [["title", "string", "required", "Nonblank valid Unicode."], ["description", "string", "required", "Nonblank valid Unicode."], ["date", "string", "required", "YYYY-MM-DDTHH:MM:SS Europe/Madrid wall clock; end must be representable in years 1..9999."], ["duration", "integer", "required", "Whole seconds 1..9223372036."], ["location", "string", "required", "Nonblank valid Unicode."], ["speaker_ids", "uuid[]", "required", "Nonempty, distinct, nonzero UUIDs of existing people."], ["language", "string", "optional", "es or en; default es."], ["published", "boolean", "optional", "Default false."], ["stream_url", "string|null", "optional", "Valid http(s) URL with host; optional port 1..65535; no userinfo, whitespace, control characters or backslashes."], ["poster_url", "string|null", "optional", "Same URL rules as stream_url."]], rules: ["Unknown fields are rejected.", "Speaker ids replace the whole speaker set on update; an empty array is invalid."]},
  {name: "TalkPatch", summary: "Patch selected talk fields. At least one writable field is required.", fields: [["title", "string", "optional", "Nonblank valid Unicode."], ["description", "string", "optional", "Nonblank valid Unicode."], ["date", "string", "optional", "YYYY-MM-DDTHH:MM:SS Europe/Madrid wall clock."], ["duration", "integer", "optional", "Whole seconds 1..9223372036."], ["location", "string", "optional", "Nonblank valid Unicode."], ["language", "string", "optional", "es or en."], ["published", "boolean", "optional", "Publish or unpublish the talk."], ["stream_url", "string|null", "optional", "Null clears; a string must satisfy the URL rules."], ["poster_url", "string|null", "optional", "Null clears; a string must satisfy the URL rules."], ["speaker_ids", "uuid[]", "optional", "Nonempty, distinct, existing people; replaces the entire set."]], rules: ["Omitted fields retain their current values.", "Null clears only stream_url and poster_url; other fields reject null.", "An empty {} patch fails with 422 and fields.patch."]},
  {name: "CompetitionCreate", summary: "Create a competition.", fields: [["name", "string", "required", "Nonblank valid Unicode."], ["description", "string", "required", "Nonblank valid Unicode."], ["date", "string", "required", "YYYY-MM-DDTHH:MM:SS Europe/Madrid wall clock; year 1..9999."], ["location", "string", "required", "Nonblank valid Unicode."], ["team_size", "integer", "optional", "1..32767; default 3."]], rules: ["A duplicate competition (name, date) pair is rejected with 409.", "This endpoint creates a competition only; teams are created with POST /api/admin/competitions/{id}/teams."]},
  {name: "CompetitionPatch", summary: "Patch selected competition fields. At least one field is required.", fields: [["name", "string", "optional", "Nonblank valid Unicode."], ["description", "string", "optional", "Nonblank valid Unicode."], ["date", "string", "optional", "YYYY-MM-DDTHH:MM:SS Europe/Madrid wall clock."], ["location", "string", "optional", "Nonblank valid Unicode."], ["team_size", "integer", "optional", "1..32767; lowering it must not invalidate existing teams."]], rules: ["No competition patch field is nullable.", "Lowering team_size below an existing team's member count fails with 422."]},
  {name: "TeamCreate", summary: "Create a team inside a competition.", fields: [["member_ids", "uuid[]", "required", "Distinct, nonzero, existing people; count 1..competition.team_size."], ["name", "string|null", "optional", "Nonblank if set; unique within the competition."], ["photo_url", "string|null", "optional", "Valid http(s) URL or null."], ["position", "integer|null", "optional", "Whole integer 1..32767 or null."]], rules: ["A person may join only one team per competition.", "Missing, invalid or too many members fail with 422."]},
  {name: "TeamPatch", summary: "Patch selected team fields. At least one field is required.", fields: [["name", "string|null", "optional", "Null clears; nonblank string otherwise."], ["photo_url", "string|null", "optional", "Null clears; valid http(s) URL otherwise."], ["position", "integer|null", "optional", "Null clears; whole integer 1..32767 otherwise."], ["member_ids", "uuid[]", "optional", "Nonnullable; replaces the entire member set."]], rules: ["Omitting member_ids leaves members unchanged.", "member_ids must stay within 1..competition.team_size."]},
  {name: "RoleUpdate", summary: "Change a user's platform role.", fields: [["role", "string", "required", "member or admin."]], rules: ["Only role changes; identity fields are untouched.", "The target user must already exist."]}
];
window.API_DATA.errorCodes = [
  ["400", "invalid_body", "Malformed JSON, unknown fields, trailing JSON, empty body, or a body that is not one non-null object."],
  ["400", "invalid_id", "A path id is not a valid UUID, or the zero UUID was supplied to any route except GET /api/people/{id}, which parses zero and follows the normal lookup."],
  ["400", "invalid_filter", "A present period value is empty, is not all, upcoming or past, or is repeated. Omitting period defaults to all and is not an error."],
  ["400", "invalid_query", "The q value is repeated, invalid UTF-8, or longer than 200 bytes after trimming."],
  ["400", "invalid_pagination", "limit or offset is repeated, non-integer, limit is outside 1..100, or offset is negative."],
  ["400", "invalid_return_to", "return_to is absolute, protocol-relative, contains backslashes or control characters, is encoded unsafely, is repeated, or exceeds 2048 UTF-8 bytes."],
  ["400", "cors_headers_denied", "A preflight requests a header other than Accept or Content-Type."],
  ["401", "unauthenticated", "The session cookie is missing, empty, duplicated, malformed, or expired."],
  ["403", "origin_denied", "A write request's Origin is not exactly the canonical configured PUBLIC_URL."],
  ["403", "cors_origin_denied", "The request Origin is not allowed by CORS."],
  ["403", "forbidden", "The authenticated account lacks the required role."],
  ["404", "not_found", "The requested resource does not exist or is not visible."],
  ["404", "person_not_found", "The legacy people route found no person for the id."],
  ["405", "method_not_allowed", "The route exists but does not accept this method; Allow is returned."],
  ["409", "conflict", "A uniqueness or team-membership constraint would be violated."],
  ["413", "request_too_large", "The JSON body exceeds 64 KiB."],
  ["415", "unsupported_media_type", "Content-Type is missing or is not application/json; parameters are allowed."],
  ["422", "validation_failed", "JSON syntax is valid but types, required fields, ranges or referenced entities are invalid. error.fields maps fields to messages."],
  ["429", "rate_limited", "The client exceeded 60 requests in a fixed 60 second window on a route under /api/auth/ or on a request whose method is not GET or HEAD. IPv6 clients are counted per /64 network and IPv4 clients per address."],
  ["500", "internal_error", "An unexpected server failure occurred."],
  ["503", "unavailable", "The readiness database ping failed or timed out."]
];
window.API_DATA.oauthCodes = [
  ["400", "oauth_callback_invalid", "The callback query is malformed or has duplicated parameters, code is missing or blank, or the error and code combination is invalid."],
  ["400", "invalid_oauth_state", "The OAuth flow cookie (__Host-caus_oauth over HTTPS, caus_oauth on HTTP localhost) is missing, duplicated or expired, or state is missing from the callback or does not match the cookie."],
  ["302", "oauth_denied", "The user refused consent: state is valid, error is access_denied and no code is present. Redirects to return_to with auth_error=oauth_denied."],
  ["302", "invalid_identity", "Google's identity token was rejected (issuer, audience, signature, nonce or email verification failed, or the authorization code is invalid, expired or already used), or the identity maps to an invalid local profile: a first name is required, a missing family name is stored as an empty last name. Redirects to return_to with auth_error=invalid_identity."],
  ["302", "identity_conflict", "The identity cannot be bound to the local account without violating a durable binding. Redirects to return_to with auth_error=identity_conflict."],
  ["302", "auth_unavailable", "OIDC discovery or signing-key retrieval is unavailable. Redirects to return_to with auth_error=auth_unavailable. Other token-exchange infrastructure failures return 500."],
  ["500", "internal_error", "An unexpected failure occurred while completing login."]
];
window.API_ENDPOINT = function (method, path, access, category, description, options) {
  const o = options || {};
  const ep = {
    method: method,
    path: path,
    access: access,
    category: category,
    description: description,
    query: o.q || [],
    body: o.b || "",
    responses: o.res || [],
    errors: o.err || [],
    shape: o.shape || "",
    example: o.ex || path,
    payload: o.payload || null,
    note: o.note || "",
    cookie: o.cookie || ""
  };
  const request = (window.API_DATA.requests || []).find(function (item) { return item.name === ep.body; });
  ep.bodyFields = request ? request.fields : [];
  const common = [];
  if (access !== "public") common.push("401 unauthenticated");
  if (access === "admin") common.push("403 forbidden");
  if (method !== "GET") common.push("403 origin_denied");
  if (o.b) common.push("400 invalid_body", "413 request_too_large", "415 unsupported_media_type");
  ep.errors = ep.errors.concat(common).filter(function (item, index, list) { return list.indexOf(item) === index; });
  const lines = ["BASE_URL=" + window.API_DATA.baseUrl, "PUBLIC_URL=" + window.API_DATA.publicUrl];
  const args = ['curl -i "$BASE_URL' + ep.example + '"'];
  if (ep.method !== "GET") args[0] += " -X " + ep.method;
  if (ep.access !== "public") args.push("-b 'caus_session=YOUR_SESSION_COOKIE'");
  if (ep.cookie) args.push("-b '" + ep.cookie + "'");
  if (ep.method !== "GET") args.push('-H "Origin: $PUBLIC_URL"');
  if (ep.payload) {
    args.push('-H "Content-Type: application/json"');
    args.push("-d '" + JSON.stringify(ep.payload) + "'");
  }
  lines.push(args.join(" \\\n  "));
  ep.curl = lines.join("\n");
  return ep;
};
