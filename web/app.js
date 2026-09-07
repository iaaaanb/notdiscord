// Cliente M2: protocolo JSON con canales múltiples.

const $ = (id) => document.getElementById(id);
const joinView = $("join"), chatView = $("chat");
const joinForm = $("join-form"), nickInput = $("nick"), joinError = $("join-error");
const msgForm = $("msg-form"), msgInput = $("msg");
const chanForm = $("chan-form"), chanInput = $("chan");
const log = $("log"), onlineList = $("online"), channelList = $("channels");
const status = $("status"), channelTitle = $("channel-title");

let myNick = null;
let currentChannel = null;
let channels = [];

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
      channels = d.channels;
      joinView.hidden = true;
      chatView.hidden = false;
      setChannel(d.channel);
      renderOnline(d.online);
      msgInput.focus();
      break;

    case "message":
      if (d.channel === currentChannel) addMessage(d);
      break;

    case "channel_list":
      channels = d.channels;
      renderChannels();
      break;

    case "channel_joined":
      setChannel(d.name);
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

chanForm.addEventListener("submit", (ev) => {
  ev.preventDefault();
  const name = chanInput.value.trim();
  if (!name) return;
  send("create_channel", { name });
  chanInput.value = "";
});

function setChannel(name) {
  currentChannel = name;
  channelTitle.textContent = `# ${name}`;
  msgInput.placeholder = `Mensaje a # ${name}`;
  log.replaceChildren(); // sin historial todavía: llega en M3
  system(`estás en # ${name}`);
  renderChannels();
  msgInput.focus();
}

function renderChannels() {
  channelList.replaceChildren(...channels.map((name) => {
    const li = document.createElement("li");
    const btn = document.createElement("button");
    btn.type = "button";
    btn.textContent = `# ${name}`;
    btn.className = name === currentChannel ? "chan current" : "chan";
    btn.addEventListener("click", () => {
      if (name !== currentChannel) send("join_channel", { name });
    });
    li.appendChild(btn);
    return li;
  }));
}

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
