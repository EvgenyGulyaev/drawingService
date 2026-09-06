#!/usr/bin/env bash
set -Eeuo pipefail

component=${1:?Expected api or web}
release_id=${2:?Expected run ID, attempt and commit SHA}
[[ $component == api || $component == web ]]
[[ $release_id =~ ^[0-9]+-[0-9]+-[0-9a-f]{40}$ ]]
base=/opt/crocodile
incoming="$base/incoming/$component-$release_id"
release="$base/releases/$component-$release_id"

# Both repositories update one release pointer. Serialize across repositories.
exec 9>"$base/.deploy.lock"
flock -w 120 9
previous=$(readlink -f "$base/current")
[[ $previous == "$base/releases/"* && -d $previous ]]
[[ ! -e $release && -d $incoming ]]
if [[ $component == api ]]; then
    test -s "$incoming/crocodile"
else
    test -s "$incoming/index.html"
fi

switch_release() {
    ln -sfnT "$1" "$base/current.next"
    mv -Tf "$base/current.next" "$base/current"
}

activated=false
rollback() {
    trap - ERR
    if "$activated"; then
        switch_release "$previous"
        if [[ $component == api ]]; then
            sudo -n /usr/bin/systemctl restart crocodile.service
        fi
    fi
    rm -rf -- "$release" "$incoming"
    echo "Deployment failed; restored $previous" >&2
    exit 1
}

trap rollback ERR
mkdir "$release"
if [[ $component == api ]]; then
    install -m 755 "$incoming/crocodile" "$release/crocodile"
    cp -a "$previous/web" "$release/web"
else
    # The running API keeps this inode mapped; share it instead of retaining a deleted copy.
    ln "$previous/crocodile" "$release/crocodile"
    cp -a "$incoming" "$release/web"
    chmod -R a+rX "$release/web"
fi
activated=true
switch_release "$release"
if [[ $component == api ]]; then
    sudo -n /usr/bin/systemctl restart crocodile.service
    healthy=false
    for _attempt in {1..15}; do
        # The game returns JSON 404 for a missing room; no production room is created.
        code=$(curl -s --max-time 2 -o /dev/null -w '%{http_code}' \
            http://127.0.0.1:8091/api/game/rooms/NOTFOUND) || code=000
        if [[ $code == 404 ]] && systemctl is-active --quiet crocodile.service; then
            healthy=true
            break
        fi
        sleep 1
    done
    "$healthy"
else
    curl --fail --silent --show-error --max-time 10 \
        --resolve draw.wedding-en.ru:443:127.0.0.1 https://draw.wedding-en.ru/ \
        | cmp -s "$release/web/index.html" -
fi
trap - ERR
rm -r "$incoming"
[[ $(readlink -f "$base/current") == "$release" ]]
find "$base/releases" -mindepth 1 -maxdepth 1 ! -path "$release" -exec rm -rf -- {} +
echo "Deployed $component: $release"
