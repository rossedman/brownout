---
title: Playground
hide:
  - toc
---

# Playground

Edit the rules and the request to see what the middleware would do with it. The page runs the library's matching logic in your browser: validation, path and method matching, expiry, and first-match-wins ordering.

<div class="pg" markdown="0">
  <div class="pg__rules">
    <label for="pg-rules">Rules: the same JSON as <code>PUT /rules</code></label>
    <textarea id="pg-rules" spellcheck="false" aria-describedby="pg-rules-help"></textarea>
    <p id="pg-rules-help" class="pg__help">Rules are checked top to bottom. Move one up to see the first match change.</p>
  </div>
  <div class="pg__req">
    <label for="pg-path">Request</label>
    <div class="pg__line">
      <select id="pg-method" aria-label="Method">
        <option>GET</option><option>POST</option><option>PUT</option><option>PATCH</option><option>DELETE</option>
      </select>
      <input id="pg-path" value="/api/orders" aria-label="Path" spellcheck="false" autocomplete="off">
    </div>
    <div class="pg__presets" id="pg-presets" aria-label="Example requests">
      <button type="button" data-m="GET" data-p="/api/health">GET /api/health</button>
      <button type="button" data-m="GET" data-p="/api/orders">GET /api/orders</button>
      <button type="button" data-m="POST" data-p="/api/orders">POST /api/orders</button>
      <button type="button" data-m="GET" data-p="/search">GET /search</button>
      <button type="button" data-m="GET" data-p="/reports/q3">GET /reports/q3</button>
    </div>
    <div id="pg-result" class="pg__result" aria-live="polite"></div>
  </div>
</div>

## What it shows

- **Validation:** an invalid rule set is rejected as a whole, as `PUT /rules` would reject it with a 400 and keep the current rules.
- **The trace:** every rule checked, in order, and why it was skipped, until the first match.
- **The response:** the status, headers and body a shed request gets from the default responder.

Partial brownouts are random per request, so the playground shows the share that gets through rather than one outcome. `ttl` is accepted and treated as unexpired; set `expires_at` to a past time to see an expired rule skipped. See [Rules](rules.md) for every field.
