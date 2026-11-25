// The browser-held session state machine: only a connected session accepts
// input, every input carries a per-session sequence, a repeated sequence is
// answered from the receipt already held, and a terminal failure ends the
// session with its own code. The control plane is a scripted answer, so these
// run without a browser.

import assert from "node:assert/strict";
import test from "node:test";

import {
  applyFailure,
  attach,
  CODE_CONNECTION_LOST,
  CODE_PAGE_CLOSED,
  createSession,
  end,
  failureState,
  open,
  stop,
  status,
  sendSequence,
  send,
  STATE_CLOSED,
  STATE_CONNECTED,
  STATE_FAILED,
} from "../../assets/session.js";

// scriptedAPI answers one terminal surface from a table of responses.
function scriptedAPI(answers) {
  const calls = [];
  const api = {
    calls,
    createSession: async (target) => {
      calls.push({ route: "create", target });
      return answers.create;
    },
    attach: async (sessionID, token) => {
      calls.push({ route: "attach", sessionID, token });
      return answers.attach;
    },
    input: async (sessionID, token, sequence, data) => {
      calls.push({ route: "input", sessionID, token, sequence, data });
      return answers.input;
    },
    renew: async (sessionID) => {
      calls.push({ route: "renew", sessionID });
      return answers.renew;
    },
    status: async (sessionID) => {
      calls.push({ route: "status", sessionID });
      return answers.status;
    },
    audit: async (sessionID) => {
      calls.push({ route: "audit", sessionID });
      return answers.audit;
    },
    end: async (sessionID) => {
      calls.push({ route: "end", sessionID });
      return answers.end;
    },
  };
  return api;
}

const created = {
  ok: true,
  body: {
    sessionId: "s-1",
    targetType: "node",
    targetId: "node-a",
    targetUid: "uid-a",
    namespace: "",
    container: "",
    executionIdentity: "root@node",
    state: "connecting",
    expiresAt: "2026-11-25T10:05:00Z",
    idleTimeoutMinutes: 10,
    limits: { account: 4, target: 4 },
    usage: { account: 1, target: 1 },
    token: "terminal-token-1",
  },
};

const connected = {
  ok: true,
  body: { sessionId: "s-1", state: "connected", expiresAt: "2026-11-25T10:05:00Z" },
};

