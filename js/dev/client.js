// tifa dev: reloads the page after each rebuild and shows build errors in an overlay. The error
// page of a failed server render also reloads after a rebuild that kept the version, such as a
// fix in TSX. A page that shows its Error component for a failed server render carries that
// failure in window.__tifaDevPageError, {title, text}, which the overlay shows while no build
// error does. Errors of the browser (hydration mismatches, uncaught errors and rejections) show in a
// corner panel, see below.
// It needs no imports and no exports, so it works bundled into the dev client entry or as a plain script.
(() => {
  if (window.__tifaDev) return;
  window.__tifaDev = true;

  const base = "/__tifa/dev/";
  let first = pageTiming("tifa-dev", window.__tifaDevServed); // the version this page was built from, null while unknown
  let firstRender = pageTiming("tifa-dev-render", window.__tifaDevRendered); // the count of builds served then
  let overlay = null;

  // The dev proxy names the build behind every answer in a Server-Timing header. The Performance
  // API exposes it in secure contexts, which include localhost and 127.0.0.1. Elsewhere the page
  // falls back to what client.js was served with, then to the first status it gets, and can miss
  // a rebuild that finished while it loaded.
  function pageTiming(name, served) {
    const nav = performance.getEntriesByType("navigation")[0];
    for (const timing of (nav && nav.serverTiming) || []) {
      if (timing.name === name) return timing.description;
    }
    if (served !== undefined) return String(served);
    return null;
  }

  function onStatus(raw) {
    let status;
    try {
      status = JSON.parse(raw);
    } catch (err) {
      console.error("tifa dev: unreadable status", raw, err);
      return;
    }
    if (first === null) first = status.version;
    if (firstRender === null) firstRender = String(status.render);
    if (status.error) {
      show(status.title || "Error", status.error);
    } else if (status.version !== first) {
      location.reload();
    } else if (window.__tifaDevErrorPage === true && String(status.render) !== firstRender) {
      location.reload();
    } else {
      showPageError();
    }
  }

  function showPageError() {
    const failed = window.__tifaDevPageError;
    if (failed) show(failed.title, failed.text);
    else hide();
  }

  function show(title, text) {
    hide();
    const host = document.createElement("tifa-dev-overlay");
    const root = host.attachShadow({ mode: "open" });
    root.innerHTML = `<style>
      :host { all: initial; }
      .backdrop { position: fixed; inset: 0; z-index: 2147483647; display: flex; align-items: flex-start;
        justify-content: center; padding: 6vh 16px; box-sizing: border-box; background: rgba(15, 15, 20, 0.6);
        font: 14px/1.5 ui-sans-serif, system-ui, sans-serif; }
      .panel { width: min(960px, 100%); max-height: 88vh; display: flex; flex-direction: column; overflow: hidden;
        background: #1b1b22; color: #ececf1; border-radius: 10px; border-top: 4px solid #f05252;
        box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5); }
      header { display: flex; align-items: center; gap: 12px; padding: 14px 18px; border-bottom: 1px solid #2e2e38; }
      h1 { flex: 1; margin: 0; font-size: 16px; font-weight: 600; color: #ff8a8a; }
      p { margin: 0; padding: 10px 18px 0; color: #a1a1ad; font-size: 13px; }
      button { font: inherit; font-size: 13px; color: #ececf1; background: #2e2e38; border: 0; border-radius: 6px;
        padding: 5px 12px; cursor: pointer; }
      button:hover { background: #3a3a46; }
      pre { margin: 0; padding: 14px 18px 18px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere;
        font: 13px/1.55 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; user-select: text; }
    </style>
    <div class="backdrop" role="alertdialog" aria-modal="true" aria-labelledby="title">
      <div class="panel">
        <header><h1 id="title"></h1><button type="button">Dismiss</button></header>
        <p>This page reloads once a build succeeds.</p>
        <pre></pre>
      </div>
    </div>`;
    root.querySelector("h1").textContent = title;
    root.querySelector("pre").textContent = text.replace(/\x1b\[[0-9;]*m/g, "");
    root.querySelector("button").addEventListener("click", hide);
    document.documentElement.appendChild(host);
    overlay = host;
  }

  function hide() {
    if (overlay !== null) overlay.remove();
    overlay = null;
  }

  // Errors of the browser: the runtime reports the ones React catches as a "tifa:browser-error"
  // event on window, {kind: "recoverable" | "uncaught", message, stack, componentStack}, and the
  // errors no one caught arrive as window "error" and "unhandledrejection" events. They show in
  // a small panel in a corner that leaves the page usable, apart from the overlay of a build
  // error, which covers it and stays on top.
  const maxBrowserErrors = 20;
  const browserErrors = []; // oldest first: {key, kind, title, text, count}
  let errorPanel = null;

  function browserTitle(kind, message) {
    if (kind === "recoverable") return /hydrat/i.test(message) ? "Hydration mismatch" : "Recoverable error";
    if (kind === "rejection") return "Unhandled rejection";
    return "Uncaught error";
  }

  function noteBrowserError(kind, message, stack, componentStack) {
    message = String(message || "");
    stack = String(stack || "");
    const key = kind + "\n" + message + "\n" + stack;
    const seen = browserErrors.find((entry) => entry.key === key);
    if (seen) {
      seen.count++;
    } else {
      let text = stack.includes(message) ? stack : message + (stack ? "\n" + stack : "");
      if (componentStack) text += "\n\nComponent stack:" + componentStack;
      browserErrors.push({ key, title: browserTitle(kind, message), text, count: 1 });
      if (browserErrors.length > maxBrowserErrors) browserErrors.shift();
    }
    renderBrowserErrors();
  }

  function renderBrowserErrors() {
    if (errorPanel !== null) errorPanel.remove();
    errorPanel = null;
    if (browserErrors.length === 0) return;
    const host = document.createElement("tifa-dev-errors");
    const root = host.attachShadow({ mode: "open" });
    const style = document.createElement("style");
    style.textContent = `
      :host { all: initial; }
      .stack { position: fixed; right: 16px; bottom: 16px; z-index: 2147483646; width: min(560px, calc(100vw - 32px));
        max-height: 60vh; overflow: auto; display: flex; flex-direction: column; gap: 8px;
        font: 14px/1.5 ui-sans-serif, system-ui, sans-serif; }
      .entry { background: #1b1b22; color: #ececf1; border-radius: 8px; border-left: 4px solid #f05252;
        box-shadow: 0 8px 30px rgba(0, 0, 0, 0.4); overflow: hidden; flex: none; }
      header { display: flex; align-items: center; gap: 10px; padding: 8px 12px; }
      h2 { flex: 1; margin: 0; font-size: 14px; font-weight: 600; color: #ff8a8a; }
      .count { font-size: 12px; background: #f05252; color: #fff; border-radius: 999px; padding: 0 8px; }
      button { font: inherit; font-size: 12px; color: #ececf1; background: #2e2e38; border: 0; border-radius: 6px;
        padding: 3px 10px; cursor: pointer; }
      button:hover { background: #3a3a46; }
      pre { margin: 0; padding: 0 12px 12px; max-height: 240px; overflow: auto; white-space: pre-wrap;
        overflow-wrap: anywhere; font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
        user-select: text; }
      .all { align-self: flex-end; }`;
    const stack = document.createElement("div");
    stack.className = "stack";
    if (browserErrors.length > 1) {
      const all = document.createElement("button");
      all.className = "all";
      all.textContent = "Dismiss all";
      all.addEventListener("click", () => {
        browserErrors.length = 0;
        renderBrowserErrors();
      });
      stack.append(all);
    }
    // The newest error first.
    for (const entry of [...browserErrors].reverse()) {
      const box = document.createElement("div");
      box.className = "entry";
      const head = document.createElement("header");
      const title = document.createElement("h2");
      title.textContent = entry.title;
      head.append(title);
      if (entry.count > 1) {
        const count = document.createElement("span");
        count.className = "count";
        count.textContent = "×" + entry.count;
        head.append(count);
      }
      const dismiss = document.createElement("button");
      dismiss.textContent = "Dismiss";
      dismiss.addEventListener("click", () => {
        browserErrors.splice(browserErrors.indexOf(entry), 1);
        renderBrowserErrors();
      });
      head.append(dismiss);
      const body = document.createElement("pre");
      body.textContent = entry.text.replace(/\x1b\[[0-9;]*m/g, "");
      box.append(head, body);
      stack.append(box);
    }
    root.append(style, stack);
    document.documentElement.appendChild(host);
    errorPanel = host;
  }

  addEventListener("tifa:browser-error", (event) => {
    const d = event.detail || {};
    noteBrowserError(d.kind === "recoverable" ? "recoverable" : "uncaught", d.message, d.stack, d.componentStack);
  });
  addEventListener("error", (event) => {
    const err = event.error;
    if (err instanceof Error) noteBrowserError("uncaught", err.message, err.stack);
    else noteBrowserError("uncaught", event.message + (event.filename ? "\n    at " + event.filename + ":" + event.lineno + ":" + event.colno : ""), "");
  });
  addEventListener("unhandledrejection", (event) => {
    const reason = event.reason;
    if (reason instanceof Error) noteBrowserError("rejection", reason.message, reason.stack);
    else noteBrowserError("rejection", typeof reason === "string" ? reason : JSON.stringify(reason) ?? String(reason), "");
  });

  if (window.__tifaDevPageError) showPageError();

  // A page restored from the back-forward cache may be from an older build.
  addEventListener("pageshow", (event) => {
    if (event.persisted) location.reload();
  });

  let source = null;
  function listen() {
    if (source !== null) return;
    source = new EventSource(base + "events");
    source.addEventListener("status", (event) => onStatus(event.data));
    addEventListener("pagehide", () => source.close());
  }

  if (typeof SharedWorker !== "function") {
    listen();
    return;
  }
  let worker;
  try {
    worker = new SharedWorker(base + "worker.js", { name: "tifa-dev" });
  } catch (err) {
    console.warn("tifa dev: SharedWorker failed, this tab opens its own connection", err);
    listen();
    return;
  }
  // The worker can also fail later, for example when a Content-Security-Policy blocks it, and a
  // working one always sends the status at once. Either way, a silent worker means this tab listens itself.
  const giveUp = (why) => {
    clearTimeout(timer);
    worker.port.onmessage = null;
    worker.port.close();
    console.warn("tifa dev: " + why + ", this tab opens its own connection");
    listen();
  };
  const timer = setTimeout(() => giveUp("the SharedWorker sent nothing within 3s"), 3000);
  worker.addEventListener("error", () => giveUp("the SharedWorker failed"));
  worker.port.onmessage = (event) => {
    clearTimeout(timer);
    onStatus(event.data);
  };
  addEventListener("pagehide", () => worker.port.postMessage("close"));
})();
