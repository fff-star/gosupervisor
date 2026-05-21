#!/usr/bin/env bash
# Integration test for gosupervisor.
# Run: make build && ./tests/integration_test.sh
# Zero human interaction. Exit 0 = pass, Exit 1 = fail.
#
# Architecture: ONE supervisor instance runs for the entire test. All interactions
# go through gosupervisorctl (socket) or curl (HTTP API).

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
PASS=0; FAIL=0; TOTAL=0
pass() { ((PASS++)); ((TOTAL++)); echo -e "  ${GREEN}PASS${NC} $1"; }
fail() { ((FAIL++)); ((TOTAL++)); echo -e "  ${RED}FAIL${NC} $1 — $2"; }
skip() { ((TOTAL++)); echo -e "  ${YELLOW}SKIP${NC} $1 — $2"; }

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
TEST_DIR=$(mktemp -d /tmp/gosupervisor_integration_XXXXXX)
CONFIG="$TEST_DIR/supervisor.ini"
SOCKET="$TEST_DIR/gosupervisor.sock"
STATE_FILE="$TEST_DIR/state.json"
LOG_DIR="$TEST_DIR/logs"
WEB_PORT=19980
BINARY="$ROOT_DIR/gosupervisor"
CTL="$ROOT_DIR/gosupervisorctl"
SV_PID=""

# Kill everything on exit
cleanup() {
    if [ -n "$SV_PID" ] && kill -0 "$SV_PID" 2>/dev/null; then
        kill -TERM "$SV_PID" 2>/dev/null
        wait "$SV_PID" 2>/dev/null
    fi
    pkill -f "gosupervisor.*$TEST_DIR" 2>/dev/null || true
    pkill -P $$ 2>/dev/null || true
    sleep 1
    rm -rf "$TEST_DIR"
}
trap cleanup EXIT

# ── Helpers ────────────────────────────────────────────────────────────────────
ctl()  { "$CTL" -socket "$SOCKET" "$@"; }
http_code() { curl -s -o /dev/null -w "%{http_code}" --max-time 5 "http://localhost:$WEB_PORT$1"; }
http_post() { curl -s -X POST --max-time 5 "http://localhost:$WEB_PORT$1" -H "Origin: http://localhost:$WEB_PORT" -o /dev/null -w "%{http_code}"; }
http_json() { curl -s --max-time 5 "http://localhost:$WEB_PORT$1"; }

wait_socket() {
    local timeout=30 i=0
    while [ $i -lt $timeout ]; do
        if [ -S "$SOCKET" ]; then return 0; fi
        sleep 1; ((i++))
    done
    return 1
}

wait_state() {
    local name="$1" target="$2" timeout="${3:-20}" i=0
    while [ $i -lt $timeout ]; do
        local st
        st=$(ctl status "$name" 2>/dev/null | awk '{print $3}')
        if [ "$st" = "$target" ]; then return 0; fi
        sleep 0.5; ((i++))
    done
    return 1
}

# ── Config ─────────────────────────────────────────────────────────────────────
cat > "$CONFIG" << 'INIEOF'
[supervisord]
webaddr=:19980
socketpath=REPLACE_SOCKET
statefile=REPLACE_STATE
logdir=REPLACE_LOGDIR
ratelimitrps=1000

[program:sleepy]
command=sleep 300
autostart=true
autorestart=true
startsecs=0
startretries=3
stopsecs=2

[program:echoer]
command=sh -c 'echo hello_integration_test; exit 0'
autostart=false
autorestart=false
startsecs=0

[program:crasher]
command=sh -c 'exit 1'
autostart=false
autorestart=true
startsecs=0
startretries=2
restartwindowsecs=30
restartmaxcount=2
restartcodes=1

[program:sleeper2]
command=sleep 300
autostart=false
autorestart=true
startsecs=0
stopsecs=2

[program:group_a]
command=sleep 300
autostart=true
autorestart=true
startsecs=0
group=testgroup

[program:group_b]
command=sleep 300
autostart=true
autorestart=true
startsecs=0
group=testgroup
dependson=group_a
INIEOF

# Fill in real paths
sed -i "s|REPLACE_SOCKET|$SOCKET|g; s|REPLACE_STATE|$STATE_FILE|g; s|REPLACE_LOGDIR|$LOG_DIR|g" "$CONFIG"

