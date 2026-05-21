// JavaScript fixture for cross-language indexing tests. Mirrors the
// TS handler in spirit (request/response builders) so the JS parser
// (introduced by C10) has symbols to chunk and the eval fixture has
// queries to exercise it.

/**
 * createClient builds a thin HTTP client bound to a base URL. The
 * returned object exposes get / post wrappers that share retry and
 * timeout behavior.
 */
export function createClient(baseURL, options = {}) {
  const timeout = options.timeout ?? 5000;
  const retries = options.retries ?? 3;
  return {
    get: (path) => fetchWithRetry(baseURL + path, { method: "GET" }, timeout, retries),
    post: (path, body) => fetchWithRetry(baseURL + path, { method: "POST", body }, timeout, retries),
  };
}

/**
 * parseResponse converts the raw fetch Response into a structured
 * { ok, status, data } object. JSON parsing failures are surfaced as
 * data=null rather than thrown so the caller can branch on .ok.
 */
export async function parseResponse(resp) {
  let data = null;
  try {
    data = await resp.json();
  } catch {
    data = null;
  }
  return { ok: resp.ok, status: resp.status, data };
}

/**
 * formatError serializes a network error into a user-presentable string.
 * The error code, status, and the first line of the message are kept;
 * stack traces are stripped to avoid leaking internal paths to UIs.
 */
export function formatError(err) {
  const code = err.code ?? "UNKNOWN";
  const status = err.status ?? 0;
  const msg = String(err.message ?? "").split("\n")[0];
  return `[${code}/${status}] ${msg}`;
}

/**
 * fetchWithRetry is the internal helper backing createClient's get/post.
 * Retries the request up to `attempts` times with linear backoff, then
 * gives up and rethrows the last error.
 */
async function fetchWithRetry(url, init, timeoutMs, attempts) {
  let lastErr;
  for (let i = 0; i < attempts; i++) {
    try {
      const ctrl = new AbortController();
      const t = setTimeout(() => ctrl.abort(), timeoutMs);
      const resp = await fetch(url, { ...init, signal: ctrl.signal });
      clearTimeout(t);
      return resp;
    } catch (e) {
      lastErr = e;
      await new Promise((r) => setTimeout(r, 100 * (i + 1)));
    }
  }
  throw lastErr;
}
