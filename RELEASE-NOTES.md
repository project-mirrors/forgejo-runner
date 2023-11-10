# Release Notes

## 3.2.0

* Support LXC container capabilities via `lxc:lxc://debian:bookworm:k8s` or  `lxc:lxc://debian:bookworm:docker lxc k8s`

## 3.1.0

The `self-hosted` label that was hardwired to be a LXC container
running `debian:bullseye` was reworked and documented ([user guide](https://forgejo.org/docs/next/user/actions/#jobsjob_idruns-on) and [admin guide](https://forgejo.org/docs/next/admin/actions/#labels-and-runs-on)).

There now are two different schemes: `lxc://` for LXC containers and
`host://` for running directly on the host.

* Support the `host://` scheme for running directly on the host.
* Support the `lxc://` scheme in labels
* Update [code.forgejo.org/forgejo/act v1.14.0](https://code.forgejo.org/forgejo/act/pulls/19) to implement both self-hosted and LXC schemes

## 3.0.3

* Update [code.forgejo.org/forgejo/act v1.13.0](https://code.forgejo.org/forgejo/runner/pulls/106) to keep up with github.com/nektos/act

## 3.0.2

* Update [code.forgejo.org/forgejo/act v1.12.0](https://code.forgejo.org/forgejo/runner/pulls/106) to upgrade the node installed in the LXC container to node20

## 3.0.1

* Update [code.forgejo.org/forgejo/act v1.11.0](https://code.forgejo.org/forgejo/runner/pulls/86) to resolve a bug preventing actions based on node20 from running, such as [checkout@v4](https://code.forgejo.org/actions/checkout/src/tag/v4).

## 3.0.0

* Publish a rootless OCI image
* Refactor the release process

## 2.5.0

* Update [code.forgejo.org/forgejo/act v1.10.0](https://code.forgejo.org/forgejo/runner/pulls/71)

## 2.4.0

* Update [code.forgejo.org/forgejo/act v1.9.0](https://code.forgejo.org/forgejo/runner/pulls/64)

## 2.3.0

* Add support for [offline registration](https://forgejo.org/docs/next/admin/actions/#offline-registration).
