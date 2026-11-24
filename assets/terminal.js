// The terminal page shell: it turns the page projection the control plane
// serves and the sessions the browser holds into one document (CONSOLE-C-8 to
// CONSOLE-C-17). Every function here is pure — it takes the state and returns
// the HTML the browser shows, so the rendered page is judged without a DOM.
//
// The page never holds a cluster credential: it reads the control plane's page
// projection and drives the session surface, and the terminal token lives in
// the session's memory alone (KOSD-C-12, KOSD-C-91, CONSOLE-C-14).

import { escapeHTML } from "./html.js";

// Session states the control plane reports (KOSD-C-90). Only connected accepts
// input, and the page never shows another state as connected.
export const STATE_LABELS = {
  connecting: "连接中",
  connected: "已连接",
  closed: "已结束",
  failed: "失败",
};

// The terminal failure codes the design fixes (CONSOLE-C-12). Each is shown
// as its own reason; the two that end a session are terminal states and are
// never folded into a permission refusal.
export const TERMINAL_FAILURE_CODES = [
  "terminal_node_unreachable",
  "terminal_target_ended",
  "terminal_executor_unavailable",
  "terminal_credential_boundary_unavailable",
];

// Target type labels (KOSD-C-95).
export const TARGET_TYPE_LABELS = { node: "节点", pod: "Pod" };

// EMPTY_TARGETS is the page's statement when no permitted target exists: no
// empty terminal prompt stands in for a session (CONSOLE-C-17).
export const EMPTY_TARGETS = "无可连接目标";

// NOT_CONNECTED is the page's statement that a session is not accepting input.
export const NOT_CONNECTED = "会话未连接，不能输入";

// renderTerminal draws the whole page from one state.
export function renderTerminal(state) {
  const {
    status = "loading",
    page = null,
    registry = [],
    selection = emptySelection(),
    confirming = false,
    sessions = [],
    notice = null,
    readError = null,
    audit = null,
    networkDown = false,
  } = state || {};

  if (status === "loading") {
    return `<div class="terminal" data-state="loading"><p class="loading" data-state="loading">正在读取终端页面…</p></div>`;
  }
  // A page the registry does not list is one this session cannot see: neither
  // its entry nor its body appears (DASH-C-22, KOSD-F-16).
  if (status === "hidden") {
    return `<div class="terminal" data-state="hidden"><p class="empty-state" data-empty="page-hidden">${escapeHTML(HIDDEN_NOTE)}</p></div>`;
  }
  if (status === "error") {
    const error = readError || { code: "internal", message: "终端页面读取失败" };
    return `<div class="terminal" data-state="error"><p class="failure" data-failure="${escapeHTML(error.code)}">${escapeHTML(error.message)}</p></div>`;
  }

  const items = itemMap(page);
  return `
<div class="terminal" data-state="ready">
  <header class="terminal-header">
    <span class="brand">终端</span>
    ${renderStaleMark(page)}
    ${renderNotice(notice)}
  </header>
  ${renderTargetTypes(items.targetTypes)}
  ${renderTargetSelection(items.nodeTargets, items.podTargets, selection)}
  ${renderConfirm(items.targetTypes, selection, confirming)}
  ${renderSessionState(items.sessionState, sessions)}
  ${renderSessionAreas(sessions, networkDown)}
  ${renderAudit(audit != null ? audit : items.audit)}
</div>`;
}

// HIDDEN_NOTE is what the page shows when the identity cannot see it.
export const HIDDEN_NOTE = "本页不可见：提供它的组件未生效，或当前身份看不到它。";

// renderTargetTypes draws which target kinds the account may use and the
// execution identity each reaches the target as (CONSOLE-C-9, CONSOLE-C-10).
function renderTargetTypes(value) {
  const types = value && typeof value === "object" ? value : {};
  const rows = ["node", "pod"]
    .map((kind) => {
      const entry = types[kind];
      if (!entry || entry.available !== true) return "";
      return `<div class="target-type" data-target-type="${escapeHTML(kind)}">
        <span class="target-type-name">${escapeHTML(TARGET_TYPE_LABELS[kind] || kind)}</span>
        <span class="target-type-identity" data-identity="${escapeHTML(kind)}">执行身份：${escapeHTML(entry.identity || "")}</span>
      </div>`;
    })
    .join("");
  return `<section class="panel" data-panel="target-types"><h2>目标类型与执行身份</h2><div class="target-types">${rows}</div></section>`;
}

