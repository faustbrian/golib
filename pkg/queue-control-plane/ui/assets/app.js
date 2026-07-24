const session = { tenant: "", keyID: "", key: "", view: "workers", next: "" };

const views = {
  workers: {
    label: "Workers",
    kicker: "Live fleet",
    path: "workers",
    collection: "workers",
    cursor: "after",
  },
  queues: {
    label: "Queues",
    kicker: "Live backend status",
    path: "queues",
    collection: "queues",
    cursor: "cursor",
  },
  failures: {
    label: "Failures",
    kicker: "Payload hidden",
    path: "failures",
    collection: "records",
    cursor: "cursor",
  },
  dead_letters: {
    label: "Dead letters",
    kicker: "Payload hidden",
    path: "dead-letters",
    collection: "records",
    cursor: "cursor",
  },
  audit: {
    label: "Audit",
    kicker: "Tamper-evident history",
    path: "audit",
    collection: "events",
    cursor: "after",
  },
  commands: {
    label: "Commands",
    kicker: "Durable outcomes",
    path: "commands",
    collection: "records",
    cursor: "cursor",
  },
};

const byID = (id) => document.getElementById(id);

const authHeaders = () => ({
  "X-Queue-Control-Key-ID": session.keyID,
  "X-Queue-Control-Key": session.key,
});

const endpoint = (view, cursor = "") => {
  const definition = views[view];
  const separator = definition.path.includes("?") ? "&" : "?";
  const pagination = cursor
    ? `${separator}${definition.cursor}=${encodeURIComponent(cursor)}`
    : "";

  return `/v1/tenants/${encodeURIComponent(session.tenant)}/${definition.path}${pagination}`;
};

const requestJSON = async (url, options = {}) => {
  const response = await fetch(url, {
    ...options,
    headers: { ...authHeaders(), ...(options.headers || {}) },
  });
  const body = await response
    .json()
    .catch(() => ({ code: "invalid_response" }));
  if (!response.ok) {
    throw new Error(body.code || `http_${response.status}`);
  }

  return body;
};

const displayValue = (value) => {
  if (value === null || value === undefined) return "—";
  if (typeof value === "object") return JSON.stringify(value);

  return String(value);
};

const renderRows = (rows, append) => {
  const results = byID("results");
  if (!append) results.replaceChildren();
  if (rows.length === 0 && !append) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "No records in this bounded view.";
    results.append(empty);
    return;
  }
  for (const row of rows) {
    const article = document.createElement("article");
    article.className = "result-card";
    const entries = Object.entries(row);
    const title = document.createElement("h3");
    title.textContent = displayValue(
      entries.find(([key]) =>
        ["worker_id", "queue", "id", "sequence", "idempotency_key"].includes(
          key,
        ),
      )?.[1] || "Record",
    );
    article.append(title);
    const list = document.createElement("dl");
    for (const [key, value] of entries) {
      const term = document.createElement("dt");
      term.textContent = key.replaceAll("_", " ");
      const description = document.createElement("dd");
      description.textContent = displayValue(value);
      list.append(term, description);
    }
    article.append(list);
    results.append(article);
  }
};

const nextCursor = (body) => body.next_cursor || body.next_sequence || "";

const loadView = async (append = false) => {
  const definition = views[session.view];
  byID("notice").textContent = "Loading bounded results…";
  byID("refresh").disabled = true;
  try {
    const body = await requestJSON(
      endpoint(session.view, append ? session.next : ""),
    );
    const rows = Array.isArray(body[definition.collection])
      ? body[definition.collection]
      : [];
    renderRows(rows, append);
    session.next = nextCursor(body);
    byID("load-more").hidden = !session.next;
    byID("notice").textContent =
      `${rows.length} record${rows.length === 1 ? "" : "s"} loaded.`;
  } catch (error) {
    byID("notice").textContent =
      `Unable to load ${definition.label.toLowerCase()}: ${error.message}`;
    if (!append) byID("results").replaceChildren();
  } finally {
    byID("refresh").disabled = false;
  }
};

const selectView = (view) => {
  session.view = view;
  session.next = "";
  const definition = views[view];
  byID("view-title").textContent = definition.label;
  byID("view-kicker").textContent = definition.kicker;
  for (const button of document.querySelectorAll("[data-view]")) {
    button.setAttribute(
      "aria-current",
      button.dataset.view === view ? "page" : "false",
    );
  }
  void loadView();
};

const commandBody = () => {
  const action = byID("action").value;
  const body = {
    idempotency_key: byID("idempotency-key").value,
    reason: byID("reason").value,
    action,
    target: {
      kind: byID("target-kind").value,
      name: byID("target-name").value,
    },
    requested_at: new Date().toISOString(),
    confirmed: byID("confirmed").checked,
  };
  if (action === "bulk_retry")
    body.selection = { limit: Number(byID("selection-limit").value) };
  if (action === "replay")
    body.replay = {
      destination: byID("replay-destination").value,
      idempotency_policy: byID("replay-policy").value,
    };
  if (action === "scale")
    body.scale = { replicas: Number(byID("scale-replicas").value) };

  return body;
};

byID("session-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  session.tenant = byID("tenant").value.trim();
  session.keyID = byID("key-id").value.trim();
  session.key = byID("api-key").value;
  byID("api-key").value = "";
  byID("connection-status").textContent = `Tenant ${session.tenant}`;
  byID("workspace").hidden = false;
  byID("idempotency-key").value = crypto.randomUUID();
  selectView("workers");
});

for (const button of document.querySelectorAll("[data-view]")) {
  button.addEventListener("click", () => selectView(button.dataset.view));
}
byID("refresh").addEventListener("click", () => loadView());
byID("load-more").addEventListener("click", () => loadView(true));
byID("command-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const output = byID("command-result");
  output.textContent = "Submitting…";
  try {
    const result = await requestJSON(
      `/v1/tenants/${encodeURIComponent(session.tenant)}/commands`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(commandBody()),
      },
    );
    output.textContent = JSON.stringify(result, null, 2);
    byID("idempotency-key").value = crypto.randomUUID();
    if (session.view === "commands") void loadView();
  } catch (error) {
    output.textContent = `Command rejected: ${error.message}`;
  }
});
