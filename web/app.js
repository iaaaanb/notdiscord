// Cliente M5: chat + canales de voz por WebRTC en malla.
//
// El audio NO pasa por el servidor: cada par abre un RTCPeerConnection
// directo con cada otro par. El servidor solo rutea los mensajes
// "signal" para que los navegadores se encuentren.

const $ = (id) => document.getElementById(id);
const joinView = $("join"), chatView = $("chat");
const joinForm = $("join-form"), nickInput = $("nick"), joinError = $("join-error");
const msgForm = $("msg-form"), msgInput = $("msg"), msgBtn = $("msg-send");
const chanForm = $("chan-form"), chanInput = $("chan");
const log = $("log"), onlineList = $("online"), channelList = $("channels");
const statusEl = $("status"), retryBtn = $("retry"), jumpBtn = $("jump");
const channelTitle = $("channel-title");
const voiceTitle = $("voice-title"), voiceMembers = $("voice-members");
const voiceElsewhere = $("voice-elsewhere");
const voiceBtn = $("voice-btn"), muteBtn = $("mute-btn");

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

// --- estado de voz ---
// Servidores STUN: le dicen a tu navegador cuál es su IP pública para
// que el otro par sepa dónde encontrarlo. En la misma LAN no hacen
// falta; detrás de un NAT estricto (típico de una red universitaria)
// puede que ni con esto alcance y necesites un TURN.
const rtcConfig = { iceServers: [{ urls: "stun:stun.l.google.com:19302" }] };

let localStream = null;      // tu micrófono
let wantVoice = false;       // querías estar en voz (sobrevive reconexiones)
let voiceChannel = null;     // canal de voz confirmado por el servidor
let muted = false;
let voiceState = {};         // canal → [nicks en voz]
const peers = new Map();     // nick → { pc, pending, audio, stopMeter }
const speaking = new Map();  // nick → bool

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
    // Los RTCPeerConnection sobreviven a la caída del WebSocket, pero
    // el servidor ya nos dio de baja de la voz. Los cerramos para
    // rearmarlos limpios al reconectar.
    closeAllPeers();
    voiceChannel = null;
    updateComposer();
    renderVoice();

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
      voiceState = d.voice ?? {};
      joinView.hidden = true;
      chatView.hidden = false;
      setChannel(d.channel);
      renderOnline(d.online);
      updateComposer();
      // Si estabas en voz y se cayó el socket, vuelves a entrar: los
      // peers se rearman desde cero.
      if (wantVoice) send("voice_join");
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

    case "voice_state":
      if (d.members.length) voiceState[d.channel] = d.members;
      else delete voiceState[d.channel];

      if (d.members.includes(myNick)) {
        voiceChannel = d.channel;
        syncPeers(d.members);
      } else if (voiceChannel === d.channel) {
        voiceChannel = null;   // te sacaron (o te cambiaste de canal)
        closeAllPeers();
      }
      renderVoice();
      renderChannels();
      break;

    case "signal":
      onSignal(d);
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
  renderVoice();
  updateComposer();
}

function updateComposer() {
  const live = authed && ws && ws.readyState === WebSocket.OPEN;
  msgInput.disabled = !live;
  msgBtn.disabled = !live;
  chanInput.disabled = !live;
  voiceBtn.disabled = !live;
  msgInput.placeholder = live ? `Mensaje a # ${currentChannel}` : "sin conexión…";
}