// renderTargetSelection draws the node and Pod candidate lists. A candidate the
// account may not connect to never appears (CONSOLE-C-10); with no permitted
// target the page states it instead of showing an empty prompt (CONSOLE-C-17).
// Each row carries its own target identity and container, so selecting a Pod
// carries the container the page submits (CONSOLE-C-8).
function renderTargetSelection(nodeValue, podValue, selection) {
  const nodes = targetItems(nodeValue);
  const pods = podItems(podValue);
  const nodeRows = nodes.length
    ? nodes
        .map(
          (node) => `
      <div class="target-row" data-target="node" data-target-id="${escapeHTML(node.targetId)}" data-target-uid="${escapeHTML(node.targetUid)}" data-namespace="${escapeHTML(node.namespace || "")}" data-container="${escapeHTML(node.container || "")}" data-application="">
        <label class="target-label"><input type="radio" name="target" value="node" data-field="target-type" ${selectionMatches(selection, nodeIdentity(node)) ? "checked" : ""}><span class="target-name">${escapeHTML(node.targetId)}</span></label>
        <span class="target-fields">命名空间 ${escapeHTML(node.namespace || "（空）")} · 容器 ${escapeHTML(node.container || "（空）")}</span>
      </div>`,
        )
        .join("")
    : `<p class="empty-state" data-empty="node-targets">${escapeHTML(EMPTY_TARGETS)}</p>`;

  const podRows = pods.length
    ? pods
        .map(
          (pod) => {
            const containers = Array.isArray(pod.containers) ? pod.containers : [];
            const preselected = containers.length === 1 ? containers[0] : "";
            // The row shows the container the page currently holds for this Pod.
            // A Pod is identified by namespace, name and UID, so a selection
            // made on another Pod never leaks its container here (CONSOLE-C-8).
            const held = selectionMatches(selection, podIdentity(pod)) ? selection.container || "" : preselected;
            return `
      <div class="target-row" data-target="pod" data-target-id="${escapeHTML(pod.pod)}" data-target-uid="${escapeHTML(pod.targetUid)}" data-namespace="${escapeHTML(pod.namespace || "")}" data-application="${escapeHTML(pod.application || "")}" data-container="${escapeHTML(held)}">
        <label class="target-label"><input type="radio" name="target" value="pod" data-field="target-type" ${selectionMatches(selection, podIdentity(pod)) ? "checked" : ""}><span class="target-name">${escapeHTML(pod.namespace)} / ${escapeHTML(pod.pod)}</span></label>
        <span class="target-fields">容器 ${escapeHTML(containerText(pod, selection))}</span>
        ${containerControl(pod, containers, selection)}
      </div>`;
          },
        )
        .join("")
    : `<p class="empty-state" data-empty="pod-targets">${escapeHTML(EMPTY_TARGETS)}</p>`;

  return `<section class="panel" data-panel="target-selection">
    <h2>目标选择</h2>
    <div class="target-group"><h3>节点</h3>${nodeRows}</div>
    <div class="target-group"><h3>Pod</h3>${podRows}</div>
  </section>`;
}

// containerControl is the picker a multi-container Pod offers. A Pod with one
// container is preselected on the row and needs no control; a multi-container
// Pod is never preselected and lists each container. The option the page
// currently holds for this Pod is marked, so a redraw after a choice keeps it
// (CONSOLE-C-8).
function containerControl(pod, containers, selection) {
  if (containers.length <= 1) return "";
  const held = selectionMatches(selection, podIdentity(pod)) ? selection.container || "" : "";
  const options = containers
    .map((name) => `<option value="${escapeHTML(name)}"${name === held ? " selected" : ""}>${escapeHTML(name)}</option>`)
    .join("");
  return `<select class="container-select" data-field="container" data-container-for="${escapeHTML(pod.pod)}" aria-label="选择容器"><option value="">（请选择容器）</option>${options}</select>`;
}

