# server.py
import http.server
import ssl
from pathlib import Path
import sys

CERT = Path("../certs/localhost+2.pem")
KEY = Path("../certs/localhost+2-key.pem")

if not CERT.exists() or not KEY.exists():
    print(f"Cannot find {CERT} and/or {KEY}", file=sys.stderr)
    sys.exit(1)

server_address = ("", 8080)
httpd = http.server.HTTPServer(server_address, http.server.SimpleHTTPRequestHandler)

context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
context.load_cert_chain(certfile=str(CERT), keyfile=str(KEY))
httpd.socket = context.wrap_socket(httpd.socket, server_side=True)

print(
    f"Serving HTTPS on https://{server_address[0] or 'localhost'}:{server_address[1]}"
)
httpd.serve_forever()