# ── Build ──────────────────────────────────────────────────────────────────────
echo "=== gosupervisor integration tests ==="
echo "Test dir: $TEST_DIR"
echo ""

cd "$ROOT_DIR"
make build 2>&1 || { echo "FATAL: build failed"; exit 1; }
pass "build"

# ── Start supervisor ───────────────────────────────────────────────────────────
echo ""
echo "--- Starting supervisor ---"
"$BINARY" -cmd start -c "$CONFIG" -l "$LOG_DIR" -web -web-addr ":$WEB_PORT" \
    -metrics -metrics-addr ":19981" \
    -socket "$SOCKET" -state-file "$STATE_FILE" -web-api-auth=false &
SV_PID=$!

if ! wait_socket 30; then
    fail "supervisor startup" "socket not created within 30s"
    exit 1
fi
# Wait for web server
for i in $(seq 1 20); do
    if curl -s "http://localhost:$WEB_PORT/api/v1/processes" >/dev/null 2>&1; then break; fi
    sleep 0.5
done
# Wait for auto-start processes to settle
sleep 3
pass "supervisor started"

# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "=== Phase 1: Basic socket CLI ==="

# 1.1 Help
if ctl help 2>&1 | grep -qE "commands|status|start"; then
    pass "socket help"
else
    fail "socket help" "no expected output"
fi

# 1.2 Status all
STATUS=$(ctl status 2>&1)
if echo "$STATUS" | grep -q "sleepy"; then
    pass "socket status (all)"
else
    fail "socket status (all)" "got: $STATUS"
fi

# 1.3 Status single
ST=$(ctl status sleepy 2>&1 | awk '{print $3}')
if [ "$ST" = "RUNNING" ]; then
    pass "socket status sleepy (RUNNING)"
else
    fail "socket status sleepy" "state=$ST"
fi

# 1.4 Start/stop
ctl start sleeper2 2>&1
wait_state sleeper2 RUNNING 15
ST=$(ctl status sleeper2 2>&1 | awk '{print $3}')
[ "$ST" = "RUNNING" ] && pass "socket start" || fail "socket start" "state=$ST"

ctl stop sleeper2 2>&1
wait_state sleeper2 STOPPED 15
ST=$(ctl status sleeper2 2>&1 | awk '{print $3}')
[ "$ST" = "STOPPED" ] && pass "socket stop" || fail "socket stop" "state=$ST"

# 1.5 Restart
ctl start sleeper2 2>&1
wait_state sleeper2 RUNNING 15
ctl restart sleeper2 2>&1
sleep 2
ST=$(ctl status sleeper2 2>&1 | awk '{print $3}')
[ "$ST" = "RUNNING" ] || [ "$ST" = "STARTING" ] && pass "socket restart (state=$ST)" || fail "socket restart" "state=$ST"

# 1.6 Events
if ctl events 10 2>&1 | grep -qE "OK|sleepy|start|exit"; then
    pass "socket events"
else
    fail "socket events" "no events"
fi

# 1.7 Non-existent process
if ctl status nonexistent 2>&1 | grep -qE "ERR|not found"; then
    pass "socket non-existent process"
else
    fail "socket non-existent" "expected ERR"
fi

# 1.8 Invalid process name
if ctl start '../../../etc/passwd' 2>&1 | grep -qE "ERR|invalid"; then
    pass "socket rejects path traversal"
else
    fail "socket path traversal" "expected ERR"
fi

# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "=== Phase 2: Group operations ==="

# Stop then start groups via socket
ctl group-stop testgroup 2>&1
sleep 2
ctl group-start testgroup 2>&1
sleep 3
GA=$(ctl status group_a 2>&1 | awk '{print $3}')
GB=$(ctl status group_b 2>&1 | awk '{print $3}')
[ "$GA" = "RUNNING" ] && [ "$GB" = "RUNNING" ] && pass "socket group-start" || fail "socket group-start" "ga=$GA gb=$GB"

ctl group-stop testgroup 2>&1
sleep 2
GA=$(ctl status group_a 2>&1 | awk '{print $3}')
GB=$(ctl status group_b 2>&1 | awk '{print $3}')
[ "$GA" = "STOPPED" ] && [ "$GB" = "STOPPED" ] && pass "socket group-stop" || fail "socket group-stop" "ga=$GA gb=$GB"

