#!/bin/sh
set -eu

user="${CONSOLE_UI_USERNAME:-admin}"
pass="${CONSOLE_UI_PASSWORD:-change-me}"
port="${CONSOLE_UI_PORT:-8088}"

htpasswd -bc /etc/nginx/.htpasswd "$user" "$pass"
export CONSOLE_UI_PORT="$port"
envsubst '${CONSOLE_UI_PORT}' < /etc/nginx/templates/console-proxy.conf.template > /etc/nginx/conf.d/default.conf

exec nginx -g 'daemon off;'
