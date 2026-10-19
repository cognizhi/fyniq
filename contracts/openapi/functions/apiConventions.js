// The conventions of PRD-001 §8.18.1 that every operation gets, checked over the whole contract
// (`make api-check`, part of `make test`): deprecation with six months' notice (FR-INT-06),
// Idempotency-Key on writes (FR-INT-08), ETag and If-Match on versioned records (FR-INT-09), cursor
// lists (FR-INT-10). An operation opts out of one only with
// x-fyniq-exempt naming the convention and the reason; TestConventionExemptions in internal/api lists
// every exemption, so a new one is reviewed.
const WRITES = ['post', 'put', 'patch', 'delete'];
const METHODS = ['get', ...WRITES];
const DATE = /^\d{4}-\d{2}-\d{2}$/;

// sixMonthsAfter is the date six calendar months after a YYYY-MM-DD date, as Go's AddDate(0, 6, 0).
const sixMonthsAfter = (date) => {
  const d = new Date(`${date}T00:00:00Z`);
  d.setUTCMonth(d.getUTCMonth() + 6);
  return d.toISOString().slice(0, 10);
};

// deprecation returns what a deprecated operation lacks: its dates, at least six months apart, its note,
// and the Deprecation and Sunset headers on its responses (FR-INT-06).
const deprecation = (op) => {
  const name = op.operationId;
  const on = op['x-fyniq-deprecated-on'];
  const sunset = op['x-sunset'];
  if (!op.deprecated) {
    return sunset || on ? [`${name} has a sunset or deprecation date but is not deprecated (FR-INT-06).`] : [];
  }
  const out = [];
  if (!DATE.test(on || '') || !DATE.test(sunset || '')) {
    out.push(`${name} is deprecated without x-fyniq-deprecated-on and x-sunset dates (YYYY-MM-DD) (FR-INT-06).`);
  } else if (sunset < sixMonthsAfter(on)) {
    out.push(`${name}'s sunset ${sunset} is less than six months after its deprecation on ${on} (FR-INT-06).`);
  }
  if (!/^https:\/\//.test(op['x-fyniq-deprecation-note'] || '')) {
    out.push(`${name} is deprecated without x-fyniq-deprecation-note, the page that says what to use instead (FR-INT-06).`);
  }
  for (const [status, r] of Object.entries(op.responses || {})) {
    const headers = Object.keys((r && r.headers) || {}).map((h) => h.toLowerCase());
    if (!headers.includes('deprecation') || !headers.includes('sunset')) {
      out.push(`${name}'s ${status} response does not document the Deprecation and Sunset headers (FR-INT-06).`);
    }
  }
  return out;
};

const exempt = (op, convention) => {
  const reason = op && op['x-fyniq-exempt'] && op['x-fyniq-exempt'][convention];
  return typeof reason === 'string' && reason.trim().length >= 40;
};

const header = (op, name) =>
  (op.parameters || []).some((p) => p && p.in === 'header' && String(p.name).toLowerCase() === name.toLowerCase());

const query = (op, name) => (op.parameters || []).some((p) => p && p.in === 'query' && p.name === name);

const successes = (op) =>
  Object.entries(op.responses || {}).filter(([status]) => /^2\d\d$/.test(status)).map(([, r]) => r || {});

const returnsHeader = (op, name) =>
  successes(op).some((r) => Object.keys(r.headers || {}).some((h) => h.toLowerCase() === name.toLowerCase()));

const body = (op) => {
  for (const r of successes(op)) {
    const s = r.content && r.content['application/json'] && r.content['application/json'].schema;
    if (s) return s;
  }
  return null;
};

const isArray = (s) => !!s && (s.type === 'array' || (Array.isArray(s.type) && s.type.includes('array')));

// A list is an operation named list… or one whose body is a page of items.
const isList = (op) => /^list[A-Z]/.test(op.operationId || '') || isArray(body(op) && body(op).properties && body(op).properties.items);

const parent = (path) => {
  const i = path.lastIndexOf('/');
  const last = path.slice(i + 1);
  return i > 0 && !last.startsWith('{') ? path.slice(0, i) : null;
};

export default (paths) => {
  const out = [];
  const flag = (path, method, message) => out.push({ message, path: ['paths', path, method] });
  for (const [path, item] of Object.entries(paths || {})) {
    for (const method of METHODS) {
      if (item[method]) deprecation(item[method]).forEach((m) => flag(path, method, m));
    }
    for (const method of WRITES) {
      const op = item[method];
      if (!op) continue;
      // FR-INT-08: a POST that creates or changes state takes Idempotency-Key.
      if (method === 'post' && !header(op, 'Idempotency-Key') && !exempt(op, 'Idempotency-Key')) {
        flag(path, method, `${op.operationId} changes state but takes no Idempotency-Key header (FR-INT-08).`);
      }
      // FR-INT-09: a command on a record you can read (the record itself, or an action under it) requires
      // If-Match, and the record's read returns its ETag.
      const record = paths[path] && paths[path].get && method !== 'post' ? path
        : (parent(path) && paths[parent(path)] && paths[parent(path)].get && parent(path).endsWith('}') ? parent(path) : null);
      if (record && !exempt(op, 'If-Match')) {
        if (!header(op, 'If-Match')) {
          flag(path, method, `${op.operationId} changes the record at ${record} but takes no If-Match header (FR-INT-09).`);
        } else if (!(op.responses || {})['412'] || !(op.responses || {})['428']) {
          flag(path, method, `${op.operationId} takes If-Match but does not document 412 and 428 (FR-INT-09).`);
        }
        if (!returnsHeader(paths[record].get, 'ETag')) {
          flag(record, 'get', `${paths[record].get.operationId} reads a record that commands change, but returns no ETag (FR-INT-09).`);
        }
      }
    }
    // FR-INT-10: a list is cursor-paginated.
    const get = item.get;
    if (get && isList(get) && !exempt(get, 'cursor')) {
      const page = body(get);
      if (!query(get, 'cursor') || !query(get, 'page_size') || !(page && page.properties && page.properties.next_cursor)) {
        flag(path, 'get', `${get.operationId} is a list but is not cursor-paginated: it needs the cursor and page_size query parameters and next_cursor in its body (FR-INT-10).`);
      }
    }
  }
  return out;
};
