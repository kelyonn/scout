from __future__ import annotations

import logging

from scout_brain.logging_scrub import REDACTED, ScrubbingFilter, scrub


def _log_with_filter(msg: str, *args: object) -> str:
    logger = logging.getLogger("test_logging_scrub")
    logger.setLevel(logging.INFO)
    logger.addFilter(ScrubbingFilter())
    records: list[logging.LogRecord] = []

    class _CaptureHandler(logging.Handler):
        def emit(self, record: logging.LogRecord) -> None:
            records.append(record)

    handler = _CaptureHandler()
    logger.addHandler(handler)
    try:
        logger.info(msg, *args)
    finally:
        logger.removeHandler(handler)
        logger.filters.clear()
    return records[0].getMessage()


def test_scrub_redacts_email_in_message() -> None:
    out = _log_with_filter("failed to process resume for kalyan15122005@gmail.com")
    assert "kalyan15122005@gmail.com" not in out
    assert REDACTED in out


def test_scrub_redacts_email_in_lazy_args() -> None:
    out = _log_with_filter("contact on file: %s", "ops@example.com")
    assert "ops@example.com" not in out


def test_scrub_redacts_phone_number() -> None:
    out = _log_with_filter("contact: %s", "+91 8792894576")
    assert "8792894576" not in out


def test_scrub_redacts_bearer_token() -> None:
    out = _log_with_filter("outbound request auth: %s", "Bearer sk-abcdefghijklmnopqrstuvwxyz123456")
    assert "abcdefghijklmnopqrstuvwxyz123456" not in out


def test_scrub_leaves_ordinary_fields_alone() -> None:
    out = _log_with_filter(
        "source fetched id=%s kind=%s items=%s", "0192b7c4-1234-4abc-9def-000000000001", "ats_greenhouse", 47
    )
    assert "0192b7c4-1234-4abc-9def-000000000001" in out
    assert "ats_greenhouse" in out
    assert REDACTED not in out


def test_scrub_function_is_idempotent_on_clean_text() -> None:
    text = "role_family=swe.backend confidence=0.91"
    assert scrub(text) == text
