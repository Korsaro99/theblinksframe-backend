# Production deploy

How The Blinks runs on the Oracle VM (x86_64, Ubuntu, user `ubuntu`) behind Cloudflare, and how
to switch over from the storefront demo. Everything here is in git except `.env`, which holds the
secrets and lives only on the VM.

| File                         | What it is                                                          |
| ---------------------------- | ------------------------------------------------------------------- |
| `compose.yaml`               | The five services, their settings, networks and the database volume |
| `cloudflared/config.yml`     | Which hostname goes to which service                                |
| `postgres-init/10-app-role.sh` | Makes the API's own database login on the first start             |
| `.env.example`               | The secrets and versions `.env` must hold                           |
| `migrate.Dockerfile`         | The migrations image (Liquibase with the changelog baked in)        |
| `../Dockerfile`              | The API image                                                       |

On the VM, `docker` needs `sudo`, so every command below has it. The production folder is
`~/the-blinks/prod`. The demo stays in `~/the-blinks/frontend` until the switch-over, then is removed.

## How it fits together

```text
visitor ──HTTPS──▶ Cloudflare edge ◀══ tunnel (outbound from the VM) ══ tunnel container
                                                                          │  cloudflared/config.yml
                         theblinksframes.com ─────────────────────────────┼──▶ web:4000  (Angular SSR)
                         api.theblinksframes.com ─────────────────────────┴──▶ api:8080  (Go API)
                                                                                 │
                         web ──server-side rendering, http://api:8080──▶ api     │
                                                                                 ▼
                                                     migrate (runs, exits) ──▶ postgres:5432
```

Docker ideas in Go terms:

- **An image** is like the output of `go build` plus the filesystem it needs (CA certificates,
  a user). It is built once on your Mac, pushed to GitHub's registry (`ghcr.io`) and pulled by
  the VM. Each image is tagged with the git commit it came from, so you always know what runs.
- **A container** is one running process of an image. `restart: unless-stopped` is a supervisor
  that starts it again if it crashes or the VM reboots.
- **`compose.yaml`** is a declarative `main()` for the whole system: what to start, with which
  environment, in which order. `depends_on` with `condition: service_healthy` waits for the
  other container's health check (like a readiness probe), and
  `service_completed_successfully` waits for `migrate` to exit with status 0, like
  `if err := migrate(); err != nil { return err }` before starting the server.
- **`${JWT_SECRET:?...}`** reads from `.env`, and refuses to start if it is missing, just like
  `config.Load` collecting errors. Each container gets only the variables listed under its
  `environment:`; Postgres never sees the JWT secret, and the web server sees no secret at all.
- **Networks** are private LANs between containers, where a service name (`api`, `postgres`) is
  a hostname. `db` is `internal`: Postgres can't reach the internet, and only `api` and
  `migrate` can reach Postgres. Nothing is published on the VM's own ports.
- **A volume** (`pgdata`) is a disk that outlives containers. Recreating Postgres keeps the data;
  `docker compose down -v` deletes it. Never use `-v` here.
- **The tunnel** works like a long-poll the other way round: `cloudflared` dials out to
  Cloudflare and keeps the connection open, and Cloudflare sends visitors' requests down it.
  So the VM needs no open ports (only SSH), and there is no certificate to manage on it:
  Cloudflare does HTTPS at the edge. `cloudflared/config.yml` is the router, like a
  `switch r.Host` in front of two handlers.

`api`, `web` and `tunnel` also run with a read-only filesystem, no Linux capabilities and no way
to gain privileges, so a bug in our code can't change the container it runs in.

### Client IPs (`TRUSTED_PROXIES`)

The login rate limit counts attempts per IP using Gin's `c.ClientIP()`. Every request reaches the
API from a container, so without help every visitor would share the tunnel container's IP, and
ten wrong passwords from anyone would lock everyone out.

- Cloudflare puts the visitor's IP in `Cf-Connecting-IP`, and also **appends** it to
  `X-Forwarded-For`. If the visitor sent their own `X-Forwarded-For: 6.6.6.6`, the API receives
  `6.6.6.6, <real IP>`: the left part is whatever the visitor typed, and only the right-most
  entry is trustworthy.
