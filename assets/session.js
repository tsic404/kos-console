// One browser-held terminal session and its state machine (KOSD-C-90 to
// KOSD-C-92). It mirrors the control plane's own states: only connected
// accepts input, every input carries a per-session sequence, a repeated
// sequence is answered from the receipt already held, and a session that left
// the live set refuses every further action.
//
// The terminal token lives in this object's memory alone. It never reaches the
// address, browser storage, a log or the rendered output (KOSD-C-91,
// CONSOLE-C-14).

import { CODE_INVALID_RESPONSE, CODE_UNREACHABLE } from "./api.js";
import { isTerminalFailure } from "./terminal.js";

// Session states (KOSD-C-90).
export const STATE_CONNECTING = "connecting";
export const STATE_CONNECTED = "connected";
export const STATE_CLOSED = "closed";
export const STATE_FAILED = "failed";

// Refusal codes the client itself makes before a control-plane answer.
export const CODE_NO_SESSION = "terminal_no_session";
export const CODE_NOT_CONNECTED = "terminal_not_connected";
export const CODE_ENDED = "terminal_session_ended";
export const CODE_NO_TOKEN = "terminal_no_token";

// CODE_CONNECTION_LOST is the state the page ends a session in when a request
// never reached the control plane or its answer could not be read. The session
// is ended locally and irreversibly: the browser never reuses its id, token or
// sequence to act on it again (CONSOLE-C-11).
export const CODE_CONNECTION_LOST = "terminal_connection_lost";

// CODE_PAGE_CLOSED is the state a session is left in when the page itself goes
// away. The end request is still owed, so the page asks the control plane once
// and the local state is dropped (CONSOLE-C-11, CONSOLE-C-14).
export const CODE_PAGE_CLOSED = "terminal_page_closed";

// createSession holds one session the browser opened.
export function createSession({ api, selection }) {
  const session = {
    api,
    selection,
    sessionId: "",
    token: "",
    targetType: selection && selection.type ? selection.type : "",
    targetId: selection && selection.targetId ? selection.targetId : "",
    targetUid: selection && selection.targetUid ? selection.targetUid : "",
    namespace: selection && selection.namespace ? selection.namespace : "",
    container: selection && selection.container ? selection.container : "",
    executionIdentity: "",
    state: "",
    reason: "",
    message: "",
    expiresAt: "",
    idleTimeoutMinutes: 0,
    limits: {},
    usage: {},
    output: [],
    receipt: null,
    nextSequence: 0,
    ended: false,
    // endRequested is true once the session's end was asked for or the control
    // plane already reported it outside the live set, so a repeated end has no
    // further side effect (CONSOLE-C-11).
    endRequested: false,
    // reapPending is true while the control plane may still hold a session the
    // page stopped locally without a confirmed end. The page asks for its end
    // whenever it can reach the control plane, and never resumes it
    // (CONSOLE-C-11).
    reapPending: false,
    // endSettled is true once the control plane confirmed the session's end.
    // The debt-clearing outcome lives here, apart from the session's own state,
    // so a clearing request that fails never rewrites a failure (CONSOLE-C-12).
    endSettled: false,
    // endUnknown is true when the last end request never produced an answer, so
    // the target process may still have run something (CONSOLE-C-11).
    endUnknown: false,
    // terminalAudited is true only after a terminal session's whole trail was
    // read, so a read that was cut short is continued later (KOSD-C-96).
    terminalAudited: false,
    // auditCursor is the cursor of the next unread audit page, so a session
    // with more events than one read's budget is continued where it stopped
    // instead of re-reading the pages already held (KOSD-C-96).
    auditCursor: "",
  };
  applyView(session, { state: STATE_CONNECTING });
  return session;
}