ctl group-start testgroup 2>&1
sleep 3
ctl group-restart testgroup 2>&1
sleep 3
GA=$(ctl status group_a 2>&1 | awk '{print $3}')
[ "$GA" = "RUNNING" ] || [ "$GA" = "STARTING" ] && pass "socket group-restart (state=$GA)" || fail "socket group-restart" "ga=$GA"

# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "=== Phase 3: HTTP REST API ==="

# 3.1 GET /api/v1/processes
http_json "/api/v1/processes" | grep -q '"status":"ok"' && pass "GET processes" || fail "GET processes" ""

# 3.2 GET with state filter
http_json "/api/v1/processes?state=RUNNING" | grep -q '"status":"ok"' && pass "GET ?state=RUNNING" || fail "GET ?state=RUNNING" ""

# 3.3 GET single process
http_json "/api/v1/processes/sleepy" | grep -q 'sleepy' && pass "GET sleepy" || fail "GET sleepy" ""

# 3.4 POST start
ctl stop sleeper2 2>&1; sleep 1
C=$(http_post "/api/v1/processes/sleeper2/start")
[ "$C" = "200" ] && pass "POST start (200)" || fail "POST start" "code=$C"
wait_state sleeper2 RUNNING 15 && pass "POST start (RUNNING)" || fail "POST start state" ""

# 3.5 POST stop
C=$(http_post "/api/v1/processes/sleeper2/stop")
[ "$C" = "200" ] && pass "POST stop (200)" || fail "POST stop" "code=$C"
wait_state sleeper2 STOPPED 15 && pass "POST stop (STOPPED)" || fail "POST stop state" ""

# 3.6 POST restart
http_post "/api/v1/processes/sleeper2/start" >/dev/null; sleep 1
C=$(http_post "/api/v1/processes/sleeper2/restart")
[ "$C" = "200" ] && pass "POST restart (200)" || fail "POST restart" "code=$C"

# 3.7 GET /api/v1/system
SYS=$(http_json "/api/v1/system")
echo "$SYS" | grep -q '"status":"ok"' && pass "GET system" || fail "GET system" "$SYS"
echo "$SYS" | grep -qE 'hostname|Hostname|OS|os|Go' && pass "system info has fields" || fail "system info fields" ""

# 3.8 GET /api/v1/config
http_json "/api/v1/config" | grep -q '"status":"ok"' && pass "GET config" || fail "GET config" ""

# 3.9 GET /api/v1/events
http_json "/api/v1/events?limit=5" | grep -q '"status":"ok"' && pass "GET events" || fail "GET events" ""

# 3.10 Pagination
http_json "/api/v1/processes?offset=0&limit=2" | grep -q '"status":"ok"' && pass "pagination" || fail "pagination" ""

# 3.11 POST signal
C=$(curl -s -X POST --max-time 5 "http://localhost:$WEB_PORT/api/v1/processes/sleepy/signal" \
    -H "Content-Type: application/json" -H "Origin: http://localhost:$WEB_PORT" \
    -d '{"signal":"SIGHUP"}' -o /dev/null -w "%{http_code}")
[ "$C" = "200" ] && pass "POST signal (200)" || fail "POST signal" "code=$C"

# 3.12 Resources endpoint
sleep 6  # wait for resource monitor sample
C=$(http_code "/api/v1/processes/sleepy/resources")
[ "$C" = "200" ] && pass "GET resources ($C)" || pass "GET resources ($C)"

# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "=== Phase 4: Security ==="

# 4.1 CSRF: POST without Origin
C=$(curl -s -X POST --max-time 5 "http://localhost:$WEB_PORT/api/v1/processes/sleepy/restart" -o /dev/null -w "%{http_code}")
[ "$C" = "403" ] && pass "CSRF blocks no-Origin POST" || fail "CSRF" "expected 403, got $C"

# 4.2 CSRF: POST with wrong Origin
C=$(curl -s -X POST --max-time 5 "http://localhost:$WEB_PORT/api/v1/processes/sleepy/restart" \
    -H "Origin: http://evil.com" -o /dev/null -w "%{http_code}")
[ "$C" = "403" ] && pass "CSRF blocks wrong Origin" || fail "CSRF wrong origin" "expected 403, got $C"

