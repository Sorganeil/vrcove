# VRCove in Coolify

This runtime uses the already deployed Linux binary; it does not rebuild the
application or copy databases into an image. Build frontend assets before building
a replacement Go binary, following the main project README.

From the repository root:

```sh
docker build -t local/vrcove-runtime:20261029 deploy/docker
```

Import `deploy/docker/compose.yaml` as a Coolify Docker Compose service. The image
must exist on the deployment server first. CA certificates support outbound
VRChat HTTPS. No TCP ports are published; the host's consolidated
`hosthatch-tunnel.service` connects to `/run/vrcove/vrcove.sock`.
The container has a 1 GiB memory cap, a 640 MiB Go soft memory target and a
256-task limit.

The configuration expects host UID/GID 988 and these existing paths:

- `/usr/local/bin/vrcove`: executable, mounted read-only.
- `/etc/vrcove/env`: root-owned mode 600; Docker reads it as an env file.
- `/var/lib/vrcove`: writable existing databases, owned by 988:988.
- `/run/vrcove`: mode 0750, owned by 988:988; socket mode remains 0660.

Adjust numeric ownership for another host. Install `sockets.conf` as
`/etc/tmpfiles.d/coolify-vrcove.conf`, then run
`systemd-tmpfiles --create /etc/tmpfiles.d/coolify-vrcove.conf`.
This recreates the socket directory at boot after retiring the systemd app unit.

The shared tunnel unit is provided as `hosthatch-tunnel.service`. Install it once
per host, with its tunnel token stored separately at
`/etc/hosthatch-tunnel/tunnel.token` (root-owned mode 0600 inside a mode 0700
directory). Its `vrcove` and `cunnybin` supplementary groups permit both app
sockets; adjust the groups when deploying only one app on another host. Manage
hostname routes in Cloudflare. Keep the older per-app tunnel services disabled.

Before cutover, use SQLite's backup API for both catalog.sqlite and accounts.sqlite
and save the old unit and workload inventory. Stop and disable only vrcove.service,
then recreate the socket directory (systemd removes it on stop), and start the
Coolify service. Remove only the `vrcove-app` boot-controller entry; preserve the
consolidated tunnel's systemd startup entry. Compare local socket responses and public HTTPS, ownership,
database integrity and outbound TLS. Never start Coolify's proxy on the Plesk host.

Rollback: stop the Coolify service using POST, confirm the container is stopped,
restore the app's boot entry and enable/start vrcove.service. Keep the current
databases; do not overwrite live data with backups automatically. The tunnel needs
no restart. After atomically replacing a bind-mounted binary, recreate the
container so Docker mounts the new inode.

Secrets, database contents and operator logs stay outside Git.
