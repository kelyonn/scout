from __future__ import annotations

from typing import Any

import psycopg


def fetchone(cur: psycopg.Cursor[Any]) -> tuple[Any, ...]:
    """cur.fetchone() narrowed from `tuple | None` to `tuple` for call sites
    that immediately unpack the result — the query's own RETURNING/WHERE
    clause is what actually guarantees the row exists, not this assert."""
    row = cur.fetchone()
    assert row is not None
    result: tuple[Any, ...] = row
    return result