function renderChannels() {
  channelList.replaceChildren(...channels.map((name) => {
    const li = document.createElement("li");
    const btn = document.createElement("button");
    btn.type = "button";
    btn.textContent = `# ${name}`;
    btn.className = name === currentChannel ? "chan current" : "chan";
    const inVoice = voiceState[name]?.length ?? 0;
    if (inVoice) {
      const badge = document.createElement("span");
      badge.className = "badge";
      badge.textContent = inVoice;
      badge.title = `${inVoice} en voz`;
      btn.appendChild(badge);
    }
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

// --- voz (webrtc) ---

voiceBtn.addEventListener("click", () => {
  if (voiceChannel && voiceChannel === currentChannel) leaveVoice();
  else joinVoice();
});
muteBtn.addEventListener("click", toggleMute);

async function joinVoice() {
  if (!authed) return;
  if (!localStream) {
    try {
      localStream = await navigator.mediaDevices.getUserMedia({
        audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
        video: false,
      });
    } catch (err) {
      system(`no pude abrir el micrófono: ${err.name}`);
      return;
    }
    watchLevel(myNick, localStream);
  }
  wantVoice = true;
  send("voice_join");
  renderVoice();
}

function leaveVoice() {
  wantVoice = false;
  voiceChannel = null;
  send("voice_leave");
  closeAllPeers();
  if (localStream) {
    localStream.getTracks().forEach((t) => t.stop());
    localStream = null;
  }
  stopMeter(myNick);
  muted = false;
  renderVoice();
}

function toggleMute() {
  const track = localStream?.getAudioTracks()[0];
  if (!track) return;
  track.enabled = !track.enabled;
  muted = !track.enabled;
  if (muted) speaking.set(myNick, false);
  renderVoice();
}

const sendSignal = (to, payload) => send("signal", { to, payload });

// syncPeers deja abierta exactamente una conexión por participante.
//
// Regla para no chocar: de cada par, ofrece el nick menor. Si ambos
// ofrecieran a la vez las dos ofertas se pisarían (eso se llama glare)
// y habría que negociar quién cede. Comparar strings evita el problema
// entero y no necesita estado.
function syncPeers(members) {
  if (!localStream) return; // sin micrófono no hay nada que negociar
  const others = members.filter((n) => n !== myNick);

  for (const nick of others) {
    if (peers.has(nick)) continue;
    const peer = createPeer(nick);
    if (myNick < nick) makeOffer(nick, peer);
  }
  for (const nick of [...peers.keys()]) {
    if (!others.includes(nick)) closePeer(nick);
  }
}

function createPeer(nick) {
  const pc = new RTCPeerConnection(rtcConfig);
  const peer = { pc, pending: [], audio: null };
  peers.set(nick, peer);

  for (const track of localStream.getTracks()) pc.addTrack(track, localStream);

  pc.addEventListener("icecandidate", (ev) => {
    if (ev.candidate) sendSignal(nick, { candidate: ev.candidate });
  });

  pc.addEventListener("track", (ev) => {
    const stream = ev.streams[0];
    peer.audio = new Audio();      // hay que guardarlo: si lo recolecta el GC, se corta
    peer.audio.srcObject = stream;
    peer.audio.autoplay = true;
    peer.audio.play().catch(() => system(`no pude reproducir a ${nick}`));
    watchLevel(nick, stream);
  });

  pc.addEventListener("connectionstatechange", () => {
    if (pc.connectionState === "failed") {
      system(`se cayó la conexión de voz con ${nick} (¿NAT estricto? haría falta un TURN)`);
    }
  });

  return peer;
}

async function makeOffer(nick, peer) {
  try {
    await peer.pc.setLocalDescription(await peer.pc.createOffer());
    sendSignal(nick, { desc: peer.pc.localDescription });
  } catch (err) {
    system(`error negociando con ${nick}: ${err.message}`);
  }
}

async function onSignal({ from, payload }) {
  if (!localStream || !voiceChannel) return;
  const peer = peers.get(from) ?? createPeer(from);
  const pc = peer.pc;

  try {
    if (payload.desc) {
      await pc.setRemoteDescription(payload.desc);
      // Los candidatos que llegaron antes de la descripción remota no
      // se podían agregar todavía; ahora sí.
      for (const cand of peer.pending.splice(0)) {
        await pc.addIceCandidate(cand).catch(() => {});
      }
      if (payload.desc.type === "offer") {
        await pc.setLocalDescription(await pc.createAnswer());
        sendSignal(from, { desc: pc.localDescription });
      }
    } else if (payload.candidate) {
      if (pc.remoteDescription) await pc.addIceCandidate(payload.candidate);
      else peer.pending.push(payload.candidate);
    }
  } catch (err) {
    system(`error de señalización con ${from}: ${err.message}`);
  }
}

function closePeer(nick) {
  const peer = peers.get(nick);
  if (!peer) return;
  peer.pc.close();
  if (peer.audio) peer.audio.srcObject = null;
  peers.delete(nick);
  stopMeter(nick);
}

function closeAllPeers() {
  for (const nick of [...peers.keys()]) closePeer(nick);
}

// --- detector de voz (el puntito que se enciende) ---

let audioCtx = null;
const meters = new Map(); // nick → función para desmontar el análisis

// watchLevel mide el volumen del stream y prende el indicador cuando
// pasa un umbral. Es puro Web Audio: nada de esto viaja por la red.
function watchLevel(nick, stream) {
  stopMeter(nick);
  audioCtx ??= new AudioContext();
  const source = audioCtx.createMediaStreamSource(stream);
  const analyser = audioCtx.createAnalyser();
  analyser.fftSize = 512;
  source.connect(analyser);

  const samples = new Uint8Array(analyser.fftSize);
  let frame = 0;

  const tick = () => {
    analyser.getByteTimeDomainData(samples);
    let sum = 0;
    for (const v of samples) {
      const x = (v - 128) / 128; // centrado en 0
      sum += x * x;
    }
    const rms = Math.sqrt(sum / samples.length);
    const isSpeaking = rms > 0.02 && !(nick === myNick && muted);
    if (speaking.get(nick) !== isSpeaking) {
      speaking.set(nick, isSpeaking);
      renderVoice();
    }
    frame = requestAnimationFrame(tick);
  };
  tick();

  meters.set(nick, () => {
    cancelAnimationFrame(frame);
    source.disconnect();
    speaking.delete(nick);
  });
}

function stopMeter(nick) {
  meters.get(nick)?.();
  meters.delete(nick);
}

function renderVoice() {
  const shown = voiceChannel ?? currentChannel;
  const members = voiceState[shown] ?? [];

  voiceTitle.textContent = shown ? `Voz — # ${shown}` : "Voz";
  voiceMembers.replaceChildren(...members.map((nick) => {
    const li = document.createElement("li");
    li.className = "voice-member"
      + (speaking.get(nick) ? " speaking" : "")
      + (nick === myNick && muted ? " muted" : "");
    li.textContent = nick + (nick === myNick ? " (tú)" : "");
    return li;
  }));

  const elsewhere = voiceChannel && voiceChannel !== currentChannel;
  voiceElsewhere.hidden = !elsewhere;
  if (elsewhere) voiceElsewhere.textContent = `sigues en voz en # ${voiceChannel}`;

  const here = voiceChannel === currentChannel;
  voiceBtn.textContent = here ? "Salir de voz" : elsewhere ? "Mover voz aquí" : "Entrar a voz";
  voiceBtn.className = here ? "leave" : "";
  voiceBtn.disabled = !authed;
  muteBtn.hidden = !voiceChannel;
  muteBtn.textContent = muted ? "Activar micrófono" : "Silenciar";
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
