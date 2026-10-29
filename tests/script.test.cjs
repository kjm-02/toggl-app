const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { test } = require("node:test");
const vm = require("node:vm");

const source = readFileSync(require.resolve("../static/script.js"), "utf8");
const date = "2026-10-29";

function setup({ onRequest, loggedIn = true } = {}) {
  const requests = [];
  const navigations = [];
  const toasts = [];
  const select = text => ({ options: [{ text }], selectedIndex: 0 });
  const elements = {
    date: { value: date, addEventListener() {} },
    task: { value: "test task" },
    project_name: select("その他"),
    work_class: select("管理"),
    start_time: { value: "09:00" },
    end_time: { value: "10:00" },
  };
  const row = {
    dataset: { id: "42" },
    children: ["edited task", "", "", "09:00", "10:00"].map(value => ({
      innerText: "unchanged",
      querySelector: () => ({ value }),
    })),
    querySelector: selector => elements[selector.slice(1)],
  };
  const location = {
    origin: "https://work.example",
    set href(value) { navigations.push(value); },
  };
  const context = vm.createContext({
    URL,
    document: {
      body: { dataset: { login: String(loggedIn) } },
      querySelectorAll: () => [],
      getElementById: id => elements[id],
    },
    window: { location },
    button: { closest: () => row },
    async fetch(url, options) {
      assert.equal(navigations.length, 0, "must not navigate before all writes finish");
      const request = { url, method: options.method, body: options.body && JSON.parse(options.body) };
      requests.push(request);
      if (onRequest) {
        const response = await onRequest(request, requests.length);
        if (response) return response;
      }
      return { ok: true };
    },
    recordToast: (message, type) => toasts.push({ message, type }),
  });
  vm.runInContext(source, context);
  vm.runInContext("showToast = recordToast", context);
  return {
    requests, navigations, toasts, elements, row,
    run: expression => vm.runInContext(expression, context),
  };
}

test("afternoon assembly closes active work and saves both times before navigating", async () => {
  const app = setup();
  await app.run("afternoonAssembly()");
  assert.deepEqual(app.requests.map(request => request.url), [
    "https://work.example/works/end", "https://work.example/works/start",
  ]);
  assert.equal(app.requests[0].body.works.end_time, `${date} 13:00:00`);
  assert.deepEqual(app.requests[1].body.works, {
    project_name: "その他", work_class: "管理", task_name: "昼礼",
    start_time: `${date} 13:00:00`, end_time: `${date} 13:15:00`, memo: "実装",
  });
  assert.deepEqual(app.navigations, [`https://work.example/?date=${date}`]);
});

for (const [action, task, start, end] of [
  ["lunch()", "昼休み", "11:45:00", "12:45:00"],
  ["dailyScrum()", "デイリースクラム", "11:00:00", "11:30:00"],
  ["resister()", "test task", "09:00", "10:00"],
]) {
  test(`${action} saves a complete interval in one request and refreshes once`, async () => {
    const app = setup();
    await app.run(action);
    assert.equal(app.requests.length, 1);
    assert.equal(app.requests[0].body.works.task_name, task);
    assert.equal(app.requests[0].body.works.start_time, `${date} ${start}`);
    assert.equal(app.requests[0].body.works.end_time, `${date} ${end}`);
    assert.deepEqual(app.navigations, [`https://work.example/?date=${date}`]);
  });
}

test("start waits for end then create, with one final refresh", async () => {
  const app = setup();
  await app.run("start()");
  assert.deepEqual(app.requests.map(request => request.url), [
    "https://work.example/works/end", "https://work.example/works/start",
  ]);
  assert.equal(app.requests[0].body.works.end_time, app.requests[1].body.works.start_time);
  assert.equal(app.requests[1].body.works.end_time, null);
  assert.equal(app.navigations.length, 1);
});

for (const [action, method, path] of [
  ["end()", "POST", "/works/end"],
  ["saveRow(button)", "PATCH", "/works/42"],
  ["deleteRow(button)", "DELETE", "/works/42"],
]) {
  test(`${action} refreshes the selected date after success`, async () => {
    const app = setup();
    await app.run(action);
    assert.equal(app.requests.length, 1);
    assert.equal(app.requests[0].method, method);
    assert.equal(app.requests[0].url, `https://work.example${path}`);
    assert.deepEqual(app.navigations, [`https://work.example/?date=${date}`]);
  });
}

for (const action of ["start()", "end()", "resister()", "lunch()", "dailyScrum()", "afternoonAssembly()", "saveRow(button)", "deleteRow(button)"]) {
  test(`${action} reports failed writes without navigating or continuing`, async () => {
    const app = setup({
      onRequest: () => ({ ok: false, status: 500, json: async () => ({ error: "database error" }) }),
    });
    await app.run(action);
    assert.equal(app.requests.length, 1);
    assert.equal(app.navigations.length, 0);
    assert.deepEqual(app.toasts, [{ message: "database error", type: "error" }]);
    assert.ok(app.row.children.every(cell => cell.innerText === "unchanged"));
  });
}

test("failed assembly insert does not refresh, even after active work was closed", async () => {
  const app = setup({
    onRequest: (_, count) => count === 2
      ? { ok: false, status: 400, json: async () => ({ error: "insert failed" }) }
      : undefined,
  });
  await app.run("afternoonAssembly()");
  assert.equal(app.requests.length, 2);
  assert.equal(app.navigations.length, 0);
  assert.equal(app.toasts[0].message, "insert failed");
});

test("network failure allows retry and never displays success", async () => {
  let fail = true;
  const app = setup({ onRequest() { if (fail) throw new Error("offline"); } });
  await app.run("end()");
  assert.equal(app.navigations.length, 0);
  assert.equal(app.toasts[0].message, "offline");
  fail = false;
  await app.run("end()");
  assert.equal(app.navigations.length, 1);
});

test("pending request blocks duplicate actions and delays navigation until completion", async () => {
  let finish;
  const app = setup({ onRequest: () => new Promise(resolve => { finish = resolve; }) });
  const pending = app.run("end()");
  await app.run("end()");
  assert.equal(app.requests.length, 1);
  assert.equal(app.navigations.length, 0);
  finish();
  await pending;
  assert.equal(app.navigations.length, 1);
});

test("logged-out actions redirect to login without writes", async () => {
  const app = setup({ loggedIn: false });
  await app.run("afternoonAssembly()");
  assert.equal(app.requests.length, 0);
  assert.deepEqual(app.navigations, ["/login"]);
});

test("times use zero-padded 24-hour format independent of browser locale", () => {
  const app = setup();
  assert.equal(app.run("formatTime({ getHours: () => 9, getMinutes: () => 3, getSeconds: () => 5 })"), "09:03:05");
  assert.equal(app.run("formatTime({ getHours: () => 13, getMinutes: () => 15, getSeconds: () => 0 })"), "13:15:00");
});
