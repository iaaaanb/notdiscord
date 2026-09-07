// M0: cliente mínimo de prueba. Envía texto plano y muestra el echo.
// En M1 esto pasa a hablar el protocolo JSON ({type, data}).

const log = document.getElementById("log");
const form = document.getElementById("form");
const input = document.getElementById("input");
const status = document.getElementById("status");

const proto = location.protocol === "https:" ? "wss:" : "ws:";
const ws = new WebSocket(`${proto}//${location.host}/ws`);

function setStatus(state, text) {
  status.dataset.state = state;
  status.textContent = text;
}

function addLine(kind, text) {
  const li = document.createElement("li");
  li.className = kind;
  li.textContent = text;
  log.appendChild(li);
  log.scrollTop = log.scrollHeight;
}

ws.addEventListener("open", () => setStatus("open", "conectado"));
ws.addEventListener("close", () => setStatus("closed", "desconectado"));
ws.addEventListener("error", () => setStatus("closed", "error de conexión"));
ws.addEventListener("message", (ev) => addLine("recv", `← ${ev.data}`));

form.addEventListener("submit", (ev) => {
  ev.preventDefault();
  const text = input.value.trim();
  if (!text || ws.readyState !== WebSocket.OPEN) return;
  ws.send(text);
  addLine("sent", `→ ${text}`);
  input.value = "";
});
