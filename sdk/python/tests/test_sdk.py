"""Protocol-level tests for both Python SDK clients."""

import json
import socketserver
import threading
import tempfile
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from lightwight_ns import DiscoveryClient, RegistrationClient, pick_weighted


class AgentHandler(socketserver.StreamRequestHandler):
    def handle(self):
        for line in self.rfile:
            request = json.loads(line)
            if request["method"] == "agent.status":
                result = {"ready": True, "registryVersion": 7, "lastSyncAt": "2026-01-01T00:00:00Z"}
            elif request["method"] == "discovery.resolveV2":
                result = {"registryVersion": 7, "stale": False, "endpoints": [{"instanceId": "one", "address": "127.0.0.1", "port": 8081, "weight": 25, "version": "v2", "metadata": {"zone": "a"}}]}
            else:
                result = {"registryVersion": 7, "endpoints": [{"address": "127.0.0.1", "port": 8081}]}
            self.wfile.write((json.dumps({"jsonrpc": "2.0", "id": request["id"], "result": result}) + "\n").encode())


class CenterHandler(BaseHTTPRequestHandler):
    requests = []

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        self.requests.append((self.command, self.path, body))
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"instanceId":"instance-1","leaseTtlSeconds":30}')

    def do_DELETE(self):
        self.do_POST()

    def log_message(self, *_args):
        pass


class SDKTest(unittest.TestCase):
    def test_discovery(self):
        with socketserver.ThreadingTCPServer(("127.0.0.1", 0), AgentHandler) as server:
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            with DiscoveryClient(server.server_address) as client:
                self.assertEqual(client.resolve("orders")[0].port, 8081)
                self.assertEqual(client.agent_status().registry_version, 7)
                result = client.resolve_v2("orders", namespace="prod", version="v2")
                self.assertEqual(pick_weighted(result.endpoints).weight, 25)
                self.assertEqual(result.endpoints[0].metadata["zone"], "a")
            server.shutdown()
            thread.join()

    def test_unix_discovery(self):
        with tempfile.TemporaryDirectory() as directory:
            path = directory + "/agent.sock"
            with socketserver.ThreadingUnixStreamServer(path, AgentHandler) as server:
                thread = threading.Thread(target=server.serve_forever, daemon=True)
                thread.start()
                with DiscoveryClient(unix_socket=path) as client:
                    self.assertEqual(client.resolve("orders")[0].address, "127.0.0.1")
                server.shutdown()
                thread.join()

    def test_registration_and_deregistration(self):
        CenterHandler.requests = []
        with ThreadingHTTPServer(("127.0.0.1", 0), CenterHandler) as server:
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            client = RegistrationClient("http://127.0.0.1:{}".format(server.server_port))
            with client.register("orders", "127.0.0.1", 8081, namespace="prod", version="v2", weight=25) as lease:
                self.assertEqual(lease.instance_id, "instance-1")
                lease.drain()
            self.assertEqual([item[0] for item in CenterHandler.requests], ["POST", "POST", "DELETE"])
            self.assertEqual(CenterHandler.requests[0][2]["namespace"], "prod")
            self.assertTrue(CenterHandler.requests[1][1].endswith("/drain"))
            self.assertEqual(CenterHandler.requests[0][2]["token"], CenterHandler.requests[1][2]["token"])
            server.shutdown()
            thread.join()


if __name__ == "__main__":
    unittest.main()
