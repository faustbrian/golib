import { expect, test } from "@playwright/test";

const consoleURL = "http://127.0.0.1:18080";

const connect = async (page) => {
  await page.goto(consoleURL);
  await page.getByLabel("Tenant").fill("tenant-1");
  await page.getByLabel("Key ID").fill("browser-key");
  await page.getByLabel("API key").fill("browser-secret");
  await page.getByRole("button", { name: "Connect" }).click();
};

test("the embedded console keeps credentials ephemeral and renders status", async ({
  page,
}) => {
  const workerRequest = page.waitForRequest(
    (request) =>
      request.url().endsWith("/v1/tenants/tenant-1/workers") &&
      request.method() === "GET",
  );
  await connect(page);
  const request = await workerRequest;

  expect(request.headers()["x-queue-control-key-id"]).toBe("browser-key");
  expect(request.headers()["x-queue-control-key"]).toBe("browser-secret");
  await expect(page.getByRole("heading", { name: "worker-1" })).toBeVisible();
  await expect(page.getByText("Tenant tenant-1")).toBeVisible();
  await expect(page.getByLabel("API key")).toHaveValue("");
  expect(
    await page.evaluate(() => ({
      local: localStorage.length,
      session: sessionStorage.length,
    })),
  ).toEqual({ local: 0, session: 0 });

  await page.getByRole("button", { name: "Queues" }).click();
  await expect(page.getByRole("heading", { name: "critical" })).toBeVisible();
  await expect(page.getByText(/"value":7/)).toBeVisible();
});

test("untrusted API values render as text without executing markup", async ({
  page,
}) => {
  const payload = '<img src=x onerror="globalThis.__queueControlXSS=true">';
  await page.route("**/v1/tenants/tenant-1/workers", async (route) => {
    await route.fulfill({
      json: {
        workers: [
          {
            queues: [payload],
            state: "running",
            tenant_id: "tenant-1",
            version: "v1.0.0",
            worker_id: payload,
          },
        ],
      },
    });
  });

  await connect(page);

  await expect(page.getByRole("heading", { name: payload })).toBeVisible();
  await expect(page.locator("#results img")).toHaveCount(0);
  expect(
    await page.evaluate(() => globalThis.__queueControlXSS),
  ).toBeUndefined();
});

test("critical workflows expose keyboard-reachable labeled controls", async ({
  page,
}) => {
  await page.goto(consoleURL);
  await page.keyboard.press("Tab");
  await expect(page.getByLabel("Tenant")).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(page.getByLabel("Key ID")).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(page.getByLabel("API key")).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "Connect" })).toBeFocused();

  await connect(page);
  const unnamedControls = await page
    .locator("button, input, select, textarea")
    .evaluateAll((controls) =>
      controls
        .filter(
          (control) =>
            !control.labels?.length &&
            !control.getAttribute("aria-label") &&
            !control.textContent?.trim(),
        )
        .map((control) => `${control.tagName.toLowerCase()}#${control.id}`),
    );

  expect(unnamedControls).toEqual([]);
  await expect(
    page.getByRole("navigation", { name: "Administrative views" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Submit audited command" }),
  ).toBeVisible();
});

test("failure inspection stays hidden and commands use the public envelope", async ({
  page,
}) => {
  await connect(page);
  const failureRequest = page.waitForRequest(
    (request) =>
      request.url().includes("/v1/tenants/tenant-1/failures") &&
      request.method() === "GET",
  );
  await page.getByRole("button", { name: "Failures" }).click();
  const failureURL = new URL((await failureRequest).url());
  expect(failureURL.searchParams.has("payload")).toBe(false);
  await expect(page.getByRole("heading", { name: "failure-1" })).toBeVisible();
  await expect(page.getByText("payload visibility")).toBeVisible();
  await expect(page.getByText("hidden", { exact: true })).toBeVisible();

  await page.getByLabel("Target name").fill("critical");
  await page.getByLabel("Reason").fill("maintenance window");
  const commandRequest = page.waitForRequest(
    (request) =>
      request.url().endsWith("/v1/tenants/tenant-1/commands") &&
      request.method() === "POST",
  );
  await page.getByRole("button", { name: "Submit audited command" }).click();
  const request = await commandRequest;
  const body = request.postDataJSON();

  expect(body.action).toBe("pause");
  expect(body.target).toEqual({ kind: "queue", name: "critical" });
  expect(body.reason).toBe("maintenance window");
  expect(body.idempotency_key).toMatch(/^[0-9a-f-]{36}$/);
  expect(body.requested_at).toMatch(/Z$/);
  await expect(page.getByText('"status": "accepted"')).toBeVisible();
});

test("every public mutation uses its action-specific API envelope", async ({
  page,
}) => {
  await connect(page);
  await page.getByText("Action-specific options").click();
  const cases = [
    { action: "pause", kind: "queue" },
    { action: "resume", kind: "queue" },
    { action: "drain", kind: "worker" },
    { action: "terminate", kind: "worker_group" },
    { action: "retry", kind: "failure" },
    { action: "bulk_retry", kind: "failure", selection: 17 },
    { action: "delete", kind: "dead_letter" },
    { action: "purge", kind: "queue", confirmed: true },
    {
      action: "replay",
      kind: "failure",
      confirmed: true,
      replay: {
        destination: "recovery",
        idempotency_policy: "replace_duplicate",
      },
    },
    { action: "scale", kind: "workload", replicas: 3 },
  ];

  for (const command of cases) {
    await page.getByLabel("Action").selectOption(command.action);
    await page.getByLabel("Target kind").selectOption(command.kind);
    await page.getByLabel("Target name").fill(`${command.action}-target`);
    await page.getByLabel("Reason").fill("maintenance window");
    await page
      .getByLabel("I confirm this operation when required.")
      .setChecked(Boolean(command.confirmed));
    if (command.selection)
      await page.getByLabel("Selection limit").fill(String(command.selection));
    if (command.replay) {
      await page
        .getByLabel("Replay destination")
        .fill(command.replay.destination);
      await page
        .getByLabel("Replay policy")
        .selectOption(command.replay.idempotency_policy);
    }
    if (command.replicas !== undefined)
      await page.getByLabel("Scale replicas").fill(String(command.replicas));

    const commandRequest = page.waitForRequest(
      (request) =>
        request.url().endsWith("/v1/tenants/tenant-1/commands") &&
        request.method() === "POST",
    );
    await page.getByRole("button", { name: "Submit audited command" }).click();
    const body = (await commandRequest).postDataJSON();

    expect(body.action).toBe(command.action);
    expect(body.target).toEqual({
      kind: command.kind,
      name: `${command.action}-target`,
    });
    expect(body.selection).toEqual(
      command.selection ? { limit: command.selection } : undefined,
    );
    expect(body.replay).toEqual(command.replay);
    expect(body.scale).toEqual(
      command.replicas === undefined
        ? undefined
        : { replicas: command.replicas },
    );
    await expect(page.getByText('"status": "accepted"')).toBeVisible();
  }
});
