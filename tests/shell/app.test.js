// The terminal document's bootstrap: it reads the page registry the control
// plane serves, withdraws the page when the registry does not list it, reads
// the page projection, and lets a deep link prefill a target without
// establishing a session. The document, the address and the fetch are stubs,
// so these run without a browser.

import assert from "node:assert/strict";
import test from "node:test";

import { createShell } from "../../assets/app.js";
import { EMPTY_TARGETS } from "../../assets/terminal.js";

const PAGE = {
  id: "terminal",
  title: "终端",
  items: [
    { id: "target-types", value: { node: { available: true, identity: "root@node" }, pod: { available: true } } },
    { id: "node-targets", value: { items: [{ targetType: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" }], reason: "", empty: "无可连接目标" } },
    { id: "pod-targets", value: { items: [], reason: "", empty: "无可连接目标" } },
    { id: "session-state", value: { sessions: [], limits: { account: 4, target: 4 }, usage: { account: 0, target: 0 }, idleTimeoutMinutes: 10 } },
    { id: "session-audit", value: { sessions: [], rule: "只展示获准读取的元数据" } },
  ],
};

// jsonResponse is one control-plane answer as fetch hands it back.
function jsonResponse(body, { status = 200 } = {}) {
  return { ok: status >= 200 && status < 300, status, json: async () => body };
}

// bootstrap stands up the document and the address a browser would hand the
// page. The fetch answers from the routes the page calls, in order.
function bootstrap({ routes = {}, hash = "" } = {}) {
  const root = { innerHTML: "", dataset: {}, addEventListener() {}, getElementById() { return null; } };
  const document = { getElementById: (id) => (id === "terminal" ? root : null) };
  const window = { location: { hash } };
  const calls = [];
  const fetchFn = async (path) => {
    calls.push(path);
    if (path === "/api/v1/pages") return jsonResponse({ items: [{ id: "terminal" }] });
    if (path === "/api/v1/pages/terminal") return jsonResponse(routes.page ?? PAGE);
    return jsonResponse({ error: { code: "not_found", message: "无此路径" } }, { status: 404 });
  };
  return { document, window, root, calls, fetchFn };
}

test("a page the registry does not list is withdrawn with its entry", async () => {
  const fixture = bootstrap();
  fixture.fetchFn = async (path) => {
    if (path === "/api/v1/pages") return jsonResponse({ items: [] });
    return jsonResponse({});
  };
  const shell = createShell({ document: fixture.document, window: fixture.window, fetchFn: fixture.fetchFn });
  await shell.settle();
  assert.match(fixture.root.innerHTML, /data-state="hidden"/);
});

test("the registry listing the page leads to the rendered projection", async () => {
  const fixture = bootstrap();
  const shell = createShell({ document: fixture.document, window: fixture.window, fetchFn: fixture.fetchFn });
  await shell.settle();
  assert.match(fixture.root.innerHTML, /data-state="ready"/);
  assert.match(fixture.root.innerHTML, /data-panel="target-selection"/);
});

test("a page read refusal states the control plane's own code", async () => {
  const fixture = bootstrap({ routes: { page: null } });
  fixture.fetchFn = async (path) => {
    if (path === "/api/v1/pages") return jsonResponse({ items: [{ id: "terminal" }] });
    return jsonResponse({ error: { code: "terminal_target_source_unavailable", message: "来源不可用" } }, { status: 503 });
  };
  const shell = createShell({ document: fixture.document, window: fixture.window, fetchFn: fixture.fetchFn });
  await shell.settle();
  assert.match(fixture.root.innerHTML, /data-state="error"/);
  assert.match(fixture.root.innerHTML, /data-failure="terminal_target_source_unavailable"/);
});

test("a deep link prefills the target without establishing a session", async () => {
  const fixture = bootstrap({ hash: "#/terminal?target=node&targetId=node-a&targetUid=uid-a" });
  const shell = createShell({ document: fixture.document, window: fixture.window, fetchFn: fixture.fetchFn });
  await shell.settle();
  assert.match(fixture.root.innerHTML, /data-impact="node"/);
  assert.doesNotMatch(fixture.root.innerHTML, /data-session=/);
});

// A document stub that records the listeners a shell binds and lets a test
// dispatch the events a browser would fire.
function listeningStub({ hash = "" } = {}) {
  const rootListeners = {};
  const windowListeners = {};
  const root = {
    innerHTML: "",
    dataset: {},
    addEventListener(type, handler) {
      (rootListeners[type] ||= []).push(handler);
    },
    removeEventListener(type, handler) {
      rootListeners[type] = (rootListeners[type] || []).filter((entry) => entry !== handler);
    },
  };
  const document = { getElementById: (id) => (id === "terminal" ? root : null) };
  const window = {
    location: { hash },
    addEventListener(type, handler) {
      (windowListeners[type] ||= []).push(handler);
    },
    removeEventListener(type, handler) {
      windowListeners[type] = (windowListeners[type] || []).filter((entry) => entry !== handler);
    },
  };
  const fire = (target, type, event) => {
    const map = target === window ? windowListeners : rootListeners;
    for (const handler of map[type] || []) handler(event);
  };
  return { root, document, window, fire, rootListeners, windowListeners };
}

// controlPlane is a scripted terminal surface over an in-memory session store.
function controlPlane({ page } = {}) {
  const calls = [];
  const sessions = {};
  const audits = {};
  const token = "terminal-token";
  let next = 1;
  const fetchFn = async (path, init = {}) => {
    const method = init.method || "GET";
    const body = init.body ? JSON.parse(init.body) : null;
    calls.push({ path, method, body });
    if (path === "/api/v1/pages") return jsonResponse({ items: [{ id: "terminal" }] });
    if (path === "/api/v1/pages/terminal") return jsonResponse(page);
    if (method === "POST" && path === "/api/v1/terminal/sessions") {
      const sid = `s-${next++}`;
      sessions[sid] = {
        sessionId: sid,
        targetType: body.targetType,
        targetId: body.targetId,
        targetUid: body.targetUid,
        namespace: body.namespace,
        container: body.container,
        executionIdentity: body.targetType === "node" ? "root@node" : `container:${body.namespace}/${body.targetId}/${body.container}`,
        state: "connecting",
        expiresAt: "2026-11-25T10:05:00Z",
        idleTimeoutMinutes: 10,
        limits: { account: 4, target: 4 },
        usage: { account: 1, target: 1 },
        token,
      };
      return jsonResponse(sessions[sid], { status: 201 });
    }
    // A read that continues a page carries its cursor in the query string.
    const [route] = path.split("?");
    const match = route.match(/^\/api\/v1\/terminal\/sessions\/([^/]+)(\/[a-z]+)?$/);
    if (!match) return jsonResponse({ error: { code: "not_found", message: "无此路径" } }, { status: 404 });
    const id = match[1];
    const suffix = match[2] || "";
    if (method === "POST" && suffix === "/attach") {
      sessions[id].state = "connected";
      return jsonResponse(sessions[id]);
    }
    if (method === "GET" && suffix === "") return jsonResponse(sessions[id]);
    if (method === "POST" && suffix === "/renew") return jsonResponse({ ...sessions[id], token });
    if (method === "POST" && suffix === "/input") {
      const failure = inputErrors[id] || inputErrors["*"];
      if (failure) return jsonResponse(failure, { status: 409 });
      return jsonResponse({ sequence: 1, result: "accepted", output: Buffer.from("ok").toString("base64") });
    }
    if (method === "DELETE" && suffix === "") {
      sessions[id].state = "closed";
      // A real end writes an end event, which is what a later audit read sees.
      if (!audits[id]) audits[id] = [];
      audits[id] = [...audits[id], { eventId: audits[id].length + 1, sessionId: id, event: "end", result: "accepted" }];
      return jsonResponse(sessions[id]);
    }
    if (method === "GET" && suffix === "/audit") {
      const failure = auditErrors[id] || auditErrors["*"];
      if (failure) return jsonResponse(failure, { status: 503 });
      const cursorMatch = /[?&]cursor=(\d+)/.exec(path);
      const cursor = cursorMatch ? Number(cursorMatch[1]) : 0;
      const pages = auditPages[id];
      if (!pages) return jsonResponse({ items: audits[id] || [] });
      const page = pages[cursor] || { items: [] };
      return jsonResponse({
        items: page.items,
        ...(page.nextCursor ? { nextCursor: String(page.nextCursor) } : {}),
      });
    }
    return jsonResponse({ error: { code: "not_found", message: "无此路径" } }, { status: 404 });
  };
  const auditPages = {};
  const auditErrors = {};
  const inputErrors = {};
  return {
    fetchFn,
    calls,
    sessions,
    setAudit: (id, events) => {
      audits[id] = events;
    },
    // setAuditPages scripts a paginated trail: each entry is one page the
    // control plane answers, with the cursor of the page after it.
    setAuditPages: (id, pages) => {
      auditPages[id] = pages;
    },
    setAuditError: (id, error) => {
      auditErrors[id] = error;
    },
    setInputError: (id, error) => {
      inputErrors[id] = error;
    },
  };
}

const NODE_PAGE = {
  id: "terminal",
  title: "终端",
  items: [
    { id: "target-types", value: { node: { available: true, identity: "root@node" } } },
    { id: "node-targets", value: { items: [{ targetType: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" }], reason: "", empty: EMPTY_TARGETS } },
    { id: "pod-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
    { id: "session-state", value: null },
    { id: "session-audit", value: null },
  ],
};

function podPage(containers, selectedContainer = "") {
  return {
    id: "terminal",
    title: "终端",
    items: [
      { id: "target-types", value: { pod: { available: true } } },
      { id: "node-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
      { id: "pod-targets", value: { items: [{ namespace: "home", application: "shop", pod: "shop-0", targetUid: "uid-0", containers, selectedContainer }], reason: "", empty: EMPTY_TARGETS } },
      { id: "session-state", value: null },
      { id: "session-audit", value: null },
    ],
  };
}

// twoNodePage offers two node targets, so the page can hold two sessions at
// once and a teardown has more than one end request to issue.
function twoNodePage() {
  return {
    id: "terminal",
    title: "终端",
    items: [
      { id: "target-types", value: { node: { available: true, identity: "root@node" } } },
      {
        id: "node-targets",
        value: {
          items: [
            { targetType: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" },
            { targetType: "node", targetId: "node-b", targetUid: "uid-b", namespace: "", container: "" },
          ],
          reason: "",
          empty: EMPTY_TARGETS,
        },
      },
      { id: "pod-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
      { id: "session-state", value: null },
      { id: "session-audit", value: null },
    ],
  };
}

// sameNamePodPage offers two Pods that share a name in different namespaces, so
// a container choice made on one must never be applied to the other.
function sameNamePodPage() {
  return {
    id: "terminal",
    title: "终端",
    items: [
      { id: "target-types", value: { pod: { available: true } } },
      { id: "node-targets", value: { items: [], reason: "", empty: EMPTY_TARGETS } },
      {
        id: "pod-targets",
        value: {
          items: [
            { namespace: "home", application: "shop", pod: "shop-0", targetUid: "uid-home", containers: ["web", "worker"], selectedContainer: "" },
            { namespace: "reports", application: "shop", pod: "shop-0", targetUid: "uid-reports", containers: ["web", "worker"], selectedContainer: "" },
          ],
          reason: "",
          empty: EMPTY_TARGETS,
        },
      },
      { id: "session-state", value: null },
      { id: "session-audit", value: null },
    ],
  };
}

function rowStub(attrs) {
  return { dataset: attrs };
}
function changeEvent(field, row, value) {
  return { target: { dataset: { field }, value, closest: () => row } };
}
function clickEvent(action, attrs = {}) {
  return { target: { dataset: { action, ...attrs } }, preventDefault() {} };
}
function submitEvent(sessionID, value) {
  return { target: { dataset: { input: sessionID }, elements: { data: { value } } }, preventDefault() {} };
}

test("selecting a Pod carries its container into the session the page creates", async () => {
  const plane = controlPlane({ page: podPage(["web"], "web") });
  const stub = listeningStub();
  const shell = createShell({ document: stub.document, window: stub.window, fetchFn: plane.fetchFn });
  await shell.settle();
  stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "pod", targetId: "shop-0", targetUid: "uid-0", namespace: "home", container: "web" })));
  stub.fire(stub.root, "click", clickEvent("connect"));
  await shell.settle();
  const create = plane.calls.find((call) => call.method === "POST" && call.path === "/api/v1/terminal/sessions");
  assert.ok(create, "a session creation must be requested");
  assert.equal(create.body.targetId, "shop-0");
  assert.equal(create.body.container, "web");
});

test("a multi-container Pod's chosen container is carried into the session", async () => {
  const plane = controlPlane({ page: podPage(["web", "worker"]) });
  const stub = listeningStub();
  const shell = createShell({ document: stub.document, window: stub.window, fetchFn: plane.fetchFn });
  await shell.settle();
  const podRow = rowStub({ target: "pod", targetId: "shop-0", targetUid: "uid-0", namespace: "home", container: "" });
  stub.fire(stub.root, "change", changeEvent("target-type", podRow));
  stub.fire(stub.root, "change", changeEvent("container", podRow, "worker"));
  stub.fire(stub.root, "click", clickEvent("connect"));
  await shell.settle();
  const create = plane.calls.find((call) => call.method === "POST" && call.path === "/api/v1/terminal/sessions");
  assert.ok(create, "a session creation must be requested");
  assert.equal(create.body.container, "worker");
});

test("a Pod selected without a container is refused before creating a session", async () => {
  const plane = controlPlane({ page: podPage(["web", "worker"]) });
  const stub = listeningStub();
  const shell = createShell({ document: stub.document, window: stub.window, fetchFn: plane.fetchFn });
  await shell.settle();
  const podRow = rowStub({ target: "pod", targetId: "shop-0", targetUid: "uid-0", namespace: "home", container: "" });
  stub.fire(stub.root, "change", changeEvent("target-type", podRow));
  stub.fire(stub.root, "click", clickEvent("connect"));
  await shell.settle();
  const create = plane.calls.find((call) => call.method === "POST" && call.path === "/api/v1/terminal/sessions");
  assert.equal(create, undefined, "no session may be created without a container");
  assert.match(stub.root.innerHTML, /请先为该 Pod 选择容器/);
});

test("refresh lands a control-plane state change on the page", async () => {
  const plane = controlPlane({ page: NODE_PAGE });
  const stub = listeningStub();
  const shell = createShell({ document: stub.document, window: stub.window, fetchFn: plane.fetchFn });
  await shell.settle();
  stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
  stub.fire(stub.root, "click", clickEvent("connect"));
  await shell.settle();
  assert.match(stub.root.innerHTML, /data-state="connected"/);
  const sid = Object.keys(plane.sessions)[0];
  plane.sessions[sid].state = "closed";
  plane.sessions[sid].reason = "terminal_session_ended";
  await shell.refresh();
  assert.match(stub.root.innerHTML, /data-state="closed"/);
  assert.match(stub.root.innerHTML, /<textarea[^>]*disabled/);
});

test("destroy ends the sessions the browser still holds", async () => {
  const plane = controlPlane({ page: NODE_PAGE });
  const stub = listeningStub();
  const shell = createShell({ document: stub.document, window: stub.window, fetchFn: plane.fetchFn });
  await shell.settle();
  stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
  stub.fire(stub.root, "click", clickEvent("connect"));
  await shell.settle();
  const sid = Object.keys(plane.sessions)[0];
  shell.destroy();
  await shell.settle();
  const end = plane.calls.find((call) => call.method === "DELETE" && call.path === `/api/v1/terminal/sessions/${sid}`);
  assert.ok(end, "destroy must end the session");
  assert.match(stub.root.innerHTML, /data-state="closed"/);
});

test("a session established after the page loaded is read into the audit panel", async () => {
  const plane = controlPlane({ page: NODE_PAGE });
  const stub = listeningStub();
  const shell = createShell({ document: stub.document, window: stub.window, fetchFn: plane.fetchFn });
  await shell.settle();
  stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
  stub.fire(stub.root, "click", clickEvent("connect"));
  await shell.settle();
  const sid = Object.keys(plane.sessions)[0];
  assert.ok(plane.calls.some((call) => call.path === `/api/v1/terminal/sessions/${sid}/audit`), "the new session's audit must be read");
  plane.setAudit(sid, [{ eventId: 1, sessionId: sid, event: "establish", result: "accepted" }]);
  stub.fire(stub.root, "submit", submitEvent(sid, "ls"));
  await shell.settle();
  assert.match(stub.root.innerHTML, new RegExp(`data-audit-session="${sid}"`));
  assert.match(stub.root.innerHTML, /establish/);
});

test("a request that never reaches the control plane stops input", async () => {
  const plane = controlPlane({ page: NODE_PAGE });
  const stub = listeningStub();
  let offline = false;
  const fetchFn = async (path, init) => {
    if (offline && init && init.method === "POST" && path.endsWith("/input")) throw new Error("network down");
    return plane.fetchFn(path, init);
  };
  const shell = createShell({ document: stub.document, window: stub.window, fetchFn });
  await shell.settle();
  stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
  stub.fire(stub.root, "click", clickEvent("connect"));
  await shell.settle();
  const sid = Object.keys(plane.sessions)[0];
  assert.doesNotMatch(stub.root.innerHTML, /<textarea[^>]*disabled/);
  offline = true;
  stub.fire(stub.root, "submit", submitEvent(sid, "ls"));
  await shell.settle();
  assert.match(stub.root.innerHTML, /<textarea[^>]*disabled/);
});

test("a container choice binds to the Pod's namespace and UID, not just its name", () => {
  // Two Pods in different namespaces share the name shop-0. Choosing home's
  // shop-0 and then a container on reports' row must create the reports target.
  return (async () => {
    const plane = controlPlane({ page: sameNamePodPage() });
    const stub = listeningStub();
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn: plane.fetchFn });
    await shell.settle();
    const home = rowStub({ target: "pod", targetId: "shop-0", targetUid: "uid-home", namespace: "home", container: "" });
    const reports = rowStub({ target: "pod", targetId: "shop-0", targetUid: "uid-reports", namespace: "reports", container: "" });
    stub.fire(stub.root, "change", changeEvent("target-type", home));
    stub.fire(stub.root, "change", changeEvent("container", reports, "worker"));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await shell.settle();
    const create = plane.calls.find((call) => call.method === "POST" && call.path === "/api/v1/terminal/sessions");
    assert.ok(create, "a session creation must be requested");
    assert.equal(create.body.namespace, "reports");
    assert.equal(create.body.targetUid, "uid-reports");
    assert.equal(create.body.container, "worker");
  })();
});

test("a container choice survives the redraw that follows it", () => {
  return (async () => {
    const plane = controlPlane({ page: podPage(["web", "worker"]) });
    const stub = listeningStub();
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn: plane.fetchFn });
    await shell.settle();
    const row = rowStub({ target: "pod", targetId: "shop-0", targetUid: "uid-0", namespace: "home", container: "" });
    stub.fire(stub.root, "change", changeEvent("target-type", row));
    stub.fire(stub.root, "change", changeEvent("container", row, "worker"));
    assert.match(stub.root.innerHTML, /<option value="worker" selected>/);
    assert.match(stub.root.innerHTML, /data-container="worker"/);
    assert.match(stub.root.innerHTML, /data-confirm-container="worker"/);
  })();
});

test("the command the form submits ends the line the shell reads", () => {
  return (async () => {
    const plane = controlPlane({ page: NODE_PAGE });
    const stub = listeningStub();
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn: plane.fetchFn });
    await shell.settle();
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await shell.settle();
    const sid = Object.keys(plane.sessions)[0];
    stub.fire(stub.root, "submit", submitEvent(sid, "id"));
    await shell.settle();
    const input = plane.calls.find((call) => call.method === "POST" && call.path.endsWith("/input"));
    assert.ok(input, "the input must be sent");
    assert.equal(input.body.data, Buffer.from("id\n").toString("base64"));
    assert.match(stub.root.innerHTML, /<div class="output-line" data-sequence="1">ok<\/div>/);
    // A multi-line edit keeps its own line ends and only gains the last one.
    stub.fire(stub.root, "submit", submitEvent(sid, "ls\ndate\n"));
    await shell.settle();
    const sent = plane.calls.filter((call) => call.method === "POST" && call.path.endsWith("/input"));
    assert.equal(sent[1].body.data, Buffer.from("ls\ndate\n").toString("base64"));
  })();
});

test("a session the network cut off is never resumed when the network returns", () => {
  return (async () => {
    const plane = controlPlane({ page: NODE_PAGE });
    const stub = listeningStub();
    let offline = false;
    const fetchFn = async (path, init) => {
      if (offline && init && init.method === "POST" && path.endsWith("/input")) throw new Error("network down");
      return plane.fetchFn(path, init);
    };
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn });
    await shell.settle();
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await shell.settle();
    const sid = Object.keys(plane.sessions)[0];
    offline = true;
    stub.fire(stub.root, "submit", submitEvent(sid, "id"));
    await shell.settle();
    assert.match(stub.root.innerHTML, /terminal_connection_lost/);
    assert.match(stub.root.innerHTML, /input_result_unknown/);
    offline = false;
    await shell.refresh();
    assert.match(stub.root.innerHTML, /data-state="closed"/);
    assert.match(stub.root.innerHTML, /<textarea[^>]*disabled/);
    assert.equal(plane.calls.filter((call) => call.method === "GET" && call.path === `/api/v1/terminal/sessions/${sid}`).length, 0);
    assert.equal(plane.calls.filter((call) => call.path.endsWith("/renew")).length, 0);
    const before = plane.calls.filter((call) => call.method === "POST" && call.path.endsWith("/input")).length;
    stub.fire(stub.root, "submit", submitEvent(sid, "id"));
    await shell.settle();
    assert.equal(plane.calls.filter((call) => call.method === "POST" && call.path.endsWith("/input")).length, before);
  })();
});