const selection = { type: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" };

test("open requests the session and holds the one token the control plane delivered", async () => {
  const api = scriptedAPI({ create: created, attach: connected });
  const session = createSession({ api, selection });
  const failure = await open(session, selection);
  assert.equal(failure, null);
  assert.equal(session.sessionId, "s-1");
  assert.equal(session.token, "terminal-token-1");
  assert.equal(api.calls[0].route, "create");
  assert.deepEqual(api.calls[0].target, {
    extension: "kos-console",
    targetType: "node",
    targetId: "node-a",
    targetUid: "uid-a",
    namespace: "",
    container: "",
  });
});

test("attach sends the terminal token, not the console session", async () => {
  const api = scriptedAPI({ create: created, attach: connected });
  const session = createSession({ api, selection });
  await open(session, selection);
  await attach(session);
  assert.equal(session.state, STATE_CONNECTED);
  assert.equal(api.calls.find((call) => call.route === "attach").token, "terminal-token-1");
});

test("a refusal at creation is kept as the control plane's own code", async () => {
  const api = scriptedAPI({ create: { ok: false, error: { code: "terminal_credential_boundary_unavailable", message: "无法保证凭据隔离" } } });
  const session = createSession({ api, selection });
  const failure = await open(session, selection);
  assert.equal(failure.code, "terminal_credential_boundary_unavailable");
  assert.equal(session.state, STATE_FAILED);
  assert.equal(session.reason, "terminal_credential_boundary_unavailable");
});

test("only a connected session accepts input", async () => {
  const api = scriptedAPI({ create: created, attach: connected, input: { ok: true, body: { sequence: 1, result: "accepted" } } });
  const session = createSession({ api, selection });
  await open(session, selection);
  const refusal = await send(session, "id");
  assert.equal(refusal.code, "terminal_not_connected");
  assert.equal(api.calls.filter((call) => call.route === "input").length, 0);
  await attach(session);
  const failure = await send(session, "id");
  assert.equal(failure, null);
  assert.equal(api.calls.find((call) => call.route === "input").sequence, 1);
});

test("each input carries an increasing per-session sequence", async () => {
  const api = scriptedAPI({
    create: created,
    attach: connected,
    input: { ok: true, body: { sequence: 1, result: "accepted", output: "ok" } },
  });
  const session = createSession({ api, selection });
  await open(session, selection);
  await attach(session);
  await send(session, "first");
  await send(session, "second");
  const sequences = api.calls.filter((call) => call.route === "input").map((call) => call.sequence);
  assert.deepEqual(sequences, [1, 2]);
});

test("a repeated sequence is answered from the receipt already held and is not delivered again", async () => {
  const api = scriptedAPI({
    create: created,
    attach: connected,
    input: { ok: true, body: { sequence: 1, result: "accepted", output: "ok" } },
  });
  const session = createSession({ api, selection });
  await open(session, selection);
  await attach(session);
  await send(session, "id");
  // A retry after a lost answer repeats the same sequence number.
  await sendSequence(session, 1, "id");
  assert.equal(api.calls.filter((call) => call.route === "input").length, 1);
  assert.equal(session.receipt.duplicate, true);
  assert.equal(session.receipt.result, "accepted");
});

test("a failure after connection shows failed with its own code and stops input", async () => {
  const api = scriptedAPI({
    create: created,
    attach: connected,
    input: { ok: false, error: { code: "terminal_executor_unavailable", message: "执行服务不可用" } },
  });
  const session = createSession({ api, selection });
  await open(session, selection);
  await attach(session);
  const failure = await send(session, "id");
  assert.equal(failure.code, "terminal_executor_unavailable");
  assert.equal(session.state, STATE_FAILED);
  assert.equal(session.reason, "terminal_executor_unavailable");
  const next = await send(session, "id");
  assert.equal(next.code, "terminal_session_ended");
});

test("end ends the session and a repeated end makes no second request", async () => {
  const api = scriptedAPI({ create: created, attach: connected, end: { ok: true, body: { sessionId: "s-1", state: "closed" } } });
  const session = createSession({ api, selection });
  await open(session, selection);
  await attach(session);
  await end(session);
  await end(session);
  assert.equal(api.calls.filter((call) => call.route === "end").length, 1);
  assert.equal(session.state, STATE_CLOSED);
});

test("the terminal failure codes map to the states the design fixes", () => {
  assert.equal(failureState("terminal_executor_unavailable"), STATE_FAILED);
  assert.equal(failureState("terminal_credential_boundary_unavailable"), STATE_FAILED);
  assert.equal(failureState("terminal_node_unreachable"), STATE_FAILED);
  assert.equal(failureState("terminal_target_ended"), STATE_FAILED);
  assert.equal(failureState("terminal_session_limit"), STATE_CLOSED);
  assert.equal(failureState("terminal_audit_unavailable"), STATE_CLOSED);
});

test("the session keeps the target, container and execution identity the control plane reported", async () => {
  const api = scriptedAPI({ create: created, attach: connected });
  const session = createSession({ api, selection });
  await open(session, selection);
  assert.equal(session.targetType, "node");
  assert.equal(session.targetId, "node-a");
  assert.equal(session.targetUid, "uid-a");
  assert.equal(session.container, "");
  assert.equal(session.executionIdentity, "root@node");
});

test("status refreshes the view and ends a session that left the live set", async () => {
  const api = scriptedAPI({
    create: created,
    attach: connected,
    status: { ok: true, body: { sessionId: "s-1", state: "closed", reason: "terminal_session_ended" } },
  });
  const session = createSession({ api, selection });
  await open(session, selection);
  await attach(session);
  assert.equal(session.state, STATE_CONNECTED);
  const failure = await status(session);
  assert.equal(failure, null);
  assert.equal(session.state, STATE_CLOSED);
  assert.equal(session.ended, true);
  const refusal = await send(session, "id");
  assert.equal(refusal.code, "terminal_session_ended");
});

test("a request that never reached the control plane ends the session for good", async () => {
  const api = scriptedAPI({
    create: created,
    attach: connected,
    input: { ok: false, error: { code: "unreachable", message: "网络不可达" } },
  });
  const session = createSession({ api, selection });
  await open(session, selection);
  await attach(session);
  const failure = await send(session, "id");
  assert.equal(failure.code, "unreachable");
  assert.equal(session.state, STATE_CLOSED);
  assert.equal(session.reason, CODE_CONNECTION_LOST);
  assert.equal(session.token, "", "a lost connection must not leave the token behind");
  assert.equal(session.ended, true);
  const refused = await send(session, "id");
  assert.equal(refused.code, "terminal_session_ended");
  assert.equal(api.calls.filter((call) => call.route === "input").length, 1);
  assert.equal(api.calls.filter((call) => call.route === "renew").length, 0);
});

test("stop drops the token and the output and still owes the end request", async () => {
  const api = scriptedAPI({
    create: created,
    attach: connected,
    input: { ok: true, body: { sequence: 1, result: "accepted", output: "ok" } },
    end: { ok: true, body: { sessionId: "s-1", state: "closed" } },
  });
  const session = createSession({ api, selection });
  await open(session, selection);
  await attach(session);
  await send(session, "id");
  assert.notEqual(session.output.length, 0);
  stop(session);
  assert.equal(session.state, STATE_CLOSED);
  assert.equal(session.reason, CODE_PAGE_CLOSED);
  assert.equal(session.token, "");
  assert.deepEqual(session.output, []);
  assert.equal(session.endRequested, false, "the end request is still owed");
  await end(session);
  assert.equal(api.calls.filter((call) => call.route === "end").length, 1);
});

test("an end clears the token, the receipt and the output", async () => {
  const api = scriptedAPI({
    create: created,
    attach: connected,
    input: { ok: true, body: { sequence: 1, result: "accepted", output: "ok" } },
    end: { ok: true, body: { sessionId: "s-1", state: "closed" } },
  });
  const session = createSession({ api, selection });
  await open(session, selection);
  await attach(session);
  await send(session, "id");
  assert.notEqual(session.receipt, null);
  await end(session);
  assert.equal(session.token, "");
  assert.equal(session.receipt, null);
  assert.deepEqual(session.output, []);
});

test("an audit refusal ends the session with its own reason", async () => {
  const api = scriptedAPI({ create: created, attach: connected });
  const session = createSession({ api, selection });
  await open(session, selection);
  await attach(session);
  applyFailure(session, { code: "terminal_audit_unavailable", message: "终端审计不可用" });
  assert.equal(session.state, STATE_CLOSED);
  assert.equal(session.reason, "terminal_audit_unavailable");
  assert.equal(session.ended, true);
  assert.equal(session.endRequested, false, "the end request is still owed");
  const refused = await send(session, "id");
  assert.equal(refused.code, "terminal_session_ended");
});