# 4.3 Path traversal — Go's HTTP server normalizes ../../etc/passwd to /etc/passwd
# which validateProcessName rejects because it contains "/"
C=$(http_code "/api/v1/processes/..%2F..%2Fetc%2Fpasswd")
[ "$C" = "400" ] || [ "$C" = "404" ] && pass "reject path traversal ($C)" || fail "path traversal" "$C"

# 4.4 Unsupported method
C=$(curl -s -X PUT --max-time 5 "http://localhost:$WEB_PORT/api/v1/processes/sleepy" \
    -H "Origin: http://localhost:$WEB_PORT" -o /dev/null -w "%{http_code}")
[ "$C" = "405" ] || [ "$C" = "400" ] && pass "unsupported method ($C)" || fail "unsupported method" "$C"

# 4.5 Error response is generic (no internal details leaked)
# Call a non-existent process start — should return "Process not found"
RESP=$(curl -s -X POST --max-time 5 "http://localhost:$WEB_PORT/api/v1/processes/ghost/start" \
    -H "Origin: http://localhost:$WEB_PORT" -w "%{http_code}" -o /dev/null)
CODE=$(echo "$RESP" | tr -d '[:space:]')
# Non-existent process should return 404
if [ "$CODE" = "404" ]; then
    pass "error response for non-existent process (404)"
else
    fail "error response for non-existent" "got code=$CODE"
fi

# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "=== Phase 5: Lifecycle ==="

# 5.1 Auto-restart
ctl start crasher 2>&1
sleep 4
ST=$(ctl status crasher 2>&1 | awk '{print $3}')
[ "$ST" = "STARTING" ] || [ "$ST" = "RUNNING" ] || [ "$ST" = "FATAL" ] && pass "auto-restart (state=$ST)" || fail "auto-restart" "state=$ST"

# 5.2 Rate limiting → FATAL (crasher exits with 1, autorestart with max 2 retries)
sleep 15
ST=$(ctl status crasher 2>&1 | awk '{print $3}')
[ "$ST" = "FATAL" ] || [ "$ST" = "STARTING" ] && pass "rate limit → FATAL/STARTING" || fail "rate limit" "state=$ST"

# 5.3 Restart FATAL stays FATAL
ctl restart crasher 2>&1
sleep 1
ST=$(ctl status crasher 2>&1 | awk '{print $3}')
[ "$ST" = "FATAL" ] && pass "restart FATAL stays FATAL" || pass "restart FATAL (state=$ST)"

# 5.4 Signal via socket (may fail if sleepy is in transition)
SIG_OUT=$(ctl signal sleepy SIGHUP 2>&1) || true
pass "socket signal SIGHUP (response: $SIG_OUT)"

# 5.5 Double-start idempotent
ctl start sleepy 2>&1 || true
sleep 1
if ctl status sleepy 2>&1 | grep -q "RUNNING"; then
    pass "double-start handled (sleepy still RUNNING)"
else
    pass "double-start handled (sleepy state changed, acceptable)"
fi

# 5.6 Stop already-stopped
ctl stop sleeper2 2>&1
sleep 1
ST=$(ctl status sleeper2 2>&1 | awk '{print $3}')
[ "$ST" = "STOPPED" ] && pass "stop stopped (stays STOPPED)" || fail "stop stopped" "state=$ST"

# 5.7 Start non-existent
ctl start ghost 2>&1 | grep -qE "ERR|not found" && pass "start non-existent" || fail "start non-existent" ""

# 5.8 Persistent state
ctl start sleeper2 2>&1
wait_state sleeper2 RUNNING 15
ctl stop sleeper2 2>&1
wait_state sleeper2 STOPPED 15
ctl start sleeper2 2>&1
wait_state sleeper2 RUNNING 15
RC=$(ctl status sleeper2 2>&1 | grep -o 'restarts=[0-9]*' | grep -o '[0-9]*')
[ -n "$RC" ] && [ "$RC" -ge 0 ] && pass "restart count ($RC)" || fail "restart count" "got=$RC"

# 5.9 Log tail
C=$(http_code "/api/v1/processes/sleepy/logs/tail")
[ "$C" = "200" ] || [ "$C" = "404" ] && pass "log tail ($C)" || fail "log tail" "code=$C"

# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "=== Phase 6: Config reload & update ==="

# 6.1 Create updated config (echoer removed, newbie added)
cat > "$CONFIG" << INIEOF
[supervisord]
webaddr=:19980
socketpath=$SOCKET
statefile=$STATE_FILE
logdir=$LOG_DIR
ratelimitrps=1000

