#!/usr/bin/env python3
"""Black-box verification of CPAMP price history; no real providers or production data.

Usage: python3 verify.py /absolute/path/to/cpa-manager-plus
Requires Docker and the locally cached python:3.13-alpine image.
"""
import argparse
import tempfile
import datetime as dt
import hashlib
import json
import math
import os
import pathlib
import sqlite3
import subprocess
import sys
import time
import traceback
import uuid

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("binary", type=pathlib.Path)
parser.add_argument("--report-dir", type=pathlib.Path)
parser.add_argument("--upgrade-from", type=pathlib.Path)
args = parser.parse_args()
ROOT = args.report_dir.resolve() if args.report_dir else pathlib.Path(tempfile.mkdtemp(prefix="cpamp-pricing-report-"))
ROOT.mkdir(parents=True, exist_ok=True)
BINARY = args.binary.resolve()
INITIAL_BINARY = args.upgrade_from.resolve() if args.upgrade_from else BINARY
NAME = "cpamp-pricing-check-" + uuid.uuid4().hex[:10]
DATA = ROOT / ("data-" + NAME)
DATA.mkdir()
ADMIN = "synthetic-test-key-not-a-production-credential"
HOUR = 3_600_000
DAY = 24 * HOUR
BASE = int(time.time() * 1000) // DAY * DAY - DAY
CHANGE = BASE + 12 * HOUR
OLD = CHANGE - HOUR + 1000
NEW = CHANGE + 1000
checks = []
responses = {}
CLIENT = '''import json,sys,urllib.request,urllib.error
p=json.load(sys.stdin)
r=urllib.request.Request('http://127.0.0.1:18317'+p['path'],method=p['method'],data=None if p['body'] is None else json.dumps(p['body']).encode(),headers={'Authorization':'Bearer '+p['key'],'Content-Type':'application/json'})
try:
 with urllib.request.urlopen(r,timeout=20) as x: print(json.dumps({'status':x.status,'body':json.load(x)}))
except urllib.error.HTTPError as x: print(json.dumps({'status':x.code,'body':x.read().decode()}))
'''


def docker(*args, check=True, **kwargs):
    result = subprocess.run(["docker", *args], check=False, capture_output=True, text=True, **kwargs)
    if check and result.returncode != 0:
        raise RuntimeError(f"docker {args[0]} failed: {result.stderr}")
    return result


def api(method, path, body=None, label=None, expected=200):
    raw = docker("exec", "-i", NAME, "python3", "-c", CLIENT,
                 input=json.dumps(dict(method=method, path=path, body=body, key=ADMIN)))
    result = json.loads(raw.stdout)
    if label:
        responses[label] = result
    if result["status"] != expected:
        raise AssertionError(f"{method} {path}: expected {expected}, got {result}")
    return result["body"]


def ready():
    deadline = time.monotonic() + 45
    last = None
    while time.monotonic() < deadline:
        try:
            api("GET", "/health")
            api("GET", "/v0/management/model-prices")
            return
        except Exception as exc:
            last = exc
            time.sleep(.25)
    raise RuntimeError(f"container did not become ready: {last}")


def check(label, actual, expected):
    passed = math.isclose(actual, expected, abs_tol=1e-8, rel_tol=1e-8)
    checks.append(dict(check=label, actual=actual, expected=expected, passed=passed))
    print(f"{'PASS' if passed else 'FAIL'} {label}: {actual} (expected {expected})", flush=True)


def book(factor, effective=None):
    base = dict(prompt=1 * factor, completion=4 * factor, cache=.1 * factor,
                cacheRead=.1 * factor, cacheCreation=1.25 * factor,
                promptConfigured=True, completionConfigured=True,
                cacheReadConfigured=True, cacheCreationConfigured=True)
    result = {"mock-" + kind: dict(base) for kind in ["plain", "cache", "context", "priority"]}
    result["mock-context"]["contextTiers"] = [dict(
        thresholdTokens=200000, prompt=2 * factor, completion=6 * factor,
        promptConfigured=True, completionConfigured=True)]
    result["mock-priority"]["serviceTiers"] = [dict(
        mode="fast", serviceTier="priority", prompt=3 * factor,
        completion=9 * factor, promptConfigured=True, completionConfigured=True)]
    if effective is not None:
        for price in result.values():
            price["effectiveFromMs"] = effective
    return result


