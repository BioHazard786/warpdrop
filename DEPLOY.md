# Deploying WarpDrop

So you've decided to self-host. You brave, beautiful soul. Here is how you run WarpDrop on your own iron without losing your mind (hopefully).

## The Stack

The `docker-compose.yml` runs three containers:

| Service       | Bound to          | What it is                                      |
| ------------- | ----------------- | ----------------------------------------------- |
| **Frontend**  | `127.0.0.1:3000`  | Next.js UI                                      |
| **Backend**   | `127.0.0.1:8080`  | Go signaling server (WebSocket at `/ws`)        |
| **Installer** | `127.0.0.1:8000`  | The `curl \| bash` endpoint                     |

**No bundled reverse proxy.** WarpDrop assumes you already have Caddy, Nginx, or similar running on the host — fighting for port 443 with your existing setup is nobody's idea of a good time. Ports are bound to `127.0.0.1` so only a same-host proxy can reach them.

**TURN server**: not enabled by default. ~90% of users don't need it, and configuring it is a headache. See section 4 if your friends can't connect.

---

## Prerequisites

1.  **A Server**: A VPS, a Raspberry Pi, or an old laptop under your bed.
2.  **A Domain**: A real domain (e.g., `cool-file-sharing.com`) pointing to your server's IP.
3.  **Docker & Docker Compose**: If you don't have this, stop here and go learn Docker.
4.  **A reverse proxy on the host**: Caddy or Nginx, already serving traffic on 443.

---

## 1. The 30-Second Setup

This assumes you are root or have sudo powers.

```bash
# 1. Clone the repo (duh)
git clone https://github.com/BioHazard786/warpdrop.git
cd warpdrop

# 2. Setup config
cp .env.example .env

# 3. Edit the config
nano .env
# Set DOMAIN to your actual domain.
```

---

## 2. Launch the Containers 🚀

```bash
docker compose up -d
```

Now the three services are running on localhost, waiting for a proxy to talk to them.

```bash
curl http://127.0.0.1:3000   # frontend
curl http://127.0.0.1:8080/health   # backend
curl http://127.0.0.1:8000/health   # installer
```

If those respond, you're halfway there.

---

## 3. Hook Up Your Reverse Proxy

Pick your fighter.

### Option A: Caddy (the easy mode)

Caddy provisions Let's Encrypt certs for you automatically. There's a sample at [`deploy/Caddyfile.sample`](deploy/Caddyfile.sample). Drop it into your global Caddyfile (usually `/etc/caddy/Caddyfile`), replace `YOUR_DOMAIN`, and reload:

```bash
sudo caddy reload --config /etc/caddy/Caddyfile
```

The shape of it:

```caddy
YOUR_DOMAIN {
    handle /ws* { reverse_proxy 127.0.0.1:8080 }
    handle     { reverse_proxy 127.0.0.1:3000 }
}

install.YOUR_DOMAIN {
    reverse_proxy 127.0.0.1:8000
}
```

WebSocket upgrades, HTTPS, and HTTP→HTTPS redirects all "just work" — that's the whole point of Caddy.

### Option B: Nginx (the configurable mode)

Sample at [`deploy/nginx.conf.sample`](deploy/nginx.conf.sample). Copy it into `/etc/nginx/sites-available/warpdrop`, symlink to `sites-enabled`, replace `YOUR_DOMAIN`, then get certs with certbot:

```bash
sudo certbot --nginx -d YOUR_DOMAIN -d install.YOUR_DOMAIN
sudo nginx -t && sudo systemctl reload nginx
```

The Nginx sample already handles WebSocket upgrades for the `/ws` location — don't strip that part out or transfers will silently break.

### Option C: Some Other Proxy

Three things your config MUST do or nothing will work and you'll cry:

1.  Forward `/` to `127.0.0.1:3000`.
2.  Forward `/ws` to `127.0.0.1:8080` **with WebSocket upgrade headers** (`Upgrade`, `Connection`).
3.  Forward `install.YOUR_DOMAIN` to `127.0.0.1:8000`.

Also set the `Host` header and `X-Forwarded-Proto: https`.

---

## 4. The "My Friends Can't Connect" Mode (Enabling TURN)

If file transfers fail between different networks (e.g., WiFi to 4G), your NAT is being difficult. You need a TURN server.

1.  **Open `docker-compose.yml`** and uncomment the `coturn` service.
2.  **Update `.env`**: set `NEXT_PUBLIC_TURN_SERVER`, `NEXT_PUBLIC_TURN_USERNAME`, and `NEXT_PUBLIC_TURN_PASSWORD`. Uncomment the matching `NEXT_PUBLIC_TURN_*` lines inside the `frontend` block of `docker-compose.yml`.
3.  **Provision a TLS cert for `turn.YOUR_DOMAIN`** with your host proxy (point an A record at the server and add a stub vhost in Caddy/Nginx — Caddy will fetch the cert on first request). Mount the resulting cert files into `/etc/coturn/certs` inside the `coturn` container.
4.  **Open the TURN ports** on your firewall (UFW / cloud security group):
    - `3478` (TCP & UDP)
    - `5349` (TCP & UDP)
    - `60000-60128` (UDP — relay range)

Then restart:

```bash
docker compose up -d coturn
```

---

## Troubleshooting

- **"It says 404"**: Did you set `DOMAIN` correctly in `.env` AND in your proxy config?
- **"SSL Error"**: Caddy — check `journalctl -u caddy`. Nginx — check `sudo certbot certificates`. Either way, Let's Encrypt needs port 80 reachable.
- **"Transfer stuck at 0%"**: You almost certainly need the TURN server. See step 4.
- **"WebSocket disconnects immediately"**: Your proxy isn't forwarding upgrade headers on `/ws`. Re-read the sample config.
- **"Connection refused on 3000/8080/8000"**: The containers bind to `127.0.0.1` only. If your proxy lives on a *different* host, edit `docker-compose.yml` to drop the `127.0.0.1:` prefix from the `ports` mappings — then make damn sure your firewall blocks those ports from the public internet.