// applyView records one control-plane view of the session (KOSD-C-90).
function applyView(session, view) {
  if (!view || typeof view !== "object") return;
  if (typeof view.sessionId === "string" && view.sessionId !== "") session.sessionId = view.sessionId;
  if (typeof view.targetType === "string" && view.targetType !== "") session.targetType = view.targetType;
  if (typeof view.targetId === "string" && view.targetId !== "") session.targetId = view.targetId;
  if (typeof view.targetUid === "string" && view.targetUid !== "") session.targetUid = view.targetUid;
  if (typeof view.namespace === "string") session.namespace = view.namespace;
  if (typeof view.container === "string") session.container = view.container;
  if (typeof view.executionIdentity === "string" && view.executionIdentity !== "") session.executionIdentity = view.executionIdentity;
  if (typeof view.state === "string" && view.state !== "") session.state = view.state;
  if (typeof view.reason === "string") session.reason = view.reason;
  if (typeof view.expiresAt === "string") session.expiresAt = view.expiresAt;
  if (typeof view.idleTimeoutMinutes === "number") session.idleTimeoutMinutes = view.idleTimeoutMinutes;
  if (view.limits && typeof view.limits === "object") session.limits = view.limits;
  if (view.usage && typeof view.usage === "object") session.usage = view.usage;
  // A view that left the live set is one the control plane already ended, so no
  // further end request is needed for it.
  if (!isLive(session.state)) session.endRequested = true;
}

// create requests one session and holds the one token the control plane
// delivered on the first creation (KOSD-C-90, KOSD-C-91).
export async function open(session, selection) {
  const result = await session.api.createSession({
    extension: "kos-console",
    targetType: selection.type,
    targetId: selection.targetId,
    targetUid: selection.targetUid,
    namespace: selection.namespace,
    container: selection.container,
  });
  if (!result.ok) {
    applyFailure(session, result.error);
    return result.error;
  }
  const body = result.body && typeof result.body === "object" ? result.body : {};
  // A creation that answers after the page stopped this session keeps its id
  // but never its token: the page ended the session while the request was in
  // flight, so the control plane's session still has to be ended and nothing
  // may be resumed (CONSOLE-C-11).
  if (session.ended) {
    if (typeof body.sessionId === "string" && body.sessionId !== "") {
      session.sessionId = body.sessionId;
      session.reapPending = true;
    }
    return null;
  }
  applyView(session, body);
  if (typeof body.token === "string" && body.token !== "") session.token = body.token;
  return null;
}

// attach connects the session with its terminal token. One attempt, no retry
// and no fallback to another credential (KOSD-C-91, CONSOLE-C-11).
export async function attach(session) {
  if (session.ended || !isLive(session.state)) {
    return refusal(CODE_ENDED, "会话已结束，不能接入");
  }
  if (!session.token) return refusal(CODE_NO_TOKEN, "该会话没有终端令牌，不能接入");
  const result = await session.api.attach(session.sessionId, session.token);
  if (!result.ok) {
    applyFailure(session, result.error);
    return result.error;
  }
  // An attach that answers after the page stopped the session never brings it
  // back, and the token it would have used is dropped with the session
  // (CONSOLE-C-11, CONSOLE-C-14).
  if (session.ended) return null;
  applyView(session, result.body);
  return null;
}

// send delivers one input batch with the next per-session sequence number
// (KOSD-C-92). A repeated sequence is answered from the receipt already held
// and is never delivered again.
export async function send(session, data) {
  let sequence = session.nextSequence;
  if (sequence === 0) sequence = 1;
  return sendSequence(session, sequence, data);
}

// sendSequence delivers one batch with the caller's sequence number. A caller
// that retries a delivery after a lost answer repeats the same number; the
// control plane then answers the original receipt and does not deliver the
// batch twice (KOSD-C-92).
export async function sendSequence(session, sequence, data) {
  if (session.ended) return refusal(CODE_ENDED, "会话已结束，不能输入");
  if (!session.sessionId) return refusal(CODE_NO_SESSION, "没有会话，不能输入");
  if (!isLive(session.state) || session.state !== STATE_CONNECTED) {
    return refusal(CODE_NOT_CONNECTED, "只有已连接的会话接受输入");
  }
  const held = session.receipt && session.receipt.sequence === sequence ? session.receipt : null;
  if (held) {
    session.receipt = { ...held, duplicate: true };
    return null;
  }
  const result = await session.api.input(session.sessionId, session.token, sequence, data);
  // A request that was in flight when the session ended must not restore the
  // receipt or output it may have produced (CONSOLE-C-11).
  if (session.ended) return null;
  if (!result.ok) {
    applyFailure(session, result.error);
    return result.error;
  }
  const receipt = result.body || {};
  session.receipt = receipt;
  if (typeof receipt.output === "string" && receipt.output !== "") {
    session.output.push({ sequence, text: receipt.output });
  }
  if (typeof receipt.sequence === "number" && receipt.sequence >= session.nextSequence) {
    session.nextSequence = receipt.sequence + 1;
  }
  return null;
}

