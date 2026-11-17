# Node removal and SSH retries

`DELETE /api/admin/nodes/{id}` accepts administrator sessions or registered API tokens.

- `{ "cleanup": false }` removes the node from the panel and subscription/server assignments without SSH credentials. Server containers and files are left in place. The old agent credentials are rejected after removal. Failed installations and failed cleanup attempts can be removed this way.
- `{ "cleanup": true, "login": "root", "password": "..." }` starts SSH cleanup, stops the node containers and removes installed files before removing panel assignments. It returns HTTP 202; poll the node for operation status. On failure, retry cleanup or remove only from the panel.

For compatibility, omitting `cleanup` keeps the previous SSH-cleanup behavior and requires credentials. Panel-only removal returns HTTP 204. An active installation or cleanup returns HTTP 409 to prevent racing workers. The same IP can be added again after removal.

`POST /api/admin/nodes` accepts optional `reset_host_key: true` when retrying a failed installation. By default, retries preserve the pinned SSH host key. Explicitly resetting trust allows the next installation attempt to pin the server's new key; old installation tokens cannot change it. Use this option when the machine was reinstalled and its host key changed. It does not bypass SSH authentication: a valid login and password are still required. Ordinary reboots generally preserve SSH host keys.

No environment variables or migrations are required.
