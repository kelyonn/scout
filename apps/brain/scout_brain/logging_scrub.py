"""PII-scrubbing logging.Filter — the Python side of the same backstop
packages/logging (Go) implements for apps/api/collector/notifier. See that
package's doc comment for the full rationale: this catches email
addresses, phone numbers, and bearer-token/API-key-shaped strings
wherever they appear in a log record, but is not a substitute for never
logging job.description_text or resume.raw_text directly (no regex
signature exists for "is this a resume").
"""

from __future__ import annotations

import logging
import re

REDACTED = "[REDACTED]"

_EMAIL = re.compile(r"[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}")

# Same tuning as packages/logging's Go implementation: requires a leading
# "+" or an internal separator, so a bare digit run (a UUID segment, a
# content hash) is never matched — only a genuinely phone-shaped string is.
_PHONE = re.compile(
    r"\+\d{1,3}[-.\s]?\d{2,5}(?:[-.\s]?\d{2,5}){1,3}\b"
    r"|\b\d{2,5}[-.\s]\d{2,5}(?:[-.\s]\d{2,5})*\b"
)

_TOKEN = re.compile(
    r"\bBearer\s+[A-Za-z0-9._-]{10,}\b|\bsk-[A-Za-z0-9]{10,}\b|\bgh[opsu]_[A-Za-z0-9]{10,}\b",
    re.IGNORECASE,
)


def scrub(text: str) -> str:
    text = _EMAIL.sub(REDACTED, text)
    text = _TOKEN.sub(REDACTED, text)
    text = _PHONE.sub(REDACTED, text)
    return text


class ScrubbingFilter(logging.Filter):
    """Attach to the root logger (see worker.py) so every handler
    downstream sees already-scrubbed records — a filter on a Logger runs
    before any of its Handlers, same ordering guarantee Go's Scrub gets
    from wrapping the terminal slog.Handler.
    """

    def filter(self, record: logging.LogRecord) -> bool:
        record.msg = scrub(str(record.msg))
        if record.args:
            if isinstance(record.args, dict):
                record.args = {
                    key: (scrub(value) if isinstance(value, str) else value)
                    for key, value in record.args.items()
                }
            else:
                record.args = tuple(
                    scrub(arg) if isinstance(arg, str) else arg for arg in record.args
                )
        return True
