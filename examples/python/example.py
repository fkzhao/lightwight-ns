"""Use the Python SDK for discovery or a renewable registration."""

import argparse
import os
import signal
import threading

from lightwight_ns import DiscoveryClient, RegistrationClient


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=["discover", "register"])
    parser.add_argument("service")
    parser.add_argument("address", nargs="?")
    parser.add_argument("port", type=int, nargs="?")
    args = parser.parse_args()
    if args.action == "discover":
        host, port = os.getenv("NS_AGENT_TCP_ADDR", "127.0.0.1:9091").rsplit(":", 1)
        with DiscoveryClient((host, int(port))) as agent:
            print(agent.resolve(args.service))
        return
    if args.address is None or args.port is None:
        parser.error("register requires ADDRESS PORT")
    stop = threading.Event()
    signal.signal(signal.SIGINT, lambda *_: stop.set())
    signal.signal(signal.SIGTERM, lambda *_: stop.set())
    center = RegistrationClient(os.getenv("NS_CENTER_HTTP_URL", "http://127.0.0.1:8080"))
    with center.register(args.service, args.address, args.port) as lease:
        print("registered", lease.instance_id, flush=True)
        stop.wait()


if __name__ == "__main__":
    main()