- Gin only reads `X-Forwarded-For` when the TCP peer is in `TRUSTED_PROXIES`, and then walks it
  from the right, stopping at the first address that isn't a trusted proxy. With
  `TRUSTED_PROXIES=172.28.10.10` (the tunnel's fixed address), that is exactly the real IP.
- Requests from any other container (for example `web` rendering a page) are taken at their TCP
  address and their forwarded headers are ignored, so nothing inside can fake an IP either.

So no code change is needed. Checked locally: a request from `172.28.10.10` with
`X-Forwarded-For: 6.6.6.6, 203.0.113.9` was logged as `203.0.113.9`; the same header from
another container was logged as that container's address.

(The alternative is Gin's `TrustedPlatform = gin.PlatformCloudflare`, which reads
`Cf-Connecting-IP`. Gin then believes that header from **any** peer, and it needs a code change,
so the fixed tunnel address is the safer choice.)

### Database logins

The postgres image makes `blinks` (`POSTGRES_USER`) the owner and a superuser. Liquibase migrates
as `blinks`. The API logs in as `blinks_app`, created by `postgres-init/10-app-role.sh` on the
very first start, which may only read and write rows: no `CREATE`, `ALTER`, `DROP` or `TRUNCATE`.
A SQL injection bug in the API therefore can't drop a table. (Tested: `blinks_app` can read
`staff_users`, and gets "permission denied" for `CREATE TABLE`, `TRUNCATE` and `DROP`.)

`APP_DB_PASSWORD` is used only on that first start. To change it later:
`sudo docker compose exec postgres psql -U blinks -d blinks -c "ALTER ROLE blinks_app PASSWORD '<new>'"`,
then update `.env` and run `sudo docker compose up -d api`. If the database ever exists without
`blinks_app` (restored from elsewhere), run the init script by hand:
`sudo docker compose exec postgres sh /docker-entrypoint-initdb.d/10-app-role.sh`, then
`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO blinks_app` and the
same for sequences (`USAGE, SELECT ON ALL SEQUENCES`).

## The tunnel: `blinks-prod`

Production gets its own tunnel, `blinks-prod`, instead of reusing the demo's `blinks-demo`:

- **A fresh secret.** The demo's token has sat in `~/the-blinks/frontend/.env` since the demo
  began; the shop's token is made for it alone.
- **A rehearsal.** The API is live on `api.theblinksframes.com` while the demo still serves the
  site, so you can test production before visitors see it.
- **An instant switch, and a way back.** Switching is re-pointing two DNS records, which takes
  effect within seconds; until the demo is stopped, pointing them back undoes it.
- **No trap.** Two connectors on one tunnel are load-balanced, so running the demo and
  production on the same tunnel would send visitors to either at random. Separate tunnels can't.

Afterwards the demo's tunnel and token are deleted.

## On your Mac: publish the images

The VM pulls three private images from `ghcr.io/korsaro99`. Docker is already logged in to
`ghcr.io` for the demo (frontend README, "Publish a new image").

**Backend** (API and migrations), from a clean commit in this repo. It prints the tag to use:

```bash
make publish
```

**Frontend**, the production build (not the demo), from a clean commit in `theblinksframe-frontend`.
Its Dockerfile builds production by default, with the browser's API address
`https://api.theblinksframes.com`:

```bash
c=$(git rev-parse --short HEAD) && docker build --platform linux/amd64 -t ghcr.io/korsaro99/theblinksframe-frontend:$c . && docker push ghcr.io/korsaro99/theblinksframe-frontend:$c && echo "WEB_TAG=$c"
```

Both build for the VM's x86 CPU (`linux/amd64`) while compiling natively on the Mac.

## On the VM (once)

### Files

Make the folder and copy the deploy files over (run the `scp` on your Mac, from this repo):

```bash
mkdir -p ~/the-blinks/prod
```

```bash
scp -r deploy/compose.yaml deploy/cloudflared deploy/postgres-init ubuntu@<vm-ip>:~/the-blinks/prod/
```

Copy them again whenever they change. If Docker on the VM isn't logged in to `ghcr.io` yet
(the demo needed it too), use a classic GitHub token with only `read:packages`:

```bash
sudo docker login ghcr.io -u Korsaro99
```

### Create the tunnel

Same pattern as the demo: throwaway `cloudflared` containers, with the login and the tunnel's
files kept in the Docker volume `blinks-cloudflared`, so nothing is installed on the VM. The
login from the demo (`cert.pem`) is still in that volume, so there's no need to log in again.

Create the tunnel:

```bash
sudo docker run --rm -v blinks-cloudflared:/home/nonroot cloudflare/cloudflared:2026.9.1 tunnel create blinks-prod
```

