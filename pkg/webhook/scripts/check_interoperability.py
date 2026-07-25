#!/usr/bin/env python3
"""Independently generate and verify webhook v1 golden vectors."""

from __future__ import annotations

import argparse
import base64
import hashlib
import hmac
import json
from pathlib import Path
from urllib.parse import parse_qs, urlencode


ROOT = Path(__file__).resolve().parent.parent
FIXTURE = ROOT / "testdata" / "vectors" / "v1.json"


def b64(value: bytes) -> str:
    return base64.urlsafe_b64encode(value).rstrip(b"=").decode("ascii")


def canonical(vector: dict[str, object]) -> bytes:
    query_values = parse_qs(
        str(vector["raw_query"]), keep_blank_values=True, strict_parsing=False
    )
    query = urlencode(
        [(key, value) for key in sorted(query_values) for value in query_values[key]]
    )
    metadata = "\n".join(
        f"{b64(key.encode())}={b64(value.encode())}"
        for key, value in sorted(dict(vector["metadata"]).items())
    )
    body = base64.urlsafe_b64decode(str(vector["body_base64url"]) + "==")
    lines = [
        "webhook-v1",
        f"algorithm:{vector['algorithm']}",
        f"timestamp:{vector['timestamp']}",
        f"nonce:{b64(str(vector['nonce']).encode())}",
        f"key-id:{b64(str(vector['key_id']).encode())}",
        f"method:{vector['method']}",
        f"path:{b64(str(vector['path']).encode())}",
        f"query:{b64(query.encode())}",
        f"host:{b64(str(vector['host']).lower().encode())}",
        f"content-type:{b64(str(vector['content_type']).encode())}",
        f"idempotency-key:{b64(str(vector['idempotency_key']).encode())}",
        f"body-sha256:{b64(hashlib.sha256(body).digest())}",
        f"metadata:{b64(metadata.encode())}",
        "",
    ]
    return "\n".join(lines).encode()


def generate() -> dict[str, object]:
    common: dict[str, object] = {
        "key_id": "interop-key",
        "key_material_base64url": b64(b"cross-language-test-key"),
        "timestamp": 1_700_000_000,
        "nonce": "python-fixture-nonce",
        "method": "post",
        "path": "/hooks/%2Forders",
        "raw_query": "b=two&a=2&a=1&empty=",
        "host": "RECEIVER.example:443",
        "content_type": "application/json",
        "idempotency_key": "event-interop-1",
        "body_base64url": b64(b'{"hello":"world"}\n'),
        "metadata": {"tenant": "acme", "unicode": "snowman \u2603"},
    }
    vectors: list[dict[str, object]] = []
    for algorithm in ("sha256", "sha512"):
        vector = dict(common)
        vector["name"] = f"hmac-{algorithm}-python"
        vector["algorithm"] = algorithm
        canonical_bytes = canonical(vector)
        key = base64.urlsafe_b64decode(str(vector["key_material_base64url"]) + "==")
        digest = hashlib.sha256 if algorithm == "sha256" else hashlib.sha512
        vector["canonical_base64url"] = b64(canonical_bytes)
        vector["signature_base64url"] = b64(hmac.new(key, canonical_bytes, digest).digest())
        vectors.append(vector)
    return {"version": "v1", "generator": "python-stdlib", "vectors": vectors}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--print", action="store_true", dest="print_fixture")
    args = parser.parse_args()
    generated = generate()
    if args.print_fixture:
        print(json.dumps(generated, ensure_ascii=False, indent=2) + "\n", end="")
        return 0
    checked_in = json.loads(FIXTURE.read_text(encoding="utf-8"))
    if checked_in != generated:
        raise SystemExit("interoperability fixture differs from independent generator")
    print("interoperability fixture verified with Python stdlib")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
