#!/usr/bin/env bash
# Sets up the shared VPS (Debian 13) that hosts changeloom prod and cou-alumni. Run as root, once, and again
# whenever you want to add keys or repair drift: every step checks before it changes anything.
# Usage: sudo bash bootstrap.sh [public-key-file...]
#   Each file's keys are added to the deploy user. With no files, the keys of the user running sudo are copied.
# It creates the deploy user (SSH key only, docker group), turns off root and password SSH logins, enables ufw
# (22, 80, 443), adds swap, enables unattended upgrades, installs Docker Engine with the compose plugin and Caddy
# from their apt repos, and makes /etc/caddy/sites/ (one site file per project) and /srv/<project>/.
# See deploy/README.md, section VPS.
set -euo pipefail

DEPLOY_USER="${DEPLOY_USER:-deploy}"
SWAP_SIZE="${SWAP_SIZE:-2G}"
SWAP_FILE="${SWAP_FILE:-/swapfile}"
# Each project's compose files and secrets live in /srv/<name>, owned by the deploy user.
PROJECT_DIRS="${PROJECT_DIRS:-changeloom cou-alumni-staging cou-alumni-production}"
# Docker's json-file logs are rotated at this size and count per container.
LOG_MAX_SIZE="${LOG_MAX_SIZE:-10m}"
LOG_MAX_FILE="${LOG_MAX_FILE:-3}"
# Seconds apt waits for the dpkg lock: on a fresh server apt-daily and unattended-upgrades often hold it.
APT_LOCK_TIMEOUT="${APT_LOCK_TIMEOUT:-900}"

log() { echo "==> $*"; }
die() {
	echo "bootstrap: $*" >&2
	exit 1
}

# write_file <path> <mode> <content>: replaces the file only when its content differs; returns 1 when unchanged.
write_file() {
	local path="$1" mode="$2" content="$3"
	if [[ -f $path ]] && [[ "$(cat "$path")" == "$content" ]]; then
		chmod "$mode" "$path"
		return 1
	fi
	local tmp
	tmp="$(mktemp "${path}.XXXXXX")"
	printf '%s\n' "$content" >"$tmp"
	chmod "$mode" "$tmp"
	mv "$tmp" "$path"
}

[[ $EUID -eq 0 ]] || die "run as root: sudo bash $0"
# shellcheck source=/dev/null
. /etc/os-release
[[ ${ID:-} == debian && ${VERSION_CODENAME:-} == trixie ]] ||
	die "expected Debian 13 (trixie), found ${PRETTY_NAME:-unknown}"

