#!/bin/sh

# Link our postgres db to grafana.
# sed -i -e 's|<HTTP_PORT>|'$PORT'|' /etc/grafana/grafana.ini
sed -i -e 's|<DATABASE_URL>|'$DATABASE_URL'|' /etc/grafana/grafana.ini
# Startup grafana & tailscale.
/run.sh &
/app/app