test("a pagehide ends every session at once and drops what the page must not keep", () => {
  return (async () => {
    const plane = controlPlane({ page: twoNodePage() });
    const stub = listeningStub();
    const hanging = new Promise(() => {});
    const fetchFn = async (path, init) => {
      if (init && init.method === "DELETE" && path.endsWith("/s-1")) {
        plane.calls.push({ path, method: "DELETE" });
        return hanging;
      }
      return plane.fetchFn(path, init);
    };
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn });
    await shell.settle();
    const connectNode = (id, uid) => {
      stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: id, targetUid: uid, namespace: "", container: "" })));
      stub.fire(stub.root, "click", clickEvent("connect"));
    };
    connectNode("node-a", "uid-a");
    await shell.settle();
    connectNode("node-b", "uid-b");
    await shell.settle();
    stub.fire(stub.root, "submit", submitEvent("s-1", "id"));
    await shell.settle();
    assert.match(stub.root.innerHTML, /<div class="output-line" data-sequence="1">ok<\/div>/);
    stub.fire(stub.window, "pagehide", {});
    await Promise.resolve();
    await Promise.resolve();
    const ends = plane.calls.filter((call) => call.method === "DELETE");
    assert.equal(ends.length, 2, "both ends must be fired without waiting for the first");
    assert.match(stub.root.innerHTML, /data-empty="output"/);
    assert.doesNotMatch(stub.root.innerHTML, />ok</);
    const before = plane.calls.length;
    await shell.refresh();
    assert.equal(plane.calls.length, before, "the refresh loop is stopped");
  })();
});

