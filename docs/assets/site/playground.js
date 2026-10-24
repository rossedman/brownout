// Playground: mirrors Rule.Validate, matchPath and Controller.Check from
// brownout.go so rules can be tried in the browser. Keep it in step with the
// Go code when matching or validation changes. Runs on Material's document$
// so it survives instant navigation.
(function () {
  var defaultRules = {
    rules: [
      { id: "health", path: "/api/health", allow_percent: 100 },
      { id: "readonly", path: "/api/*", methods: ["POST", "PUT", "PATCH", "DELETE"], retry_after_seconds: 60 },
      { id: "search", path: "/search", allow_percent: 20 },
      { id: "reports", path: "/reports/*", status_code: 410, message: "Reports are paused during the incident." }
    ]
  };

  var STATUS_TEXT = {
    200: "OK", 403: "Forbidden", 404: "Not Found", 410: "Gone", 418: "I'm a teapot",
    429: "Too Many Requests", 451: "Unavailable For Legal Reasons", 500: "Internal Server Error",
    502: "Bad Gateway", 503: "Service Unavailable", 504: "Gateway Timeout"
  };

  function esc(s) {
    return String(s).replace(/[&<>"]/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c];
    });
  }

  function validate(r) {
    var p = r.path;
    if (!r.id) return "rule id is required";
    if (!p) return "rule " + r.id + ": path is required";
    if (p !== "*" && p.charAt(0) !== "/") return 'rule ' + r.id + ': path "' + p + '" must start with "/" or be "*"';
    if (p.replace(/\*$/, "").indexOf("*") >= 0) return 'rule ' + r.id + ': path "' + p + '" may only contain "*" at the end';
    var a = r.allow_percent == null ? 0 : r.allow_percent;
    if (typeof a !== "number" || a < 0 || a > 100) return "rule " + r.id + ": allow_percent must be between 0 and 100";
    var s = r.status_code == null ? 0 : r.status_code;
    if (s !== 0 && (s < 100 || s > 599)) return "rule " + r.id + ": status_code " + s + " is not a valid HTTP status";
    if ((r.retry_after_seconds || 0) < 0) return "rule " + r.id + ": retry_after_seconds must not be negative";
    if (r.ttl && r.expires_at) return "rule " + r.id + ": set either ttl or expires_at, not both";
    return null;
  }

  function matchPath(pattern, path) {
    return pattern.slice(-1) === "*" ? path.indexOf(pattern.slice(0, -1)) === 0 : pattern === path;
  }

  function expired(r) {
    return !!r.expires_at && !r.ttl && Date.parse(r.expires_at) <= Date.now();
  }

  function init() {
    var rulesEl = document.getElementById("pg-rules");
    if (!rulesEl) return;
    var methodEl = document.getElementById("pg-method");
    var pathEl = document.getElementById("pg-path");
    var resultEl = document.getElementById("pg-result");
    // Pretty-print, but keep short arrays such as methods on one line.
    rulesEl.value = JSON.stringify(defaultRules, null, 2).replace(/\[\s+([^\[\]{}]*?)\s+\]/g, function (_, items) {
      return "[" + items.split(/,\s+/).join(", ") + "]";
    });

    function render(res) {
      if (res.kind === "err") {
        resultEl.className = "pg__result pg__result--err";
        resultEl.innerHTML = '<div class="pg__status">400 rejected</div><p>' + esc(res.msg) +
          '</p><p class="pg__muted">The admin API would return 400 and leave the current rules unchanged.</p>';
        return;
      }
      var trace = '<ul class="pg__trace">' + res.trace.map(function (t) {
        return '<li class="' + (t.hit ? "is-hit" : "") + '">' + esc(t.text) + "</li>";
      }).join("") + "</ul>";
      if (res.kind === "none") {
        resultEl.className = "pg__result pg__result--ok";
        resultEl.innerHTML = '<div class="pg__status">→ your handler</div><p>No rule matches, so the request passes through untouched.</p>' + trace;
        return;
      }
      var r = res.rule;
      var allow = r.allow_percent || 0;
      var status = r.status_code || 503;
      var text = STATUS_TEXT[status] || "";
      var head = ["HTTP/1.1 " + status + (text ? " " + text : "")];
      if (r.retry_after_seconds > 0) head.push("Retry-After: " + r.retry_after_seconds);
      head.push("Content-Type: text/plain; charset=utf-8", "", r.message || text);
      var shed = '<pre class="pg__resp">' + esc(head.join("\n")) + "</pre>";
      var id = "<code>" + esc(r.id) + "</code>";
      if (allow >= 100) {
        resultEl.className = "pg__result pg__result--ok";
        resultEl.innerHTML = '<div class="pg__status">→ your handler</div><p>Rule ' + id + " matches and allows 100%, so later rules are never checked.</p>" + trace;
      } else if (allow > 0) {
        resultEl.className = "pg__result pg__result--warn";
        resultEl.innerHTML = '<div class="pg__status">' + allow + "% pass · " + (100 - allow) + "% shed</div>" +
          "<p>Rule " + id + " matches. About " + allow + "% of these requests reach your handler; the rest get:</p>" +
          '<div class="pg__meter" role="img" aria-label="' + allow + '% allowed through"><div style="width:' + allow + '%"></div></div>' +
          shed + trace;
      } else {
        resultEl.className = "pg__result pg__result--bad";
        resultEl.innerHTML = '<div class="pg__status">' + status + " shed</div><p>Rule " + id + " matches and sheds all matching traffic.</p>" + shed + trace;
      }
    }

    function evaluate() {
      var doc;
      try { doc = JSON.parse(rulesEl.value); } catch (e) { return render({ kind: "err", msg: "Invalid JSON: " + e.message }); }
      var rules = doc && doc.rules;
      if (!Array.isArray(rules)) return render({ kind: "err", msg: 'Expected {"rules": [...]}' });
      var seen = {};
      for (var i = 0; i < rules.length; i++) {
        var err = validate(rules[i] || {});
        if (err) return render({ kind: "err", msg: err });
        if (seen[rules[i].id]) return render({ kind: "err", msg: 'duplicate rule id "' + rules[i].id + '"' });
        seen[rules[i].id] = true;
      }
      var method = methodEl.value;
      var path = pathEl.value || "/";
      var trace = [];
      for (var j = 0; j < rules.length; j++) {
        var r = rules[j];
        var methods = (r.methods || []).map(function (m) { return String(m).toUpperCase(); });
        var skip = null;
        if (methods.length && methods.indexOf(method) < 0) skip = "method " + method + " not in [" + methods.join(", ") + "]";
        else if (!matchPath(r.path, path)) skip = "path doesn't match " + r.path;
        else if (expired(r)) skip = "expired";
        if (skip) { trace.push({ text: "skip  " + r.id + ": " + skip }); continue; }
        trace.push({ hit: true, text: "match " + r.id + ": first match decides" });
        return render({ kind: "match", rule: r, trace: trace });
      }
      render({ kind: "none", trace: trace });
    }

    rulesEl.addEventListener("input", evaluate);
    pathEl.addEventListener("input", evaluate);
    methodEl.addEventListener("change", evaluate);
    document.getElementById("pg-presets").addEventListener("click", function (e) {
      var b = e.target.closest("button");
      if (!b) return;
      methodEl.value = b.dataset.m;
      pathEl.value = b.dataset.p;
      evaluate();
    });
    evaluate();
  }

  if (window.document$) window.document$.subscribe(init);
  else document.addEventListener("DOMContentLoaded", init);
})();
