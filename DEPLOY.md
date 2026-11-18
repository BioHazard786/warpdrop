# Deploying WarpDrop

One container does everything: the web app, signaling, the encrypted relay and `/api/config`. You bring a reverse proxy (Caddy or Nginx) for HTTPS.

## The stack

| Service | Bound to | What it is |
|---|---|---|
| `warpdrop` | `127.0.0.1:8080` | The app: web UI, `/ws`, `/relay/`, `/api/config`, `/healthz` |
| `installer` | `127.0.0.1:8000` | The `curl \| bash` endpoint for the CLI |
| `coturn` | host network | **Optional** TURN server, only with `--profile turn` |

Nothing binds a public port: a same-host proxy is the only way in.

## Prerequisites

1. A server with Docker and Docker Compose.
2. A domain pointing at it (browsers need HTTPS for WebRTC and the download service worker).
3. Caddy or Nginx on the host, serving 443.

## 1. Run it

```bash
git clone https://github.com/BioHazard786/warpdrop.git && cd warpdrop
cp .env.example .env          # optional: every setting has a working default
docker compose up -d
curl -s http://127.0.0.1:8080/healthz        # ok
curl -s http://127.0.0.1:8080/api/config     # {"v":2,"iceServers":[...],"relay":true,...}
```

To pin a release instead of `latest`, set `WARPDROP_VERSION=2.0.0` in `.env`.

## 2. Hook up your proxy

### Caddy

[`deploy/Caddyfile.sample`](deploy/Caddyfile.sample): replace `YOUR_DOMAIN`, reload Caddy. It gets certificates itself and handles the WebSocket upgrades on `/ws` and `/relay/` without extra settings.

### Nginx

[`deploy/nginx.conf.sample`](deploy/nginx.conf.sample): replace `YOUR_DOMAIN`, add your certificate paths (certbot works), then `nginx -t && systemctl reload nginx`. Two parts matter and must stay:

- `/ws` and `/relay/` need the `Upgrade`/`Connection` headers, or signaling and the relay silently break.
- `proxy_buffering off` on those locations, so relayed file data streams instead of piling up in Nginx.

### Anything else

Proxy everything on `YOUR_DOMAIN` to `127.0.0.1:8080` with WebSocket upgrade support, set `Host` and `X-Forwarded-For`, and give relay connections a long read timeout (an hour is fine).

Both samples were validated with `caddy validate` / `nginx -t`, and a transfer through the Nginx sample over TLS was byte-verified.

## 3. Settings

All optional. Set them in `.env` (compose passes them to the container).

| Variable | Default | What it does |
|---|---|---|
| `RELAY` | `on` | The encrypted WebSocket relay used when direct WebRTC fails. `off` disables it. |
| `RELAY_MAX_MBPS` | `0` | Per-connection relay speed cap in megabits per second. `0` = unlimited. |
| `RELAY_MAX_CONNS` | `200` | Simultaneous relayed transfers (each is two sockets). |
| `ALLOWED_ORIGINS` | empty | Extra browser origins allowed to open `/ws`, comma separated. The site's own host is always allowed. The CLI sends no origin and is always allowed. |
| `TRUSTED_PROXIES` | `127.0.0.1,172.16.0.0/12` | Peers whose `X-Forwarded-For` is trusted, so rate limits see the real client IP. The default covers a host proxy reaching the container through Docker's bridge. **Set this to your proxy's address if it isn't on this host.** |
| `RATE_LIMIT_SCALE` | `1` | Multiplies the per-IP limits (below). Raise it when many people share one address. |
| `STUN_URLS` | Google STUN | STUN servers, comma separated. |
| `TURN_URLS`, `TURN_SECRET` | empty | TURN is offered to clients only when **both** are set. |

### Rate limits

Per client IP: **10 new rooms per minute (burst 5)**, 30 joins per minute (burst 10), 30 relay requests per minute (burst 10), and 20 simultaneous connections. They exist so room numbers can't be guessed or the server flooded. If an office, school or carrier-grade NAT puts hundreds of users behind one address, they share these limits: set `RATE_LIMIT_SCALE=5` (or more).

Behind a proxy, rate limiting is only per real user if `TRUSTED_PROXIES` is correct. If it is wrong, everyone appears to come from the proxy's address and shares one limit.

## 4. TURN (optional)

You usually don't need it: when a direct connection fails, WarpDrop automatically relays through the app itself, with the same end-to-end encryption. Run TURN only if you want to keep transfers peer-to-peer across strict NATs, or to take relay bandwidth off the app server.

1. Pick a secret: `openssl rand -hex 32`.
2. In `.env`:
   ```
   TURN_SECRET=<the secret>
   TURN_REALM=turn.example.com
   TURN_EXTERNAL_IP=203.0.113.10                    # the server's public IP
   TURN_URLS=turn:turn.example.com:3478?transport=udp,turn:turn.example.com:3478?transport=tcp
   ```
3. Open **3478 (TCP and UDP)** and **49160-49200 (UDP)** in your firewall, and point `turn.example.com` at the server.
4. `docker compose --profile turn up -d`

There are no static TURN users. The app hands every client a **time-limited credential** (24 hours) derived from `TURN_SECRET`, so nothing secret ships in the web bundle.

The shipped `deploy/turnserver.conf` is hardened and was tested against a real coturn: the relay **refuses to connect to private, loopback, link-local (including cloud metadata `169.254.169.254`) and CGNAT addresses**, so a TURN credential can't be used to reach your internal network. It also caps concurrent allocations (100), allocations per user (12) and per-session speed (100 Mbit/s). Wrong or expired credentials are rejected. `docker compose --profile turn up` refuses to start without `TURN_SECRET`, `TURN_REALM` and `TURN_EXTERNAL_IP`.

TLS (`turns:` on 5349) is off by default because it needs certificates. To enable it, mount your certificate and key, remove `no-tls`/`no-dtls` from `deploy/turnserver.conf`, and add `cert=` and `pkey=` lines.

## 5. Updating

```bash
docker compose pull && docker compose up -d
```

v2 is not compatible with v1: update the server and the CLI together.

## Troubleshooting

- **Transfers connect but never move / "WebSocket disconnects immediately"**: the proxy isn't forwarding upgrade headers on `/ws` and `/relay/`.
- **`rate_limited` for everyone**: many users behind one IP (raise `RATE_LIMIT_SCALE`), or `TRUSTED_PROXIES` doesn't match your proxy so all users look like one.
- **"A direct connection could not be made and the relay is disabled"**: you set `RELAY=off` and the peers need relaying. Re-enable it or add TURN.
- **Works on Wi-Fi, fails on mobile data**: that's what the relay (on by default) and TURN are for. Check `/api/config` shows `"relay":true`.
- **Browser console shows `Content Security Policy` errors**: something in front is rewriting responses or stripping headers. The app sets its own CSP; don't override it.
- **Downloads of large files fail in Safari**: Safari buffers in memory; use Chrome, Firefox, Edge or the CLI.
- **`connection refused` from your proxy on a different host**: the ports bind to `127.0.0.1` only. If the proxy lives elsewhere, change the `ports:` mapping and firewall it so only the proxy can reach it, and set `TRUSTED_PROXIES` to the proxy's address.
- **Logs**: structured JSON on stdout (`docker compose logs warpdrop`). Room codes and secrets are never logged.