test("an audit read follows the cursor and keeps the events pages already carried", () => {
  return (async () => {
    const plane = controlPlane({ page: NODE_PAGE });
    const stub = listeningStub();
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn: plane.fetchFn });
    await shell.settle();
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await shell.settle();
    const sid = Object.keys(plane.sessions)[0];
    plane.setAuditPages(sid, {
      0: { items: [{ eventId: 1, sessionId: sid, event: "establish", result: "accepted" }], nextCursor: 2 },
      2: {
        items: [
          { eventId: 2, sessionId: sid, event: "input", result: "accepted", sequence: 1 },
          { eventId: 3, sessionId: sid, event: "end", result: "accepted" },
        ],
      },
    });
    stub.fire(stub.root, "submit", submitEvent(sid, "id"));
    await shell.settle();
    assert.match(stub.root.innerHTML, /data-event="establish"/);
    assert.match(stub.root.innerHTML, /data-event="input"/);
    assert.match(stub.root.innerHTML, /data-event="end"/);
    const firstPage = (stub.root.innerHTML.match(/data-event="establish"/g) || []).length;
    assert.equal(firstPage, 1, "the first page must not be duplicated");
  })();
});

test("a control-plane end lands in the audit panel after a refresh", () => {
  return (async () => {
    const plane = controlPlane({ page: NODE_PAGE });
    const stub = listeningStub();
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn: plane.fetchFn });
    await shell.settle();
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await shell.settle();
    const sid = Object.keys(plane.sessions)[0];
    plane.sessions[sid].state = "closed";
    plane.setAudit(sid, [
      { eventId: 1, sessionId: sid, event: "establish", result: "accepted" },
      { eventId: 9, sessionId: sid, event: "end", result: "accepted" },
    ]);
    await shell.refresh();
    assert.match(stub.root.innerHTML, /data-event="end"/);
    assert.match(stub.root.innerHTML, /data-event="establish"/);
  })();
});

