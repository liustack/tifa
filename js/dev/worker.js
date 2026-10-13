// tifa dev: one SSE connection shared by every tab of this origin.
// Each tab connects through a port, gets the latest status at once and every status after it.
// The browser ends the worker once no tab uses it.

const ports = new Set();
let source = null;
let latest = null;

function listen() {
  source = new EventSource("/__tifa/dev/events");
  source.addEventListener("status", (event) => {
    latest = event.data;
    for (const port of ports) port.postMessage(latest);
  });
}

self.onconnect = (event) => {
  const port = event.ports[0];
  ports.add(port);
  port.onmessage = (message) => {
    if (message.data === "close") ports.delete(port);
  };
  if (source === null) listen();
  else if (latest !== null) port.postMessage(latest);
};
