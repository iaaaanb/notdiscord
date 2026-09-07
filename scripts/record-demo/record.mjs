// Graba la demo de notdiscord manejando dos Chromium con Playwright.
//
// Un chat se demuestra con dos personas, así que esto abre dos navegadores
// independientes (Ana y Bruno) y graba un video de cada uno; record.sh los
// pega lado a lado. Lo que se ve pasando de un panel al otro está pasando
// de verdad: son dos clientes hablando por WebSocket con el servidor, y la
// voz va peer-to-peer entre ellos.
//
// Salida: out/ana.webm, out/bruno.webm
import { chromium } from "playwright";
import fs from "node:fs";
import path from "node:path";

const BASE = process.env.DEMO_URL || "http://localhost:8099";
const OUT = path.resolve("out");
const W = Number(process.env.PANE_W || 600);
const H = Number(process.env.PANE_H || 620);

fs.rmSync(OUT, { recursive: true, force: true });
fs.mkdirSync(OUT, { recursive: true });

// Cursor sintético: Playwright no dibuja el puntero en el video, así que lo
// pintamos nosotros escuchando los mismos eventos que dispara page.mouse.
const CURSOR_SCRIPT = () => {
  const install = () => {
    if (document.getElementById("__demo_cursor")) return;
    const c = document.createElement("div");
    c.id = "__demo_cursor";
    c.style.cssText = `
      position: fixed; left: -100px; top: -100px; z-index: 2147483647;
      width: 18px; height: 18px; margin: -9px 0 0 -9px; border-radius: 50%;
      background: rgba(255,255,255,0.95);
      box-shadow: 0 0 0 2px rgba(0,0,0,0.5), 0 3px 10px rgba(0,0,0,0.6);
      pointer-events: none; transition: transform 90ms ease;`;
    document.documentElement.appendChild(c);

    document.addEventListener("mousemove", (e) => {
      c.style.left = e.clientX + "px";
      c.style.top = e.clientY + "px";
    }, true);

    document.addEventListener("mousedown", (e) => {
      c.style.transform = "scale(0.65)";
      const r = document.createElement("div");
      r.style.cssText = `
        position: fixed; left: ${e.clientX}px; top: ${e.clientY}px;
        z-index: 2147483646; width: 18px; height: 18px; margin: -9px 0 0 -9px;
        border-radius: 50%; border: 2px solid rgba(79,138,109,0.95);
        pointer-events: none; transition: all 420ms ease-out;`;
      document.documentElement.appendChild(r);
      requestAnimationFrame(() => {
        r.style.width = "60px";
        r.style.height = "60px";
        r.style.margin = "-30px 0 0 -30px";
        r.style.opacity = "0";
      });
      setTimeout(() => r.remove(), 500);
    }, true);

    document.addEventListener("mouseup", () => {
      c.style.transform = "scale(1)";
    }, true);
  };
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", install);
  } else {
    install();
  }
};

const wait = (ms) => new Promise((r) => setTimeout(r, ms));

// actor envuelve una pestaña con los helpers de movimiento y tipeo, para que
// el guión de abajo se lea como lo que hace la persona.
function actor(name, page) {
  const moveTo = (x, y, steps = 24) => page.mouse.move(x, y, { steps });

  const clickOn = async (locator, { after = 420 } = {}) => {
    const box = await locator.boundingBox();
    if (!box) throw new Error(`${name}: elemento sin bounding box`);
    await moveTo(box.x + box.width / 2, box.y + box.height / 2);
    await wait(240);
    await page.mouse.down();
    await wait(90);
    await page.mouse.up();
    await wait(after);
  };

  const typeInto = async (locator, text, { delay = 34, after = 220 } = {}) => {
    await clickOn(locator, { after: 140 });
    await locator.pressSequentially(text, { delay });
    await wait(after);
  };

  // say escribe un mensaje y lo manda con Enter.
  const say = async (text, { after = 620 } = {}) => {
    await typeInto(page.locator("#msg"), text, { after: 160 });
    await page.keyboard.press("Enter");
    await wait(after);
  };

  const join = async (nick) => {
    await typeInto(page.locator("#nick"), nick, { after: 200 });
    // exact: "Entrar" también matchearía el botón "Entrar a voz".
    await clickOn(page.getByRole("button", { name: "Entrar", exact: true }), { after: 700 });
    await page.locator("#chat").waitFor({ state: "visible" });
  };

  return { name, page, moveTo, clickOn, typeInto, say, join };
}