test("an audit refusal ends the session and refuses further input", () => {
  return (async () => {
    const plane = controlPlane({ page: NODE_PAGE });
    plane.setAuditError("*", { error: { code: "terminal_audit_unavailable", message: "终端审计不可用" } });
    const stub = listeningStub();
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn: plane.fetchFn });
    await shell.settle();
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await shell.settle();
    const sid = Object.keys(plane.sessions)[0];
    assert.match(stub.root.innerHTML, /data-state="closed"/);
    assert.match(stub.root.innerHTML, /data-notice="terminal_audit_unavailable"/);
    assert.match(stub.root.innerHTML, /<textarea[^>]*disabled/);
    const before = plane.calls.filter((call) => call.method === "POST" && call.path.endsWith("/input")).length;
    stub.fire(stub.root, "submit", submitEvent(sid, "id"));
    await shell.settle();
    assert.equal(plane.calls.filter((call) => call.method === "POST" && call.path.endsWith("/input")).length, before, "no input may be sent while audit is unavailable");
  })();
});

test("a session cut off by the network has its remote end released when the network returns", () => {
  return (async () => {
    const plane = controlPlane({ page: NODE_PAGE });
    const stub = listeningStub();
    let offline = false;
    const fetchFn = async (path, init) => {
      if (offline) throw new Error("network down");
      return plane.fetchFn(path, init);
    };
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn });
    await shell.settle();
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await shell.settle();
    const sid = Object.keys(plane.sessions)[0];
    offline = true;
    stub.fire(stub.root, "submit", submitEvent(sid, "id"));
    await shell.settle();
    // The page ended the session locally, but the control plane still holds it.
    assert.equal(plane.sessions[sid].state, "connected");
    assert.match(stub.root.innerHTML, /terminal_connection_lost/);
    offline = false;
    await shell.refresh();
    const released = plane.calls.find((call) => call.method === "DELETE" && call.path === `/api/v1/terminal/sessions/${sid}`);
    assert.ok(released, "the old remote session must be ended once the control plane is reachable");
    assert.equal(plane.sessions[sid].state, "closed");
    assert.match(stub.root.innerHTML, /<textarea[^>]*disabled/);
  })();
});

