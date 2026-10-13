// tifa dev: reloads the page after each rebuild and shows build errors in an overlay.
// It needs no imports and no exports, so it works bundled into the dev client entry or as a plain script.
(() => {
  if (window.__tifaDev) return;
  window.__tifaDev = true;

  const base = "/__tifa/dev/";
  let first = null; // the version this page was built from
  let overlay = null;

  function onStatus(raw) {
    let status;
    try {
      status = JSON.parse(raw);
    } catch (err) {
      console.error("tifa dev: unreadable status", raw, err);
      return;
    }
    if (first === null) first = status.version;
    if (status.error) {
      show(status.title || "Error", status.error);
    } else if (status.version !== first) {
      location.reload();
    } else {
      hide();
    }
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

  // A page restored from the back-forward cache may be from an older build.
  addEventListener("pageshow", (event) => {
    if (event.persisted) location.reload();
  });

  if (typeof SharedWorker === "function") {
    try {
      const worker = new SharedWorker(base + "worker.js", { name: "tifa-dev" });
      worker.port.onmessage = (event) => onStatus(event.data);
      addEventListener("pagehide", () => worker.port.postMessage("close"));
      return;
    } catch (err) {
      console.warn("tifa dev: SharedWorker failed, this tab opens its own connection", err);
    }
  }
  const source = new EventSource(base + "events");
  source.addEventListener("status", (event) => onStatus(event.data));
  addEventListener("pagehide", () => source.close());
})();
