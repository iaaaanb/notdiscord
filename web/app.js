// Cliente M4: reconexión automática con backoff, sesión persistente por
// pestaña, separadores de día y autoscroll que no te interrumpe.

const $ = (id) => document.getElementById(id);
const joinView = $("join"), chatView = $("chat");
const joinForm = $("join-form"), nickInput = $("nick"), joinError = $("join-error");
const msgForm = $("msg-form"), msgInput = $("msg"), msgBtn = $("msg-send");
const chanForm = $("chan-form"), chanInput = $("chan");
const log = $("log"), onlineList = $("online"), channelList = $("channels");
const statusEl = $("status"), retryBtn = $("retry"), jumpBtn = $("jump");
const channelTitle = $("channel-title");

// sessionStorage y no localStorage: es por pestaña, así puedes abrir dos
// pestañas con nicks distintos (y F5 no te saca del chat).
const SESSION_KEY = "notdiscord.session";

let session = loadSession(); // {nick, token, channel} | null
let myNick = null;
let authed = false;          // el servidor confirmó el nick en ESTE socket
let channels = [];
let currentChannel = null;

let ws = null;
let attempt = 0;             // intentos de reconexión seguidos
let retryTimer = null, countdownTimer = null, stableTimer = null;

let unread = 0;              // mensajes llegados mientras leías más arriba
let lastDay = null, lastAuthor = null, lastTime = 0;

// --- sesión ---

function loadSession() {
  try { return JSON.parse(sessionStorage.getItem(SESSION_KEY)); } catch { return null; }
}
function saveSession() {
  try { sessionStorage.setItem(SESSION_KEY, JSON.stringify(session)); } catch { /* modo privado */ }
}
function clearSession() {
  session = null; myNick = null;
  try { sessionStorage.removeItem(SESSION_KEY); } catch { /* modo privado */ }
}

// --- conexión ---

function connect() {
  clearTimeout(retryTimer);
  clearInterval(countdownTimer);

  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  ws = new WebSocket(`${proto}//${location.host}/ws`);
  setStatus("connecting", attempt ? "reconectando…" : "conectando…");

  ws.addEventListener("open", () => {
    setStatus("open", "conectado");
    // Si la conexión aguanta 5s la damos por buena y reseteamos el
    // backoff; así un servidor que acepta y cierra al toque no nos deja
    // reintentando cada 500ms para siempre.
    stableTimer = setTimeout(() => { attempt = 0; }, 5000);
    if (session) {
      send("set_nick", { nick: session.nick, session: session.token, channel: session.channel });
    }
    updateComposer();
  });

  ws.addEventListener("message", onMessage);

  ws.addEventListener("close", (ev) => {
    ws = null;
    authed = false;
    clearTimeout(stableTimer);
    updateComposer();

    // 1008: el servidor le entregó el nick a otra conexión con nuestro
    // mismo token. Reintentar solo provocaría una pelea infinita.
    if (ev.code === 1008) {
      clearSession();
      showJoin("abriste esta sesión en otra pestaña o ventana");
      setStatus("closed", "desconectado");
      return;
    }
    scheduleRetry();
  });
}

function scheduleRetry() {
  // Backoff exponencial 0.5s → 30s, con jitter para no sincronizar a
  // todos los clientes si el servidor se cae y vuelve.
  const base = Math.min(30_000, 500 * 2 ** attempt);
  const delay = Math.round(base * (0.8 + Math.random() * 0.4));
  attempt++;

  let left = Math.ceil(delay / 1000);
  const tick = () => {
    setStatus("closed", left <= 1 ? "reconectando…" : `reconectando en ${left}s`);
    left--;
  };
  tick();
  countdownTimer = setInterval(tick, 1000);
  retryTimer = setTimeout(connect, delay);
}

function retryNow() {
  if (ws) return;
  attempt = 0;
  connect();
}

// El navegador sabe antes que nosotros que volvió la red.
window.addEventListener("online", retryNow);
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible") retryNow();
});
retryBtn.addEventListener("click", retryNow);

const send = (type, data) => {
  if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type, data }));
};

function setStatus(state, text) {
  statusEl.dataset.state = state;
  statusEl.textContent = text;
  retryBtn.hidden = state !== "closed";
}

// --- protocolo ---

function onMessage(ev) {
  let env;
  try { env = JSON.parse(ev.data); } catch { return; }
  const d = env.data ?? {};

  switch (env.type) {
    case "nick_ok":
      myNick = d.nick;
      authed = true;
      attempt = 0;
      channels = d.channels;
      session = { nick: d.nick, token: d.session, channel: d.channel };
      saveSession();
      joinView.hidden = true;
      chatView.hidden = false;
      setChannel(d.channel);
      renderOnline(d.online);
      updateComposer();
      msgInput.focus();
      break;

    case "message":
      if (d.channel === currentChannel) addMessage(d);
      break;

    case "history":
      if (d.channel !== currentChannel) break;
      clearLog();
      d.messages.forEach(addMessage);
      system(d.messages.length
        ? `estás en # ${d.channel} — últimos ${d.messages.length} mensajes`
        : `estás en # ${d.channel} — sin mensajes aún`);
      scrollToBottom();
      break;

    case "channel_list":
      channels = d.channels;
      renderChannels();
      break;

    case "channel_joined":
      setChannel(d.name);
      if (session) { session.channel = d.name; saveSession(); }
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
      if (authed) {
        system(`error: ${d.message}`);
      } else if (session) {
        // Falló el reingreso automático (alguien tomó el nick mientras
        // no estábamos, o el servidor perdió las sesiones).
        clearSession();
        showJoin("no pude recuperar tu nick, elige otro");
      } else {
        joinError.textContent = d.message;
      }
      break;
  }
}

