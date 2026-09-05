import socket
import sys
import os
import subprocess
import time
import requests

BASE_URL = os.getenv("BASE_URL", "http://localhost")
COMPOSE_PROJECT = os.getenv("COMPOSE_PROJECT_NAME", "smartgrid")

SERVICES = {
    "nginx": {"host": "localhost", "port": 3001, "type": "http", "path": "/"},
    "mosquitto": {"host": "localhost", "port": 1883, "type": "tcp"},
    "influxdb3": {"host": "localhost", "port": 8181, "type": "http", "path": "/health"},
    "postgres": {"host": "localhost", "port": 5432, "type": "tcp"},
    "grafana": {"host": "localhost", "port": 3000, "type": "http", "path": "/api/health"},
    "prometheus": {"host": "localhost", "port": 9090, "type": "http", "path": "/-/ready"},
    "cadvisor": {"host": "localhost", "port": 8080, "type": "http", "path": "/"},
    "postgres-exporter": {"host": "localhost", "port": 9187, "type": "http", "path": "/metrics"},
}


def check_docker_running():
    try:
        result = subprocess.run(
            ["docker", "ps", "--format", "{{.Names}}"],
            capture_output=True, text=True, check=True
        )
        containers = [c.strip() for c in result.stdout.splitlines() if c.strip()]
        expected = [f"{COMPOSE_PROJECT}-{s}" for s in SERVICES]
        missing = [c for c in expected if c not in containers]
        if missing:
            print(f"FAIL: Docker containers not running: {missing}")
            return False
        print("PASS: All Docker containers are running")
        return True
    except (subprocess.CalledProcessError, FileNotFoundError) as e:
        print(f"FAIL: Docker check failed: {e}")
        return False


def check_tcp(host, port, timeout=5):
    try:
        with socket.create_connection((host, port), timeout=timeout):
            return True
    except (socket.timeout, ConnectionRefusedError, OSError):
        return False


def check_http(url, timeout=5):
    try:
        resp = requests.get(url, timeout=timeout)
        return resp.status_code < 500
    except requests.RequestException:
        return False


def check_service(name, config):
    host = config["host"]
    port = config["port"]
    if config["type"] == "tcp":
        if check_tcp(host, port):
            print(f"PASS: {name} is reachable on {host}:{port}")
            return True
        else:
            print(f"FAIL: {name} is NOT reachable on {host}:{port}")
            return False
    elif config["type"] == "http":
        url = f"{BASE_URL}:{port}{config.get('path', '/')}"
        if check_http(url):
            print(f"PASS: {name} HTTP OK ({url})")
            return True
        else:
            print(f"FAIL: {name} HTTP failed ({url})")
            return False
    return False


def main():
    results = []
    print("=" * 50)
    print("SmartGrid Environment Test")
    print("=" * 50)

    results.append(("Docker containers", check_docker_running()))
    time.sleep(1)

    for name, config in SERVICES.items():
        results.append((name, check_service(name, config)))
        time.sleep(0.2)

    print("=" * 50)
    passed = sum(1 for _, r in results if r)
    total = len(results)
    print(f"Result: {passed}/{total} passed")

    if passed < total:
        sys.exit(1)
    sys.exit(0)


if __name__ == "__main__":
    main()