Give it the API hostname. This adds a proxied `CNAME` from `api` to
`<tunnel-id>.cfargotunnel.com` (visible under **DNS → Records**); it's a new name, so it
doesn't touch the demo:

```bash
sudo docker run --rm -v blinks-cloudflared:/home/nonroot cloudflare/cloudflared:2026.9.1 tunnel route dns blinks-prod api.theblinksframes.com
```

The site's two hostnames stay on the demo until "Switch over from the demo".

### Secrets

Write `.env` without any secret appearing on screen or in your shell history. `umask 077` makes
the file readable by you alone (the same as `chmod 600`). Put in the two tags from "Publish the
images":

```bash
cd ~/the-blinks/prod && (umask 077; { echo "BACKEND_TAG=<backend commit>"; echo "WEB_TAG=<frontend commit>"; printf 'POSTGRES_PASSWORD=%s\n' "$(openssl rand -hex 32)"; printf 'APP_DB_PASSWORD=%s\n' "$(openssl rand -hex 32)"; printf 'JWT_SECRET=%s\n' "$(openssl rand -base64 48)"; } > .env)
```

Add the tunnel's token:

```bash
cd ~/the-blinks/prod && (umask 077; printf 'TUNNEL_TOKEN=%s\n' "$(sudo docker run --rm -v blinks-cloudflared:/home/nonroot cloudflare/cloudflared:2026.9.1 tunnel token blinks-prod)" >> .env)
```

### First owner account

The API creates the first OWNER from `OWNER_BOOTSTRAP_*` when there is none. `read -s` keeps the
password off the screen. Use a long passphrase without `'` in it (the quotes stop compose from
reading a `$` in it as a variable):

```bash
cd ~/the-blinks/prod && read -r -p 'Owner name: ' n && read -r -p 'Owner phone: ' p && read -rs -p 'Owner password (12 to 72 characters): ' pw && echo && (umask 077; printf "OWNER_BOOTSTRAP_NAME=%s\nOWNER_BOOTSTRAP_PHONE=%s\nOWNER_BOOTSTRAP_PASSWORD='%s'\n" "$n" "$p" "$pw" >> .env) && unset pw
```

Check the file: these should print `9` and `-rw-------`:

```bash
cd ~/the-blinks/prod && grep -c '=.' .env && ls -l .env | cut -c1-10
```

## Start it

Start everything. The demo keeps serving the site; production is live on the API hostname only:

```bash
cd ~/the-blinks/prod && sudo docker compose up -d
```

Then check. `postgres`, `api` and `web` should be `healthy`, `migrate` `exited (0)`, and
`tunnel` `Up` (cloudflared's image has no health check):

```bash
sudo docker compose ps -a
```

```bash
sudo docker compose logs migrate api | tail -20
```

```bash
sudo docker compose logs tunnel | grep -i 'registered tunnel connection'
```

The API logs `created the first OWNER account`, and the tunnel up to four "Registered tunnel
connection" lines. Now take the bootstrap values out (all three: the API refuses to start with
only some of them), and restart the API without them:

```bash
sed -i '/^OWNER_BOOTSTRAP_/d' .env && sudo docker compose up -d api
```

The storefront renders inside the VM (should print `200`):

```bash
sudo docker compose exec web wget -qS -O /dev/null --header 'Host: theblinksframes.com' http://127.0.0.1:4000/ 2>&1 | grep 'HTTP/'
```

And from your Mac, the API answers through Cloudflare and the tunnel (should print
`{"database":"ok","status":"ok"}`):

```bash
curl -s https://api.theblinksframes.com/healthz
```

## Switch over from the demo

Point the site at the production tunnel. `--overwrite-dns` replaces the demo's record for each
name:

```bash
sudo docker run --rm -v blinks-cloudflared:/home/nonroot cloudflare/cloudflared:2026.9.1 tunnel route dns --overwrite-dns blinks-prod theblinksframes.com
```

```bash
sudo docker run --rm -v blinks-cloudflared:/home/nonroot cloudflare/cloudflared:2026.9.1 tunnel route dns --overwrite-dns blinks-prod www.theblinksframes.com
```

