import { expect, test } from "@playwright/test";

const approvedOrigin = "http://127.0.0.1:18080";
const approvedPage = `${approvedOrigin}/security-probe`;
const hostileOrigin = "http://127.0.0.1:18082";
const apiOrigin = "http://127.0.0.1:18081";
const probeURL = (journey) => `${apiOrigin}/probe/${journey}`;
const observationsURL = `${apiOrigin}/observations`;

const readObservations = (page) =>
  page.evaluate(async (url) => {
    const response = await fetch(url);

    return response.json();
  }, observationsURL);

test.beforeEach(async ({ page }) => {
  await page.goto(approvedPage);
});

test("approved origins receive bounded data and defensive headers", async ({
  page,
}) => {
  const url = probeURL("approved-origin");
  const responsePromise = page.waitForResponse(
    (response) =>
      response.url() === url && response.request().method() === "GET",
  );

  const result = await page.evaluate(async (target) => {
    const response = await fetch(target, { credentials: "include" });

    return {
      body: await response.json(),
      status: response.status,
    };
  }, url);
  const response = await responsePromise;
  const headers = await response.allHeaders();
  const requestHeaders = await response.request().allHeaders();

  expect(result).toEqual({ body: { status: "ok" }, status: 200 });
  expect(headers["access-control-allow-origin"]).toBe(approvedOrigin);
  expect(headers["access-control-allow-credentials"]).toBe("true");
  expect(headers["cache-control"]).toBe("no-store");
  expect(headers["content-security-policy"]).toBe(
    "default-src 'none'; frame-ancestors 'none'",
  );
  expect(headers["referrer-policy"]).toBe("no-referrer");
  expect(headers["x-content-type-options"]).toBe("nosniff");
  expect(headers["x-frame-options"]).toBe("DENY");
  expect(requestHeaders.origin).toBe(approvedOrigin);
});

test("the browser admits only the documented preflight surface", async ({
  page,
}) => {
  const allowedURL = probeURL("allowed-preflight");
  const mutationPromise = page.waitForResponse(
    (response) =>
      response.url() === allowedURL && response.request().method() === "POST",
  );

  const result = await page.evaluate(async (url) => {
    const response = await fetch(url, {
      body: JSON.stringify({ probe: true }),
      headers: {
        "Content-Type": "application/json",
        "X-Queue-Control-Key-ID": "browser-test",
      },
      method: "POST",
    });

    return response.status;
  }, allowedURL);
  const mutation = await mutationPromise;
  const allowedObservations = await readObservations(page);
  const allowedRequests = allowedObservations.filter(
    (observation) => observation.path === "/probe/allowed-preflight",
  );

  expect(allowedRequests).toEqual([
    {
      method: "OPTIONS",
      origin: approvedOrigin,
      path: "/probe/allowed-preflight",
      status: 204,
    },
    {
      method: "POST",
      origin: approvedOrigin,
      path: "/probe/allowed-preflight",
      status: 200,
    },
  ]);
  expect(mutation.status()).toBe(200);
  expect(result).toBe(200);

  const forbiddenURL = probeURL("forbidden-preflight");
  const blocked = await page.evaluate(async (url) => {
    try {
      await fetch(url, {
        headers: { "X-Not-Allowed": "blocked" },
        method: "POST",
      });

      return false;
    } catch {
      return true;
    }
  }, forbiddenURL);
  const forbiddenObservations = await readObservations(page);
  const forbiddenRequests = forbiddenObservations.filter(
    (observation) => observation.path === "/probe/forbidden-preflight",
  );

  expect(blocked).toBe(true);
  expect(forbiddenRequests).toEqual([
    {
      method: "OPTIONS",
      origin: approvedOrigin,
      path: "/probe/forbidden-preflight",
      status: 403,
    },
  ]);
});

test("cookie mutations require a matching CSRF header", async ({
  context,
  page,
}) => {
  await context.addCookies([
    {
      name: "csrf_token",
      sameSite: "Lax",
      url: apiOrigin,
      value: "browser-csrf-token",
    },
  ]);

  const rejected = await page.evaluate(async (url) => {
    const response = await fetch(url, {
      credentials: "include",
      headers: { "X-CSRF-Token": "wrong-token" },
      method: "POST",
    });

    return response.status;
  }, probeURL("csrf-rejected"));
  expect(rejected).toBe(403);

  const acceptedURL = probeURL("csrf-accepted");
  const acceptedResponsePromise = page.waitForResponse(
    (response) =>
      response.url() === acceptedURL && response.request().method() === "POST",
  );
  const accepted = await page.evaluate(async (url) => {
    const response = await fetch(url, {
      credentials: "include",
      headers: { "X-CSRF-Token": "browser-csrf-token" },
      method: "POST",
    });

    return response.status;
  }, acceptedURL);
  const acceptedResponse = await acceptedResponsePromise;
  const requestHeaders = await acceptedResponse.request().allHeaders();

  expect(accepted).toBe(200);
  expect(requestHeaders.cookie).toContain("csrf_token=browser-csrf-token");
});

test("an unapproved page origin cannot read the API response", async ({
  page,
}) => {
  await page.goto(hostileOrigin);
  const url = probeURL("hostile-origin");
  const failedPromise = page.waitForEvent(
    "requestfailed",
    (request) => request.url() === url && request.method() === "GET",
  );

  const result = await page.evaluate(async (target) => {
    try {
      await fetch(target);

      return { blocked: false };
    } catch (error) {
      return { blocked: true, error: error.name };
    }
  }, url);
  const failedRequest = await failedPromise;
  await page.goto(approvedPage);
  const observations = await readObservations(page);
  const hostileRequests = observations.filter(
    (observation) => observation.path === "/probe/hostile-origin",
  );

  expect(result).toEqual({ blocked: true, error: "TypeError" });
  expect(failedRequest.failure()).not.toBeNull();
  expect(hostileRequests).toEqual([
    {
      method: "GET",
      origin: hostileOrigin,
      path: "/probe/hostile-origin",
      status: 403,
    },
  ]);
});
