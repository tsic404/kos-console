// The terminal page shell's own checks: the panel order the design fixes, the
// empty state that replaces a prompt, the failure rendering that keeps a code
// verbatim, and the input gate that only a connected session passes. The
// renderer is pure, so these run without a browser.

import assert from "node:assert/strict";
import test from "node:test";

import { renderTerminal, EMPTY_TARGETS, STATE_LABELS } from "../../assets/terminal.js";

// pageState builds one ready page state with the projection values a control
// plane would answer.
function pageState(overrides = {}) {
  return {
    status: "ready",
    page: {
      id: "terminal",
      title: "终端",
      stale: false,
      items: [
        { id: "target-types", value: { node: { available: true, identity: "root@node" }, pod: { available: true } } },
        { id: "node-targets", value: { items: [], reason: "节点目标只列获准建立会话的节点", empty: EMPTY_TARGETS } },
        { id: "pod-targets", value: { items: [], reason: "Pod 候选来自所属应用的投影", empty: EMPTY_TARGETS } },
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

test("the page shows the target type, then selection, then confirmation", () => {
  const html = renderTerminal(pageState());
  const order = ["target-types", "target-selection", "confirm", "session-state", "sessions"].map(
    (panel) => html.indexOf(`data-panel="${panel}"`),
  );
  for (let i = 1; i < order.length; i += 1) {
    assert.ok(order[i] > order[i - 1], `panel ${order[i]} must follow ${order[i - 1]}`);
  }
});

test("a target type that is not available is not offered", () => {
  const state = pageState({
    page: {
      id: "terminal",
      stale: false,
      items: [
        { id: "target-types", value: { node: { available: false, identity: "root@node" }, pod: { available: true } } },
        { id: "node-targets", value: { items: [], reason: "非 Owner 不显示节点类型", empty: EMPTY_TARGETS } },
        { id: "pod-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
      ],
    },
  });
  const html = renderTerminal(state);
  assert.doesNotMatch(html, /data-target-type="node"/);
  assert.match(html, /data-target-type="pod"/);
});

test("an empty target list states there is nothing to connect to and shows no prompt", () => {
  const html = renderTerminal(pageState());
  assert.match(html, new RegExp(EMPTY_TARGETS));
  assert.match(html, /data-empty="node-targets"/);
  assert.doesNotMatch(html, /data-input=/);
});

test("the node target rows carry empty namespace and container strings", () => {
  const state = pageState({
    page: {
      id: "terminal",
      stale: false,
      items: [
        { id: "target-types", value: { node: { available: true, identity: "root@node" } } },
        {
          id: "node-targets",
          value: { items: [{ targetType: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" }], reason: "", empty: EMPTY_TARGETS },
        },
        { id: "pod-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
      ],
    },
  });
  const html = renderTerminal(state);
  const row = html.match(/data-target-id="node-a"[\s\S]*?data-namespace="([^"]*)" data-container="([^"]*)"/);
  assert.ok(row, "the node row must be rendered");
  assert.equal(row[1], "");
  assert.equal(row[2], "");
});

test("a multi-container Pod is never preselected and says so", () => {
  const state = pageState({
    page: {
      id: "terminal",
      stale: false,
      items: [
        { id: "target-types", value: { pod: { available: true } } },
        { id: "node-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
        {
          id: "pod-targets",
          value: {
            items: [{ namespace: "home", application: "shop", pod: "shop-0", targetUid: "uid-0", containers: ["web", "worker"], selectedContainer: "" }],
            reason: "",
            empty: EMPTY_TARGETS,
          },
        },
      ],
    },
  });
  const html = renderTerminal(state);
  assert.match(html, /未选择，需明确选择/);
});

test("a single-container Pod names its container", () => {
  const state = pageState({
    page: {
      id: "terminal",
      stale: false,
      items: [
        { id: "target-types", value: { pod: { available: true } } },
        { id: "node-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
        {
          id: "pod-targets",
          value: {
            items: [{ namespace: "home", application: "shop", pod: "shop-0", targetUid: "uid-0", containers: ["web"], selectedContainer: "web" }],
            reason: "",
            empty: EMPTY_TARGETS,
          },
        },
      ],
    },
  });
  assert.match(renderTerminal(state), /容器 web/);
});

test("a node selection states the impact may exceed one application", () => {
  const state = pageState({
    selection: { type: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" },
  });
  assert.match(renderTerminal(state), /data-impact="node"/);
});

test("a failed session shows its own code and stops input", () => {
  const state = pageState({
    sessions: [
      {
        sessionId: "s-1",
        targetType: "pod",
        targetId: "shop-0",
        container: "web",
        state: "failed",
        reason: "terminal_executor_unavailable",
        message: "执行服务不可用",
        expiresAt: "",
        idleTimeoutMinutes: 10,
        limits: { account: 4, target: 4 },
        usage: { account: 1, target: 1 },
        output: [],
        receipt: null,
        nextSequence: 1,
      },
    ],
  });
  const html = renderTerminal(state);
  assert.match(html, /data-failure="terminal_executor_unavailable"/);
  assert.match(html, new RegExp(STATE_LABELS.failed));
  assert.match(html, /disabled/);
  assert.doesNotMatch(html, /data-state="connected"/);
});

test("a connected session accepts input and shows its own target", () => {
  const state = pageState({
    sessions: [
      {
        sessionId: "s-2",
        targetType: "node",
        targetId: "node-a",
        container: "",
        state: "connected",
        reason: "",
        expiresAt: "2026-11-25T10:00:00Z",
        idleTimeoutMinutes: 10,
        limits: { account: 4, target: 4 },
        usage: { account: 1, target: 1 },
        output: [{ sequence: 1, text: "ok" }],
        receipt: { sequence: 1, result: "accepted" },
        nextSequence: 2,
      },
    ],
  });
  const html = renderTerminal(state);
  assert.match(html, /data-session="s-2"/);
  assert.match(html, /data-state="connected"/);
  assert.match(html, /data-output="s-2"/);
  assert.match(html, /data-input="s-2"/);
  assert.doesNotMatch(html, /<input[^>]*disabled/);
});

test("a hidden page states it is not visible", () => {
  const html = renderTerminal({ status: "hidden" });
  assert.match(html, /data-state="hidden"/);
});

test("a single-container Pod carries its container as the row's preselection", () => {
  const state = pageState({
    page: {
      id: "terminal",
      stale: false,
      items: [
        { id: "target-types", value: { pod: { available: true } } },
        { id: "node-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
        {
          id: "pod-targets",
          value: {
            items: [{ namespace: "home", application: "shop", pod: "shop-0", targetUid: "uid-0", containers: ["web"], selectedContainer: "web" }],
            reason: "",
            empty: EMPTY_TARGETS,
          },
        },
      ],
    },
  });
  const html = renderTerminal(state);
  const row = html.match(/data-target="pod"[^>]*data-container="([^"]*)"/);
  assert.ok(row, "the pod row must be rendered");
  assert.equal(row[1], "web");
  assert.doesNotMatch(html, /data-field="container"/);
});

test("a multi-container Pod offers a container selection control naming each container", () => {
  const state = pageState({
    page: {
      id: "terminal",
      stale: false,
      items: [
        { id: "target-types", value: { pod: { available: true } } },
        { id: "node-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
        {
          id: "pod-targets",
          value: {
            items: [{ namespace: "home", application: "shop", pod: "shop-0", targetUid: "uid-0", containers: ["web", "worker"], selectedContainer: "" }],
            reason: "",
            empty: EMPTY_TARGETS,
          },
        },
      ],
    },
  });
  const html = renderTerminal(state);
  assert.match(html, /data-field="container"/);
  assert.match(html, /<option value="web"/);
  assert.match(html, /<option value="worker"/);
  const row = html.match(/data-target="pod"[^>]*data-container="([^"]*)"/);
  assert.ok(row, "the pod row must not preselect a container");
  assert.equal(row[1], "");
});

test("the session state panel reads the limits, usage and idle bound from the session's own view", () => {
  const state = pageState({
    page: {
      id: "terminal",
      stale: false,
      items: [
        { id: "target-types", value: {} },
        { id: "node-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
        { id: "pod-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
        { id: "session-state", value: { sessions: [], rule: "" } },
      ],
    },
    sessions: [
      {
        sessionId: "s-1",
        limits: { account: 4, target: 2 },
        usage: { account: 1, target: 1 },
        idleTimeoutMinutes: 7,
      },
    ],
  });
  const html = renderTerminal(state);
  assert.match(html, /data-account-limit="4"/);
  assert.match(html, /data-account-usage="1"/);
  assert.match(html, /data-target-limit="2"/);
  assert.match(html, /data-target-usage="1"/);
  assert.match(html, /data-idle-minutes="7"/);
});

test("a session area shows its target, container and execution identity instead of an unnamed target", () => {
  const state = pageState({
    sessions: [
      {
        sessionId: "s-1",
        targetType: "pod",
        targetId: "shop-0",
        container: "web",
        executionIdentity: "container:home/shop-0/web",
        state: "connected",
        output: [],
      },
    ],
  });
  const html = renderTerminal(state);
  assert.match(html, /data-target="shop-0"/);
  assert.match(html, /data-identity="container:home\/shop-0\/web"/);
  assert.doesNotMatch(html, /未命名目标/);
});

// podTargetsPage is one page whose Pod list is the given candidates.
function podTargetsPage(items) {
  return {
    id: "terminal",
    stale: false,
    items: [
      { id: "target-types", value: { pod: { available: true } } },
      { id: "node-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
      { id: "pod-targets", value: { items, reason: "", empty: EMPTY_TARGETS } },
      { id: "session-state", value: null },
      { id: "session-audit", value: null },
    ],
  };
}

test("a chosen container shows in the row, the picker and the confirmation", () => {
  const state = pageState({
    page: podTargetsPage([
      { namespace: "home", application: "shop", pod: "shop-0", targetUid: "uid-0", containers: ["web", "worker"], selectedContainer: "" },
    ]),
    selection: { type: "pod", targetId: "shop-0", targetUid: "uid-0", namespace: "home", container: "worker" },
  });
  const html = renderTerminal(state);
  assert.match(html, /<option value="worker" selected>/);
  assert.match(html, /data-container="worker"/);
  assert.match(html, /data-confirm-container="worker"/);
  assert.match(html, /容器 worker/);
});

test("two Pods sharing a name keep their own container state", () => {
  const state = pageState({
    page: podTargetsPage([
      { namespace: "home", application: "shop", pod: "shop-0", targetUid: "uid-home", containers: ["web", "worker"], selectedContainer: "" },
      { namespace: "reports", application: "shop", pod: "shop-0", targetUid: "uid-reports", containers: ["web", "worker"], selectedContainer: "" },
    ]),
    selection: { type: "pod", targetId: "shop-0", targetUid: "uid-home", namespace: "home", container: "worker" },
  });
  const html = renderTerminal(state);
  const home = html.match(/data-namespace="home"[^>]*data-container="([^"]*)"/);
  const reports = html.match(/data-namespace="reports"[^>]*data-container="([^"]*)"/);
  assert.ok(home && reports, "both rows must be rendered");
  assert.equal(home[1], "worker");
  assert.equal(reports[1], "", "the other Pod must not take this Pod's container");
});

test("the session state panel keeps each session's own bounds and says unknown instead of zero", () => {
  const state = pageState({
    page: { id: "terminal", stale: false, items: [] },
    sessions: [
      { sessionId: "s-failed", state: "failed", limits: {}, usage: {}, idleTimeoutMinutes: 0 },
      { sessionId: "s-live", state: "connected", limits: { account: 4, target: 2 }, usage: { account: 3, target: 1 }, idleTimeoutMinutes: 7 },
    ],
  });
  const html = renderTerminal(state);
  // The failed first session must not hide the live one's real numbers.
  assert.match(html, /data-session="s-live"[^>]*data-account-limit="4"/);
  assert.match(html, /data-session="s-live"[^>]*data-target-limit="2"/);
  assert.match(html, /data-session="s-live"[^>]*data-idle-minutes="7"/);
  // A value the control plane did not report is unknown, never a made-up zero.
  assert.match(html, /data-session="s-failed"[^>]*data-account-limit="未知"/);
});

test("two sessions show their own bounds side by side", () => {
  const state = pageState({
    page: { id: "terminal", stale: false, items: [] },
    sessions: [
      { sessionId: "s-1", limits: { account: 4, target: 4 }, usage: { account: 2, target: 1 }, idleTimeoutMinutes: 10 },
      { sessionId: "s-2", limits: { account: 4, target: 2 }, usage: { account: 3, target: 2 }, idleTimeoutMinutes: 5 },
    ],
  });
  const html = renderTerminal(state);
  assert.match(html, /data-session="s-1"[^>]*data-idle-minutes="10"/);
  assert.match(html, /data-session="s-2"[^>]*data-idle-minutes="5"/);
  assert.match(html, /data-session="s-2"[^>]*data-target-limit="2"/);
});

// sessionRow is one session area with the fields the terminal surface carries.
function sessionRow(overrides = {}) {
  return {
    sessionId: "s-1",
    targetType: "node",
    targetId: "node-a",
    container: "",
    state: "closed",
    reason: "",
    message: "",
    expiresAt: "",
    idleTimeoutMinutes: 10,
    limits: { account: 4, target: 4 },
    usage: { account: 1, target: 1 },
    output: [],
    receipt: null,
    nextSequence: 1,
    endSettled: false,
    endUnknown: false,
    reapPending: false,
    ...overrides,
  };
}

// The three outcomes of a session's remote end are drawn apart from the
// session's own failure reason, and the failure code the control plane or the
// page already reported is never rewritten by that statement (CONSOLE-C-11,
// CONSOLE-C-12). Every starting state keeps its own code while the end result
// is unconfirmed, pending or settled.
const SETTLEMENT_STARTS = [
  { name: "a failed session", session: { state: "failed", reason: "terminal_executor_unavailable", message: "执行服务不可用" } },
  { name: "a session the audit closed", session: { state: "closed", reason: "terminal_audit_unavailable", message: "终端审计不可用" } },
  { name: "a session whose live end failed", session: { state: "closed", reason: "end_result_unknown", message: "结束请求未得到确认" } },
];

test("an unconfirmed end states the target result is unknown beside the session's own code", () => {
  for (const start of SETTLEMENT_STARTS) {
    const html = renderTerminal(pageState({ sessions: [sessionRow({ ...start.session, endUnknown: true, reapPending: true })] }));
    // The original code stays readable: the settlement is never folded into it.
    assert.match(html, new RegExp(`data-failure="${start.session.reason}"`), `${start.name} keeps its own code`);
    assert.match(html, new RegExp(`data-state="${start.session.state}"`), `${start.name} keeps its own state`);
    // The unconfirmed end is its own statement with the wording the design fixes.
    assert.match(html, /data-settlement="unknown"/, `${start.name} shows the unconfirmed end`);
    assert.match(html, /data-end-unknown="true"/);
    assert.match(html, /data-reap-pending="true"/);
    assert.match(html, /结束未确认/);
    assert.match(html, /目标进程结果未知/);
    assert.match(html, /可能仍有未完成的副作用/);
    // And it never turns an ended session into an input-taking one.
    assert.match(html, /<textarea[^>]*disabled/);
    assert.doesNotMatch(html, /data-state="connected"/);
  }
});

test("a debt still owed reads as pending, not as a confirmed or unknown end", () => {
  for (const start of SETTLEMENT_STARTS) {
    const html = renderTerminal(pageState({ sessions: [sessionRow({ ...start.session, reapPending: true })] }));
    assert.match(html, new RegExp(`data-failure="${start.session.reason}"`), `${start.name} keeps its own code`);
    assert.match(html, /data-settlement="pending"/, `${start.name} shows the owed end`);
    assert.match(html, /data-reap-pending="true"/);
    assert.match(html, /data-end-unknown="false"/);
    assert.doesNotMatch(html, /目标进程结果未知/, `${start.name} must not claim an unknown result while the end is merely owed`);
  }
});

test("a confirmed end collapses the warning to a settlement beside the session's own code", () => {
  for (const start of SETTLEMENT_STARTS) {
    const html = renderTerminal(pageState({ sessions: [sessionRow({ ...start.session, endSettled: true })] }));
    assert.match(html, new RegExp(`data-failure="${start.session.reason}"`), `${start.name} keeps its own code`);
    assert.match(html, /data-settlement="settled"/, `${start.name} shows the confirmed end`);
    assert.match(html, /data-end-settled="true"/);
    assert.match(html, /data-end-unknown="false"/);
    assert.match(html, /data-reap-pending="false"/);
    assert.doesNotMatch(html, /结束未确认/);
    assert.doesNotMatch(html, /目标进程结果未知/);
  }
});

test("a live session with nothing owed draws no settlement statement", () => {
  const html = renderTerminal(pageState({ sessions: [sessionRow({ state: "connected" })] }));
  assert.doesNotMatch(html, /data-settlement=/);
});
