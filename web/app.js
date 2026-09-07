// Cliente M1: habla el protocolo JSON {type, data} con el servidor.

const $ = (id) => document.getElementById(id);
const joinView = $("join"), chatView = $("chat");
const joinForm = $("join-form"), nickInput = $("nick"), joinError = $("join-error");
const msgForm = $("msg-form"), msgInput = $("msg");
const log = $("log"), onlineList = $("online"), status = $("status");

let myNick = null;

const proto = location.protocol === "https:" ? "wss:" : "ws:";
const ws = new WebSocket(`${proto}//${location.host}/ws`);

const send = (type, data) => ws.send(JSON.stringify({ type, data }));

ws.addEventListener("close", () => {
  status.dataset.state = "closed";
  status.textContent = "desconectado — recarga la página";
});

ws.addEventListener("message", (ev) => {
  let env;
  try { env = JSON.parse(ev.data); } catch { return; }
  const d = env.data ?? {};

  switch (env.type) {
    case "nick_ok":
      myNick = d.nick;
      joinView.hidden = true;
      chatView.hidden = false;
      renderOnline(d.online);
      system(`entraste como ${d.nick}`);
      msgInput.focus();
      break;

    case "message":
      addMessage(d);
      break;

    case "user_joined":
      if (d.nick !== myNick) system(`${d.nick} entró`);
      renderOnline(d.online);
      break;

    case "user_left":
      system(`${d.nick} salió`);
      renderOnline(d.online);
      break;

    case "error":
      if (!myNick) joinError.textContent = d.message;
      else system(`error: ${d.message}`);
      break;
  }
});

joinForm.addEventListener("submit", (ev) => {
  ev.preventDefault();
  joinError.textContent = "";
  const nick = nickInput.value.trim();
  if (nick) send("set_nick", { nick });
});

msgForm.addEventListener("submit", (ev) => {
  ev.preventDefault();
  const content = msgInput.value.trim();
  if (!content || ws.readyState !== WebSocket.OPEN) return;
  send("send_message", { content });
  msgInput.value = "";
});

function addMessage({ author, content, sent_at }) {
  const li = document.createElement("li");
  li.className = "msg" + (author === myNick ? " mine" : "");

  const meta = document.createElement("div");
  meta.className = "meta";
  const time = new Date(sent_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  meta.textContent = `${author} · ${time}`;

  const body = document.createElement("div");
  body.textContent = content; // textContent: sin riesgo de inyectar HTML

  li.append(meta, body);
  appendToLog(li);
}

function system(text) {
  const li = document.createElement("li");
  li.className = "system";
  li.textContent = text;
  appendToLog(li);
}

function appendToLog(li) {
  // Autoscroll solo si ya estabas abajo (no interrumpe lectura del historial)
  const atBottom = log.scrollHeight - log.scrollTop - log.clientHeight < 40;
  log.appendChild(li);
  if (atBottom) log.scrollTop = log.scrollHeight;
}

function renderOnline(nicks) {
  onlineList.replaceChildren(...nicks.map((n) => {
    const li = document.createElement("li");
    li.textContent = n + (n === myNick ? " (tú)" : "");
    return li;
  }));
}
