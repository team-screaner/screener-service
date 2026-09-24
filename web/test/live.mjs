import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { setTimeout as delay } from "node:timers/promises";
import { Window } from "happy-dom";

const base = process.env.SCREENER_URL;
if (!base)
  throw new Error(
    "Set SCREENER_URL to a development deployment before running the live frontend test.",
  );
test(
  "browser application: register → matrix → evidence → assessment → plan → XLSX → logout",
  { timeout: 60000 },
  async () => {
    const nativeFetch = globalThis.fetch;
    const win = new Window({
      url: base + "/",
      settings: {
        enableJavaScriptEvaluation: false,
        disableCSSFileLoading: true,
        disableJavaScriptFileLoading: true,
      },
    });
    const previous = new Map();
    for (const [key, value] of Object.entries({
      window: win,
      document: win.document,
      location: win.location,
      FormData: win.FormData,
      sessionStorage: win.sessionStorage,
      navigator: win.navigator,
      confirm: () => true,
      fetch: (url, options) => nativeFetch(new URL(url, base), options),
    })) {
      previous.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
      Object.defineProperty(globalThis, key, {
        value,
        writable: true,
        configurable: true,
      });
    }
    const page = win.document;
    let token = "";
    const wait = async (predicate, message) => {
      for (let i = 0; i < 300; i++) {
        if (predicate()) return;
        await delay(20);
      }
      throw new Error(
        message +
          ": " +
          [...page.querySelectorAll('[role="alert"]')]
            .map((n) => n.textContent)
            .join(" | "),
      );
    };
    const click = async (selector) => {
      await wait(
        () =>
          page.querySelector(selector) &&
          !page.querySelector(selector).disabled,
        "Cannot click " + selector,
      );
      page.querySelector(selector).click();
    };
    const fill = (selector, value) => {
      const el = [...page.querySelectorAll(selector)].find((node) =>
        node.matches("input,textarea,select"),
      );
      assert.ok(el, "Missing " + selector);
      el.value = value;
      el.dispatchEvent(new win.Event("input", { bubbles: true }));
      el.dispatchEvent(new win.Event("change", { bubbles: true }));
    };
    const submit = async (id) => {
      await wait(
        () => !page.querySelector(`${id} button[type=submit]`).disabled,
        "Submit still busy",
      );
      page
        .querySelector(id)
        .dispatchEvent(
          new win.Event("submit", { bubbles: true, cancelable: true }),
        );
    };
    const route = async (name) => {
      win.location.hash = "/" + name;
      await wait(
        () =>
          page.querySelector(".page-heading h1")?.textContent ===
          {
            catalog: "Каталог матриц",
            overview: "Мой рост",
            assessment: "Самооценка",
            plan: "План развития",
            settings: "Агенты и доступ",
          }[name],
        "Route did not change to " + name,
      );
    };
    try {
      page.write(
        await readFile(new URL("../dist/index.html", import.meta.url), "utf8"),
      );
      await import("../dist/app.js?live=" + Date.now());
      await click('[data-action="auth-toggle"]');
      fill('[name="name"]', "Frontend verification");
      fill('[name="email"]', `frontend-${crypto.randomUUID()}@example.test`);
      fill('[name="password"]', "frontend-local-test-password");
      await submit("#auth-form");
      await wait(() => page.querySelector(".sidebar"), "Registration failed");
      token = win.sessionStorage.getItem("screener.session");
      assert.ok(token);
      await route("catalog");
      await click('[data-action="matrix-preview"]');
      await wait(
        () => page.querySelector("#assign-form"),
        "Matrix preview failed",
      );
      assert.ok(page.querySelector("#modal").open);
      await submit("#assign-form");
      await wait(
        () => page.querySelector(".journey-banner"),
        "Assignment failed",
      );
      await route("assessment");
      const firstCard = page.querySelector(".assessment-card");
      const reqID = firstCard.dataset.requirement;
      await click(
        `.assessment-card[data-requirement="${reqID}"] [data-action="evidence"]`,
      );
      fill('[name="title"]', "Shipped the verified frontend");
      fill(
        '[name="description"]',
        "Implemented the UI and verified the full growth workflow against PostgreSQL.",
      );
      await submit("#evidence-form");
      await wait(
        () =>
          !page.querySelector("#modal").open &&
          page.querySelector(
            `input[data-req="${reqID}"][data-draft="evidence"]`,
          ),
        "Evidence save failed",
      );
      const evidenceCheckbox = page.querySelector(
        `input[data-req="${reqID}"][data-draft="evidence"]`,
      );
      evidenceCheckbox.checked = true;
      evidenceCheckbox.dispatchEvent(
        new win.Event("change", { bubbles: true }),
      );
      fill(`select[data-req="${reqID}"][data-draft="score"]`, "4");
      await submit("#assessment-form");
      await wait(
        () => page.querySelector(".journey-banner"),
        "Assessment save failed",
      );
      const metrics = [...page.querySelectorAll(".metric strong")].map(
        (n) => n.textContent,
      );
      assert.notEqual(metrics[0], "0%");
      assert.match(metrics[1], /^1/);
      await route("plan");
      await click('[data-action="plan"]');
      fill('[name="action"]', "Document the next design tradeoff");
      await submit("#plan-form");
      await wait(() => page.querySelector(".plan-row"), "Plan save failed");
      const status = page.querySelector("select[data-plan]");
      await wait(
        () => !page.querySelector('[data-action="plan"]').disabled,
        "Plan still saving",
      );
      fill("#" + status.id, "done");
      await wait(
        () => page.querySelector(".plan-state.done"),
        "Plan status was not updated",
      );
      await route("settings");
      await click('[data-action="token"]');
      fill('[name="name"]', "Disposable frontend agent");
      await submit("#token-form");
      await wait(
        () => page.querySelector("#new-token"),
        "Token creation failed",
      );
      assert.ok(page.querySelector("#new-token").value.length > 20);
      await click('[data-action="close"]');
      await wait(
        () => page.querySelector('[data-action="revoke"]'),
        "Token list did not refresh",
      );
      await click('[data-action="revoke"]');
      await wait(
        () =>
          page.querySelector(".token-row .badge")?.textContent === "Отозван",
        "Token revocation failed",
      );
      const assignments = await (
        await nativeFetch(base + "/api/v1/user-matrices", {
          headers: { Authorization: "Bearer " + token },
        })
      ).json();
      const workbook = await nativeFetch(
        base + "/api/v1/export/xlsx?user_matrix_id=" + assignments.items[0].id,
        { headers: { Authorization: "Bearer " + token } },
      );
      assert.equal(workbook.status, 200);
      const workbookBytes = new Uint8Array(await workbook.arrayBuffer());
      await route("catalog");
      await click('[data-action="import"]');
      fill('#import-form [name="name"]', "Imported frontend verification");
      const transfer = new win.DataTransfer();
      transfer.items.add(
        new win.File([workbookBytes], "frontend-roundtrip.xlsx", {
          type: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
        }),
      );
      page.querySelector('#import-form input[type="file"]').files =
        transfer.files;
      await submit("#import-form");
      await wait(
        () => page.querySelector('[data-action="confirm-import"]'),
        "XLSX preview failed",
      );
      await click('[data-action="confirm-import"]');
      await wait(
        () => page.querySelector('[data-action="publish"]'),
        "Import confirmation failed",
      );
      await click('[data-action="publish"]');
      await wait(
        () => page.querySelector("#assign-form"),
        "Imported draft publication failed",
      );
      assert.match(
        page.querySelector("#modal-title").textContent,
        /Imported frontend verification/,
      );
      await click('[data-action="close"]');
      await click('[data-action="logout"]');
      await wait(() => page.querySelector("#auth-form"), "Logout failed");
      assert.equal(win.sessionStorage.getItem("screener.session"), null);
      const revoked = await nativeFetch(base + "/api/v1/me", {
        headers: { Authorization: "Bearer " + token },
      });
      assert.equal(revoked.status, 401);
      token = "";
    } finally {
      if (token)
        await nativeFetch(base + "/api/v1/auth/logout", {
          method: "POST",
          headers: { Authorization: "Bearer " + token },
        });
      await win.happyDOM.close();
      for (const [key, descriptor] of previous) {
        if (descriptor) Object.defineProperty(globalThis, key, descriptor);
        else delete globalThis[key];
      }
    }
  },
);