// containerText states a Pod's container selection: a multi-container Pod is
// never preselected and says so, a single-container Pod names its container
// (CONSOLE-C-8).
function containerText(pod, selection) {
  const containers = Array.isArray(pod.containers) ? pod.containers : [];
  if (containers.length === 0) return "（空）";
  const held = selectionMatches(selection, podIdentity(pod)) ? selection.container : "";
  if (held) return held;
  if (containers.length === 1) return escapeHTML(containers[0]);
  if (pod.selectedContainer) return escapeHTML(pod.selectedContainer);
  return "未选择，需明确选择";
}

// podIdentity is one Pod candidate as the identity matcher reads it.
function podIdentity(pod) {
  return { type: "pod", targetId: pod.pod, namespace: pod.namespace, targetUid: pod.targetUid };
}

// nodeIdentity is one node candidate as the identity matcher reads it.
function nodeIdentity(node) {
  return { type: "node", targetId: node.targetId, namespace: node.namespace, targetUid: node.targetUid };
}

// selectionMatches reports whether one candidate is the target the page
// currently holds. Type, namespace, name and UID all take part, so two Pods
// that share a name in different namespaces never exchange identity
// (CONSOLE-C-8, KOSD-C-95).
export function selectionMatches(selection, candidate) {
  if (!selection || !candidate) return false;
  return (
    selection.type === candidate.type &&
    selection.targetId === (candidate.targetId || "") &&
    selection.namespace === (candidate.namespace || "") &&
    selection.targetUid === (candidate.targetUid || "")
  );
}

// renderConfirm draws the execution identity and the impact confirmation. A
// node connection confirms that the impact may exceed a single application;
// cancelling establishes no session (CONSOLE-C-9).
function renderConfirm(types, selection, confirming) {
  const identity = executionIdentityOf(types, selection.type);
  const targetLabel = selection.targetId ? escapeHTML(selection.targetId) : "未选择目标";
  const namespace = selection.namespace ? escapeHTML(selection.namespace) : "（空）";
  const container = selection.container ? escapeHTML(selection.container) : selection.type === "pod" ? "未选择" : "（无容器）";
  return `<section class="panel" data-panel="confirm">
    <h2>执行身份与影响确认</h2>
    <p class="confirm-line" data-identity="${escapeHTML(identity)}">目标 ${targetLabel} 的执行身份：${escapeHTML(identity)}</p>
    <p class="confirm-fields" data-confirm-namespace="${escapeHTML(selection.namespace || "")}" data-confirm-container="${escapeHTML(selection.container || "")}">命名空间 ${namespace} · 容器 ${container}</p>
    ${
      selection.type === "node"
        ? `<p class="confirm-impact" data-impact="node">节点连接的影响可能超出单个应用，确认后才会建立会话。</p>`
        : ""
    }
    <div class="confirm-actions">
      <button type="button" class="confirm-connect" data-action="connect" ${selection.targetId ? "" : "disabled"}>连接并确认</button>
      ${confirming ? `<span class="confirm-pending" data-pending="connect">正在建立会话…</span>` : ""}
    </div>
  </section>`;
}

// executionIdentityOf resolves the execution identity a selection reaches the
// target as, from the target type the page declared (CONSOLE-C-9).
function executionIdentityOf(types, type) {
  const entry = types && types[type];
  if (entry && typeof entry.identity === "string") return entry.identity;
  return type === "pod" ? "容器身份" : "";
}

