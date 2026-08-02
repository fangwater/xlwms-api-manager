#!/usr/bin/env bash
set -euo pipefail
cd /home/ubuntu/xlwms-api-manager
set -a
source .env
set +a
exec ./bin/xlwms-server