EXPECTED = {"plain": 1., "cache": 1.245, "context": 1.2, "priority": 3.9}


def event(kind, label, timestamp):
    model = "mock-" + kind
    value = dict(request_id=label + "-" + kind,
                 event_hash=hashlib.sha256((label + kind).encode()).hexdigest(),
                 timestamp_ms=timestamp,
                 timestamp=dt.datetime.fromtimestamp(timestamp / 1000, dt.timezone.utc).isoformat(),
                 model=model, requested_model=model, resolved_model=model,
                 provider="mock", auth_provider_snapshot="mock",
                 auth_index="mock-auth", source="mock-account",
                 account_snapshot="mock-account", auth_file_snapshot="mock.json",
                 api_key_hash=hashlib.sha256(b"synthetic-api-key").hexdigest(),
                 source_hash=hashlib.sha256(b"synthetic-source").hexdigest(),
                 input_tokens=1000000, output_tokens=0, total_tokens=1000000,
                 failed=False, cache_input_mode="included_in_input")
    if kind == "cache":
        value.update(output_tokens=100000, cache_read_tokens=200000,
                     cache_creation_tokens=100000, total_tokens=1100000)
    if kind == "context":
        value.update(input_tokens=300000, output_tokens=100000, total_tokens=400000)
    if kind == "priority":
        value.update(output_tokens=100000, total_tokens=1100000, service_tier="priority")
    return value


def ingest(label, timestamp):
    events = [event(kind, label, timestamp + i) for i, kind in enumerate(EXPECTED)]
    result = api("POST", "/v0/management/usage/import", events, "import-" + label)
    check(label + " imported events", result["added"], len(events))


def analytics(label, start, end, model=None):
    request = dict(from_ms=start, to_ms=end, now_ms=end, time_zone="UTC",
                   filters={"include_failed": True},
                   include=dict(summary=True, model_stats=True, timeline=True,
                                heatmap=True, channel_share=True, account_stats=True,
                                credential_stats=True, api_key_stats=True,
                                events_page={"limit": 100}, granularity="hour"))
    if model:
        request["filters"]["models"] = [model]
    return api("POST", "/v0/management/monitoring/analytics", request, label)


def verify_window(label, start, end, factor):
    for kind, expected in EXPECTED.items():
        result = analytics(label + "-" + kind, start, end, "mock-" + kind)
        check(label + " " + kind + " historical request cost", result["summary"]["total_cost"], expected * factor)
        check(label + " " + kind + " request count", result["summary"]["total_calls"], 1)


def aggregate(label, expected):
    result = analytics(label, BASE, BASE + DAY)
    check(label + " summary", result["summary"]["total_cost"], expected)
    for field in ["model_stats", "timeline", "heatmap", "channel_share", "account_stats"]:
        check(label + " " + field, sum(row["cost"] for row in result.get(field, [])), expected)
    for field in ["credential_stats", "api_key_stats"]:
        check(label + " " + field, sum(row["cost"] for row in result.get(field, [])), expected)
    dashboard = api("GET", f"/v0/management/dashboard/summary?today_start_ms={BASE}&now_ms={BASE+DAY}", label=label + "-dashboard")
    check(label + " dashboard total", dashboard["today"]["total_cost"], expected)
    check(label + " dashboard channel health", sum(row["cost"] for row in dashboard["channel_health"]), expected)


def db_snapshot(label):
    connection = sqlite3.connect(f"file:{DATA / 'usage.sqlite'}?mode=ro", uri=True)
    connection.row_factory = sqlite3.Row
    result = {}
    for query in [
        "select model,effective_from_ms,price_json from model_price_history order by model,effective_from_ms",
        "select model,timestamp_ms,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,event_hash from usage_events order by timestamp_ms,model",
        "select rollup_name,status,coverage_event_id,target_event_id,structure_revision from usage_pricing_rollup_state",
        "select rollup_name,status,coverage_event_id,target_event_id,structure_revision from usage_monitoring_rollup_state",
    ]:
        result[query] = [dict(row) for row in connection.execute(query)]
    connection.close()
    responses[label] = result