test("a creation that answers after teardown is ended at once and never attached", () => {
  return (async () => {
    const plane = controlPlane({ page: NODE_PAGE });
    const stub = listeningStub();
    let release;
    const gate = new Promise((resolve) => {
      release = resolve;
    });
    let started;
    const inFlight = new Promise((resolve) => {
      started = resolve;
    });
    const fetchFn = async (path, init) => {
      if (init && init.method === "POST" && path === "/api/v1/terminal/sessions") {
        started();
        await gate;
      }
      return plane.fetchFn(path, init);
    };
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn });
    await shell.settle();
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await inFlight;
    shell.destroy();
    release();
    await shell.settle();
    const sid = Object.keys(plane.sessions)[0];
    assert.ok(sid, "the control plane did allocate a session");
    const released = plane.calls.find((call) => call.method === "DELETE" && call.path === `/api/v1/terminal/sessions/${sid}`);
    assert.ok(released, "the late creation must be ended");
    assert.equal(plane.sessions[sid].state, "closed");
    assert.equal(plane.calls.filter((call) => call.path.endsWith("/attach")).length, 0, "a late creation is never attached");
  })();
});

test("a confirmation queued behind a creation does not create a session after teardown", () => {
  return (async () => {
    const plane = controlPlane({ page: twoNodePage() });
    const stub = listeningStub();
    let release;
    const gate = new Promise((resolve) => {
      release = resolve;
    });
    let started;
    const inFlight = new Promise((resolve) => {
      started = resolve;
    });
    const fetchFn = async (path, init) => {
      if (init && init.method === "POST" && path === "/api/v1/terminal/sessions") {
        started();
        await gate;
      }
      return plane.fetchFn(path, init);
    };
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn });
    await shell.settle();
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await inFlight;
    // A second confirmation is queued behind the first creation.
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-b", targetUid: "uid-b", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    shell.destroy();
    release();
    await shell.settle();
    const creates = plane.calls.filter((call) => call.method === "POST" && call.path === "/api/v1/terminal/sessions");
    assert.equal(creates.length, 1, "the queued confirmation must not create a session after teardown");
  })();
});

