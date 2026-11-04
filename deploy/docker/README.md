# Docker deployment template

Use this container configuration as a starting point. Set the image, user IDs, file paths and resource limits for your environment before deploying.

Keep your deployment layout, hosting-provider details, tunnel configuration, credentials and runbooks outside this repository. Do not commit production configuration or data.

The container has no published TCP ports. Its root filesystem is read-only, it drops capabilities, and it limits memory use and process counts. Check socket permissions, database integrity, outbound HTTPS and backups in your deployment.

`tunnel.service` is an example connector service. Store its token in the root-only file named by `LoadCredential`, and keep that file out of Git. Set the local socket groups and loopback metrics port for your deployment.
