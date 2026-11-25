// CONSOLE-TC-22's browser-asset criteria: with no permitted target the page
// states there is nothing to connect to and shows no empty prompt; a failed
// session keeps its own target and reason without touching another session's
// state; and the narrow-screen layout keeps the target and connection state
// first, the selection operable, and each terminal area scrolling on its own
// above a reachable disconnect entry (CONSOLE-C-17).

import assert from "node:assert/strict";
import test from "node:test";

import { renderTerminal, EMPTY_TARGETS } from "../../../assets/terminal.js";

const NODE_TARGETS = { items: [], reason: "节点目标只列获准建立会话的节点", empty: EMPTY_TARGETS };
const POD_TARGETS = { items: [], reason: "Pod 候选来自所属应用的投影", empty: EMPTY_TARGETS };

function pageState(overrides = {}) {
  return {
    status: "ready",
    page: {
      id: "terminal",
      title: "终端",
      stale: false,
      items: [
        { id: "target-types", value: { node: { available: true, identity: "root@node" }, pod: { available: true } } },
        { id: "node-targets", value: NODE_TARGETS },
        { id: "pod-targets", value: POD_TARGETS },
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

test("the empty page states there is nothing to connect to and shows no prompt", () => {
  const html = renderTerminal(pageState());
  assert.match(html, new RegExp(EMPTY_TARGETS));
  assert.doesNotMatch(html, /data-input=/);
  assert.doesNotMatch(html, /data-session=/);
});

test("a failed session keeps its own target and reason and leaves the other session's state alone", () => {
  const failed = {
    sessionId: "s-pod",
    targetType: "pod",
    targetId: "shop-0",
    container: "web",
    state: "failed",
    reason: "terminal_target_ended",
    expiresAt: "",
    idleTimeoutMinutes: 10,
    limits: { account: 4, target: 4 },
    usage: { account: 2, target: 1 },
    output: [],
    receipt: null,
    nextSequence: 1,
  };
  const live = {
    sessionId: "s-node",
    targetType: "node",
    targetId: "node-a",
    container: "",
    state: "connected",
    reason: "",
    expiresAt: "2026-11-25T10:05:00Z",
    idleTimeoutMinutes: 10,
    limits: { account: 4, target: 4 },
    usage: { account: 2, target: 1 },
    output: [],
    receipt: null,
    nextSequence: 1,
  };
  const html = renderTerminal(pageState({ sessions: [failed, live] }));
  // The bounds panel carries each session's own line, so an area is located by
  // its article, not by the first data-session that mentions it.
  const failedArea = html.slice(html.indexOf('class="session-area" data-session="s-pod"'));
  assert.match(failedArea, /data-failure="terminal_target_ended"/);
  assert.match(failedArea, /data-target="shop-0"/);
  const liveArea = html.slice(html.indexOf('class="session-area" data-session="s-node"'));
  assert.match(liveArea, /data-state="connected"/);
  assert.doesNotMatch(liveArea, /data-failure=/);
});

test("the narrow-screen order keeps target and state before the terminal areas", () => {
  const html = renderTerminal(pageState({ sessions: [{ sessionId: "s-1", targetType: "node", targetId: "node-a", state: "connected", output: [] }] }));
  const order = ["target-types", "target-selection", "confirm", "session-state", "sessions"].map(
    (panel) => html.indexOf(`data-panel="${panel}"`),
  );
  for (let i = 1; i < order.length; i += 1) {
    assert.ok(order[i] > order[i - 1], `the ${order[i]} panel must follow the previous one`);
  }
});

test("each terminal area carries its own scroll region and a reachable disconnect entry", () => {
  const html = renderTerminal(
    pageState({
      sessions: [
        { sessionId: "s-1", targetType: "node", targetId: "node-a", state: "connected", output: [] },
        { sessionId: "s-2", targetType: "pod", targetId: "shop-0", container: "web", state: "connected", output: [] },
      ],
    }),
  );
  const areas = [...html.matchAll(/<article class="session-area"[\s\S]*?<\/article>/g)];
  assert.equal(areas.length, 2);
  for (const area of areas) {
    assert.match(area[0], /class="session-output"/);
    assert.match(area[0], /data-action="end"/);
  }
});