test("an audit trail longer than one read's budget continues from its saved cursor", () => {
  return (async () => {
    const plane = controlPlane({ page: NODE_PAGE });
    const stub = listeningStub();
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn: plane.fetchFn });
    await shell.settle();
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await shell.settle();
    const sid = Object.keys(plane.sessions)[0];
    // Nine pages, two events each; the end is on the last page, past the
    // eight-page budget of one read.
    const pages = {};
    for (let page = 0; page < 9; page += 1) {
      const first = page * 2 + 1;
      const last = page === 8;
      pages[page * 2] = {
        items: [
          { eventId: first, sessionId: sid, event: "input", result: "accepted", sequence: first },
          { eventId: first + 1, sessionId: sid, event: last ? "end" : "renew", result: "accepted" },
        ],
        ...(page < 8 ? { nextCursor: page * 2 + 2 } : {}),
      };
    }
    plane.setAuditPages(sid, pages);
    plane.sessions[sid].state = "closed";
    const before = plane.calls.length;
    await shell.refresh();
    const firstPass = plane.calls.slice(before).filter((call) => call.path.includes("/audit"));
    assert.equal(firstPass.length, 8, "one read spends its whole page budget");
    assert.equal(firstPass.filter((call) => !call.path.includes("cursor=")).length, 1, "only the first page starts at the beginning");
    assert.doesNotMatch(stub.root.innerHTML, /data-event="end"/);
    const second = plane.calls.length;
    await shell.refresh();
    const secondPass = plane.calls.slice(second).filter((call) => call.path.includes("/audit"));
    assert.ok(secondPass.some((call) => call.path.includes("cursor=16")), "the read continues from the saved cursor");
    assert.equal(secondPass.filter((call) => !call.path.includes("cursor=")).length, 0, "the read never restarts from the first page");
    assert.match(stub.root.innerHTML, /data-event="end"/);
    assert.equal((stub.root.innerHTML.match(/data-event="input"/g) || []).length, 9, "each input event is shown once");
  })();
});

