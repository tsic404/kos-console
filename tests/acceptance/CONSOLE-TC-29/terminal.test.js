// CONSOLE-TC-29's browser-asset criteria: the empty state states there is
// nothing to connect to, then a real target connects, takes input and
// disconnects while a second failed session keeps its own target and reason,
// all with the controls the narrow-screen journey needs (CONSOLE-C-17).

import assert from "node:assert/strict";
import test from "node:test";

import { renderTerminal, EMPTY_TARGETS, STATE_LABELS } from "../../../assets/terminal.js";

function pageState(overrides = {}) {
  return {
    status: "ready",
    page: {
      id: "terminal",
      title: "终端",
      stale: false,
      items: [
        { id: "target-types", value: { node: { available: true, identity: "root@node" }, pod: { available: true } } },
        { id: "node-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
        { id: "pod-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
        { id: "session-state", value: null },
        { id: "session-audit", value: null },
      ],
    },
    registry: [{ id: "terminal" }],
    selection: { type: "", targetId: "", targetUid: "", namespace: "", container: "" },
    confirming: false,
    sessions: [],
    notice: null,
    readError: null,
    ...overrides,
  };
}

test("the empty state states there is nothing to connect to before any session exists", () => {
  const html = renderTerminal(pageState());
  assert.match(html, new RegExp(EMPTY_TARGETS));
  assert.doesNotMatch(html, /data-input=/);
});

test("a live session and a failed session are each distinguishable with their own target and state", () => {
  const html = renderTerminal(
    pageState({
      sessions: [
        {
          sessionId: "s-node",
          targetType: "node",
          targetId: "node-a",
          container: "",
          state: "connected",
          reason: "",
          expiresAt: "2026-11-25T10:05:00Z",
          idleTimeoutMinutes: 10,
          limits: { account: 4, target: 4 },
          usage: { account: 1, target: 1 },
          output: [{ sequence: 1, text: "uid=1000" }],
          receipt: { sequence: 1, result: "accepted" },
          nextSequence: 2,
        },
        {
          sessionId: "s-pod",
          targetType: "pod",
          targetId: "shop-0",
          container: "web",
          state: "failed",
          reason: "terminal_target_ended",
          message: "容器已退出",
          expiresAt: "",
          idleTimeoutMinutes: 10,
          limits: { account: 4, target: 4 },
          usage: { account: 1, target: 1 },
          output: [],
          receipt: null,
          nextSequence: 1,
        },
      ],
    }),
  );
  const live = html.slice(html.indexOf('data-session="s-node"'));
  const failed = html.slice(html.indexOf('data-session="s-pod"'));
  assert.match(live, /data-state="connected"/);
  assert.match(live, /data-target="node-a"/);
  assert.match(live, /data-output="s-node"/);
  assert.match(failed, new RegExp(STATE_LABELS.failed));
  assert.match(failed, /data-failure="terminal_target_ended"/);
  assert.match(failed, /data-target="shop-0"/);
});

test("a disconnected session stops input at the document", () => {
  const html = renderTerminal(
    pageState({
      sessions: [
        {
          sessionId: "s-closed",
          targetType: "node",
          targetId: "node-a",
          container: "",
          state: "closed",
          reason: "terminal_session_ended",
          expiresAt: "",
          idleTimeoutMinutes: 10,
          limits: { account: 4, target: 4 },
          usage: { account: 0, target: 0 },
          output: [],
          receipt: null,
          nextSequence: 1,
        },
      ],
    }),
  );
  assert.match(html, /data-input="s-closed"/);
  assert.match(html, /<textarea[^>]*disabled/);
});