// --- formularios ---

joinForm.addEventListener("submit", (ev) => {
  ev.preventDefault();
  joinError.textContent = "";
  const nick = nickInput.value.trim();
  if (!nick) return;
  if (!ws) { joinError.textContent = "sin conexión con el servidor"; return; }
  send("set_nick", { nick });
});

msgForm.addEventListener("submit", (ev) => {
  ev.preventDefault();
  const content = msgInput.value.trim();
  if (!content || !authed) return;
  send("send_message", { content });
  msgInput.value = "";
});

chanForm.addEventListener("submit", (ev) => {
  ev.preventDefault();
  const name = chanInput.value.trim();
  if (!name || !authed) return;
  send("create_channel", { name });
  chanInput.value = "";
});

// --- vistas ---

function showJoin(error) {
  authed = false;
  chatView.hidden = true;
  joinView.hidden = false;
  joinError.textContent = error ?? "";
  nickInput.focus();
}

function setChannel(name) {
  currentChannel = name;
  channelTitle.textContent = `# ${name}`;
  clearLog(); // el history llega enseguida y pinta el contenido
  renderChannels();
  updateComposer();
}

function updateComposer() {
  const live = authed && ws && ws.readyState === WebSocket.OPEN;
  msgInput.disabled = !live;
  msgBtn.disabled = !live;
  chanInput.disabled = !live;
  msgInput.placeholder = live ? `Mensaje a # ${currentChannel}` : "sin conexión…";
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

function renderOnline(nicks) {
  onlineList.replaceChildren(...nicks.map((n) => {
    const li = document.createElement("li");
    li.textContent = n + (n === myNick ? " (tú)" : "");
    return li;
  }));
}

// --- log ---

function clearLog() {
  log.replaceChildren();
  lastDay = null; lastAuthor = null; lastTime = 0;
  unread = 0;
  updateJump();
}

function dayLabel(date) {
  const today = new Date();
  const yesterday = new Date(today);
  yesterday.setDate(today.getDate() - 1);
  if (date.toDateString() === today.toDateString()) return "hoy";
  if (date.toDateString() === yesterday.toDateString()) return "ayer";
  return date.toLocaleDateString([], { weekday: "long", day: "numeric", month: "long" });
}

function addMessage({ author, content, sent_at }) {
  const at = new Date(sent_at);

  if (at.toDateString() !== lastDay) {
    const sep = document.createElement("li");
    sep.className = "day";
    sep.textContent = dayLabel(at);
    appendToLog(sep);
    lastDay = at.toDateString();
    lastAuthor = null;
  }

  // Mensajes seguidos del mismo autor en 5 minutos van sin cabecera,
  // como en discord.
  const grouped = author === lastAuthor && at - lastTime < 5 * 60_000;

  const li = document.createElement("li");
  li.className = "msg" + (author === myNick ? " mine" : "") + (grouped ? " grouped" : "");

  if (!grouped) {
    const meta = document.createElement("div");
    meta.className = "meta";
    const time = at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    meta.textContent = `${author} · ${time}`;
    li.appendChild(meta);
  }

  const body = document.createElement("div");
  body.className = "body";
  body.textContent = content; // textContent: sin riesgo de inyectar HTML
  body.title = at.toLocaleString();
  li.appendChild(body);

  lastAuthor = author;
  lastTime = at;
  appendToLog(li, true);
}

function system(text) {
  const li = document.createElement("li");
  li.className = "system";
  li.textContent = text;
  lastAuthor = null; // corta el agrupado
  appendToLog(li);
}

function appendToLog(li, counts = false) {
  const stick = atBottom();
  log.appendChild(li);
  if (stick) {
    scrollToBottom();
  } else if (counts) {
    unread++;
    updateJump();
  }
}

const atBottom = () => log.scrollHeight - log.scrollTop - log.clientHeight < 40;

function scrollToBottom() {
  log.scrollTop = log.scrollHeight;
  unread = 0;
  updateJump();
}

function updateJump() {
  jumpBtn.hidden = unread === 0;
  jumpBtn.textContent = unread === 1 ? "1 mensaje nuevo ↓" : `${unread} mensajes nuevos ↓`;
}

log.addEventListener("scroll", () => {
  if (atBottom() && unread) { unread = 0; updateJump(); }
});
jumpBtn.addEventListener("click", scrollToBottom);

// --- arranque ---

if (session) {
  // Recargaste la página: mostramos el chat de una y el set_nick
  // automático lo rellena en cuanto abra el socket.
  myNick = session.nick;
  joinView.hidden = true;
  chatView.hidden = false;
  setChannel(session.channel ?? "general");
} else {
  nickInput.focus();
}
connect();