key_files=("$@")
if ((${#key_files[@]} == 0)); then
	[[ -n ${SUDO_USER:-} && $SUDO_USER != root ]] ||
		die "pass a public key file, or run through sudo so the invoking user's keys can be copied"
	sudo_home="$(getent passwd "$SUDO_USER" | cut -d: -f6)"
	key_files=("$sudo_home/.ssh/authorized_keys")
fi
for f in "${key_files[@]}"; do
	[[ -r $f ]] || die "cannot read key file $f"
	grep -qE '^(ssh-|ecdsa-|sk-)' "$f" || die "$f has no SSH public keys"
done

export DEBIAN_FRONTEND=noninteractive

log "Base packages"
apt-get -o DPkg::Lock::Timeout="$APT_LOCK_TIMEOUT" update -q
apt-get -o DPkg::Lock::Timeout="$APT_LOCK_TIMEOUT" install -y -q ca-certificates curl gnupg ufw unattended-upgrades

log "Deploy user $DEPLOY_USER"
if ! id "$DEPLOY_USER" >/dev/null 2>&1; then
	useradd --create-home --shell /bin/bash "$DEPLOY_USER"
fi
deploy_home="$(getent passwd "$DEPLOY_USER" | cut -d: -f6)"
install -d -m 700 -o "$DEPLOY_USER" -g "$DEPLOY_USER" "$deploy_home/.ssh"
auth="$deploy_home/.ssh/authorized_keys"
touch "$auth"
for f in "${key_files[@]}"; do
	while IFS= read -r line; do
		[[ $line =~ ^(ssh-|ecdsa-|sk-) ]] || continue
		grep -qxF "$line" "$auth" || printf '%s\n' "$line" >>"$auth"
	done <"$f"
done
chown "$DEPLOY_USER:$DEPLOY_USER" "$auth"
chmod 600 "$auth"

log "SSH: keys only, no root login"
# sshd keeps the first value it reads and includes sshd_config.d/*.conf in name order, so 00- beats
# provider files such as 50-cloud-init.conf that turn password logins on.
if write_file /etc/ssh/sshd_config.d/00-hardening.conf 644 "# Managed by changeloom deploy/vps/bootstrap.sh
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes"; then
	sshd -t || die "sshd rejected the new config; fix /etc/ssh/sshd_config.d/00-hardening.conf before logging out"
	systemctl reload ssh
fi

log "Firewall"
ufw default deny incoming >/dev/null
ufw default allow outgoing >/dev/null
ufw allow 22/tcp >/dev/null
ufw allow 80/tcp >/dev/null
# UDP 443 is HTTP/3, which Caddy serves; clients fall back to TCP without it.
ufw allow 443 >/dev/null
ufw --force enable >/dev/null

log "Swap $SWAP_SIZE"
if ! swapon --show=NAME --noheadings | grep -qxF "$SWAP_FILE"; then
	if [[ ! -f $SWAP_FILE ]]; then
		fallocate -l "$SWAP_SIZE" "$SWAP_FILE"
		chmod 600 "$SWAP_FILE"
		mkswap "$SWAP_FILE" >/dev/null
	fi
	swapon "$SWAP_FILE"
fi
grep -qE "^$SWAP_FILE\s" /etc/fstab || echo "$SWAP_FILE none swap sw 0 0" >>/etc/fstab
# Prefer dropping page cache over swapping out the services.
if write_file /etc/sysctl.d/99-swappiness.conf 644 "vm.swappiness=10"; then
	sysctl -q --system
fi

log "Unattended upgrades (Debian security updates; reboots stay manual)"
write_file /etc/apt/apt.conf.d/20auto-upgrades 644 'APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";' || true
systemctl enable --now unattended-upgrades >/dev/null

log "Docker Engine and compose plugin"
install -d -m 755 /etc/apt/keyrings
if [[ ! -s /etc/apt/keyrings/docker.asc ]]; then
	curl -fsSL --max-time 60 https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
	chmod 644 /etc/apt/keyrings/docker.asc
fi
docker_repo_changed=0
write_file /etc/apt/sources.list.d/docker.sources 644 "Types: deb
URIs: https://download.docker.com/linux/debian
Suites: $VERSION_CODENAME
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc" && docker_repo_changed=1

log "Caddy"
if [[ ! -s /usr/share/keyrings/caddy-stable-archive-keyring.gpg ]]; then
	curl -fsSL --max-time 60 https://dl.cloudsmith.io/public/caddy/stable/gpg.key |
		gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
	chmod 644 /usr/share/keyrings/caddy-stable-archive-keyring.gpg
fi
caddy_repo_changed=0
write_file /etc/apt/sources.list.d/caddy-stable.list 644 \
	"deb [signed-by=/usr/share/keyrings/caddy-stable-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main" &&
	caddy_repo_changed=1

if ((docker_repo_changed || caddy_repo_changed)) || ! command -v docker >/dev/null || ! command -v caddy >/dev/null; then
	apt-get -o DPkg::Lock::Timeout="$APT_LOCK_TIMEOUT" update -q
fi
apt-get -o DPkg::Lock::Timeout="$APT_LOCK_TIMEOUT" install -y -q docker-ce docker-ce-cli containerd.io docker-compose-plugin caddy

# Containers publish on 127.0.0.1 only: Docker's port rules bypass ufw, so a 0.0.0.0 port would be public.
if write_file /etc/docker/daemon.json 644 "{
  \"log-driver\": \"json-file\",
  \"log-opts\": { \"max-size\": \"$LOG_MAX_SIZE\", \"max-file\": \"$LOG_MAX_FILE\" }
}"; then
	systemctl restart docker
fi
systemctl enable --now docker containerd >/dev/null
# The docker group is root-equivalent; the deploy user needs it to run compose.
usermod -aG docker "$DEPLOY_USER"

# Projects drop their site file into /etc/caddy/sites/ (group-writable by the deploy user) and reload Caddy
# through the one sudo command below.
install -d -m 2775 -o root -g "$DEPLOY_USER" /etc/caddy/sites
if write_file /etc/caddy/Caddyfile 644 "# Managed by changeloom deploy/vps/bootstrap.sh. Each project adds its own <name>.caddy site file.
import /etc/caddy/sites/*.caddy"; then
	caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null
	systemctl reload-or-restart caddy
fi
systemctl enable --now caddy >/dev/null
sudoers="$DEPLOY_USER ALL=(root) NOPASSWD: /usr/bin/systemctl reload caddy"
if write_file /etc/sudoers.d/"$DEPLOY_USER"-caddy 440 "$sudoers"; then
	visudo -cqf /etc/sudoers.d/"$DEPLOY_USER"-caddy || {
		rm -f /etc/sudoers.d/"$DEPLOY_USER"-caddy
		die "visudo rejected the caddy reload rule"
	}
fi

log "Project directories"
for name in $PROJECT_DIRS; do
	install -d -m 750 -o "$DEPLOY_USER" -g "$DEPLOY_USER" "/srv/$name"
done

log "Done"
echo "Check from your machine, in a new terminal, before closing this session:"
echo "  ssh $DEPLOY_USER@<vps-ip> 'docker version --format {{.Server.Version}} && docker compose version'"
