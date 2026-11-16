#!/usr/bin/env bash
(
set -euo pipefail
app_password="$(cat /run/secrets/app_password)"
migrator_password="$(cat /run/secrets/migrator_password)"
[[ "$app_password" =~ ^[a-f0-9]{64}$ && "$migrator_password" =~ ^[a-f0-9]{64}$ ]]
[[ "$MYSQL_DATABASE" == siyige_demo && "$MYSQL_APP_USER" == siyige_demo_app && "$MYSQL_MIGRATION_USER" == siyige_demo_migrator ]]
bash /opt/siyige/mysql-client.sh root <<SQL
CREATE USER 'siyige_demo_app'@'%' IDENTIFIED BY '${app_password}';
CREATE USER 'siyige_demo_migrator'@'%' IDENTIFIED BY '${migrator_password}';
GRANT SELECT, INSERT, CREATE, ALTER, INDEX, REFERENCES ON \`siyige\_demo\`.* TO 'siyige_demo_migrator'@'%';
SQL
)
