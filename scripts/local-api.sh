#!/bin/bash
# 用本机凭证调用正在运行的 omniHub 服务。用法: scripts/local-api.sh METHOD PATH [JSON]
CFG="${OMNIHUB_CONFIG_DIR:-$HOME/.config/omnihub}/local-client.json"
T=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['token'])" "$CFG")
U=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['url'])" "$CFG")
curl -s -H "X-Omni-Local-Token: $T" -H 'Content-Type: application/json' -X "$1" "$U$2" ${3:+-d "$3"}
echo
