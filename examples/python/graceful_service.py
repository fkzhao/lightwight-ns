"""A signal-aware HTTP service that drains discovery before waiting for requests."""

import os
import signal
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from lightwight_ns import RegistrationClient


class Server(ThreadingHTTPServer):
    daemon_threads = False

    def get_request(self):
        connection, address = super().get_request()
        connection.settimeout(5)
        return connection, address


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(204 if self.path == "/health" else 200)
        self.end_headers()
        if self.path != "/health":
            self.wfile.write(b"hello\n")


def main():
    server = Server((os.getenv("NS_SERVICE_IP", "127.0.0.1"), int(os.getenv("NS_SERVICE_PORT", "8081"))), Handler)
    stop = threading.Event()
    signal.signal(signal.SIGINT, lambda *_: stop.set())
    signal.signal(signal.SIGTERM, lambda *_: stop.set())
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    client = RegistrationClient(os.getenv("NS_CENTER_HTTP_URL", "http://127.0.0.1:8080"))
    try:
        with client.register("order-service", server.server_address[0], server.server_port) as lease:
            stop.wait()
            try:
                lease.drain()
            finally:
                server.shutdown()
                server.server_close()
                worker.join()
    finally:
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    main()