test("an audit end the page did not see ends the session and releases it", () => {
  return (async () => {
    const plane = controlPlane({ page: NODE_PAGE });
    const stub = listeningStub();
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn: plane.fetchFn });
    await shell.settle();
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await shell.settle();
    const sid = Object.keys(plane.sessions)[0];
    // The control plane recorded the end while the page still shows connected.
    plane.setAudit(sid, [
      { eventId: 1, sessionId: sid, event: "establish", result: "accepted" },
      { eventId: 2, sessionId: sid, event: "end", result: "failed" },
    ]);
    stub.fire(stub.root, "submit", submitEvent(sid, "id"));
    await shell.settle();
    assert.match(stub.root.innerHTML, /data-state="closed"/);
    const released = plane.calls.find((call) => call.method === "DELETE" && call.path === `/api/v1/terminal/sessions/${sid}`);
    assert.ok(released, "the ended session must still be released");
    assert.match(stub.root.innerHTML, /<textarea[^>]*disabled/);
  })();
});

test("a failed session keeps its own code when the debt-clearing end cannot be reached", () => {
  return (async () => {
    const plane = controlPlane({ page: NODE_PAGE });
    const stub = listeningStub();
    let endOffline = false;
    const fetchFn = async (path, init) => {
      if (endOffline && init && init.method === "DELETE") throw new Error("network down");
      return plane.fetchFn(path, init);
    };
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn });
    await shell.settle();
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await shell.settle();
    const sid = Object.keys(plane.sessions)[0];
    // The clearing request cannot be reached from the start of the failure.
    endOffline = true;
    plane.setInputError(sid, { error: { code: "terminal_executor_unavailable", message: "执行服务不可用" } });
    stub.fire(stub.root, "submit", submitEvent(sid, "id"));
    await shell.settle();
    assert.match(stub.root.innerHTML, /data-state="failed"/);
    assert.match(stub.root.innerHTML, /data-failure="terminal_executor_unavailable"/);
    // The end it still owes reads as pending, not as an unknown result yet.
    assert.match(stub.root.innerHTML, /data-settlement="pending"/);
    assert.match(stub.root.innerHTML, /data-reap-pending="true"/);
    assert.doesNotMatch(stub.root.innerHTML, /目标进程结果未知/);
    // A refresh whose clearing request also fails must not rewrite the failure.
    await shell.refresh();
    assert.match(stub.root.innerHTML, /data-state="failed"/);
    assert.match(stub.root.innerHTML, /data-failure="terminal_executor_unavailable"/);
    assert.doesNotMatch(stub.root.innerHTML, /terminal_connection_lost/);
    // The unconfirmed end is stated beside that code, never in its place.
    assert.match(stub.root.innerHTML, /data-settlement="unknown"/);
    assert.match(stub.root.innerHTML, /data-end-unknown="true"/);
    assert.match(stub.root.innerHTML, /结束未确认/);
    assert.match(stub.root.innerHTML, /目标进程结果未知/);
    assert.match(stub.root.innerHTML, /可能仍有未完成的副作用/);
    assert.match(stub.root.innerHTML, /<textarea[^>]*disabled/);
    // The debt is still owed, and settling it keeps the original failure.
    endOffline = false;
    await shell.refresh();
    assert.ok(plane.calls.some((call) => call.method === "DELETE" && call.path === `/api/v1/terminal/sessions/${sid}`));
    assert.match(stub.root.innerHTML, /data-state="failed"/);
    assert.match(stub.root.innerHTML, /data-failure="terminal_executor_unavailable"/);
    // A confirmed end collapses the warning to a settlement, keeping the code.
    assert.match(stub.root.innerHTML, /data-settlement="settled"/);
    assert.match(stub.root.innerHTML, /data-end-settled="true"/);
    assert.doesNotMatch(stub.root.innerHTML, /目标进程结果未知/);
  })();
});

