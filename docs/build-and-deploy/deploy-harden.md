---
summary: 'Make a server harder to get into with `tifa deploy harden`: ssh keys only, ufw, automatic security updates, what it never touches, how it avoids locking you out, and how to undo it'
audience: app
order: 22
read_when:
  - Hardening the server of a Tifa app, or the server accepts ssh passwords and you want that off
  - Running `tifa deploy harden`, or reading its plan or its report
  - harden refused to turn on ufw, named a program and a port, or you are unsure what it changes in ssh
  - Undoing what `tifa deploy harden` did
  - Docker is installed on the server and you want to know what ufw does for it
---

# Harden the server

`tifa deploy harden` closes the doors most attacks on a new VPS come through: it turns off ssh passwords, turns on a firewall when nothing else guards the server, and makes security updates install by themselves. It works on the server, not on an app, so run it once per server.

```sh
go tool tifa deploy harden --host vps
```

Like setup, it reads the server first, prints what it found and a plan, and changes nothing until you agree. On a terminal it asks. Without a terminal, pass `--yes`. Run it again at any time: what is already right is left alone. Name several servers with `--host` repeated, and each gets its own report, plan and question. Without `--host` it uses `TIFA_DEPLOY_HOST`, else the only server in `~/.tifa`.

It logs in the way setup does, as root, or as the user in front of the host with `sudo -n` (see [When root cannot log in](deploy.md#when-root-cannot-log-in)). It changes Debian and Ubuntu servers (those with apt). On any other system it prints the report and changes nothing.

## What it changes

Three things, in this order.

**1. ssh takes keys only.** harden writes `/etc/ssh/sshd_config.d/00-tifa-harden.conf` with `PasswordAuthentication no` and `KbdInteractiveAuthentication no`. It adds `PermitRootLogin prohibit-password` only when sshd has `PermitRootLogin yes` now, so root keeps logging in with a key. The file name starts with `00` on purpose: sshd keeps the first value it reads for a setting, and reads the files of that folder in the order of their names, so a file such as `50-cloud-init.conf`, which many VPS images ship with `PasswordAuthentication yes`, would win over a later name. After the write, harden runs `sshd -t`, checks with `sshd -T` that the new values are the ones in force, and reloads sshd. Open sessions stay.

Anyone who logs in with a password is refused after this and needs a key. The web console of your provider still works, so there is a way in if you lose your key. A server that already refuses passwords is "already right".

**2. ufw, when it is off or not installed.** harden installs ufw with apt when it is missing, denies what comes in, allows what goes out, and allows these ports (tcp), in rules with the comment `tifa harden`:

- the ssh ports sshd uses, and the port this machine connected on,
- the ports the apps of Tifa on the server serve visitors on: 80 and 443 for an app with `front: none` and for the nginx of `front: nginx`.

Before it turns ufw on, harden lists everything that listens on an address other than loopback. A program that is not sshd, a socket of Tifa or the nginx of Tifa stops it: harden names the program and its port, does not turn ufw on, and changes nothing in ufw. It does not guess whether the program should stay reachable. Allow its port yourself (`sudo ufw allow <port>/tcp`), or stop the program, then run harden again. A port that a rule of ufw allows already is accounted for.

ufw keeps one rule for a port and only changes its comment when you add another. So harden adds a rule only for a port that no rule allows yet, whatever its comment, and leaves your rules and those of Tifa's nginx as they are. The plan names the ports it adds and the ports that have rules. A rule that denies or limits one of the ports stays and is named in the plan. When it denies the ssh port, ufw stays off.

When ufw is on already, harden only reports: it never edits the rules of a firewall someone set up. It says so when no rule allows ssh. It never closes or limits the ssh port.

**3. Automatic security updates.** harden installs `unattended-upgrades` when it is missing, sets `Unattended-Upgrade` to 1 in `/etc/apt/apt.conf.d/20auto-upgrades` when it is 0 or the file is missing, and enables `apt-daily-upgrade.timer`. Ubuntu Server ships with this on, so usually it is "already right". A `0` that you set on purpose shows in the plan as such, and changes only when you agree. When updates ask for a reboot (`/var/run/reboot-required`), harden says so. It never reboots the server.

**Advice, not a change.** When `fail2ban` is not installed, the plan says so in one line. Tifa never installs it: with key-only logins a guessed password gets nobody in, and it only cuts the log noise.

## What it never touches

The kernel settings (sysctl, BBR), the ssh port, the firewall of your provider, the rules of a ufw that is on, and anything else that is not on the list above.

## It does not lock you out

The two changes that could lock you out, the one to ssh and turning ufw on, run in a session that stays open until a new login works. harden changes the server, then logs in again from your machine with a connection of its own (never one that `ControlMaster` keeps open, which would prove nothing). That login never asks for a password, so when it works it is a key login. When that login fails, the open session undoes the change, harden reports what ssh said, and the server is as it was. The session undoes it too when harden dies before it answers.

## Docker and ufw

ufw does not filter the ports Docker publishes: Docker writes its own rules before those of ufw. When Docker is installed, the plan says so. Publish a port on `127.0.0.1` (`-p 127.0.0.1:8080:80`) when only the server itself should reach it.

## The firewall of your provider

harden leaves the firewall and security groups of your provider alone, as setup does. If they block a port a visitor needs, open it there.

## Undo

harden writes what it changed, and the state from before each change, to `/etc/tifa/harden` on the server (it belongs to root). `tifa deploy remove` does not undo hardening: it belongs to the server, not to an app. To put it back:

```sh
go tool tifa deploy harden --host vps --undo
```

It reads the record and nothing else. It prints a plan, asks the same way, then deletes the ssh file and reloads sshd (password logins come back, and a new login is checked again), turns ufw off, deletes the rules harden added (the record names their ports, and no other rule is touched) and uninstalls ufw if harden installed it, and puts the automatic updates back as they were, uninstalling the package if harden installed it. The record goes at the end. Without a record, it says there is nothing of harden to undo.

## Setup mentions it

When the server accepts ssh passwords, `tifa deploy setup` prints one line that says so and names this command. Setup changes nothing else about it.
