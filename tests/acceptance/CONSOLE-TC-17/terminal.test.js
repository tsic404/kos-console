// CONSOLE-TC-17's browser-asset criteria for the audit path: when the control
// plane reports its audit trail unavailable, the page ends the session it was
// reading for, keeps the code verbatim and stops offering input, rather than
// showing a notice over a session the user can still type into
// (CONSOLE-C-15, KOSD-C-96).

import assert from "node:assert/strict";
import test from "node:test";

import { applyFailure, createSession, end, send, STATE_CLOSED } from "../../../assets/session.js";
import { renderTerminal } from "../../../assets/terminal.js";

function pageState(sessions) {
  return {
    status: "ready",
    page: {
      id: "terminal",
      title: "终端",
      stale: false,
      items: [
        { id: "target-types", value: { node: { available: true, identity: "root@node" } } },
        { id: "node-targets", value: { items: [], reason: "", empty: "无可连接目标" } },
        { id: "pod-targets", value: { items: [], reason: "", empty: "无可连接目标" } },
        { id: "session-state", value: null },
        { id: "session-audit", value: null },
      ],
    },
    registry: [{ id: "terminal" }],
    selection: { type: "", targetId: "", targetUid: "", namespace: "", container: "" },
    confirming: false,
    sessions,
    notice: null,
    readError: null,
  };
}

// connectedAPI is one session the control plane already accepted.
function connectedAPI() {
  const calls = [];
  return {
    calls,
    createSession: async () => ({
      ok: true,
      body: { sessionId: "s-1", targetType: "node", targetId: "node-a", state: "connecting", token: "t-1" },
    }),
    attach: async () => ({ ok: true, body: { sessionId: "s-1", state: "connected" } }),
    input: async () => {
      calls.push("input");
      return { ok: true, body: { sequence: 1, result: "accepted" } };
    },
    renew: async () => ({ ok: true, body: { sessionId: "s-1", state: "connected" } }),
    status: async () => ({ ok: true, body: { sessionId: "s-1", state: "connected" } }),
    end: async () => ({ ok: true, body: { sessionId: "s-1", state: "closed" } }),
  };
}

test("an unavailable audit trail ends the session and refuses the next input", async () => {
  const api = connectedAPI();
  const selection = { type: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" };
  const session = createSession({ api, selection });
  session.sessionId = "s-1";
  session.token = "t-1";
  session.state = "connected";

  applyFailure(session, { code: "terminal_audit_unavailable", message: "终端审计不可用" });
  assert.equal(session.state, STATE_CLOSED);
  assert.equal(session.reason, "terminal_audit_unavailable");
  const refusal = await send(session, "id\n");
  assert.equal(refusal.code, "terminal_session_ended");
  assert.deepEqual(api.calls, [], "no input may reach the target after the refusal");

  await end(session);
  const html = renderTerminal(pageState([session]));
  assert.match(html, /data-state="closed"/);
  assert.match(html, /data-failure="terminal_audit_unavailable"/);
  assert.match(html, /<textarea[^>]*disabled/);
});