// renderSessionState draws the account and target bounds, the occupancy and the
// idle bound of every session the page holds (KOSD-C-90, KOSD-C-94). Each
// session's own view is its own line, so a session that failed or already ended
// never hides the numbers of the others, and a value the control plane did not
// report is shown as unknown rather than made up as a zero (CONSOLE-C-13).
function renderSessionState(value, held) {
  const reported = value && Array.isArray(value.sessions) ? value.sessions : [];
  const pool = Array.isArray(held) && held.length ? held : reported;
  const lines = pool.map((entry) => stateLine(entry)).join("");
  const body = pool.length ? lines : `<p class="empty-state" data-empty="session-state">会话状态不可用</p>`;
  return `<section class="panel" data-panel="session-state">
    <h2>会话状态与占用</h2>
    ${body}
    ${value && value.rule ? `<p class="state-rule">${escapeHTML(value.rule)}</p>` : ""}
    ${reported.length ? `<p class="state-count" data-session-count="${escapeHTML(reported.length)}">控制面报告的会话数：${escapeHTML(reported.length)}</p>` : ""}
  </section>`;
}

// stateLine draws one session's own bounds and occupancy.
function stateLine(entry) {
  const limits = entry && entry.limits && typeof entry.limits === "object" ? entry.limits : null;
  const usage = entry && entry.usage && typeof entry.usage === "object" ? entry.usage : null;
  const idle = entry && typeof entry.idleTimeoutMinutes === "number" ? entry.idleTimeoutMinutes : "未知";
  const accountLimit = bound(limits, "account");
  const accountUsage = bound(usage, "account");
  const targetLimit = bound(limits, "target");
  const targetUsage = bound(usage, "target");
  return `<p class="session-state-line" data-session="${escapeHTML(entry && entry.sessionId ? entry.sessionId : "")}" data-account-limit="${escapeHTML(accountLimit)}" data-account-usage="${escapeHTML(accountUsage)}" data-target-limit="${escapeHTML(targetLimit)}" data-target-usage="${escapeHTML(targetUsage)}" data-idle-minutes="${escapeHTML(idle)}">
    会话 ${escapeHTML(entry && entry.sessionId ? entry.sessionId : "")}：账号会话 ${escapeHTML(accountUsage)} / ${escapeHTML(accountLimit)}，目标会话 ${escapeHTML(targetUsage)} / ${escapeHTML(targetLimit)}，本会话空闲 ${escapeHTML(idle)} 分钟
  </p>`;
}

// bound reads one limit or occupancy counter. A value the control plane did not
// report is stated as unknown, never as a made-up zero (CONSOLE-C-13).
function bound(source, key) {
  const value = source && typeof source === "object" ? source[key] : undefined;
  return typeof value === "number" ? value : "未知";
}

// renderSessionAreas draws one terminal area per session the browser holds.
// Each area keeps its own target, state and failure reason, so one failure
// never overwrites another session (CONSOLE-C-11, CONSOLE-C-17).
function renderSessionAreas(sessions, networkDown) {
  if (!sessions.length) {
    return `<section class="panel" data-panel="sessions"><h2>终端区域</h2><p class="empty-state" data-empty="sessions">尚未建立会话，确认目标后才会连接。</p></section>`;
  }
  const areas = sessions.map((session) => renderSessionArea(session, networkDown)).join("");
  return `<section class="panel" data-panel="sessions"><h2>终端区域</h2><div class="session-areas">${areas}</div></section>`;
}