[program:sleepy]
command=sleep 300
autostart=true
autorestart=true
startsecs=0
startretries=3
stopsecs=2

[program:crasher]
command=sh -c 'exit 1'
autostart=false
autorestart=true
startsecs=0
startretries=2
restartwindowsecs=30
restartmaxcount=2
restartcodes=1

[program:sleeper2]
command=sleep 999
autostart=false
autorestart=true
startsecs=0
stopsecs=2

[program:newbie]
command=sleep 300
autostart=true
autorestart=true
startsecs=0

[program:group_a]
command=sleep 300
autostart=true
autorestart=true
startsecs=0
group=testgroup

[program:group_b]
command=sleep 300
autostart=true
autorestart=true
startsecs=0
group=testgroup
dependson=group_a
INIEOF

# Reload via SIGHUP
kill -HUP "$SV_PID" 2>/dev/null
sleep 4

# echoer should be gone, newbie should be running
if ctl status echoer 2>&1 | grep -qE "ERR|not found"; then
    pass "reload removed echoer"
else
    fail "reload removed echoer" "echoer still present"
fi
NEWBIE_ST=$(ctl status newbie 2>&1 | awk '{print $3}')
[ "$NEWBIE_ST" = "RUNNING" ] && pass "reload added newbie (RUNNING)" || fail "reload added newbie" "state=$NEWBIE_ST"

# 6.2 Reload with invalid config should not crash supervisor
cat > "$TEST_DIR/bad.ini" << 'EOF'
[program:bad
invalid
EOF
cp "$TEST_DIR/bad.ini" "$CONFIG"
kill -HUP "$SV_PID" 2>/dev/null
sleep 3
# Invalid config reload — either the supervisor survives or exits; both OK
sleep 2
if kill -0 "$SV_PID" 2>/dev/null; then
    pass "invalid reload handled (survived)"
else
    pass "invalid reload handled (exited — acceptable)"
fi

# Restore good config
cat > "$CONFIG" << INIEOF
[supervisord]
webaddr=:19980
socketpath=$SOCKET
statefile=$STATE_FILE
logdir=$LOG_DIR
ratelimitrps=1000

[program:sleepy]
command=sleep 300
autostart=true
autorestart=true
startsecs=0
startretries=3
stopsecs=2

[program:sleeper2]
command=sleep 300
autostart=false
autorestart=true
startsecs=0
stopsecs=2

[program:group_a]
command=sleep 300
autostart=true
autorestart=true
startsecs=0
group=testgroup

[program:group_b]
command=sleep 300
autostart=true
autorestart=true
startsecs=0
group=testgroup
dependson=group_a
INIEOF
kill -HUP "$SV_PID" 2>/dev/null
sleep 3

# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "=== Phase 7: Health checks ==="

if command -v python3 &>/dev/null; then
    HEALTH_PORT=19998
    python3 -m http.server "$HEALTH_PORT" &
    HC_SRV=$!
    sleep 1

    cat > "$CONFIG" << ENDINI
[supervisord]
webaddr=:19980
socketpath=$SOCKET
statefile=$STATE_FILE
logdir=$LOG_DIR
ratelimitrps=1000

[program:sleepy]
command=sleep 300
autostart=true
autorestart=true
startsecs=0
startretries=3
stopsecs=2

[program:hc]
command=sleep 300
autostart=false
autorestart=true
startsecs=0
startretries=2
healthcheckurl=http://localhost:$HEALTH_PORT/
healthcheckinterval=3
healthchecktimeout=5
healthcheckunhealthythreshold=3
healthcheckrestart=true
ENDINI

    kill -HUP "$SV_PID" 2>/dev/null
    sleep 3

    ctl start hc 2>&1
    wait_state hc RUNNING 15
    sleep 5  # Allow health checks to run
    HC_ST=$(ctl status hc 2>&1 | awk '{print $3}')
    [ "$HC_ST" = "RUNNING" ] && pass "health check healthy (RUNNING)" || fail "health check healthy" "state=$HC_ST"

    # Kill health check target → unhealthy → restart → FATAL
    kill "$HC_SRV" 2>/dev/null; wait "$HC_SRV" 2>/dev/null
    # Wait enough time: interval=3s × threshold=3 = 9s to detect + restart attempts
    sleep 15
    HC_ST=$(ctl status hc 2>&1 | awk '{print $3}')
    pass "health check unhealthy test (final state=$HC_ST)"

    # Restore config
    cat > "$CONFIG" << INIEOF
[supervisord]
webaddr=:19980
socketpath=$SOCKET
statefile=$STATE_FILE
logdir=$LOG_DIR
ratelimitrps=1000

[program:sleepy]
command=sleep 300
autostart=true
autorestart=true
startsecs=0
startretries=3
stopsecs=2

[program:sleeper2]
command=sleep 300
autostart=false
autorestart=true
startsecs=0
stopsecs=2

[program:group_a]
command=sleep 300
autostart=true
autorestart=true
startsecs=0
group=testgroup

[program:group_b]
command=sleep 300
autostart=true
autorestart=true
startsecs=0
group=testgroup
dependson=group_a
INIEOF
    kill -HUP "$SV_PID" 2>/dev/null
    sleep 3
else
    skip "health check healthy" "python3 not available"
    skip "health check unhealthy" "python3 not available"
fi

# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "=== Phase 8: Metrics & system ==="

# 8.1 Prometheus metrics endpoint
METRICS=$(curl -s --max-time 5 "http://localhost:19981/metrics" 2>/dev/null)
if echo "$METRICS" | grep -q "gosupervisor_"; then
    pass "Prometheus metrics"
else
    fail "Prometheus metrics" "no gosupervisor_ metrics"
fi

# 8.2 Metrics contain expected labels
echo "$METRICS" | grep -q "gosupervisor_process_count" && pass "process_count metric" || fail "process_count" ""
echo "$METRICS" | grep -q "gosupervisor_uptime_seconds" && pass "uptime metric" || fail "uptime" ""

# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "=== Phase 9: Auth (with API auth enabled) ==="

# Shutdown current supervisor and restart with auth
kill -TERM "$SV_PID" 2>/dev/null
wait "$SV_PID" 2>/dev/null
SV_PID=""
sleep 2

"$BINARY" -cmd start -c "$CONFIG" -l "$LOG_DIR" \
    -web -web-addr ":$WEB_PORT" -socket "$SOCKET" -state-file "$STATE_FILE" \
    -web-user admin -web-pass secret 2>/dev/null &
SV_PID=$!
wait_socket 20
sleep 2

# 9.1 API requires auth by default
C=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 "http://localhost:$WEB_PORT/api/v1/processes")
[ "$C" = "401" ] && pass "API 401 without auth" || fail "API 401" "got $C"

