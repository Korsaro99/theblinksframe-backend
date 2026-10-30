# Production deploy

How The Blinks runs on the Oracle VM (x86_64, Ubuntu, user `ubuntu`) behind Cloudflare, and how
to switch over from the storefront demo. Everything here is in git except `.env`, which holds the
secrets and lives only on the VM.

| File                         | What it is                                                          |
| ---------------------------- | ------------------------------------------------------------------- |
| `compose.yaml`               | The six services, their settings, networks and the database volume  |
| `cloudflared/config.yml`     | Which hostname goes to which service                                |
| `postgres-init/10-app-role.sh` | Makes the API's own database login on the first start             |
| `postgres-init/20-backup-role.sh` | Makes the backup job's read-only database login on the first start |
| `backup/`                    | The nightly backup job (image, script) and the restore drill        |
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
                                                                                 ▲
                         backup (nightly, read only) ──pg_dump───────────────────┘
                            └──encrypted with age──▶ Oracle Object Storage bucket (off the VM)
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
It builds with the browser's API address `https://api.theblinksframes.com`, pushes
`ghcr.io/korsaro99/theblinksframe-frontend:<commit>` and prints the `WEB_TAG=<commit>` line to use
(`scripts/release-publish.sh` there):

```bash
npm run release:publish
```

Both refuse to run on uncommitted changes, since the tag is the commit, and both build for the
VM's x86 CPU (`linux/amd64`) while compiling natively on the Mac.

## On the VM (once)

### Files

Make the folder and copy the deploy files over (run the `scp` on your Mac, from this repo):

```bash
mkdir -p ~/the-blinks/prod
```

