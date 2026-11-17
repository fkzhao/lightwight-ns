"""Register an instance with the registry center using a renewable lease."""

import json
import secrets
import threading
import urllib.error
import urllib.request
from typing import Optional


class RegistrationError(RuntimeError):
    def __init__(self, status: int, code: str):
        self.status = status
        self.code = code
        super().__init__("registration API {}: {}".format(status, code))


class RegistrationClient:
    def __init__(self, center_url: str = "http://127.0.0.1:8080", timeout: float = 3.0):
        self.center_url = center_url.rstrip("/")
        self.timeout = timeout

    def _request(self, method: str, path: str, body: dict) -> dict:
        request = urllib.request.Request(
            self.center_url + path,
            data=json.dumps(body).encode("utf-8"),
            headers={"Content-Type": "application/json"},
            method=method,
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                if response.status == 204:
                    return {}
                return json.load(response)
        except urllib.error.HTTPError as error:
            try:
                code = json.load(error).get("code", "unknown_error")
            except (ValueError, OSError):
                code = "unknown_error"
            raise RegistrationError(error.code, code) from error

    def register(self, service_name: str, address: str, port: int) -> "RegistrationLease":
        body = {"serviceName": service_name, "address": address,
                "port": port, "token": secrets.token_hex(32)}
        result = self._request("POST", "/api/registrations", body)
        return RegistrationLease(self, body, result)


class RegistrationLease:
    """Renew in the background and deregister when closed."""

    def __init__(self, client: RegistrationClient, body: dict, result: dict):
        self._client = client
        self._body = body
        self._instance_id = result["instanceId"]
        self._last_renewal_error: Optional[Exception] = None
        self._lock = threading.Lock()
        self._close_lock = threading.Lock()
        self._stop = threading.Event()
        self._closed = False
        interval = max(result["leaseTtlSeconds"] / 3.0, 1.0)
        self._worker = threading.Thread(target=self._renew_loop, args=(interval,), daemon=True)
        self._worker.start()

    @property
    def instance_id(self) -> str:
        with self._lock:
            return self._instance_id

    @property
    def last_renewal_error(self) -> Optional[Exception]:
        with self._lock:
            return self._last_renewal_error

    def _renew_loop(self, interval: float) -> None:
        while not self._stop.wait(interval):
            try:
                result = self._client._request("POST", "/api/registrations", self._body)
                with self._lock:
                    self._instance_id = result["instanceId"]
                    self._last_renewal_error = None
            except Exception as error:
                with self._lock:
                    self._last_renewal_error = error

    def close(self) -> None:
        with self._close_lock:
            if self._closed:
                return
            self._stop.set()
            self._worker.join(timeout=self._client.timeout + 1)
            self._client._request("DELETE", "/api/registrations/" + self.instance_id,
                                  {"token": self._body["token"]})
            self._closed = True

    def __enter__(self) -> "RegistrationLease":
        return self

    def __exit__(self, _type, _value, _traceback) -> None:
        self.close()
