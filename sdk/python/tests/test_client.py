"""The Python SDK against a fake API (PRD-001 FR-INT-17): req FR-INT-17, FR-INT-08, FR-INT-16, FR-INT-07, FR-INT-09, FR-INT-10, FR-INT-14."""
import json
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

from fyniq import Client, Problem
from fyniq.operations_gen import OPERATIONS


class Fake(HTTPServer):
    def __init__(self, answer):
        super().__init__(("127.0.0.1", 0), _Handler)
        self.answer, self.seen = answer, []


class _Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def _serve(self):
        n = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(n).decode() if n else ""
        self.server.seen.append({"method": self.command, "path": self.path, "key": self.headers.get("Idempotency-Key"), "match": self.headers.get("If-Match"),
                                 "prefer": self.headers.get("Prefer"), "auth": self.headers.get("Authorization"), "body": body})
        status, headers, payload = self.server.answer(len(self.server.seen), self)
        self.send_response(status)
        for k, v in headers.items():
            self.send_header(k, v)
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = _serve


def serve(answer):
    fake = Fake(answer)
    threading.Thread(target=fake.serve_forever, daemon=True).start()
    return fake, Client(f"http://127.0.0.1:{fake.server_port}", "tok", sleep=lambda s: None)


class ClientTests(unittest.TestCase):
    def test_a_write_keeps_its_idempotency_key_across_retries(self):
        def answer(n, h):
            if n == 1:
                return 429, {"Retry-After": "1"}, b"{}"
            return 201, {}, b'{"id": "x"}'
        fake, client = serve(answer)
        r = client.create_journal({"memo": "x"})
        fake.shutdown()
        a, b = fake.seen
        self.assertEqual(r.status, 201)
        self.assertTrue(a["key"] and a["key"] == b["key"])
        self.assertEqual((a["auth"], a["path"], a["method"]), ("Bearer tok", "/v1/journals", "POST"))

    def test_a_write_is_not_retried_after_a_503_nor_after_a_long_wait(self):
        fake, client = serve(lambda n, h: (503, {"Retry-After": "1"}, b"{}"))
        with self.assertRaises(Problem):
            client.create_journal({})
        fake.shutdown()
        self.assertEqual(len(fake.seen), 1)
        fake, client = serve(lambda n, h: (429, {"Retry-After": "3600"}, b"{}"))
        with self.assertRaises(Problem) as cm:
            client.create_journal({})
        fake.shutdown()
        self.assertEqual((cm.exception.status, len(fake.seen)), (429, 1))

    def test_a_refusal_is_a_problem_with_its_code_and_fix(self):
        body = json.dumps({"status": 422, "code": "API-INPUT-02", "title": "A field is not valid", "detail": "one field", "fix": "Correct it",
                           "errors": [{"field": "body.memo", "message": "is empty"}]}).encode()
        fake, client = serve(lambda n, h: (422, {"Content-Type": "application/problem+json"}, body))
        with self.assertRaises(Problem) as cm:
            client.create_journal({})
        fake.shutdown()
        p = cm.exception
        self.assertEqual((p.code, p.fix, p.errors[0]["field"]), ("API-INPUT-02", "Correct it", "body.memo"))

    def test_etag_preview_and_pagination(self):
        def answer(n, h):
            if h.command == "GET" and "cursor=c2" in h.path:
                return 200, {}, b'{"items": [{"id": "3"}]}'
            if h.command == "GET" and h.path.startswith("/v1/journals?") or h.path == "/v1/journals":
                return 200, {}, b'{"items": [{"id": "1"}, {"id": "2"}], "next_cursor": "c2"}'
            return 200, {"ETag": '"3"', "Preference-Applied": "return=preview"}, b"{}"
        fake, client = serve(answer)
        ids = [i["id"] for i in client.paginate(client.list_journals)]
        r = client.get_journal("abc", preview=True, if_match='"2"')
        fake.shutdown()
        self.assertEqual(ids, ["1", "2", "3"])
        last = fake.seen[-1]
        self.assertEqual((r.etag, r.previewed, last["prefer"], last["match"]), ('"3"', True, "return=preview", '"2"'))

    def test_only_the_operations_a_clients_scopes_reach_are_here(self):
        for never in ("approveApproval", "createWebhookEndpoint", "listAuditEvents", "createPersonalAccessToken", "requestTenantExport"):
            self.assertNotIn(never, OPERATIONS)
        self.assertIn("fyniq.read.reports", OPERATIONS["queryMetrics"]["scopes"])
        self.assertGreater(len(OPERATIONS), 50)


if __name__ == "__main__":
    unittest.main()