// renew exchanges the session's token. The idle bound is not reset (KOSD-C-91,
// KOSD-C-94).
export async function renew(session) {
  if (session.ended || !isLive(session.state)) {
    return refusal(CODE_ENDED, "会话已结束，不能续期");
  }
  const result = await session.api.renew(session.sessionId);
  if (!result.ok) {
    applyFailure(session, result.error);
    return result.error;
  }
  if (session.ended) return null;
  applyView(session, result.body);
  if (typeof result.body.token === "string" && result.body.token !== "") session.token = result.body.token;
  return null;
}

// status refreshes the session's view from the control plane. A view that left
// the live set ends the session here as well, so the page stops showing a
// session that can no longer accept input (KOSD-C-90, CONSOLE-C-11).
export async function status(session) {
  if (session.ended) return refusal(CODE_ENDED, "会话已结束，不能读取状态");
  if (!session.sessionId) return refusal(CODE_NO_SESSION, "没有会话，不能读取状态");
  const result = await session.api.status(session.sessionId);
  if (!result.ok) {
    applyFailure(session, result.error);
    return result.error;
  }
  if (session.ended) return null;
  applyView(session, result.body);
  if (!isLive(session.state)) session.ended = true;
  return null;
}

// end ends the session. A repeated end has no further side effect
// (KOSD-C-90, CONSOLE-C-11). A session the page stopped locally without an
// answer — a lost connection or a terminal failure — still gets its end
// request, so the control plane is asked once and the local state is cleared.
// options.keepalive lets a teardown request outlive the page (CONSOLE-C-11).
export async function end(session, options = {}) {
  // A session with no id yet still owes its remote end: the id arrives with a
  // late creation, and the page asks again then (CONSOLE-C-11).
  if (!session.sessionId) {
    session.ended = true;
    session.reapPending = true;
    session.endSettled = false;
    return null;
  }
  if (session.endRequested) return null;
  // Only a session that was still live when the end was asked for may have its
  // own state written by the end result; a session that already failed keeps
  // that failure (CONSOLE-C-12).
  const wasLive = isLive(session.state);
  session.endRequested = true;
  session.ended = true;
  const result = await session.api.end(session.sessionId, options);
  forget(session);
  if (!result.ok) {
    // The debt survives a control plane that could not be reached, so a later
    // attempt can still release the slot; a refusal means nothing is owed
    // (CONSOLE-C-11).
    const unreachable = result.error && (result.error.code === CODE_UNREACHABLE || result.error.code === CODE_INVALID_RESPONSE);
    // The clearing outcome is recorded on its own. It never goes through
    // applyFailure/abandon, which would replace a runtime or audit failure with
    // a connection-lost close (CONSOLE-C-12).
    session.endSettled = false;
    session.endUnknown = Boolean(unreachable);
    session.endRequested = !unreachable;
    session.reapPending = Boolean(unreachable);
    if (wasLive) {
      session.state = STATE_CLOSED;
      session.reason = "end_result_unknown";
      session.message = (result.error && result.error.message) || "结束请求未得到确认，目标进程结果未知";
    }
    return result.error;
  }
  session.reapPending = false;
  session.endSettled = true;
  session.endUnknown = false;
  // The trail may now carry the end event this session's audit read did not see
  // yet, so the next terminal read must not be skipped (KOSD-C-96).
  session.terminalAudited = false;
  // A session that already failed keeps its own state and reason: the end
  // response must not turn a failure into a clean close (CONSOLE-C-12).
  if (isLive(session.state) || session.state === "") applyView(session, result.body);
  return null;
}