```bash
scp -r deploy/compose.yaml deploy/cloudflared deploy/postgres-init deploy/backup ubuntu@<vm-ip>:~/the-blinks/prod/
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

### Backup settings

Compose refuses to start without them. Do "Backups → Set up (once)" below first, then come back.

Check the file: these should print `16` (`17` with `BACKUP_PING_URL`) and `-rw-------`:

```bash
cd ~/the-blinks/prod && grep -c '=.' .env && ls -l .env | cut -c1-10
```

## Start it

Start everything. The demo keeps serving the site; production is live on the API hostname only:

```bash
cd ~/the-blinks/prod && sudo docker compose up -d
```

The first `up` also builds the backup image on the VM, which takes a minute. Then check.
`postgres`, `api`, `web` and `backup` should be `healthy`, `migrate` `exited (0)`, and
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

Take the first backup by hand, then do the restore drill with it ("Backups" below). Both must
work before the switch-over.

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

## Backups

Every night at 02:30 UTC (also Ghana time), the `backup` service:

1. dumps the database with `pg_dump --format=custom` (compressed; `pg_restore` can restore it
   whole or table by table) into `/tmp`, which is RAM, so the readable copy never reaches a disk;
2. checks that `pg_restore` can read the dump;
3. encrypts it with [age](https://age-encryption.org) for a **public** key;
4. uploads it to the bucket as `daily/blinks-<date>T<time>Z.dump.age`, and the first good backup
   of each month also as `monthly/blinks-<year>-<month>.dump.age`;
5. pings a monitor (optional), which emails you when a night is missed.

In Go terms the container is one goroutine in its own process:
`for { time.Sleep(untilNext("02:30")); if err := backup(); err != nil { log(err); alert() } }`.
A failed night is logged and retried the next night; `RCLONE_RETRIES` rides out short network
blips. The bucket deletes old copies itself, and the VM's key may only add and read objects, so
nothing on the VM can delete or overwrite a backup.

### Why it's built this way

**A compose service, not a systemd timer on the VM.** Both work; the trade-off:

|                          | Compose service (chosen)                          | systemd timer on the VM                          |
| ------------------------ | ------------------------------------------------- | ------------------------------------------------ |
| Where it's defined       | In git, copied with the other deploy files        | Unit files and packages set up by hand on the VM |
| `pg_dump` version        | 18.6, the same as the server, from the image      | Ubuntu's `postgresql-client` is older; needs Postgres' own apt repository |
| A night the VM was down  | Skipped; the monitor or `ps` (unhealthy) tells you | `Persistent=true` runs it at the next boot      |
| Logs                     | `sudo docker compose logs backup`                 | `journalctl -u blinks-backup`                    |
| Cost                     | One idle container, a few MB of memory            | Nothing between runs                             |
| Secrets                  | `.env`, handed to this container only             | A root-only file on the VM                       |

Keeping everything in compose means one way of running things and nothing on the VM to forget.
A missed night is the one real loss, and the monitor covers it.

**A read-only database login.** The backup container is the only one that can reach both the
database and the internet, so it logs in as `blinks_backup`, which has Postgres' built-in
`pg_read_all_data` role: it can read every table and change nothing. Tested: `DELETE` and
`CREATE TABLE` as `blinks_backup` both get "permission denied".

**Encrypted on the VM, with a key the VM can't use.** age encrypts for a public key (like
an SSH public key in `authorized_keys`, the other way round). The private key, the only way to
read a backup, lives on your Mac and in your password manager. So the bucket's company never
sees customer names and phone numbers, and someone who breaks into the VM can't read the old
backups either. **If you lose the private key, every backup is useless**: keep it in two places.

**Off the VM.** The backups survive losing the VM, its disk or the database (a bad migration,
a mistake, an intruder).

### Where the backups go (DECIDED 2026-10-17: Oracle Object Storage, for now)

The script speaks S3, so any of these works; only the `BACKUP_S3_*` settings change.

| Option                       | Free                               | For                                                                     | Against                                                    |
| ---------------------------- | ---------------------------------- | ----------------------------------------------------------------------- | ---------------------------------------------------------- |
| **Oracle Object Storage** (chosen) | 20 GB and 50,000 requests a month (Always Free) | The account you already have, nothing new to sign up for; the key can be limited to "add and read, never delete" | **Same account as the VM** (see below) |
| Cloudflare R2                | 10 GB, no charge for downloads     | Another company; the Cloudflare account you already use; bucket locks   | Asks for a payment method on file before R2 can be enabled |
| Backblaze B2                 | 10 GB                              | Another company; no card needed; "write, no delete" keys                | A third account to keep                                    |

**The trade-off of "for now":** the likeliest way to lose everything on a free tier is the
account itself (suspended, reclaimed, locked out), and that takes these backups with the VM.
Until there's a second copy with another company, keep the file from each restore drill on your
Mac (it's encrypted): at worst you lose what happened since the last drill. Moving or adding R2
or B2 later is a new bucket and new `BACKUP_S3_*` values, nothing else.

Size: the database is tiny today (an empty one dumps to about 50 KB). Thirty dailies and twelve
monthlies stay well inside the free tier for years, and a night is three requests.

### Set up (once)

**1. The age key, on your Mac.** Install age and make the key outside any git repository:

```bash
brew install age
```

```bash
(umask 077; age-keygen -o ~/blinks-backup-key.txt)
```

It prints `Public key: age1…`; that line goes in `.env` on the VM. Save the whole key file in
your password manager now (a secure note or attachment), as the second copy.

**2. The bucket, in the Oracle Cloud console**, in your home region (where the VM is; Always
Free storage lives there). Menu names move around between console versions; the search box at
the top finds each page by name.

- **Storage → Buckets → Create bucket**: name `blinks-backups`, **Standard** tier, no
  auto-tiering, no versioning, Oracle-managed encryption. Buckets are private unless you change
  that, so leave visibility alone. On the bucket's page, note the **Namespace** (a short random
  string).
- **Let Object Storage delete expired backups.** Lifecycle rules run as the Object Storage
  service, which needs permission once. **Identity & Security → Policies**, in the root
  compartment, **Create policy** `objectstorage-lifecycle` with (your region identifier, such as
  `eu-frankfurt-1`, is in the console's top bar under the region menu):

  ```text
  Allow service objectstorage-<region> to manage object-family in tenancy
  ```

- **Lifecycle rules**, on the bucket's page under **Lifecycle policy rules → Create rule**: one
  rule named `daily-30-days` (action **Delete**, **30** days, object name filter prefix `daily/`)
  and one named `monthly-365-days` (**Delete**, **365** days, prefix `monthly/`).
- **A user for the VM, allowed only to add and read backups.** In **Identity & Security →
  Domains → Default domain**:
  - **Groups → Create group** `blinks-backup`.
  - **Users → Create user** `blinks-backup` (if it asks for an email, any address you read
    works), and add it to the group. Under **Edit user capabilities**, leave only **Customer
    secret keys** on: it can't sign in to the console or use any other kind of key.
  - **Policies** (root compartment) **→ Create policy** `blinks-backup`, with these two lines.
    The VM can then add backups and read them, but never delete or overwrite one: a stolen key
    can't wipe the history.

    ```text
    Allow group 'Default'/'blinks-backup' to read buckets in tenancy where target.bucket.name = 'blinks-backups'
    Allow group 'Default'/'blinks-backup' to manage objects in tenancy where all {target.bucket.name = 'blinks-backups', any {request.permission = 'OBJECT_CREATE', request.permission = 'OBJECT_INSPECT', request.permission = 'OBJECT_READ'}}
    ```

  - On the user's page, **Customer secret keys → Generate secret key**, named `vm-backup`. Copy
    the **secret** at once (it's shown only then), then the **Access key** from the list.
- The **S3 endpoint** is `https://<namespace>.compat.objectstorage.<region>.oraclecloud.com`.

