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

        java_classpath = None
        if "--java" in sys.argv:
            classpath_file = temp / "java-classpath.txt"
            subprocess.run(["mvn", "-q", "-f", "sdk/java/pom.xml", "package", "dependency:build-classpath",
                            "-Dmdep.outputFile=" + str(classpath_file)], cwd=ROOT, check=True)
            java_classpath = str(ROOT / "sdk/java/target/classes") + os.pathsep + classpath_file.read_text().strip()
            subprocess.run(["javac", "-cp", java_classpath, "-d", str(temp), "examples/java/FeatureCheck.java"], cwd=ROOT, check=True)
            java_classpath = str(temp) + os.pathsep + java_classpath

        http_port, rpc_port, agent_port, service_port = [free_port() for _ in range(4)]
        socket_path = temp / "agent.sock"
        base = "http://127.0.0.1:{}".format(http_port)
        center_env = {**os.environ, "NS_DB_FILE": str(temp / "registry.db"),
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
                with RegistrationClient(base).register("e2e-orders", "127.0.0.1", service_port, version="v2", weight=75, metadata={"zone": "a"}) as lease:
                    wait_for(lambda: len(client.resolve("e2e-orders")) == 1)
                    output = subprocess.check_output([str(binaries["discover"]), "-tcp", "e2e-orders"],
                                                     env=agent_env, text=True)
                    assert "127.0.0.1:{}".format(service_port) in output
                    detailed = client.resolve_v2("e2e-orders", version="v2", metadata={"zone": "a"})
                    assert len(detailed.endpoints) == 1 and detailed.endpoints[0].weight == 75
                    assert client.resolve_v2("e2e-orders", version="v1").endpoints == []
                    listener.close()
                    listener = None
                    wait_for(lambda: client.resolve("e2e-orders") == [])
                    listener = socket.socket()
                    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                    listener.bind(("127.0.0.1", service_port))
                    listener.listen()
                    wait_for(lambda: len(client.resolve("e2e-orders")) == 1)
                    lease.drain()
                    wait_for(lambda: client.resolve("e2e-orders") == [])
                    assert request(base + "/api/snapshot")["services"][0]["instances"][0]["lifecycle"] == "draining"
                wait_for(lambda: client.resolve("e2e-orders") == [])
            service_id = request(base + "/api/snapshot")["services"][0]["id"]
            request(base + "/api/services/" + service_id + "/instances",
                    {"address": "127.0.0.1", "port": service_port, "enabled": True})
            center.kill()
            center.wait(timeout=3)
            center = subprocess.Popen([str(binaries["center"])], env=center_env,
                                      stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            wait_for(lambda: request(base + "/api/health")["status"] == "ok")
            restored = request(base + "/api/snapshot")["services"][0]
            assert restored["id"] == service_id and len(restored["instances"]) == 1
            with DiscoveryClient(("127.0.0.1", agent_port)) as client:
                wait_for(lambda: len(client.resolve("e2e-orders")) == 1)
            if java_classpath:
                request(base + "/api/services", {"name": "e2e-scoped", "namespace": "prod", "group": "blue",
                                                "healthCheck": {"type": "tcp", "path": ""}})
                with DiscoveryClient(("127.0.0.1", agent_port)) as client:
                    wait_for(lambda: any(item["name"] == "e2e-scoped" for item in request(base + "/api/snapshot")["services"]))
                    expected_version = request(base + "/api/snapshot")["version"]
                    wait_for(lambda: client.agent_status().registry_version >= expected_version)
                subprocess.run(["java", "-cp", java_classpath, "FeatureCheck"], cwd=ROOT,
                               env={**os.environ, "NS_CENTER_HTTP_URL": base, "NS_TEST_AGENT_PORT": str(agent_port)}, check=True, timeout=20)
            print("center, agent, lease, health removal/recovery, Go/Python discovery, and SQLite crash recovery: OK")
        finally:
            if listener is not None:
                listener.close()
            stop(agent)
            stop(center)


if __name__ == "__main__":
    main()
