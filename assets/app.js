// The terminal page's entry: it reads the page projection the control plane
// serves, keeps the sessions the browser holds, and renders one document. The
// page modules are pure renderers, so the shell runs in a test without a
// browser.
//
// The page reads the control plane's projection (KOSD-C-53) and drives the
// session surface (KOSD-C-90). It holds no cluster credential and never
// reconnects, replays or stores a session on its own (CONSOLE-C-11).

import { createAPI, PAGE_ID, CODE_UNREACHABLE, CODE_INVALID_RESPONSE, CODE_AUDIT_UNAVAILABLE } from "./api.js";
import { renderTerminal, emptySelection, selectionFromTarget, selectionMatches } from "./terminal.js";
import * as sessions from "./session.js";

// AUDIT_MAX_PAGES bounds one audit read. The control plane paginates the trail
// and answers with the cursor of the next page, so the page follows it until a
// page is empty of events or the bound is reached (KOSD-C-96).
const AUDIT_MAX_PAGES = 8;

// createShell wires one document to the control plane and the address.
export function createShell({ document, window, fetchFn = null } = {}) {
  const root = document.getElementById("terminal");
  if (!root) return null;

  const api = createAPI({ fetchFn: fetchFn || globalThis.fetch });
  let status = "loading";
  let page = null;
  let registry = [];
  let selection = emptySelection();
  let confirming = false;
  let notice = null;
  let readError = null;
  const openSessions = [];
  let pending = Promise.resolve();
  let writing = Promise.resolve();
  let audit = null;
  let auditUnavailable = false;
  let auditUnavailableNotice = null;
  let networkDown = false;
  let destroyed = false;
  let timer = null;
  let teardownPending = Promise.resolve();
  const listeners = [];
  const REFRESH_MS = 30000;

  function draw() {
    reconcileEndNotice();
    root.innerHTML = renderTerminal({
      status,
      page,
      registry,
      selection,
      confirming,
      sessions: openSessions,
      notice,
      readError,
    audit,
    networkDown,
    });
    root.dataset.state = status;
  }

  // reconcileEndNotice drops the page-level "end result unknown" notice once no
  // session is still unconfirmed, so a retry that settled leaves no stale claim
  // beside the confirmed end (CONSOLE-C-11).
  function reconcileEndNotice() {
    if (!notice || notice.code !== "end_result_unknown") return;
    if (openSessions.some((session) => session.endUnknown)) return;
    notice = null;
  }

  // schedule queues one read or write and keeps the returned promise for the
  // caller that waits on the page reaching its state.
  function schedule(task) {
    pending = pending.then(task);
    return pending;
  }

  // loadRegistry reads the pages the session may see; an entry the registry
  // does not carry is absent from the page (DASH-C-22, KOSD-F-16).
  async function loadRegistry() {
    const result = await api.listPages();
    if (!result.ok) {
      readError = result.error;
      status = "error";
      draw();
      return;
    }
    registry = Array.isArray(result.body.items) ? result.body.items : [];
    if (!registry.some((entry) => entry && entry.id === PAGE_ID)) {
      status = "hidden";
      draw();
      return;
    }
    await loadPage();
  }

  // loadPage reads the terminal page projection. A refusal keeps the entry and
  // states the reason, so a page is either its declared projection or a
  // refusal and never an empty projection (KOSD-C-53, KOSD-C-64).
  async function loadPage({ force = false } = {}) {
    if (!force && page) return;
    status = "loading";
    draw();
    const result = await api.readPage(PAGE_ID);
    if (!result.ok) {
      readError = result.error;
      status = "error";
      draw();
      return;
    }
    page = result.body;
    audit = auditValueOf(page);
    status = "ready";
    draw();
  }

  // prefillSelection reads the target a deep link names. A deep link only
  // prefills: it never creates a session or submits a command on its own
  // (CONSOLE-C-8).
  function prefillSelection() {
    const query = new URLSearchParams(String(window.location.hash || "").split("?")[1] || "");
    const type = query.get("target");
    if (type !== "node" && type !== "pod") return;
    const targetId = query.get("targetId") || "";
    if (!targetId) return;
    selection = {
      type,
      targetId,
      targetUid: query.get("targetUid") || "",
      namespace: query.get("namespace") || "",
      container: query.get("container") || "",
    };
  }

  // connect confirms the target and requests one session. The control plane's
  // answer is kept as it came: a refusal states its own code and reason, and no
  // session area is drawn for a session that was not established (CONSOLE-C-9,
  // CONSOLE-C-12).
  async function connect() {
    // A queued confirmation that runs after the page was torn down must not
    // create a session or hold a token (CONSOLE-C-11, CONSOLE-C-14).
    if (destroyed) return;
    if (!selection.targetId) {
      notice = { kind: "error", code: "invalid_field", message: "先选择一个目标，再确认连接" };
      draw();
      return;
    }
    // A Pod target carries its container; a multi-container Pod is never
    // preselected, so an empty container is a missing choice, not a valid submit
    // (CONSOLE-C-8).
    if (selection.type === "pod" && !selection.container) {
      notice = { kind: "error", code: "invalid_field", message: "请先为该 Pod 选择容器，再确认连接" };
      draw();
      return;
    }
    confirming = true;
    notice = null;
    draw();
    const session = sessions.createSession({ api, selection });
    openSessions.push(session);
    draw();
    const failure = await sessions.open(session, selection);
    confirming = false;
    // The page may have gone away while the creation was in flight. The id the
    // control plane allocated is ended at once, never attached and never kept
    // (CONSOLE-C-11, CONSOLE-C-14).
    if (destroyed) {
      await sessions.end(session, { keepalive: true }).catch(() => {});
      return;
    }
    if (failure) {
      notice = { kind: "error", code: failure.code, message: failure.message };
    } else {
      // The control plane refuses a creation while the audit trail is not
      // writable, so a creation that succeeded is also the page's proof that the
      // trail is back (CONSOLE-C-15).
      auditUnavailable = false;
      auditUnavailableNotice = null;
      const attachFailure = await sessions.attach(session);
      if (attachFailure) {
        notice = { kind: "error", code: attachFailure.code, message: attachFailure.message };
        // A target the control plane reports as ended leaves a session that
        // still owes its end request (CONSOLE-C-11).
        await releaseEnded();
      } else {
        await readAudit(session);
        await releaseEnded();
      }
    }
    draw();
  }

  // commandBytes turns the text the form holds into the bytes the shell needs.
  // The executor starts /bin/sh and writes each batch to its stdin as it
  // arrives, so a command without a line end never runs and the next one is
  // appended to it. The line end is produced at the command-submit boundary,
  // not in the byte path every batch takes (KOSD-C-92, CONSOLE-C-11).
  function commandBytes(value) {
    const text = String(value ?? "").replace(/\r\n?/g, "\n");
    if (text === "") return "";
    return text.endsWith("\n") ? text : `${text}\n`;
  }

  // submitInput delivers one input batch to one session (KOSD-C-92). A request
  // that never reached the control plane ends the session and states that its
  // result is unknown, never a success; an audit trail that cannot be read
  // refuses the input outright (CONSOLE-C-11, CONSOLE-C-15).
  async function submitInput(session, form) {
    if (destroyed) return;
    const value = form && typeof form.elements === "object" ? String(form.elements.data?.value || "") : "";
    if (!value) return;
    if (auditUnavailable) {
      notice = auditUnavailableNotice;
      draw();
      return;
    }
    const failure = await sessions.send(session, commandBytes(value));
    if (failure) {
      if (failure.code === CODE_UNREACHABLE || failure.code === CODE_INVALID_RESPONSE) {
        networkDown = true;
        notice = { kind: "error", code: "input_result_unknown", message: "输入请求未得到确认，命令结果未知，可能仍有未完成的副作用" };
        // The control plane may still hold the session the page just ended.
        // Ask for its end at once, best-effort; an unreachable control plane
        // leaves the debt owed for the next refresh (CONSOLE-C-11).
        await releaseEnded();
      } else {
        notice = { kind: "error", code: failure.code, message: failure.message };
      }
    } else {
      networkDown = false;
      await readAudit(session);
      await releaseEnded();
    }
    draw();
  }

  // disconnect ends one session. A repeated end has no further side effect
  // (CONSOLE-C-11). An end that never got an answer states the result is
  // unknown rather than claiming the target process stopped.
  async function disconnect(session) {
    const failure = await sessions.end(session);
    if (failure) {
      if (failure.code === CODE_UNREACHABLE || failure.code === CODE_INVALID_RESPONSE) {
        notice = { kind: "error", code: "end_result_unknown", message: "结束请求未得到确认，目标进程结果未知，可能仍有未完成的副作用" };
      } else {
        notice = { kind: "error", code: failure.code, message: failure.message };
      }
    }
    await readAudit(session);
    draw();
  }

  // addListener binds one listener and remembers it so teardown can release it.
  function addListener(target, type, handler) {
    if (target && typeof target.addEventListener === "function") {
      target.addEventListener(type, handler);
      listeners.push([target, type, handler]);
    }
  }

  // removeListeners releases every listener this shell bound.
  function removeListeners() {
    for (const [target, type, handler] of listeners) {
      if (target && typeof target.removeEventListener === "function") target.removeEventListener(type, handler);
    }
    listeners.length = 0;
  }

  // auditValueOf reads the session-audit item the page projection carries.
  function auditValueOf(page) {
    const items = page && Array.isArray(page.items) ? page.items : [];
    const item = items.find((entry) => entry && entry.id === "session-audit");
    return item && item.value ? item.value : null;
  }

  // eventKey is one audit event's identity. Events without an id are kept by
  // their whole content, so a page never drops an event it cannot name
  // (KOSD-C-96).
  function eventKey(event) {
    if (!event || typeof event !== "object") return "";
    if (event.eventId !== undefined && event.eventId !== null) return `${event.sessionId || ""}/${event.eventId}`;
    return JSON.stringify(event);
  }

  // eventOrder places events by the id the control plane gave them, so a page
  // read after an earlier one shows the trail in the order it happened.
  function eventOrder(event) {
    const id = event && typeof event === "object" ? event.eventId : undefined;
    return typeof id === "number" ? id : Number.MAX_SAFE_INTEGER;
  }

  // mergeAudit folds one page of events into the audit state the panel shows.
  // Events are deduplicated by session and event id and the history already
  // held is kept, so a later page never drops an event an earlier one carried
  // and an initial projection's later pages survive the first read
  // (KOSD-C-96, CONSOLE-C-15).
  function mergeAudit(state, sessionID, events) {
    const base = state && Array.isArray(state.sessions) ? state : { sessions: [], rule: "" };
    const held = base.sessions.find((row) => row && row.sessionId === sessionID);
    const merged = held && Array.isArray(held.events) ? held.events.slice() : [];
    const seen = new Set(merged.map(eventKey));
    for (const event of events) {
      const key = eventKey(event);
      if (key && seen.has(key)) continue;
      if (key) seen.add(key);
      merged.push(event);
    }
    merged.sort((left, right) => eventOrder(left) - eventOrder(right));
    const rows = base.sessions.filter((row) => !row || row.sessionId !== sessionID);
    rows.push({ sessionId: sessionID, events: merged });
    return { sessions: rows, rule: base.rule || "" };
  }

  // nextCursor reads the cursor of the page the control plane just answered.
  function nextCursor(body) {
    const value = body && typeof body === "object" ? body.nextCursor : undefined;
    if (typeof value === "string" && value !== "") return value;
    if (typeof value === "number" && value > 0) return String(value);
    return "";
  }

  // highWaterMark is the highest event id one session's trail holds, used as the
  // cursor after a finished read (KOSD-C-96).
  function highWaterMark(state, sessionID) {
    const row = state && Array.isArray(state.sessions) ? state.sessions.find((entry) => entry && entry.sessionId === sessionID) : null;
    const events = row && Array.isArray(row.events) ? row.events : [];
    let highest = 0;
    for (const event of events) {
      if (event && typeof event.eventId === "number" && event.eventId > highest) highest = event.eventId;
    }
    return highest > 0 ? String(highest) : "";
  }

  // endedByAudit reports whether one page of events records the session's end,
  // so a control-plane end the page did not see is applied (CONSOLE-C-11).
  function endedByAudit(events) {
    return (Array.isArray(events) ? events : []).some((event) => event && event.event === "end");
  }

  // readAudit reads one session's audit events from the control plane and shows
  // them, following the cursor of every page the control plane paginates, so a
  // session established, input or ended after the page loaded is represented
  // (CONSOLE-C-15, KOSD-C-96). A read that uses its whole budget with a cursor
  // still open keeps that cursor on the session, so the next read continues
  // where this one stopped instead of re-reading the pages already held. The
  // return value says whether the trail was read to its end.
  async function readAudit(session) {
    if (!session || !session.sessionId || destroyed) return false;
    let next = session.auditCursor || "";
    let complete = false;
    for (let page = 0; page < AUDIT_MAX_PAGES; page += 1) {
      const result = await api.audit(session.sessionId, next);
      if (!result.ok) {
        await auditFailure(session, result.error);
        return false;
      }
      audit = mergeAudit(audit, session.sessionId, Array.isArray(result.body.items) ? result.body.items : []);
      // An end the control plane recorded but the page did not see ends the
      // session here and owes the remote end once (CONSOLE-C-11).
      if (sessions.isLive(session.state) && endedByAudit(result.body.items)) {
        sessions.observeEnded(session);
      }
      const following = nextCursor(result.body);
      if (following === "") {
        complete = true;
        break;
      }
      // Keep the cursor of the page after this one, so a read that runs out of
      // budget continues there instead of starting over (KOSD-C-96).
      session.auditCursor = following;
      next = following;
    }
    // A finished read resumes from the highest event it holds, so later events
    // are not hidden behind a replay of the pages already read (KOSD-C-96).
    if (complete) session.auditCursor = highWaterMark(audit, session.sessionId);
    auditUnavailable = false;
    auditUnavailableNotice = null;
    draw();
    return complete;
  }

  // auditFailure ends the session an audit read refused for and states the
  // control plane's own code. An audit trail that cannot be read stops the
  // session instead of only showing a notice, and the page refuses further
  // input until a read succeeds again (CONSOLE-C-15).
  async function auditFailure(session, error) {
    const code = error && error.code ? error.code : CODE_AUDIT_UNAVAILABLE;
    const message = (error && error.message) || "终端审计不可用";
    auditUnavailable = code === CODE_AUDIT_UNAVAILABLE || code === CODE_UNREACHABLE || code === CODE_INVALID_RESPONSE;
    notice = { kind: "error", code, message };
    if (auditUnavailable) auditUnavailableNotice = notice;
    sessions.applyFailure(session, { code, message });
    await sessions.end(session);
    draw();
  }

  // readTerminalAudit reads the end events of a session that left the live set,
  // so a control-plane end the page did not see is represented. The session is
  // marked audited only when its whole trail was read; a read cut short by the
  // page budget continues on the next refresh (CONSOLE-C-15, KOSD-C-96).
  async function readTerminalAudit(session) {
    if (!session || session.terminalAudited || sessions.isLive(session.state)) return;
    const complete = await readAudit(session);
    // A trail read while the remote end is still owed may not carry the end yet:
    // only a read taken after the end was confirmed can mark the session done,
    // so the end event the clearing request writes is not skipped
    // (CONSOLE-C-15, KOSD-C-96).
    if (complete && !session.reapPending) session.terminalAudited = true;
  }

  // refresh reads each live session's state from the control plane and renews a
  // connected one, so a revocation, an idle end or a service failure lands on
  // the page instead of leaving it showing a connected session that cannot act
  // (KOSD-C-90, CONSOLE-C-11, CONSOLE-C-14).
  async function refresh() {
    if (destroyed) return;
    // A session the page stopped locally while the control plane was out of
    // reach still owes its remote end: release it now that the control plane
    // can be asked again (CONSOLE-C-11).
    await releaseEnded();
    let reachable = true;
    for (const session of [...openSessions]) {
      // A session the page already stopped — a lost connection ends it locally
      // and irreversibly — is never read, renewed or re-enabled again
      // (CONSOLE-C-11).
      if (!sessions.isLive(session.state)) {
        await readTerminalAudit(session);
        continue;
      }
      const failure = await sessions.status(session);
      if (failure) {
        if (failure.code === CODE_UNREACHABLE || failure.code === CODE_INVALID_RESPONSE) reachable = false;
        await readTerminalAudit(session);
        continue;
      }
      await readTerminalAudit(session);
      if (session.state === sessions.STATE_CONNECTED) {
        const renewFailure = await sessions.renew(session);
        if (renewFailure && (renewFailure.code === CODE_UNREACHABLE || renewFailure.code === CODE_INVALID_RESPONSE)) reachable = false;
        await readTerminalAudit(session);
      }
    }
    networkDown = !reachable;
    if (networkDown && !notice) {
      notice = { kind: "error", code: CODE_UNREACHABLE, message: "控制面不可达，已停止输入" };
    }
    await releaseEnded();
    draw();
  }

  // endAll asks the control plane to end every session the browser still holds.
  // The requests are fired together and none of them awaits another, so one
  // request that never answers cannot hold back the others. Teardown is
  // best-effort: a page that is going away reports an unknown result rather
  // than blocking (CONSOLE-C-11).
  async function endAll() {
    await Promise.all(
      [...openSessions].map((session) =>
        session.endRequested ? Promise.resolve() : sessions.end(session, { keepalive: true }).catch(() => {}),
      ),
    );
  }

  // releaseEnded asks the control plane to end sessions whose end the page could
  // not confirm — a lost connection, a terminal failure, or a creation that
  // answered after the page was gone. Only the end request is sent: nothing is
  // resumed, attached or renewed, and the debt stays owed while the control
  // plane cannot be reached (CONSOLE-C-11).
  async function releaseEnded() {
    const owed = openSessions.filter((session) => session.reapPending && !session.endRequested && session.sessionId);
    if (!owed.length) return;
    await Promise.all(owed.map((session) => sessions.end(session, { keepalive: true }).catch(() => {})));
  }

  // teardown stops the document for good. Input and the refresh loop stop
  // synchronously and irreversibly, the state the page must not keep is
  // dropped, the listeners are released, and each session's end request is
  // fired on its own. A page that comes back from the back-forward cache
  // therefore holds no token and no history, and a new session needs a fresh
  // confirmation (CONSOLE-C-11, CONSOLE-C-14).
  function teardown() {
    if (destroyed) return;
    destroyed = true;
    if (timer && typeof window.clearInterval === "function") window.clearInterval(timer);
    timer = null;
    removeListeners();
    networkDown = true;
    for (const session of [...openSessions]) sessions.stop(session);
    teardownPending = endAll().catch(() => {});
    draw();
  }

  // The controls are drawn, not bound: one delegated listener on the root
  // reaches the control of every page this document re-renders (KOSD-C-12).
  addListener(root, "change", (event) => {
    const field = event && event.target && event.target.dataset ? event.target.dataset.field : "";
    const row = event.target && event.target.closest ? event.target.closest("[data-target]") : null;
    if (field === "target-type") {
      if (!row) return;
      selection = selectionFromTarget({
        type: row.dataset.target,
        id: row.dataset.targetId,
        uid: row.dataset.targetUid,
        namespace: row.dataset.namespace || "",
        container: row.dataset.container || "",
      });
      draw();
      return;
    }
    if (field === "container") {
      if (!row) return;
      const container = event.target.value || "";
      // A container choice belongs to the Pod row it was made on. Type,
      // namespace, name and UID all take part, so a Pod that shares its name
      // with another Pod in a different namespace keeps its own identity
      // (CONSOLE-C-8, KOSD-C-95).
      const candidate = {
        type: row.dataset.target,
        targetId: row.dataset.targetId,
        namespace: row.dataset.namespace || "",
        targetUid: row.dataset.targetUid || "",
      };
      selection =
        selectionMatches(selection, candidate)
          ? { ...selection, container }
          : selectionFromTarget({
              type: candidate.type,
              id: candidate.targetId,
              uid: candidate.targetUid,
              namespace: candidate.namespace,
              container,
            });
      draw();
    }
  });
  addListener(root, "submit", (event) => {
    const sessionID = event && event.target && event.target.dataset ? event.target.dataset.input : "";
    if (!sessionID) return;
    event.preventDefault();
    const session = openSessions.find((entry) => entry.sessionId === sessionID);
    if (!session) return;
    writing = writing.then(() => submitInput(session, event.target)).catch(() => {});
  });
  addListener(root, "click", (event) => {
    const target = event && event.target;
    if (!target || !target.dataset) return;
    const action = target.dataset.action;
    if (action === "connect") {
      event.preventDefault();
      writing = writing.then(() => connect()).catch(() => {});
      return;
    }
    if (action === "end") {
      event.preventDefault();
      const session = openSessions.find((entry) => entry.sessionId === target.dataset.session);
      if (!session) return;
      writing = writing.then(() => disconnect(session)).catch(() => {});
    }
  });

  // A page that is closed or unloaded ends the sessions it holds; minimizing the
  // window does not. The real close path is the same teardown the explicit
  // destroy takes, so a close leaves no token, no history and no refresh loop
  // behind (CONSOLE-C-11, CONSOLE-C-14).
  const onUnload = () => {
    teardown();
  };
  addListener(window, "pagehide", onUnload);
  addListener(window, "beforeunload", onUnload);

  // The document refreshes each live session's state on a fixed cadence so a
  // control-plane change lands on the page without the user acting.
  if (typeof window.setInterval === "function") {
    timer = window.setInterval(() => {
      writing = writing.then(() => refresh()).catch(() => {});
    }, REFRESH_MS);
  }

  prefillSelection();
  draw();
  schedule(loadRegistry).catch(() => draw());
  return {
    redraw: draw,
    refresh,
    destroy: teardown,
    settle: () => Promise.all([pending, writing, teardownPending]).then(() => {}),
  };
}

// connect starts the terminal document. A module loaded outside a browser has
// no document and starts nothing.
export function connect(document, window) {
  return createShell({ document, window });
}

if (typeof window !== "undefined" && typeof document !== "undefined") {
  connect(document, window);
}