In the Cloudflare dashboard, **Caching → Configuration → Purge Everything**, so nobody gets the
demo's cached HTML. Then check from your Mac: the first should print `200` and **no**
`x-robots-tag` line (that header is the demo's), the second `{"database":"ok","status":"ok"}`:

```bash
curl -sI https://theblinksframes.com/ | grep -iE '^(HTTP|x-robots-tag)'
```

```bash
curl -s https://api.theblinksframes.com/healthz
```

Log in at <https://theblinksframes.com/admin> with the owner account. The login cookies are
`Secure` and set for `theblinksframes.com`, so they only work on the real HTTPS domain.

**If something is wrong**, the same two `route dns --overwrite-dns` commands with `blinks-demo`
instead of `blinks-prod` put the demo back, as long as it's still running.

**Once you're happy**, stop the demo and delete its token:

```bash
cd ~/the-blinks/frontend && sudo docker compose down && rm .env
```

Then delete its tunnel. Wait a minute after stopping it; if it still says the tunnel has
connections, run the same command with `cleanup` instead of `delete` first:

```bash
sudo docker run --rm -v blinks-cloudflared:/home/nonroot cloudflare/cloudflared:2026.9.1 tunnel delete blinks-demo
```

Under **Networking → Tunnels** in the dashboard, only `blinks-prod` should be left, as
**Healthy**.

## Releases

Publish new images (above), put the new tag in `.env` (`BACKEND_TAG` or `WEB_TAG`), then:

```bash
cd ~/the-blinks/prod && sudo docker compose pull && sudo docker compose up -d
```

Compose only recreates the containers whose image or settings changed. A new `BACKEND_TAG` runs
that commit's migrations first, then starts the new API. After a `WEB_TAG` change, purge
Cloudflare's cache.

**Rolling back** is putting the previous tag back and running the same command. That rolls back
the code, not the database: migrations already applied stay. So write migrations the old code
can live with (add a column first, use it in a later release, remove the old one later still).
Undoing a migration needs the **newer** image, because the older one doesn't contain that
changeset or its rollback. A variable set on the command line wins over `.env`:
`sudo BACKEND_TAG=<newer commit> docker compose run --rm migrate rollback-count --count=1`.

## Everyday commands

| Task                                | Command (in `~/the-blinks/prod`)                          |
| ----------------------------------- | --------------------------------------------------------- |
| What's running                      | `sudo docker compose ps -a`                               |
| Follow the logs                     | `sudo docker compose logs -f --tail 50 api` (or `web`, `tunnel`) |
| SQL shell (as the owner)            | `sudo docker compose exec postgres psql -U blinks -d blinks` |
| Pending migrations                  | `sudo docker compose run --rm migrate status --verbose`   |
| Restart one service                 | `sudo docker compose restart api`                         |
| Stop everything (keeps the data)    | `sudo docker compose down`                                |
| Check `cloudflared/config.yml`      | `sudo docker run --rm -v "$PWD/cloudflared:/etc/cloudflared:ro" cloudflare/cloudflared:2026.9.1 tunnel --config /etc/cloudflared/config.yml ingress validate` |

After changing `compose.yaml`, `cloudflared/config.yml` or `.env`, run `sudo docker compose up -d`
(a changed `config.yml` alone needs `sudo docker compose restart tunnel`).

## Rotating secrets

| Secret              | How                                                                                                         | Effect                                     |
| ------------------- | ----------------------------------------------------------------------------------------------------------- | ------------------------------------------ |
| `JWT_SECRET`        | New value in `.env`, `sudo docker compose up -d api`                                                         | Access tokens stop working at once; the admin refreshes silently, so nobody is logged out |
| `APP_DB_PASSWORD`   | `ALTER ROLE` (see "Database logins"), then `.env`, then `up -d api`                                           | None                                       |
| `POSTGRES_PASSWORD` | `ALTER ROLE blinks PASSWORD '...'` in the SQL shell, then `.env`                                             | None (only migrations use it)              |
| `TUNNEL_TOKEN`      | `tunnel create` a new tunnel, `route dns --overwrite-dns` for all three hostnames, new token in `.env`, `up -d tunnel`, delete the old tunnel | Seconds |

If `.env` itself leaks, rotate all of them.

## Not covered here yet

From `docs/SECURITY.md` §4, still to do before launch:

- **Backups:** a daily `pg_dump` stored off the VM, and one restore tested.
- **VM:** firewall closed except SSH (the tunnel needs no inbound ports at all), SSH keys only,
  and automatic security updates (`unattended-upgrades`).
- **Image updates:** bump `postgres:18.6-alpine`, `cloudflare/cloudflared:2026.9.1` and the base
  images in the Dockerfiles on a schedule.
