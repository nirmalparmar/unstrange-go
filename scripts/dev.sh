#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# This profile deliberately ignores private .env files and cloud credentials.
export SKIP_DOTENV=true ENV=development PORT=8080 BIND_ADDRESS=127.0.0.1
export DATABASE_URL_POOLED='' TRUSTED_PROXIES=''
export DATABASE_URL='postgres://unstrange@127.0.0.1:55439/unstrange_dev?sslmode=disable'
export JWT_SECRET='local-development-only-replace-before-deploying'
export TEST_OTP=123456 APP_URL=http://localhost:8080 FRONTEND_URL=http://localhost:3000
export OPENAI_API_KEY='' AWS_ACCESS_KEY_ID='' AWS_SECRET_ACCESS_KEY='' MSG91_AUTH_TOKEN='' MSG91_WIDGET_ID=''
export REQUIRE_IDENTITY_VERIFICATION=false REQUIRE_MODERATION=false
mkdir -p .local
if [ ! -d .local/postgres ]; then initdb -D .local/postgres -A trust -U unstrange --no-locale -E UTF8; fi
if ! pg_ctl -D .local/postgres status >/dev/null 2>&1; then
 pg_ctl -D .local/postgres -l .local/postgres.log -o '-h 127.0.0.1 -p 55439 -k /tmp' start
fi
if ! psql -h 127.0.0.1 -p 55439 -U unstrange -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='unstrange_dev'" | rg -q 1; then
 createdb -h 127.0.0.1 -p 55439 -U unstrange unstrange_dev
fi
exec go run ./cmd/server