// renderSessionArea draws one session's terminal area: its target, container,
// execution identity, state, token expiry and the output and input that belong
// to it alone (CONSOLE-C-9).
function renderSessionArea(session, networkDown) {
  const target = session.targetId ? escapeHTML(session.targetId) : "未命名目标";
  const container = session.container
    ? escapeHTML(session.container)
    : session.targetType === "node"
      ? "（无容器）"
      : "（未选择容器）";
  const identity = session.executionIdentity ? escapeHTML(session.executionIdentity) : "";
  const state = STATE_LABELS[session.state] || session.state || "";
  const connected = session.state === "connected" && !networkDown;
  const failure = renderFailure(session);
  const settlement = renderSettlement(session);
  const output = (session.output || [])
    .map(
      (line) => `<div class="output-line" data-sequence="${escapeHTML(line.sequence)}">${escapeHTML(line.text)}</div>`,
    )
    .join("");
  const receipt = renderReceipt(session);
  return `<article class="session-area" data-session="${escapeHTML(session.sessionId)}" data-state="${escapeHTML(session.state)}" data-target-type="${escapeHTML(session.targetType)}" data-target="${escapeHTML(session.targetId)}" data-container="${escapeHTML(session.container || "")}" data-expires-at="${escapeHTML(session.expiresAt || "")}" data-idle-minutes="${escapeHTML(session.idleTimeoutMinutes ?? "")}" data-limits="${escapeHTML(session.limits ? session.limits.account : "")}/${escapeHTML(session.limits ? session.limits.target : "")}" data-usage="${escapeHTML(session.usage ? session.usage.account : "")}/${escapeHTML(session.usage ? session.usage.target : "")}">
    <header class="session-header">
      <span class="session-target">${target}</span>
      <span class="session-container" data-container="${escapeHTML(session.container || "")}">容器 ${container}</span>
      ${identity ? `<span class="session-identity" data-identity="${identity}">执行身份 ${identity}</span>` : ""}
      <span class="session-state" data-state="${escapeHTML(session.state)}">${escapeHTML(state)}</span>
      <span class="session-expires" data-expires="${escapeHTML(session.expiresAt || "")}">令牌到期 ${escapeHTML(session.expiresAt || "未知")}</span>
      <button type="button" class="session-end" data-action="end" data-session="${escapeHTML(session.sessionId)}">断开</button>
    </header>
    ${failure}
    ${settlement}
    <div class="session-output" data-output="${escapeHTML(session.sessionId)}">${output || `<p class="empty-output" data-empty="output">尚未收到输出</p>`}</div>
    ${receipt}
    <form class="session-input" data-input="${escapeHTML(session.sessionId)}">
      <label class="sr-only" for="input-${escapeHTML(session.sessionId)}">向 ${target} 输入</label>
      <textarea id="input-${escapeHTML(session.sessionId)}" name="data" data-field="input" rows="3" ${connected ? "" : "disabled"} placeholder="${connected ? "输入命令" : NOT_CONNECTED}"></textarea>
      <button type="submit" data-action="send" data-session="${escapeHTML(session.sessionId)}" ${connected ? "" : "disabled"}>发送</button>
    </form>
  </article>`;
}

// renderFailure draws a session's failure reason as the control plane reported
// it. The terminal failure codes are shown verbatim and the session is marked
// failed, so a failure is never shown as connected and never folded into a
// permission refusal (CONSOLE-C-12).
function renderFailure(session) {
  if (session.state !== "failed" && !session.reason) return "";
  const code = session.reason || "";
  return `<p class="session-failure" data-failure="${escapeHTML(code)}">失败原因：${escapeHTML(code)}${session.message ? "（" + escapeHTML(session.message) + "）" : ""}，已停止输入</p>`;
}

// renderSettlement draws the outcome of the session's remote end as its own
// statement, apart from the session's own failure reason: an end the page could
// not confirm leaves the target process's result unknown, and that stays
// readable beside the failure code instead of being folded into it or used to
// re-enable input (CONSOLE-C-11, CONSOLE-C-12). The statement holds while the
// debt is still owed and collapses to a confirmation once the control plane
// answers the end.
function renderSettlement(session) {
  const settled = Boolean(session.endSettled);
  const unknown = Boolean(session.endUnknown);
  const pending = Boolean(session.reapPending);
  if (!settled && !unknown && !pending) return "";
  const attrs = `data-end-settled="${settled ? "true" : "false"}" data-end-unknown="${unknown ? "true" : "false"}" data-reap-pending="${pending ? "true" : "false"}"`;
  if (unknown) {
    return `<p class="session-settlement" data-settlement="unknown" ${attrs}>结束未确认：目标进程结果未知，可能仍有未完成的副作用。</p>`;
  }
  if (pending) {
    return `<p class="session-settlement" data-settlement="pending" ${attrs}>结束尚未确认：控制面可能仍保留该会话，页面会在可达时释放。</p>`;
  }
  return `<p class="session-settlement" data-settlement="settled" ${attrs}>控制面已确认该会话结束。</p>`;
}