def start(binary):
    docker("run", "--pull", "never", "--detach", "--name", NAME, "--network", "none", "--read-only",
           "--user", f"{os.getuid()}:{os.getgid()}", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
           "--tmpfs", "/tmp:rw,size=64m", "--mount", f"type=bind,src={binary},dst=/app/cpa-manager-plus,readonly",
           "--mount", f"type=bind,src={DATA},dst=/data",
           "-e", "CPA_MANAGER_CONFIG=/data/config.json", "-e", "HTTP_ADDR=127.0.0.1:18317", "-e", "USAGE_DATA_DIR=/data",
           "-e", "USAGE_DB_PATH=/data/usage.sqlite", "-e", "CPA_MANAGER_ADMIN_KEY=" + ADMIN,
           "-e", "USAGE_DASHBOARD_HOURLY_ROLLUP_ENABLED=true",
           "python:3.13-alpine", "/app/cpa-manager-plus")


def lifetime(label, expected, calls, catch_up=False):
    target = dict(row_key="mock-account", auth_file_snapshot="mock.json",
                  auth_index="mock-auth", auth_provider_snapshot="mock", account_snapshot="mock-account")
    result = api("POST", "/v0/management/monitoring/account-history",
                 dict(accounts=[target], catch_up=catch_up), label)
    row = result["items"][0]
    check(label + " lifetime requests", row["total_requests"], calls)
    check(label + " lifetime cost", row["total_cost"], expected)
    window = api("POST", "/v0/management/monitoring/account-window-usage", dict(windows=[dict(
        **target, request_key="window", provider_window_id="test-day", period="current", from_ms=BASE, to_ms=BASE + DAY)]), label + "-window")["items"][0]
    check(label + " quota window cost", window["total_cost"], expected)
    return row["pricing_coverage"]


def verify_reconstruction():
    expected = sum(EXPECTED.values()) * 3 + 1
    coverage = lifetime("lifetime-before-reconstruction", expected, 9)
    check("lifetime undated calls visibly estimated", coverage["estimated_calls"], 5)
    check("lifetime dated calls", coverage["known_calls"], 4)
    coverage = lifetime("lifetime-stored-rollup", expected, 9, catch_up=True)
    correction = book(1, BASE)
    for price in correction.values(): price["historicalCorrection"] = True
    api("PUT", "/v0/management/model-prices", {"prices": correction}, "backdated-correction")
    saved = api("GET", "/v0/management/model-prices", label="after-backdated-current-rates")["prices"]
    check("earlier insertion preserves current prompt", saved["mock-plain"]["prompt"], 2)
    check("earlier insertion preserves current tier", saved["mock-context"]["contextTiers"][0]["prompt"], 4)
    coverage = lifetime("lifetime-reconstructed", expected, 9, catch_up=True)
    check("reconstructed lifetime dated calls", coverage["known_calls"], 9)
    check("reconstructed lifetime estimates cleared", coverage["estimated_calls"], 0)
    # Change only the earlier plain-token interval and retain all unrelated current entries.
    saved["mock-plain"].update(prompt=.5, effectiveFromMs=BASE, historicalCorrection=True)
    api("PUT", "/v0/management/model-prices", {"prices": saved}, "explicit-past-correction")
    expected -= 1 # two older plain requests, each reduced by $0.50
    lifetime("lifetime-corrected", expected, 9, catch_up=True)
    current = api("GET", "/v0/management/model-prices")["prices"]
    check("historical correction keeps later rate", current["mock-plain"]["prompt"], 2)
    refused = dict(current)
    refused["mock-plain"] = dict(prompt=.25, effectiveFromMs=BASE)
    api("PUT", "/v0/management/model-prices", {"prices": refused}, "unacknowledged-correction", expected=400)
    lifetime("rejected-correction-is-atomic", expected, 9)
    delayed = event("plain", "after-reconstruction-delayed", OLD + 20000)
    api("POST", "/v0/management/usage/import", [delayed])
    expected += .5
    lifetime("lifetime-reconstruction-late-tail", expected, 10)
    lifetime("lifetime-reconstruction-late-rollup", expected, 10, catch_up=True)
    docker("restart", NAME); ready()
    lifetime("lifetime-reconstruction-restart", expected, 10)
    # Unknown models and missing token dimensions must never look like verified zero cost.
    unknown = event("plain", "unknown-rate", NEW + 20000)
    unknown.update(model="unknown-test", requested_model="unknown-test", resolved_model="unknown-test")
    missing = event("plain", "missing-details", NEW + 30000)
    missing.update(input_tokens=0, output_tokens=0, total_tokens=1000000)
    api("POST", "/v0/management/usage/import", [unknown, missing])
    # The missing breakdown shares a priced hourly segment: conservatively flag
    # both calls in that segment, plus the separate unknown-model request.
    coverage = lifetime("lifetime-partial", expected, 12, catch_up=True)
    check("lifetime incomplete segments visibly unpriced", coverage["unpriced_calls"], 3)
    summary = analytics("partial-summary", BASE, BASE + DAY)["summary"]
    check("summary incomplete segments visibly unpriced", summary["pricing_coverage"]["unpriced_calls"], 3)
    explicit_estimate = api("GET", "/v0/management/model-prices")["prices"]
    explicit_estimate["mock-plain"].update(effectiveFromMs=BASE, historicalCorrection=True, estimated=True, prompt=.5)
    api("PUT", "/v0/management/model-prices", {"prices": explicit_estimate})
    coverage = lifetime("lifetime-explicit-estimate", expected, 12)
    check("explicit historical estimate remains visible", coverage["estimated_calls"], 3)


