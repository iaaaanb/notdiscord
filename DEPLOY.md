# Desplegar notdiscord

De un droplet vacío a una URL que tus amigos pueden abrir. Toma unos 20 minutos.

El store usa `modernc.org/sqlite` (SQLite en Go puro), así que
`CGO_ENABLED=0 go build` produce un binario estático de un solo archivo: en el
droplet no hace falta `gcc` ni librerías del sistema, y compilas siempre desde
tu máquina. De eso se encarga `deploy/deploy.sh` en el paso 6.

## 1. El droplet

En DigitalOcean: **Create → Droplet**.

- Imagen: Ubuntu LTS
- Plan: Basic / Regular, el más barato (512 MB o 1 GB)
- Autenticación: **SSH key**, no contraseña
- Región: da lo mismo por ahora — por el servidor solo pasa texto y
  señalización, no audio. Si algún día haces el SFU esto pasa a ser lo
  más importante y probablemente tengas que mudarte, porque
  DigitalOcean no tiene datacenter en Sudamérica.

Anota la IPv4 que te asigna.

## 2. El subdominio

Esto se hace donde administres el DNS de tu dominio (tu registrador, o
Cloudflare, o donde tengas los nameservers). Agregas **un registro A**:

| Tipo | Nombre       | Valor              | TTL |
|------|--------------|--------------------|-----|
| A    | `notdiscord` | la IPv4 del droplet| 300 |

El campo "Nombre" es solo la parte de adelante: escribes `notdiscord`,
no `notdiscord.tudominio.cl`. Casi todos los paneles completan el resto
solos (algunos usan `@` para la raíz del dominio).

Pon el TTL bajo (300 segundos) mientras pruebas: si te equivocas de IP,
el error se corrige en 5 minutos en vez de en un día.

**Si tu DNS está en Cloudflare**, esto es importante: deja la nubecita
en **gris (DNS only)**, no naranja. Con la nube naranja el tráfico pasa
por el proxy de Cloudflare, que termina el TLS por su cuenta e impide
que Caddy consiga su propio certificado. Y si más adelante instalas
coturn, el proxy tampoco te va a dejar pasar UDP.

Verifica que propagó antes de seguir:

```bash
dig +short notdiscord.tudominio.cl
```

Tiene que responder la IP de tu droplet. Si no responde nada, espera y
reintenta — Caddy no puede sacar el certificado hasta que esto resuelva.

## 3. Preparar el servidor

```bash
ssh root@LA_IP

apt update && apt upgrade -y

ufw allow OpenSSH
ufw allow 80
ufw allow 443
ufw enable
```

El puerto 8080 **no** se abre: el servidor escucha solo en `127.0.0.1`
y todo entra por Caddy.

## 4. Caddy

```bash
apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
  | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
  | tee /etc/apt/sources.list.d/caddy-stable.list
apt update && apt install -y caddy
```

Copia `deploy/Caddyfile` a `/etc/caddy/Caddyfile`, cámbiale el dominio
por el tuyo, y recarga:

```bash
systemctl reload caddy
```

Caddy pide el certificado a Let's Encrypt en ese momento. Si el DNS
está bien, en unos segundos ya tienes HTTPS. Para mirar cómo le fue:

```bash
journalctl -u caddy -n 50 --no-pager
```

## 5. El servicio

Copia `deploy/notdiscord.service` a
`/etc/systemd/system/notdiscord.service` y:

```bash
systemctl daemon-reload
systemctl enable notdiscord
```

Todavía no arranca porque falta el binario — eso lo pone el deploy.

## 6. Desplegar

Desde tu máquina, en la raíz del proyecto:

```bash
./deploy/deploy.sh root@LA_IP
```

Compila, copia, reemplaza el binario y reinicia el servicio. Para cada
cambio que hagas de ahora en adelante, este comando es todo el ciclo.

## 7. Probar

Abre `https://notdiscord.tudominio.cl`. Deberías ver la pantalla de
nick, con el candado en la barra de direcciones.

Prueba en este orden, porque cada paso descarta cosas distintas:

1. **Chat de texto** entre dos pestañas → el WebSocket sobre TLS anda.
2. **Voz entre dos pestañas tuyas** (con audífonos) → WebRTC anda en
   una sola red.
3. **Voz con un amigo en otra casa** → NAT traversal real. Este es el
   único que puede fallar.

## Cuando algo falla

**No aparece el candado / Caddy no consigue certificado.**
El DNS no resolvía cuando Caddy lo intentó. Confirma con `dig`, después
`systemctl reload caddy` y mira `journalctl -u caddy`.

**El chat no conecta, la consola dice error de WebSocket.**
`coder/websocket` rechaza conexiones cuyo header `Origin` no coincide
con el `Host`. Detrás de Caddy normalmente calzan. Si no, en
`ServeWS` cambia `websocket.Accept(w, r, nil)` por:

```go
websocket.Accept(w, r, &websocket.AcceptOptions{
    OriginPatterns: []string{"notdiscord.tudominio.cl"},
})
```

**El navegador no pide permiso de micrófono.**
Estás entrando por HTTP o por IP. `getUserMedia` solo funciona en
contexto seguro: HTTPS o `localhost`. Revisa que la URL diga `https://`.

**El texto anda pero la voz no, entre casas distintas.**
Esto es NAT simétrico y tu cliente ya te lo dice en el log. Es el
límite de STUN, no un bug tuyo. Dos salidas: instalar coturn en este
mismo droplet, o saltar al SFU con Pion, que lo elimina de raíz porque
todos se conectan a la IP pública del servidor en vez de entre sí.

**Se reinicia solo en loop.**
`journalctl -u notdiscord -n 50 --no-pager`. Lo más común es que no
pueda escribir la base: revisa que la ruta del `-db` esté dentro de
`/var/lib/notdiscord`, que es el único lugar donde `ProtectSystem=strict`
le deja escribir.

## Un aviso antes de compartir el link

Apenas esté público, cualquiera que tenga la URL entra y puede tomar
cualquier nick. Para un grupo de amigos alcanza con no publicarla, pero
si la vas a dejar arriba un tiempo, ponle al menos una contraseña
compartida.
