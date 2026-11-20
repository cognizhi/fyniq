"""The client of the Fyniq /v1 API (PRD-001 FR-INT-17). The operations a client's scopes can reach are generated from contracts/openapi/fyniq.v1.yaml
(operations_gen.py); this file is the conventions of section 8.18.1:

- an Idempotency-Key on every state-changing call that takes one, made once per call and kept across its retries (FR-INT-08);
- a retry of a 429 (any call: a refused request saved nothing) or of a read's 503, after the Retry-After the API names when that is short (FR-INT-16);
- the RFC 9457 problem of a refusal as a Problem exception with its stable code and its fix (FR-INT-07);
- the ETag of a read for If-Match (FR-INT-09), Prefer: return=preview (FR-INT-14) and cursor-paginated lists (FR-INT-10).

It holds no credential of its own: the token is the caller's (an integration client's or a personal access token's, FR-INT-02), sent to the base URL only. Money is a
decimal string in the JSON the API sends (ENG-10): parse it with decimal.Decimal, never float (json.loads(..., parse_float=decimal.Decimal) for any number that is not a string).
"""
from __future__ import annotations

import json
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from email.utils import parsedate_to_datetime
from typing import Any, Callable, Iterator

from .operations_gen import OPERATIONS, OperationsMixin


def new_key() -> str:
    """An Idempotency-Key: send the same one when a call is retried, so it runs at most once (FR-INT-08)."""
    return str(uuid.uuid4())


class Response:
    """An answer, whatever its status."""

    def __init__(self, status: int, headers: dict[str, str], body: bytes):
        self.status, self.headers, self.body = status, headers, body

    def json(self) -> Any:
        return json.loads(self.body or b"null")

    @property
    def etag(self) -> str | None:
        """The version of the record the answer shows, for if_match (FR-INT-09)."""
        return self.headers.get("etag")

    @property
    def previewed(self) -> bool:
        """Whether the call was a preview: nothing was saved (FR-INT-14)."""
        return "return=preview" in self.headers.get("preference-applied", "")

    @property
    def rate_limit(self) -> str | None:
        return self.headers.get("ratelimit")


class Problem(Exception):
    """The RFC 9457 problem details of a refusal, with the API's stable code and the plain-words fix (FR-INT-07)."""

    def __init__(self, status: int, body: dict[str, Any], response: Response):
        self.status, self.response = status, response
        self.code = body.get("code", "UNKNOWN")
        self.title, self.detail, self.fix = body.get("title", ""), body.get("detail", ""), body.get("fix", "")
        self.errors: list[dict[str, Any]] = body.get("errors") or []
        self.request_id = body.get("request_id")
        super().__init__(f"fyniq: {status} {self.code}: {self.detail} ({self.fix})")


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args: Any, **kwargs: Any) -> None:  # a redirect would send the token to another host
        return None


class Client(OperationsMixin):
    """Calls the API. token is the access token or a function returning a fresh one; sleep is for tests."""

    def __init__(self, base_url: str, token: str | Callable[[], str] | None = None, *, max_retries: int = 2, max_wait: float = 10.0,
                 idempotency_keys: bool = True, timeout: float = 60.0, sleep: Callable[[float], None] = time.sleep):
        self.base_url = base_url.rstrip("/")
        self._token = token
        self.max_retries, self.max_wait, self.idempotency_keys, self.timeout, self._sleep = max_retries, max_wait, idempotency_keys, timeout, sleep
        self._opener = urllib.request.build_opener(_NoRedirect)

    def call(self, operation_id: str, path_args: list[str], body: Any = None, *, query: dict[str, Any] | None = None, if_match: str | None = None,
             preview: bool = False, idempotency_key: str | None = None) -> Response:
        """Calls an operation by ID with its path arguments in the path's order. A refusal raises Problem."""
        op = OPERATIONS[operation_id]
        if len(path_args) != len(op["path_params"]):
            raise TypeError(f"{operation_id} takes {len(op['path_params'])} path arguments, got {len(path_args)}")
        path = op["path"]
        for name, value in zip(op["path_params"], path_args):
            path = path.replace("{" + name + "}", urllib.parse.quote(str(value), safe=""), 1)
        url = self.base_url + path
        if query:
            url += "?" + urllib.parse.urlencode({k: v for k, v in query.items() if v is not None}, doseq=True)
        data = None if body is None else (body if isinstance(body, bytes) else json.dumps(body).encode())
        write = op["method"] != "GET"
        key = idempotency_key
        if write and key is None and op["idempotency_key"] and self.idempotency_keys and not preview:
            key = new_key()  # once for the call: every retry sends the same one
        attempt = 0
        while True:
            req = urllib.request.Request(url, data=data, method=op["method"])
            req.add_header("Accept", "application/json")
            if data is not None:
                req.add_header("Content-Type", "application/json")
            token = self._token() if callable(self._token) else self._token
            if token:
                req.add_header("Authorization", "Bearer " + token)
            if key:
                req.add_header("Idempotency-Key", key)
            if if_match:
                req.add_header("If-Match", if_match)
            if preview:
                req.add_header("Prefer", "return=preview")
            response = self._send(req)
            wait = _retry_after(response.headers)
            retryable = response.status == 429 or (response.status == 503 and not write)
            if retryable and wait is not None and attempt < self.max_retries and wait <= self.max_wait:
                self._sleep(wait)
                attempt += 1
                continue
            if response.status >= 400:
                try:
                    body_json = response.json()
                except ValueError:
                    body_json = {"detail": response.body.decode(errors="replace")[:200]}
                raise Problem(response.status, body_json if isinstance(body_json, dict) else {}, response)
            return response

    def _send(self, req: urllib.request.Request) -> Response:
        try:
            with self._opener.open(req, timeout=self.timeout) as r:
                return Response(r.status, {k.lower(): v for k, v in r.headers.items()}, r.read())
        except urllib.error.HTTPError as e:
            return Response(e.code, {k.lower(): v for k, v in e.headers.items()}, e.read())

    def paginate(self, method: Callable[..., Response], *args: Any, **options: Any) -> Iterator[Any]:
        """Every item of a cursor-paginated list: method is one of the list methods, called with args and options, then again with each next_cursor (FR-INT-10)."""
        cursor = None
        while True:
            query = dict(options.get("query") or {})
            if cursor:
                query["cursor"] = cursor
            page = method(*args, **{**options, "query": query}).json()
            yield from page.get("items", [])
            cursor = page.get("next_cursor")
            if not cursor:
                return


def _retry_after(headers: dict[str, str]) -> float | None:
    raw = headers.get("retry-after", "").strip()
    if not raw:
        return None
    if raw.isdigit():
        return float(raw)
    try:
        return max(parsedate_to_datetime(raw).timestamp() - time.time(), 0.0)
    except (TypeError, ValueError):
        return None
