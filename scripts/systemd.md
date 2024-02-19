# Forgejo Runner with systemd User Services

It is possible to use systemd's user services together with 
[podman](https://podman.io/) to run `forgejo-runner` using a normal user 
account without any privileges and automatically start on boot.

This was last tested on Fedora 39 on 2024-02-19, but should work elsewhere as 
well.

Place the `forgejo-runner` binary in `/usr/local/bin/forgejo-runner` and make
sure it can be executed (`chmod +x /usr/local/bin/forgejo-runner`).

Install and enable `podman` as a user service:

```bash
$ sudo dnf -y install podman
```

You *may* need to reboot your system after installing `podman` as it 
modifies some system configuration(s) that may need to be activated. Without
rebooting the system my runner errored out when trying to set firewall rules, a
reboot fixed it.

Enable `podman` as a user service:

```
$ systemctl --user start podman.socket
$ systemctl --user enable podman.socket
```

Make sure processes remain after your user account logs out:

```bash
$ loginctl enable-linger
```

Create the file `/etc/systemd/user/forgejo-runner.service` with the following
content:

```
[Unit]
Description=Forgejo Runner

[Service]
Type=simple
ExecStart=/usr/local/bin/forgejo-runner daemon
Restart=on-failure

[Install]
WantedBy=default.target
```

Now activate it as a user service:

```bash
$ systemctl --user daemon-reload
$ systemctl --user start forgejo-runner
$ systemctl --user enable forgejo-runner
```

To see/follow the log of `forgejo-runner`:

```bash
$ journalctl -f -t forgejo-runner
```

If you reboot your system, all should come back automatically.