async function main() {
  const browser = await chromium.launch({
    channel: "chromium", // el Chromium completo: el headless shell no trae WebRTC
    args: [
      // Sin esto no hay demo de voz: no hay micrófono real que entregar, así
      // que Chromium sintetiza uno (un tono intermitente) y acepta el permiso
      // sin preguntar. El tono es lo que hace parpadear el indicador de
      // "hablando", que se calcula sobre el audio que realmente llega.
      "--use-fake-device-for-media-stream",
      "--use-fake-ui-for-media-stream",
      // El <audio> del par remoto arranca sin gesto del usuario.
      "--autoplay-policy=no-user-gesture-required",
      // El video se graba de la superficie de la ventana, no del viewport
      // emulado. Si la ventana no mide lo mismo, Playwright encaja la captura
      // dentro del cuadro y rellena con gris, que además se come la barra de
      // escribir. Fijar la ventana al tamaño del panel es lo que lo arregla.
      `--window-size=${W},${H}`,
    ],
  });

  const open = async (file) => {
    const context = await browser.newContext({
      viewport: { width: W, height: H },
      deviceScaleFactor: 1,
      colorScheme: "dark",
      locale: "es-CL",
      timezoneId: "America/Santiago",
      permissions: ["microphone"],
      recordVideo: { dir: OUT, size: { width: W, height: H } },
      reducedMotion: "no-preference",
    });
    await context.addInitScript(CURSOR_SCRIPT);
    const page = await context.newPage();
    await page.goto(BASE, { waitUntil: "domcontentloaded" });

    const size = await page.evaluate(() => ({ w: innerWidth, h: innerHeight }));
    if (size.w !== W || size.h !== H) {
      throw new Error(`viewport quedó en ${size.w}x${size.h}, se esperaba ${W}x${H}`);
    }
    return { context, page, file };
  };

  // Los dos contextos se crean pegados para que los videos queden alineados:
  // la grabación empieza al crear el contexto.
  const left = await open("ana.webm");
  const right = await open("bruno.webm");
  const ana = actor("ana", left.page);
  const bruno = actor("bruno", right.page);

  // Un error del servidor o de la señalización aparece en el log como
  // mensaje de sistema. Si algo así sale durante la toma, la demo estaría
  // mintiendo, así que la corrida falla.
  const problems = [];
  for (const a of [ana, bruno]) {
    a.page.on("console", (m) => {
      if (m.type() === "error") problems.push(`${a.name}: consola: ${m.text()}`);
    });
  }
  const assertNoSystemErrors = async () => {
    for (const a of [ana, bruno]) {
      const bad = await a.page.locator("#log .system", {
        hasText: /error|no pude|se cayó/i,
      }).allTextContents();
      for (const t of bad) problems.push(`${a.name}: log: ${t}`);
    }
  };

  await wait(500);

  // --- 1. entran los dos: cada uno aparece en la lista del otro ---
  await ana.join("ana");
  await wait(200);
  await bruno.join("bruno");
  await wait(600);

  // --- 2. un mensaje cruza al instante ---
  await ana.say("hola! esto es notdiscord");
  await bruno.say("llegó al instante");
  await wait(450);

  // --- 3. ana crea un canal y a bruno le aparece solo en la barra ---
  await ana.typeInto(left.page.locator("#chan"), "proyecto", { after: 200 });
  await ana.clickOn(left.page.getByRole("button", { name: "Crear canal" }), { after: 800 });
  await bruno.clickOn(right.page.locator("#channels button", { hasText: "proyecto" }), {
    after: 700,
  });
  await ana.say("acá hablamos del proyecto", { after: 700 });

  // --- 4. voz: audio peer-to-peer, el servidor solo presenta a los pares ---
  await ana.clickOn(left.page.locator("#voice-btn"), { after: 400 });
  await bruno.clickOn(right.page.locator("#voice-btn"), { after: 400 });

  for (const a of [ana, bruno]) {
    await a.page.locator("#voice-members li").nth(1).waitFor({ timeout: 15000 });
  }

  // La prueba de que el audio llegó de verdad: el indicador de "hablando" se
  // calcula con Web Audio sobre el stream recibido, así que si se prende en el
  // participante remoto es porque su audio está entrando por la conexión p2p.
  //
  // El micrófono falso de Chromium da un tono intermitente, así que el
  // indicador parpadea: hay que muestrear una ventana en vez de esperar un
  // instante puntual. Se muestrean los dos paneles a la vez, durante la pausa
  // que la toma necesita igual.
  // Muestrear desde acá se pierde los destellos cortos, así que el que mira es
  // un MutationObserver dentro de la página: la lista de voz se vuelve a pintar
  // entera en cada cambio, y él anota a todo el que haya aparecido hablando.
  const watchSpeakers = (page) =>
    page.evaluate(() => {
      window.__spoke = new Set();
      const scan = () => {
        for (const li of document.querySelectorAll("#voice-members li.speaking")) {
          window.__spoke.add(li.textContent);
        }
      };
      scan();
      new MutationObserver(scan).observe(document.getElementById("voice-members"), {
        childList: true,
        subtree: true,
        attributes: true,
      });
    });
  await Promise.all([watchSpeakers(ana.page), watchSpeakers(bruno.page)]);

  // La pausa que la toma necesita igual es la ventana de observación.
  await wait(4500);

  const seen = await Promise.all(
    [ana, bruno].map((a) => a.page.evaluate(() => [...window.__spoke])),
  );
  for (const [i, other] of [[0, "bruno"], [1, "ana"]]) {
    const remote = seen[i].filter((t) => !t.includes("(tú)"));
    if (!remote.some((t) => t.includes(other))) {
      problems.push(
        `${[ana, bruno][i].name}: nunca se vio hablar a ${other} ` +
          `(¿no llegó el audio p2p?); visto: [${seen[i].join(", ")}]`,
      );
    }
  }

  // --- 5. silenciar ---
  await ana.clickOn(left.page.locator("#mute-btn"), { after: 900 });
  await ana.moveTo(W - 40, H - 40);
  await wait(700);

  await assertNoSystemErrors();

  const videos = [left, right].map((s) => ({ video: s.page.video(), file: s.file }));
  await Promise.all([left.context.close(), right.context.close()]);
  for (const { video, file } of videos) {
    fs.renameSync(await video.path(), path.join(OUT, file));
  }
  await browser.close();

  if (problems.length) {
    console.error("la demo no quedó limpia:\n  " + problems.join("\n  "));
    process.exit(1);
  }
  console.log("listo:", videos.map((v) => path.join(OUT, v.file)).join(" "));
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