test("an audit read taken before the end is settled is read again once it settles", () => {
  return (async () => {
    const plane = controlPlane({ page: NODE_PAGE });
    const stub = listeningStub();
    let inputOffline = false;
    let endOffline = true;
    const fetchFn = async (path, init) => {
      if (inputOffline && init && init.method === "POST" && path.endsWith("/input")) throw new Error("network down");
      if (endOffline && init && init.method === "DELETE") throw new Error("network down");
      return plane.fetchFn(path, init);
    };
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn });
    await shell.settle();
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await shell.settle();
    const sid = Object.keys(plane.sessions)[0];
    // A lost input ends the session locally while the control plane still holds
    // it and the clearing request cannot be delivered.
    inputOffline = true;
    stub.fire(stub.root, "submit", submitEvent(sid, "id"));
    await shell.settle();
    inputOffline = false;
    // The trail still has no end event, so the read is complete but not final.
    await shell.refresh();
    assert.doesNotMatch(stub.root.innerHTML, /data-event="end"/);
    // Once the clearing request lands, the end event it writes must be read.
    endOffline = false;
    await shell.refresh();
    assert.ok(plane.calls.some((call) => call.method === "DELETE" && call.path === `/api/v1/terminal/sessions/${sid}`));
    assert.match(stub.root.innerHTML, /data-event="end"/);
    assert.match(stub.root.innerHTML, /<textarea[^>]*disabled/);
    assert.equal(plane.calls.filter((call) => call.path.endsWith("/attach")).length, 1, "a settled session is never attached again");
  })();
});

test("a session the audit closed keeps its own code while its owed end goes unconfirmed", () => {
  return (async () => {
    const plane = controlPlane({ page: NODE_PAGE });
    const stub = listeningStub();
    let endOffline = true;
    const fetchFn = async (path, init) => {
      if (endOffline && init && init.method === "DELETE") throw new Error("network down");
      return plane.fetchFn(path, init);
    };
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn });
    await shell.settle();
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await shell.settle();
    const sid = Object.keys(plane.sessions)[0];
    // The audit read refuses, so the page ends the session it was reading for.
    plane.setAuditError(sid, { error: { code: "terminal_audit_unavailable", message: "终端审计不可用" } });
    stub.fire(stub.root, "submit", submitEvent(sid, "id"));
    await shell.settle();
    assert.match(stub.root.innerHTML, /data-state="closed"/);
    assert.match(stub.root.innerHTML, /data-failure="terminal_audit_unavailable"/);
    // Its owed end could not be delivered: that stands beside the audit code.
    assert.match(stub.root.innerHTML, /data-settlement="unknown"/);
    assert.match(stub.root.innerHTML, /data-end-unknown="true"/);
    assert.match(stub.root.innerHTML, /data-reap-pending="true"/);
    assert.match(stub.root.innerHTML, /结束未确认/);
    assert.match(stub.root.innerHTML, /<textarea[^>]*disabled/);
    assert.doesNotMatch(stub.root.innerHTML, /terminal_connection_lost/);
    // Once the trail is readable and the end lands, the warning collapses.
    plane.setAuditError(sid, null);
    endOffline = false;
    await shell.refresh();
    assert.match(stub.root.innerHTML, /data-failure="terminal_audit_unavailable"/);
    assert.match(stub.root.innerHTML, /data-settlement="settled"/);
    assert.match(stub.root.innerHTML, /data-end-settled="true"/);
    assert.doesNotMatch(stub.root.innerHTML, /目标进程结果未知/);
  })();
});

test("a session whose live end was never confirmed states the unknown result beside its code", () => {
  return (async () => {
    const plane = controlPlane({ page: NODE_PAGE });
    const stub = listeningStub();
    let endOffline = true;
    const fetchFn = async (path, init) => {
      if (endOffline && init && init.method === "DELETE") throw new Error("network down");
      return plane.fetchFn(path, init);
    };
    const shell = createShell({ document: stub.document, window: stub.window, fetchFn });
    await shell.settle();
    stub.fire(stub.root, "change", changeEvent("target-type", rowStub({ target: "node", targetId: "node-a", targetUid: "uid-a", namespace: "", container: "" })));
    stub.fire(stub.root, "click", clickEvent("connect"));
    await shell.settle();
    const sid = Object.keys(plane.sessions)[0];
    // The user disconnects a live session and the end request never answers.
    stub.fire(stub.root, "click", clickEvent("end", { session: sid }));
    await shell.settle();
    assert.match(stub.root.innerHTML, /data-state="closed"/);
    assert.match(stub.root.innerHTML, /data-failure="end_result_unknown"/);
    assert.match(stub.root.innerHTML, /data-settlement="unknown"/);
    assert.match(stub.root.innerHTML, /data-end-unknown="true"/);
    assert.match(stub.root.innerHTML, /data-reap-pending="true"/);
    assert.match(stub.root.innerHTML, /可能仍有未完成的副作用/);
    assert.match(stub.root.innerHTML, /data-notice="end_result_unknown"/);
    assert.match(stub.root.innerHTML, /<textarea[^>]*disabled/);
    // The retry lands: the session shows a confirmed end and never resumes.
    endOffline = false;
    await shell.refresh();
    assert.match(stub.root.innerHTML, /data-settlement="settled"/);
    assert.match(stub.root.innerHTML, /data-end-settled="true"/);
    assert.doesNotMatch(stub.root.innerHTML, /目标进程结果未知/);
    assert.equal(plane.calls.filter((call) => call.path.endsWith("/attach")).length, 1, "a settled session is never attached again");
  })();
});
