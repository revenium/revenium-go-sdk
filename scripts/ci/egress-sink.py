#!/usr/bin/env python3
"""A forward proxy that sends nothing on: it records every request a test
makes through it and refuses each one, so a test run can prove that no
request left the machine."""
import socketserver
import sys
import threading

REFUSAL = b"HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"


def main(log_path, port_path):
    lock = threading.Lock()

    class Recorder(socketserver.StreamRequestHandler):
        def handle(self):
            request_line = self.rfile.readline(65536).decode("latin-1").strip()
            if not request_line:
                return
            with lock, open(log_path, "a", encoding="utf-8") as log:
                log.write(request_line + "\n")
            self.wfile.write(REFUSAL)

    socketserver.ThreadingTCPServer.daemon_threads = True
    with socketserver.ThreadingTCPServer(("127.0.0.1", 0), Recorder) as server:
        open(log_path, "a", encoding="utf-8").close()
        with open(port_path, "w", encoding="utf-8") as port_file:
            port_file.write(str(server.server_address[1]))
        server.serve_forever()


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
