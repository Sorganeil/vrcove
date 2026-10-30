# Docker deployment template

This directory provides an example container configuration. Adjust the image, user IDs, file paths, and resource limits for your own environment before deploying.

Keep deployment topology, hosting-provider details, tunnel configuration, credentials, and operational runbooks outside this repository. Do not commit production configuration or data.

The example runs without published TCP ports, uses a read-only root filesystem, drops capabilities, and limits memory and process counts. Validate socket permissions, database integrity, outbound HTTPS, and backups for your deployment.

`tunnel.service` is a generic connector example. Store the connector token separately in the root-only file named by `LoadCredential`; never commit its contents. Adjust local socket groups and the loopback metrics port for your deployment.
