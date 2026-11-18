(function () {
  const data = window.API_DATA;
  if (!data || !Array.isArray(data.endpoints)) return;
  const endpoints = data.endpoints;
  const byId = new Map();
  const cards = new Map();
  const methodClass = {GET: "get", POST: "post", PATCH: "patch", DELETE: "delete"};
  const accessLabel = {public: "Public", user: "Signed in", admin: "Admin"};
  const $ = function (id) { return document.getElementById(id); };
  const esc = function (value) {
    return String(value).replace(/[&<>"']/g, function (character) {
      return {"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"}[character];
    });
  };
  const code = function (value) { return "<code>" + esc(value) + "</code>"; };
  const slug = function (value) { return String(value).toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, ""); };
  const endpointId = function (ep) { return ep.method.toLowerCase() + "-" + slug(ep.path); };
  const requestId = function (name) { return "req-" + slug(name); };
  const modelNames = (data.models || []).map(function (model) { return model.name; }).sort(function (a, b) { return b.length - a.length; });
  const shapeHtml = function (text) {
    const escaped = esc(text);
    if (!modelNames.length) return escaped;
    const pattern = new RegExp("\\b(" + modelNames.join("|") + ")\\b", "g");
    return escaped.replace(pattern, function (name) { return '<a href="#model-' + slug(name) + '">' + name + "</a>"; });
  };
  const table = function (headers, rows) {
    return '<div class="table-wrap"><table><thead><tr>' + headers.map(function (header) { return "<th>" + esc(header) + "</th>"; }).join("") + "</tr></thead><tbody>" + rows.map(function (row) { return "<tr>" + row.map(function (cell) { return "<td>" + cell + "</td>"; }).join("") + "</tr>"; }).join("") + "</tbody></table></div>";
  };
  const queryTable = function (query) {
    return table(["Parameter", "Type", "Notes"], query.map(function (item) { return [code(item[0]), esc(item[1]), esc(item[2])]; }));
  };
  const fieldTable = function (fields) {
    return table(["Field", "Type", "Required", "Notes"], fields.map(function (item) { return [code(item[0]), code(item[1]), esc(item[2]), esc(item[3])]; }));
  };
  const responseTable = function (responses) {
    return table(["Status", "Meaning"], responses.map(function (item) { return [code(item[0]), esc(item[1])]; }));
  };
  const renderCard = function (ep, index) {
    const id = endpointId(ep);
    const method = methodClass[ep.method];
    const requestLink = ep.body ? '<p class="hint">Request model: <a href="#' + requestId(ep.body) + '">' + code(ep.body) + "</a></p>" : "";
    const query = ep.query.length ? "<h4>Query parameters</h4>" + queryTable(ep.query) : "";
    const body = ep.body ? "<h4>Request body</h4>" + requestLink + fieldTable(ep.bodyFields) : "";
    const responses = ep.responses.length ? "<h4>Responses</h4>" + responseTable(ep.responses) : "";
    const shape = ep.shape ? '<div class="shape"><span>Response shape</span><code>' + shapeHtml(ep.shape) + "</code></div>" : "";
    const errors = ep.errors.length ? '<h4>Endpoint errors</h4><ul class="error-list">' + ep.errors.map(function (item) { return "<li>" + code(item) + "</li>"; }).join("") + "</ul>" : "";
    const note = ep.note ? '<p class="callout">' + esc(ep.note) + "</p>" : "";
    const copy = '<div class="curl"><div class="curl-head"><span>cURL example</span><button class="copy-btn" type="button" data-copy="' + id + '">Copy</button></div><pre id="curl-' + id + '"><code>' + esc(ep.curl) + "</code></pre></div>";
    return '<article class="endpoint" id="' + id + '" aria-labelledby="' + id + '-title" data-index="' + index + '"><div class="endpoint-head"><div class="endpoint-title"><span class="method method-' + method + '">' + ep.method + '</span><h3 id="' + id + '-title"><code>' + esc(ep.path) + '</code></h3></div><div class="badges"><span class="badge access-' + ep.access + '">' + accessLabel[ep.access] + '</span><span class="badge category">' + esc(ep.category) + "</span></div></div>" + '<p class="endpoint-desc">' + esc(ep.description) + "</p>" + query + body + responses + shape + errors + note + copy + '<p class="hint">' + esc(data.curlNote) + "</p></article>";
  };
  const renderNav = function () {
    const groups = data.categories.map(function (category) {
      return [category, endpoints.filter(function (ep) { return ep.category === category; })];
    }).filter(function (group) { return group[1].length > 0; });
    $("endpoint-nav").innerHTML = groups.map(function (group) {
      const links = group[1].map(function (ep) {
        return '<li><a href="#' + endpointId(ep) + '"><span class="nav-method method-' + methodClass[ep.method] + '">' + ep.method + '</span><span class="nav-path">' + esc(ep.path) + "</span></a></li>";
      }).join("");
      return '<div class="nav-group"><p class="nav-category">' + esc(group[0]) + " <span>" + group[1].length + "</span></p><ul>" + links + "</ul></div>";
    }).join("");
  };
  const renderModels = function () {
    $("model-list").innerHTML = data.models.map(function (model) {
      const fields = table(["Field", "Type", "Notes"], model.fields.map(function (item) { return [code(item[0]), code(item[1]), esc(item[2])]; }));
      return '<article class="model" id="model-' + slug(model.name) + '"><div class="card-head"><h3>' + esc(model.name) + '</h3><span class="badge category">' + model.fields.length + ' fields</span></div><p>' + esc(model.summary) + "</p>" + fields + '<div class="sample"><p class="sample-title">Sample JSON</p><pre><code>' + esc(JSON.stringify(model.sample, null, 2)) + "</code></pre></div></article>";
    }).join("");
  };
  const renderRequests = function () {
    $("request-list").innerHTML = data.requests.map(function (request) {
      const rules = request.rules.length ? '<ul class="rule-list">' + request.rules.map(function (rule) { return "<li>" + esc(rule) + "</li>"; }).join("") + "</ul>" : "";
      return '<article class="request" id="' + requestId(request.name) + '"><div class="card-head"><h3>' + esc(request.name) + '</h3><span class="badge category">Request body</span></div><p>' + esc(request.summary) + "</p>" + fieldTable(request.fields) + rules + "</article>";
    }).join("");
  };
  const renderErrors = function () {
    $("error-table").innerHTML = table(["Status", "Code", "Meaning"], data.errorCodes.map(function (item) { return [code(item[0]), code(item[1]), esc(item[2])]; }));
    $("oauth-table").innerHTML = table(["Status", "Code", "Meaning"], data.oauthCodes.map(function (item) { return [code(item[0]), code(item[1]), esc(item[2])]; }));
  };
  const state = {q: "", category: "all", access: "all"};
  const matches = function (ep) {
    const query = state.q.trim().toLowerCase();
    const haystack = (ep.method + " " + ep.path + " " + ep.description).toLowerCase();
    return (!query || haystack.indexOf(query) !== -1) && (state.category === "all" || ep.category === state.category) && (state.access === "all" || ep.access === state.access);
  };
  const applyFilters = function (scroll) {
    let visible = 0;
    endpoints.forEach(function (ep) {
      const card = cards.get(endpointId(ep));
      const show = matches(ep);
      card.classList.toggle("is-filtered", !show);
      card.setAttribute("aria-hidden", String(!show));
      if (show) visible += 1;
    });
    $("result-count").textContent = visible === endpoints.length ? endpoints.length + " endpoints" : visible + " of " + endpoints.length + " endpoints";
    $("empty-state").hidden = visible !== 0;
    if (scroll) $("endpoints").scrollIntoView({behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth", block: "start"});
  };
  const resetFilters = function (focus) {
    state.q = "";
    state.category = "all";
    state.access = "all";
    $("search").value = "";
    $("category-filter").value = "all";
    $("access-filter").value = "all";
    applyFilters();
    if (focus) $("search").focus();
  };
  const reveal = function (id) {
    const ep = byId.get(id);
    if (!ep) return;
    const card = cards.get(id);
    if (card.classList.contains("is-filtered")) resetFilters(false);
    card.classList.remove("flash");
    void card.offsetWidth;
    card.classList.add("flash");
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    card.scrollIntoView({behavior: reduced ? "auto" : "smooth", block: "start"});
    window.setTimeout(function () { card.classList.remove("flash"); }, 1800);
  };
  const handleHash = function () {
    let id = window.location.hash.slice(1);
    if (!id) return;
    try { id = decodeURIComponent(id); } catch (error) { return; }
    reveal(id);
  };
  const closeNav = function () {
    $("nav-toggle").setAttribute("aria-expanded", "false");
    $("sidebar").classList.remove("is-open");
  };
  const writeClipboard = async function (text) {
    try {
      if (!navigator.clipboard || !window.isSecureContext) throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText(text);
      return;
    } catch (error) {
      const area = document.createElement("textarea");
      area.value = text;
      area.setAttribute("readonly", "");
      area.style.position = "fixed";
      area.style.top = "-1000px";
      document.body.appendChild(area);
      try {
        area.select();
        area.setSelectionRange(0, area.value.length);
        if (typeof document.execCommand !== "function" || !document.execCommand("copy")) throw new Error("copy failed");
      } finally {
        area.remove();
      }
    }
  };
  endpoints.forEach(function (ep) { byId.set(endpointId(ep), ep); });
  renderNav();
  renderModels();
  renderRequests();
  renderErrors();
  $("endpoint-list").innerHTML = endpoints.map(renderCard).join("");
  endpoints.forEach(function (ep) { cards.set(endpointId(ep), document.getElementById(endpointId(ep))); });
  $("endpoint-count").textContent = endpoints.length;
  $("nav-count").textContent = endpoints.length;
  $("search").addEventListener("input", function (event) { state.q = event.target.value; applyFilters(true); });
  $("search").addEventListener("keydown", function (event) { if (event.key === "Escape") { event.target.value = ""; state.q = ""; applyFilters(true); } });
  $("category-filter").addEventListener("change", function (event) { state.category = event.target.value; applyFilters(true); });
  $("access-filter").addEventListener("change", function (event) { state.access = event.target.value; applyFilters(true); });
  $("empty-reset").addEventListener("click", function () { resetFilters(true); });
  $("nav-toggle").addEventListener("click", function () {
    const open = $("nav-toggle").getAttribute("aria-expanded") !== "true";
    $("nav-toggle").setAttribute("aria-expanded", String(open));
    $("sidebar").classList.toggle("is-open", open);
  });
  $("sidebar").addEventListener("click", function (event) {
    const link = event.target.closest("a[href^='#']");
    if (!link) return;
    if (window.matchMedia("(max-width: 900px)").matches) closeNav();
    const target = link.getAttribute("href").slice(1);
    if (target && window.location.hash.slice(1) === target && byId.has(target)) reveal(target);
  });
  document.addEventListener("keydown", function (event) { if (event.key === "Escape") closeNav(); });
  document.addEventListener("click", async function (event) {
    const button = event.target.closest(".copy-btn");
    if (!button) return;
    const target = document.getElementById("curl-" + button.dataset.copy);
    if (!target) return;
    try {
      await writeClipboard(target.textContent);
      button.textContent = "Copied";
    } catch (error) {
      button.textContent = "Copy failed";
    }
    window.setTimeout(function () { button.textContent = "Copy"; }, 1600);
  });
  applyFilters();
  window.addEventListener("hashchange", handleHash);
  window.requestAnimationFrame(handleHash);
})();