**3. The monitor (optional, recommended).** A free [healthchecks.io](https://healthchecks.io)
check with period **1 day** and grace **2 hours**: it emails you when the nightly ping doesn't
arrive, which also catches a stopped container or a dead VM. Copy its ping URL.

**4. The settings, on the VM.** The database password is generated; the rest you paste (`read -s`
keeps the secret off the screen):

```bash
cd ~/the-blinks/prod && read -r -p 'age public key (age1...): ' pub && read -r -p 'S3 endpoint (https://...oraclecloud.com): ' ep && read -r -p 'Region (e.g. eu-frankfurt-1): ' rg && read -r -p 'Access key: ' kid && read -rs -p 'Secret key: ' sec && echo && read -r -p 'healthchecks.io ping URL (or empty): ' hc && (umask 077; { printf 'BACKUP_DB_PASSWORD=%s\n' "$(openssl rand -hex 32)"; printf 'BACKUP_AGE_RECIPIENT=%s\nBACKUP_BUCKET=blinks-backups\nBACKUP_S3_ENDPOINT=%s\nBACKUP_S3_REGION=%s\nBACKUP_S3_ACCESS_KEY_ID=%s\nBACKUP_S3_SECRET_ACCESS_KEY=%s\nBACKUP_PING_URL=%s\n' "$pub" "$ep" "$rg" "$kid" "$sec" "$hc"; } >> .env) && unset sec
```

**5. If the database already exists** (Postgres was started before this was added), its
first-start scripts won't run again, so make the backup login by hand once:

```bash
cd ~/the-blinks/prod && sudo docker compose up -d postgres && sudo docker compose exec postgres sh /docker-entrypoint-initdb.d/20-backup-role.sh
```

### Check it works

After `sudo docker compose up -d`, take one backup now instead of waiting for the night:

```bash
sudo docker compose exec backup backup.sh once
```

It prints `uploaded daily/blinks-….dump.age (… bytes)`, and the file shows in the bucket. Then
do the restore drill below with it.

If it fails with `403`, `AccessDenied` or `SignatureDoesNotMatch`: the region must be the one in
the endpoint and the bucket's (the home region); the policy must name the group as
`'Default'/'blinks-backup'` and the bucket exactly; and the user needs **Customer secret keys**
among its capabilities. Policy changes can take a minute to apply.

From then on:

| Task                        | Command (in `~/the-blinks/prod`)                    |
| --------------------------- | --------------------------------------------------- |
| Recent results              | `sudo docker compose logs --tail 20 backup`        |
| Health (a good backup in the last 26 hours?) | `sudo docker compose ps backup`   |
| A backup now                | `sudo docker compose exec backup backup.sh once`   |

### Restore drill

A backup nobody has restored is a hope, not a backup. Do this before launch, then every three
months and after any big migration, and write the date in `docs/SECURITY.md` §4.

1. In the Oracle console, **Storage → Buckets → `blinks-backups`**, open the **Objects** list,
   pick the newest file in `daily/`, and choose **Download** from its **⋮** menu.
2. From this repo on your Mac (Docker running):

   ```bash
   deploy/backup/restore-drill.sh ~/Downloads/blinks-<date>.dump.age ~/blinks-backup-key.txt
   ```

   It starts a throwaway Postgres (`blinks-restore-drill`, no ports published) with the same
   version and first-start scripts as production, restores into it in one transaction (all or
   nothing), prints the last migration and the rows in every table, checks that `blinks_app`
   can read them, and deletes the container and the decrypted copy. The numbers should look
   like the shop's (products, orders, staff). `KEEP=1` in front leaves the container running
   for a closer look; `docker rm -f -v blinks-restore-drill` removes it.
3. Keep the downloaded file (it's encrypted) in a folder on your Mac until the next drill: while
   the backups live in the same Oracle account as the VM, it's the copy that survives losing the
   account.

Tested locally on 2026-10-17 against this compose file (throwaway project, an S3 stand-in for the
bucket): the nightly run, the manual run, a failed upload (logged, loop carried on), and the
drill restoring 17 tables with their rows and grants.

### Restoring production for real

When the database is lost (new VM, broken volume) or must go back to last night. The roles'
passwords aren't in the dump: the first-start scripts make them again from `.env`.

1. On your Mac, decrypt the chosen backup, and copy it to the VM:

   ```bash
   age --decrypt --identity ~/blinks-backup-key.txt --output blinks.dump ~/Downloads/blinks-<date>.dump.age
   ```

   ```bash
   scp blinks.dump ubuntu@<vm-ip>:~/the-blinks/prod/ && rm blinks.dump
   ```

2. On the VM, stop everything. On a new VM, skip to step 3. On the old one, the database volume
   has to go so Postgres starts empty: **this deletes the current database for good**, so only
   when it's beyond saving, and take a backup first if Postgres still runs.

   ```bash
   cd ~/the-blinks/prod && sudo docker compose down && sudo docker volume rm blinks-prod_pgdata
   ```

3. Start Postgres alone (its first-start scripts make the logins), restore, and delete the copies:

   ```bash
   cd ~/the-blinks/prod && sudo docker compose up -d --wait postgres && sudo docker compose cp blinks.dump postgres:/tmp/blinks.dump && sudo docker compose exec postgres pg_restore -U blinks -d blinks --exit-on-error --single-transaction /tmp/blinks.dump && sudo docker compose exec postgres rm /tmp/blinks.dump && rm blinks.dump
   ```

4. Start the rest: `sudo docker compose up -d`. `migrate` applies only changesets newer than the
   backup. Admin sessions from after the backup are gone, so staff log in again.

The drill runs step 3's restore command exactly, against a throwaway container.

## Everyday commands

| Task                                | Command (in `~/the-blinks/prod`)                          |
| ----------------------------------- | --------------------------------------------------------- |
| What's running                      | `sudo docker compose ps -a`                               |
| Follow the logs                     | `sudo docker compose logs -f --tail 50 api` (or `web`, `tunnel`, `backup`) |
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
| `BACKUP_DB_PASSWORD` | `ALTER ROLE blinks_backup PASSWORD '...'` in the SQL shell, then `.env`, then `up -d backup`                   | None                                       |
| `BACKUP_S3_*` key   | A second Customer Secret Key on the user `blinks-backup`, new values in `.env`, `up -d backup`, then delete the old key | None                                |
| age key             | `age-keygen` a new one, its public key in `.env`, `up -d backup`. **Keep the old private key** until the last backup made with it has expired (a year, because of `monthly/`) | None |
| `TUNNEL_TOKEN`      | `tunnel create` a new tunnel, `route dns --overwrite-dns` for all three hostnames, new token in `.env`, `up -d tunnel`, delete the old tunnel | Seconds |

If `.env` itself leaks, rotate all of them.

## Not covered here yet

From `docs/SECURITY.md` §4, still to do before launch:

- **VM:** firewall closed except SSH (the tunnel needs no inbound ports at all), SSH keys only,
  and automatic security updates (`unattended-upgrades`).
- **Image updates:** bump `postgres:18.6-alpine`, `cloudflare/cloudflared:2026.9.1` and the base
  images in the Dockerfiles on a schedule.
