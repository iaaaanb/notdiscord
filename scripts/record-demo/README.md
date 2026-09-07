# record-demo

Regenera `docs/media/demo.gif`, el GIF del README. No hay que grabar la
pantalla a mano: el script levanta el servidor con una base vacía, maneja dos
Chromium con Playwright y pega los dos videos lado a lado con ffmpeg.

```bash
./scripts/record-demo/record.sh
```

Requiere `go`, `node` y `ffmpeg` en el PATH. La primera corrida instala
Playwright y su Chromium (~150 MB).

Variables: `WIDTH` (ancho del GIF, default 900), `FPS` (10), `COLORS` (64),
`PORT` (8099).

## Por qué dos navegadores

Un chat no se puede demostrar con una sola ventana. Los dos paneles del GIF son
dos clientes de verdad: el mensaje que aparece en el panel derecho viajó por el
WebSocket, el canal nuevo apareció por el broadcast del servidor, y el audio de
la parte de voz va peer-to-peer entre los dos navegadores.

Chromium corre con `--use-fake-device-for-media-stream`, que sintetiza un
micrófono con un tono intermitente. Eso es lo que hace parpadear el indicador
de "hablando", que el cliente calcula con Web Audio sobre el stream que
recibe — así que si el indicador se prende en el participante remoto, es porque
el audio efectivamente llegó por la conexión p2p. `record.mjs` lo espera de
forma explícita y falla la corrida si no pasa, para que el GIF no pueda mostrar
una voz que en realidad no conectó.

## El guión

`record.mjs` sigue este recorrido, que es el resumen de la app: los dos entran
y se ven en la lista de online → un mensaje cruza al instante → Ana crea un
canal y a Bruno le aparece solo en la barra → los dos entran a voz y se ven
hablar → Ana se silencia.
