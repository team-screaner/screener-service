import test from "node:test";
import assert from "node:assert/strict";
import { Api } from "../dist/api.js";

test("retrying an uncertain mutation reuses its key; a new successful command gets a new key", async () => {
  const calls = [];
  const api = new Api({
    fetch: async (url, options) => {
      calls.push(options);
      if (calls.length === 1) throw new TypeError("offline");
      return Response.json({ id: "saved" });
    },
  });
  await assert.rejects(api.command("/evidence", { title: "one" }));
  await api.command("/evidence", { title: "one" });
  await api.command("/evidence", { title: "one" });
  assert.equal(
    calls[0].headers["Idempotency-Key"],
    calls[1].headers["Idempotency-Key"],
  );
  assert.notEqual(
    calls[1].headers["Idempotency-Key"],
    calls[2].headers["Idempotency-Key"],
  );
});
test("expired sessions are cleared and reported to the interface", async () => {
  let expired = false;
  const api = new Api({
    fetch: async () => Response.json({ message: "Expired" }, { status: 401 }),
    onUnauthorized: () => {
      expired = true;
    },
  });
  api.setToken("expired");
  await assert.rejects(api.get("/me"));
  assert.equal(api.token, "");
  assert.equal(expired, true);
});
test("pagination keeps filters and the server cursor", async () => {
  const urls = [];
  const api = new Api({
    fetch: async (url) => {
      urls.push(url);
      return Response.json(
        urls.length === 1
          ? { items: [{ id: 1 }], next_cursor: "cursor&1" }
          : { items: [{ id: 2 }], next_cursor: "" },
      );
    },
  });
  assert.deepEqual(await api.all("/assessments", { user_matrix_id: "mine" }), [
    { id: 1 },
    { id: 2 },
  ]);
  assert.ok(urls[1].includes("user_matrix_id=mine"));
  assert.ok(urls[1].includes("cursor=cursor%261"));
});