failure = None
try:
    start(INITIAL_BINARY)
    ready()
    api("PUT", "/v0/management/model-prices", {"prices": book(1)}, "initial-prices")
    ingest("old", OLD)
    verify_window("before-update", OLD, OLD + 1000, 1)
    aggregate("before-update-all", sum(EXPECTED.values()))
    db_snapshot("db-before-update")
    api("PUT", "/v0/management/model-prices", {"prices": book(2, CHANGE)}, "updated-prices")
    verify_window("after-update-old", OLD, OLD + 1000, 1)
    # Import an older request *after* updating prices to test event-time selection.
    delayed = event("plain", "delayed", OLD + 10000)
    api("POST", "/v0/management/usage/import", [delayed], "import-delayed")
    late = analytics("delayed-old-request", OLD + 10000, OLD + 11000, "mock-plain")
    check("late import uses old rate", late["summary"]["total_cost"], 1)
    ingest("new", NEW)
    verify_window("after-update-new", NEW, NEW + 1000, 2)
    aggregate("after-update-all", sum(EXPECTED.values()) * 3 + 1)
    db_snapshot("db-after-update")
    if INITIAL_BINARY != BINARY:
        responses["before-upgrade-lifetime"] = api("POST", "/v0/management/monitoring/account-history", dict(
            catch_up=True, accounts=[dict(row_key="mock-account", auth_file_snapshot="mock.json", auth_index="mock-auth", auth_provider_snapshot="mock", account_snapshot="mock-account")]))
        docker("rm", "--force", NAME)
        start(BINARY)
    else:
        docker("restart", NAME)
    ready()
    verify_window("after-restart-old", OLD, OLD + 1000, 1)
    verify_window("after-restart-new", NEW, NEW + 1000, 2)
    aggregate("after-restart-all", sum(EXPECTED.values()) * 3 + 1)
    api("GET", "/v0/management/model-prices", label="prices-after-restart")
    db_snapshot("db-after-restart")
    verify_reconstruction()
    db_snapshot("db-after-reconstruction")
except Exception:
    failure = traceback.format_exc()
    print(failure, file=sys.stderr)
finally:
    log = docker("logs", NAME, check=False)
    (ROOT / "container.log").write_text(log.stdout + log.stderr)
    metadata = docker("inspect", NAME, check=False)
    if metadata.returncode == 0:
        obj = json.loads(metadata.stdout)[0]
        responses["isolation"] = dict(image=obj["Image"], network=obj["HostConfig"]["NetworkMode"], readonly=obj["HostConfig"]["ReadonlyRootfs"], ports=obj["NetworkSettings"]["Ports"])
    docker("rm", "--force", NAME, check=False)
    report = dict(passed=failure is None and all(row["passed"] for row in checks),
                  binary_sha256=hashlib.sha256(BINARY.read_bytes()).hexdigest(),
                  source_commit=os.environ.get("CPAMP_SOURCE_COMMIT"),
                  upgrade_from_sha256=hashlib.sha256(INITIAL_BINARY.read_bytes()).hexdigest() if INITIAL_BINARY != BINARY else None,
                  tested_at=dt.datetime.now(dt.timezone.utc).isoformat(),
                  rate_change_ms=CHANGE, checks=checks, failure=failure,
                  inference_requests=0, responses=responses)
    (ROOT / "results.json").write_text(json.dumps(report, indent=2) + "\n")
    print(f"Results: {ROOT / 'results.json'}", flush=True)
sys.exit(0 if report["passed"] else 1)
