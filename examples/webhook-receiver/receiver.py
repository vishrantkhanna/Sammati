"""Receives Sammati webhooks and checks their signature. Python 3.9 or later.

    SAMMATI_WEBHOOK_SECRET=whsec_... python receiver.py
"""
import hashlib
import hmac
import json
import os
import re
import time
from http.server import BaseHTTPRequestHandler, HTTPServer

SECRET = os.environ["SAMMATI_WEBHOOK_SECRET"].encode()


def valid(header: str, body: bytes) -> bool:
    m = re.fullmatch(r"t=(\d+),v1=([0-9a-f]{64})", header or "")
    if not m or abs(time.time() - int(m[1])) > 300:  # older than five minutes
        return False
    want = hmac.new(SECRET, m[1].encode() + b"." + body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(want, m[2])


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        if not valid(self.headers.get("X-Sammati-Signature"), body):
            self.send_response(401)
            self.end_headers()
            return
        event = json.loads(body)
        for record in event["records"]:
            # Update your own system here, for example unsubscribe on withdraw.
            print(event["event"], event["subject"][:10], record["purpose"])
        self.send_response(204)
        self.end_headers()


if __name__ == "__main__":
    HTTPServer(("0.0.0.0", 3000), Handler).serve_forever()
