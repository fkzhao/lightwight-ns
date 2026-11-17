"""Resolve services through a machine-local agent over JSON-RPC 2.0."""

import json
import socket
import threading
from dataclasses import dataclass
from typing import List, Optional, Tuple


@dataclass(frozen=True)
class Endpoint:
    address: str
    port: int


@dataclass(frozen=True)
class AgentStatus:
    ready: bool
    registry_version: int
    last_sync_at: str


class AgentRPCError(RuntimeError):
    def __init__(self, code: int, message: str):
        self.code = code
        super().__init__(message)


class DiscoveryClient:
    """A reusable, thread-safe connection to a local agent."""

    def __init__(self, address: Tuple[str, int] = ("127.0.0.1", 9091),
                 unix_socket: Optional[str] = None, timeout: float = 3.0):
        if unix_socket is None:
            self._socket = socket.create_connection(address, timeout)
        else:
            self._socket = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            self._socket.settimeout(timeout)
            self._socket.connect(unix_socket)
        self._stream = self._socket.makefile("rwb")
        self._lock = threading.Lock()
        self._next_id = 0

    def _call(self, method: str, params: dict) -> dict:
        with self._lock:
            self._next_id += 1
            request = {"jsonrpc": "2.0", "id": self._next_id,
                       "method": method, "params": params}
            self._stream.write((json.dumps(request, separators=(",", ":")) + "\n").encode("utf-8"))
            self._stream.flush()
            line = self._stream.readline(1 << 20)
            if not line or not line.endswith(b"\n"):
                raise ConnectionError("agent response is missing or too large")
            response = json.loads(line)
            if response.get("jsonrpc") != "2.0" or response.get("id") != self._next_id:
                raise ValueError("invalid agent RPC response")
            if "error" in response:
                error = response["error"]
                raise AgentRPCError(error["code"], error["message"])
            return response["result"]

    def resolve(self, service_name: str) -> List[Endpoint]:
        result = self._call("discovery.resolve", {"service": service_name})
        return [Endpoint(item["address"], item["port"]) for item in result["endpoints"]]

    def agent_status(self) -> AgentStatus:
        result = self._call("agent.status", {})
        return AgentStatus(result["ready"], result["registryVersion"], result["lastSyncAt"])

    def close(self) -> None:
        self._stream.close()
        self._socket.close()

    def __enter__(self):
        return self

    def __exit__(self, _type, _value, _traceback):
        self.close()