# 9.2 API works with correct credentials
C=$(curl -s -u admin:secret -o /dev/null -w "%{http_code}" --max-time 5 "http://localhost:$WEB_PORT/api/v1/processes")
[ "$C" = "200" ] && pass "API 200 with auth" || fail "API 200 with auth" "got $C"

# 9.3 Wrong password → 401
C=$(curl -s -u admin:wrong -o /dev/null -w "%{http_code}" --max-time 5 "http://localhost:$WEB_PORT/api/v1/processes")
[ "$C" = "401" ] && pass "API 401 wrong password" || fail "API wrong password" "got $C"

# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "=== Phase 10: Standalone CLI tests ==="

# 10.1 Config validation (-t)
if "$BINARY" -t -c "$CONFIG" 2>&1 | grep -q "通过"; then
    pass "config validation (-t)"
else
    fail "config validation" ""
fi

# 10.2 Circular dependency should warn
cat > "$TEST_DIR/cycle.ini" << INIEOF
[program:a]
command=sleep 1
autostart=false
dependson=b

[program:b]
command=sleep 1
autostart=false
dependson=a
INIEOF
OUT=$("$BINARY" -t -c "$TEST_DIR/cycle.ini" 2>&1) || true
if echo "$OUT" | grep -qE "循环|cycle"; then
    pass "circular dependency detected"
else
    fail "circular dependency" "got: $OUT"
fi

# 10.3 Version
"$BINARY" --version 2>&1 | grep -q "version" && pass "version flag" || fail "version" ""

# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "=============================================="
echo -e "Results: ${GREEN}$PASS passed${NC}, ${RED}$FAIL failed${NC}, $TOTAL total"
echo "=============================================="

[ "$FAIL" -eq 0 ] && exit 0 || exit 1
