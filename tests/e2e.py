"""Exercise center, agent, health checks, leases, and discovery on isolated ports."""

import json
import os
import pathlib
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request


ROOT = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "sdk/python"))
from lightwight_ns import DiscoveryClient, RegistrationClient  # noqa: E402


def free_port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def wait_for(condition, timeout=12):
    end = time.monotonic() + timeout
    last_error = None
    while time.monotonic() < end:
        try:
            if condition():
                return
        except Exception as error:
            last_error = error
        time.sleep(0.2)
    raise AssertionError("timed out waiting for condition: {}".format(last_error))


def request(url, body=None):
    data = None if body is None else json.dumps(body).encode("utf-8")
    req = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=2) as response:
        return json.load(response)


def stop(process):
    if process is None:
        return
    process.terminate()
    try:
        process.wait(timeout=3)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait()


def main():
    with tempfile.TemporaryDirectory() as directory:
        temp = pathlib.Path(directory)
        binaries = {}
        for name, package in (("center", "./core/registry-center"),
                              ("agent", "./core/agent"),
                              ("discover", "./tools/discover")):
            binaries[name] = temp / name
            subprocess.run(["go", "build", "-o", str(binaries[name]), package], cwd=ROOT, check=True)

        http_port, rpc_port, agent_port, service_port = [free_port() for _ in range(4)]
        socket_path = temp / "agent.sock"
        base = "http://127.0.0.1:{}".format(http_port)
        center_env = {**os.environ, "NS_DATA_FILE": str(temp / "registry.json"),
                      "NS_HTTP_ADDR": "127.0.0.1:{}".format(http_port),
                      "NS_RPC_ADDR": "127.0.0.1:{}".format(rpc_port),
                      "NS_HEALTH_INTERVAL": "200ms", "NS_WEB_DIST": str(temp / "missing")}
        agent_env = {**os.environ, "NS_CENTER_RPC_ADDR": "127.0.0.1:{}".format(rpc_port),
                     "NS_AGENT_TCP_ADDR": "127.0.0.1:{}".format(agent_port),
                     "NS_AGENT_SOCKET": str(socket_path), "NS_SYNC_INTERVAL": "200ms"}
        center = subprocess.Popen([str(binaries["center"])], env=center_env, stdout=subprocess.DEVNULL,
                                  stderr=subprocess.DEVNULL)
        agent = None
        listener = None
        try:
            wait_for(lambda: request(base + "/api/health")["status"] == "ok")
            request(base + "/api/services", {"name": "e2e-orders", "description": "",
                                               "healthCheck": {"type": "tcp", "path": ""}})
            agent = subprocess.Popen([str(binaries["agent"])], env=agent_env, stdout=subprocess.DEVNULL,
                                     stderr=subprocess.DEVNULL)
            wait_for(socket_path.exists)
            with DiscoveryClient(("127.0.0.1", agent_port)) as client:
                wait_for(lambda: client.agent_status().ready)
                assert client.resolve("e2e-orders") == []
                listener = socket.socket()
                listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                listener.bind(("127.0.0.1", service_port))
                listener.listen()
                with RegistrationClient(base).register("e2e-orders", "127.0.0.1", service_port):
                    wait_for(lambda: len(client.resolve("e2e-orders")) == 1)
                    output = subprocess.check_output([str(binaries["discover"]), "-tcp", "e2e-orders"],
                                                     env=agent_env, text=True)
                    assert "127.0.0.1:{}".format(service_port) in output
                    listener.close()
                    listener = None
                    wait_for(lambda: client.resolve("e2e-orders") == [])
                    listener = socket.socket()
                    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                    listener.bind(("127.0.0.1", service_port))
                    listener.listen()
                    wait_for(lambda: len(client.resolve("e2e-orders")) == 1)
                wait_for(lambda: client.resolve("e2e-orders") == [])
            print("center, agent, lease, health removal/recovery, and Go/Python discovery: OK")
        finally:
            if listener is not None:
                listener.close()
            stop(agent)
            stop(center)


if __name__ == "__main__":
    main()
