// The terminal page's control-plane path (KOSD-C-12, KOSD-C-90). Every call
// keeps the failure envelope the control plane wrote: the page shows the same
// code and reason a direct call would answer, and never turns a refusal into a
// page-local verdict.
//
// The console session rides the browser's own cookie on the session-scoped
// routes; the terminal token is the only credential on the attach and input
// routes and is held in memory alone, never in the address, storage or logs
// (KOSD-C-91, CONSOLE-C-14).

export const API_PREFIX = "/api/v1";

// PageIdentity is the native terminal page this component declares.
export const PAGE_ID = "terminal";

// CODE_UNREACHABLE marks a request that never produced a control-plane answer,
// so the page states the reason instead of showing an empty projection.
export const CODE_UNREACHABLE = "unreachable";

// CODE_INVALID_RESPONSE marks a body the page cannot read.
export const CODE_INVALID_RESPONSE = "invalid_response";

// CODE_AUDIT_UNAVAILABLE is the control plane's own code when the audit trail
// cannot be read or written. The page ends the session it was reading for and
// refuses further input instead of showing the refusal as a notice alone
// (CONSOLE-C-15, KOSD-C-96).
export const CODE_AUDIT_UNAVAILABLE = "terminal_audit_unavailable";

// listPath is the page registry of the caller's role.
export function listPath() {
  return `${API_PREFIX}/pages`;
}

// readPath is one declared page.
export function readPath(id) {
  return `${API_PREFIX}/pages/${encodeURIComponent(id)}`;
}

// auditPath addresses one session's audit events. A cursor continues a read the
// control plane paginated; the response carries the cursor of the next page
// (KOSD-C-96).
export function auditPath(sessionID, cursor = "") {
  const base = `${sessionPath(sessionID)}/audit`;
  return cursor === "" ? base : `${base}?cursor=${encodeURIComponent(cursor)}`;
}

// sessionPath addresses one terminal session.
export function sessionPath(sessionID) {
  return `${API_PREFIX}/terminal/sessions/${encodeURIComponent(sessionID)}`;
}

// createAPI binds the page to one fetch. The console session is the browser's
// own, so it is sent as a same-origin cookie and never as a script-readable
// value; a terminal token is passed per call.
// encodeBase64 turns one text batch into the base64 the control plane's input
// route carries (KOSD-C-92). The executor's output is base64 the same way.
function encodeBase64(text) {
  const bytes = new TextEncoder().encode(String(text ?? ""));
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

// decodeBase64 turns one base64 output back into the text the page shows. A
// value that is not base64 is shown as it arrived rather than dropped.
function decodeBase64(value) {
  if (typeof value !== "string" || value === "") return "";
  try {
    const binary = atob(value);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
    return new TextDecoder().decode(bytes);
  } catch {
    return value;
  }
}

export function createAPI({ fetchFn = globalThis.fetch } = {}) {
  async function request({ method, path, body, token = "", keepalive = false }) {
    const headers = { Accept: "application/json" };
    const init = { method, headers, credentials: "same-origin" };
    // keepalive lets a teardown request survive a page that is already going
    // away, so an end request is not dropped by the unload (CONSOLE-C-11).
    if (keepalive) init.keepalive = true;
    if (token) headers.Authorization = `Bearer ${token}`;
    if (body !== undefined) {
      headers["Content-Type"] = "application/json";
      init.body = JSON.stringify(body);
    }
    let response;
    try {
      response = await fetchFn(path, init);
    } catch (error) {
      return { ok: false, error: { code: CODE_UNREACHABLE, message: describe(error) } };
    }
    const payload = await jsonOf(response);
    if (!response.ok) {
      return { ok: false, error: failureOf(payload, response.status) };
    }
    return { ok: true, body: payload === null ? {} : payload };
  }

  return {
    // listPages returns the pages this session role may see (DASH-C-22).
    listPages: () => request({ method: "GET", path: listPath() }),
    // readPage returns the terminal page projection with its item values
    // (KOSD-C-53).
    readPage: (id) => request({ method: "GET", path: readPath(id) }),
    // createSession requests one terminal session (KOSD-C-90).
    createSession: (target) => request({ method: "POST", path: `${API_PREFIX}/terminal/sessions`, body: target }),
    // status reads one session (KOSD-C-90).
    status: (sessionID) => request({ method: "GET", path: sessionPath(sessionID) }),
    // renew exchanges the session's token (KOSD-C-91).
    renew: (sessionID) => request({ method: "POST", path: `${sessionPath(sessionID)}/renew` }),
    // attach connects one session with its terminal token (KOSD-C-91).
    attach: (sessionID, token) => request({ method: "POST", path: `${sessionPath(sessionID)}/attach`, token }),
    // input delivers one batch with the terminal token (KOSD-C-92). The batch
    // leaves as the base64 the route carries and the executor's output comes
    // back decoded, so the page sends and shows bytes, not an encoded string.
    input: async (sessionID, token, sequence, data) => {
      const result = await request({
        method: "POST",
        path: `${sessionPath(sessionID)}/input`,
        token,
        body: { sequence, data: encodeBase64(data) },
      });
      if (result.ok && result.body && typeof result.body.output === "string") {
        result.body = { ...result.body, output: decodeBase64(result.body.output) };
      }
      return result;
    },
    // end ends one session (KOSD-C-90). A repeated end has no further effect.
    // Teardown passes keepalive so the request outlives the unload.
    end: (sessionID, options = {}) => request({ method: "DELETE", path: sessionPath(sessionID), keepalive: options.keepalive === true }),
    // audit reads one session's audit events (KOSD-C-96). A cursor continues a
    // read the control plane paginated; the response carries the next cursor.
    audit: (sessionID, cursor = "") => request({ method: "GET", path: auditPath(sessionID, cursor) }),
  };
}

// failureOf reads the stable failure envelope KOSD-C-12 fixes: the control
// plane answers a failure as {"error": {code, message, ...}}, so the code and
// the user-facing reason are one level down. A flat body is still accepted, and
// a body without a code states the HTTP status rather than pretending success.
function failureOf(body, status) {
  const failure = body && typeof body === "object" && body.error && typeof body.error === "object" ? body.error : body;
  if (failure && typeof failure === "object" && typeof failure.code === "string" && failure.code !== "") {
    return {
      code: failure.code,
      message: typeof failure.message === "string" && failure.message !== "" ? failure.message : `控制面返回 HTTP ${status}`,
      stage: typeof failure.stage === "string" ? failure.stage : "",
    };
  }
  return { code: `http_${status}`, message: `控制面返回 HTTP ${status}`, stage: "" };
}

async function jsonOf(response) {
  if (typeof response.json !== "function") return null;
  try {
    return await response.json();
  } catch {
    return null;
  }
}

function describe(error) {
  if (error instanceof Error && error.message) return error.message;
  return String(error ?? "控制面不可达");
}