// renderReceipt draws one input's receipt. The page states the receipt the
// executor reported; a receipt is a receipt and never a claim that a command
// ran (CONSOLE-C-11, KOSD-C-92).
function renderReceipt(session) {
  const receipt = session.receipt;
  if (!receipt) return "";
  return `<p class="session-receipt" data-receipt="${escapeHTML(receipt.sequence)}${receipt.duplicate ? " duplicate" : ""}">输入序号 ${escapeHTML(receipt.sequence)} 的接收结果：${escapeHTML(receipt.result || "")}${receipt.duplicate ? "（重复序号，返回原接收结果，未再次送达）" : ""}</p>`;
}

// renderAudit draws the session audit metadata. The page shows the metadata the
// control plane authorises and never a batch count as a number of executed
// commands (CONSOLE-C-15, KOSD-C-96).
function renderAudit(value) {
  if (!value) return "";
  const rows = Array.isArray(value.sessions)
    ? value.sessions
        .map((entry) => {
          const events = Array.isArray(entry.events) ? entry.events : [];
          const items = events
            .map(
              (event) => `<li class="audit-event" data-event="${escapeHTML(event.event || "")}" data-sequence="${escapeHTML(event.sequence ?? "")}">${escapeHTML(event.event || "")} · ${escapeHTML(event.result || "")}${event.sequence !== undefined ? " · 输入序号 " + escapeHTML(event.sequence) : ""}</li>`,
            )
            .join("");
          return `<div class="audit-session" data-audit-session="${escapeHTML(entry.sessionId)}"><h3>会话 ${escapeHTML(entry.sessionId)}</h3>${items ? `<ul class="audit-events">${items}</ul>` : `<p class="empty-state" data-empty="audit">无审计事件</p>`}</div>`;
        })
        .join("")
    : "";
  return `<section class="panel" data-panel="audit"><h2>会话审计</h2>${rows || `<p class="empty-state" data-empty="audit">无审计事件</p>`}${value.rule ? `<p class="audit-rule">${escapeHTML(value.rule)}</p>` : ""}</section>`;
}

// renderNotice draws the page-level notice of a refused action, keeping the
// control plane's own code and reason (KOSD-C-12).
function renderNotice(notice) {
  if (!notice) return "";
  const kind = notice.kind === "error" ? "failure" : "notice";
  return `<p class="${kind}" data-notice="${escapeHTML(notice.code || "")}">${escapeHTML(notice.message)}</p>`;
}

// renderStaleMark states that the page projection was observed beyond the one
// freshness bound the control plane fixes (KOSD-C-13).
function renderStaleMark(page) {
  return page && page.stale ? `<span class="stale" data-stale="true">数据已陈旧</span>` : "";
}

// itemMap indexes one page projection's items by identity.
function itemMap(page) {
  const items = page && Array.isArray(page.items) ? page.items : [];
  const byID = {};
  for (const item of items) byID[item.id] = item;
  return {
    targetTypes: byID["target-types"] ? byID["target-types"].value : null,
    nodeTargets: byID["node-targets"] ? byID["node-targets"].value : null,
    podTargets: byID["pod-targets"] ? byID["pod-targets"].value : null,
    sessionState: byID["session-state"] ? byID["session-state"].value : null,
    audit: byID["session-audit"] ? byID["session-audit"].value : null,
  };
}

// targetItems reads the node candidate list of one item value.
function targetItems(value) {
  const items = value && Array.isArray(value.items) ? value.items : [];
  return items;
}

// podItems reads the Pod candidate list of one item value.
function podItems(value) {
  const items = value && Array.isArray(value.items) ? value.items : [];
  return items;
}

// emptySelection is a selection with no target chosen.
export function emptySelection() {
  return { type: "", targetId: "", targetUid: "", namespace: "", container: "" };
}

// selectionFromTarget builds the selection a confirmed target implies.
export function selectionFromTarget(target) {
  return {
    type: target.type,
    targetId: target.id,
    targetUid: target.uid,
    namespace: target.namespace || "",
    container: target.container || "",
  };
}

// isTerminalFailure reports whether a failure code is one of the terminal
// failure states the design fixes (CONSOLE-C-12).
export function isTerminalFailure(code) {
  return TERMINAL_FAILURE_CODES.includes(code);
}
