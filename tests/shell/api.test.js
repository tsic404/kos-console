// The control-plane path's byte round trip: a command leaves as the base64 the
// input route carries and an executor's output comes back decoded, so the page
// neither sends a raw byte string the route refuses nor shows an encoded one
// (KOSD-C-92, CONSOLE-C-11).

import assert from "node:assert/strict";
import test from "node:test";

import { createAPI } from "../../assets/api.js";

// jsonResponse is one control-plane answer as fetch hands it back.
function jsonResponse(body, { status = 200 } = {}) {
  return { ok: status >= 200 && status < 300, status, json: async () => body };
}

// captureFetch records every request and answers from one table.
function captureFetch(answer) {
  const calls = [];
  const fetchFn = async (path, init) => {
    calls.push({ path, init });
    return answer(path, init);
  };
  return { fetchFn, calls };
}

test("input encodes the batch as the base64 the control plane's route expects", async () => {
  const { fetchFn, calls } = captureFetch(() => jsonResponse({ sequence: 1, result: "accepted" }));
  const api = createAPI({ fetchFn });
  await api.input("s-1", "token-1", 1, "ls -la");
  const body = JSON.parse(calls[0].init.body);
  assert.equal(body.sequence, 1);
  assert.equal(body.data, Buffer.from("ls -la").toString("base64"));
});

test("input decodes the output the executor reported", async () => {
  const output = Buffer.from("uid=1000\n").toString("base64");
  const { fetchFn } = captureFetch(() => jsonResponse({ sequence: 1, result: "accepted", output }));
  const api = createAPI({ fetchFn });
  const result = await api.input("s-1", "token-1", 1, "id");
  assert.equal(result.ok, true);
  assert.equal(result.body.output, "uid=1000\n");
});

test("audit continues from the cursor the control plane answered", async () => {
  const { fetchFn, calls } = captureFetch(() => jsonResponse({ items: [] }));
  const api = createAPI({ fetchFn });
  await api.audit("s-1");
  await api.audit("s-1", "42");
  assert.equal(calls[0].path, "/api/v1/terminal/sessions/s-1/audit");
  assert.equal(calls[1].path, "/api/v1/terminal/sessions/s-1/audit?cursor=42");
});

test("a teardown end asks the browser to keep the request alive", async () => {
  const { fetchFn, calls } = captureFetch(() => jsonResponse({ sessionId: "s-1", state: "closed" }));
  const api = createAPI({ fetchFn });
  await api.end("s-1", { keepalive: true });
  await api.end("s-1");
  assert.equal(calls[0].init.keepalive, true);
  assert.equal(calls[1].init.keepalive, undefined);
});