// forget drops everything the session held that must not survive it: the
// terminal token, the receipt and the output history (KOSD-C-91, CONSOLE-C-14).
export function forget(session) {
  session.token = "";
  session.receipt = null;
  session.output = [];
}

// recordFailure moves the session out of the live set when the control plane
// reported a terminal failure, so no later action is offered against a session
// that can no longer accept input (CONSOLE-C-12).
//
// A request that never reached the control plane, or an answer the page could
// not read, ends the session locally and irreversibly: the page never resumes
// the same id, token or sequence after the network comes back (CONSOLE-C-11).
export function applyFailure(session, error) {
  const code = error && error.code ? error.code : "";
  const message = error && error.message ? error.message : "";
  if (code === CODE_UNREACHABLE || code === CODE_INVALID_RESPONSE) {
    abandon(session, message);
    return;
  }
  if (!session.ended && !isLive(session.state)) {
    session.ended = true;
  }
  const state = failureState(code);
  if (state) {
    session.state = state;
    session.reason = code;
    session.message = message;
    session.ended = state !== STATE_CONNECTED && state !== STATE_CONNECTING;
    // A session the control plane ended still owes its end request once, so a
    // slot it did not release itself is freed (CONSOLE-C-11).
    if (session.ended) {
      session.endRequested = false;
      session.reapPending = session.sessionId !== "";
      session.endSettled = false;
    }
  } else if (!isLive(session.state)) {
    session.ended = true;
  }
  session.message = message || session.message;
}

// abandon ends a session the page can no longer act on. The remote session is
// still owed an end, so reapPending is set and the page asks the control plane
// once it can be reached again; the result stays reported as unknown
// (CONSOLE-C-11).
export function abandon(session, message = "") {
  session.state = STATE_CLOSED;
  session.reason = CODE_CONNECTION_LOST;
  session.message = message || session.message || "浏览器与控制面的连接丢失，会话已结束";
  session.ended = true;
  session.endRequested = false;
  session.reapPending = session.sessionId !== "";
  session.endSettled = false;
  session.endUnknown = true;
  forget(session);
}

// observeEnded ends a session the audit shows the control plane already ended,
// so the page stops offering it. The page still asks for its end once, which is
// a no-op for a session the control plane already released, and never resumes
// it (CONSOLE-C-11, KOSD-C-96).
export function observeEnded(session, reason = CODE_ENDED) {
  if (!session || !isLive(session.state)) return;
  session.state = STATE_CLOSED;
  session.reason = reason;
  session.ended = true;
  session.endRequested = false;
  session.reapPending = session.sessionId !== "";
  session.endSettled = false;
  forget(session);
}

// stop ends a session because the page is going away. Like abandon, the end
// request is still owed, so the page can ask the control plane once with a
// request that outlives the unload.
export function stop(session, message = "页面已关闭，会话已结束") {
  session.state = STATE_CLOSED;
  session.reason = CODE_PAGE_CLOSED;
  session.message = message;
  session.ended = true;
  session.endRequested = false;
  // A session with no id yet still owes its remote end: a creation that answers
  // after the page is gone hands its id here (CONSOLE-C-11).
  session.reapPending = true;
  session.endSettled = false;
  forget(session);
}

// failureState maps a terminal failure code to the state the session shows. A
// target or service failure is shown as failed with its own reason; a limit or
// idle end keeps the closed state (CONSOLE-C-12).
export function failureState(code) {
  switch (code) {
    case "terminal_session_limit":
    case "terminal_idle_timeout":
    case "terminal_unauthenticated":
    case "terminal_forbidden":
    case "terminal_target_unavailable":
    case "terminal_audit_unavailable":
      return STATE_CLOSED;
    case "terminal_node_unreachable":
    case "terminal_target_ended":
    case "terminal_executor_unavailable":
    case "terminal_credential_boundary_unavailable":
      return STATE_FAILED;
    default:
      return isTerminalFailure(code) ? STATE_FAILED : "";
  }
}

// isLive reports whether a state occupies a session and may still be acted on.
export function isLive(state) {
  return state === STATE_CONNECTING || state === STATE_CONNECTED;
}

// refusal is a state-machine refusal the client makes itself.
export function refusal(code, message) {
  return { code, message, stage: "client" };
}
