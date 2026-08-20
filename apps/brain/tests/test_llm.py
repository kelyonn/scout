from __future__ import annotations

import httpx
import pytest

from scout_brain.llm import (
    CascadeClient,
    GeminiProvider,
    LLMUnavailableError,
    OllamaClient,
    OpenAICompatibleProvider,
    RateLimitedProvider,
    RateLimitExceeded,
    StructuredResponse,
    _RateLimiter,
)
from scout_brain.metrics import LLM_CALLS_TOTAL


class _MockTransport(httpx.BaseTransport):
    def __init__(self, responses: list[httpx.Response]) -> None:
        self._responses = list(responses)

    def handle_request(self, request: httpx.Request) -> httpx.Response:
        return self._responses.pop(0)


def _client_with_responses(responses: list[httpx.Response]) -> OllamaClient:
    client = OllamaClient(host="http://fake-ollama:11434")
    client._client = httpx.Client(transport=_MockTransport(responses))
    return client


def test_generate_json_parses_valid_response() -> None:
    resp = httpx.Response(200, json={"response": '{"same_role": true, "confidence": 0.9}'})
    client = _client_with_responses([resp])
    result = client.generate_json("prompt")
    assert result.data == {"same_role": True, "confidence": 0.9}


def test_generate_json_records_the_call_against_its_task_label() -> None:
    resp = httpx.Response(200, json={"response": '{"ok": true}'})
    client = _client_with_responses([resp])
    before = LLM_CALLS_TOTAL.labels(task="unit_test_task", provider="local")._value.get()

    client.generate_json("prompt", task="unit_test_task")

    after = LLM_CALLS_TOTAL.labels(task="unit_test_task", provider="local")._value.get()
    assert after == before + 1


def test_generate_json_retries_once_on_malformed_json_then_succeeds() -> None:
    bad = httpx.Response(200, json={"response": "not json at all"})
    good = httpx.Response(200, json={"response": '{"same_role": false, "confidence": 0.2}'})
    client = _client_with_responses([bad, good])
    result = client.generate_json("prompt")
    assert result.data == {"same_role": False, "confidence": 0.2}


def test_generate_json_raises_after_two_malformed_responses() -> None:
    bad1 = httpx.Response(200, json={"response": "not json"})
    bad2 = httpx.Response(200, json={"response": "still not json"})
    client = _client_with_responses([bad1, bad2])
    with pytest.raises(LLMUnavailableError):
        client.generate_json("prompt")


def test_generate_json_raises_on_http_error() -> None:
    class _FailingTransport(httpx.BaseTransport):
        def handle_request(self, request: httpx.Request) -> httpx.Response:
            raise httpx.ConnectError("connection refused", request=request)

    client = OllamaClient(host="http://fake-ollama:11434")
    client._client = httpx.Client(transport=_FailingTransport())
    with pytest.raises(LLMUnavailableError):
        client.generate_json("prompt")


# ---------------------------------------------------------------- _RateLimiter ---


class _FakeClock:
    """A controllable clock/sleep pair — advancing time only when `sleep`
    is called, so rpm-wait tests run instantly instead of for real seconds.
    """

    def __init__(self) -> None:
        self.now = 0.0
        self.slept_for: list[float] = []

    def clock(self) -> float:
        return self.now

    def sleep(self, seconds: float) -> None:
        self.slept_for.append(seconds)
        self.now += seconds


def test_rate_limiter_allows_requests_under_rpm_without_waiting() -> None:
    fake = _FakeClock()
    limiter = _RateLimiter(rpm=3, rpd=None, clock=fake.clock, sleep=fake.sleep)
    for _ in range(3):
        limiter.acquire()
    assert fake.slept_for == []


def test_rate_limiter_waits_when_rpm_exhausted_then_proceeds() -> None:
    fake = _FakeClock()
    limiter = _RateLimiter(rpm=1, rpd=None, clock=fake.clock, sleep=fake.sleep)
    limiter.acquire()
    limiter.acquire()  # rpm=1 already used -> must wait ~60s before this returns
    assert fake.slept_for, "expected the limiter to wait rather than raise or skip"
    assert fake.now >= 60


def test_rate_limiter_raises_when_rpd_exhausted() -> None:
    fake = _FakeClock()
    limiter = _RateLimiter(rpm=None, rpd=1, clock=fake.clock, sleep=fake.sleep)
    limiter.acquire()
    with pytest.raises(RateLimitExceeded):
        limiter.acquire()
    assert fake.slept_for == [], "a daily limit should raise immediately, never wait"


def test_rate_limiter_budget_used_ratio_tracks_rpd_usage() -> None:
    fake = _FakeClock()
    limiter = _RateLimiter(rpm=None, rpd=4, clock=fake.clock, sleep=fake.sleep)
    assert limiter.budget_used_ratio == 0.0

    limiter.acquire()
    assert limiter.budget_used_ratio == 0.25

    limiter.acquire()
    limiter.acquire()
    assert limiter.budget_used_ratio == 0.75


def test_rate_limiter_budget_used_ratio_expires_with_the_day_window() -> None:
    fake = _FakeClock()
    limiter = _RateLimiter(rpm=None, rpd=2, clock=fake.clock, sleep=fake.sleep)
    limiter.acquire()
    fake.now += 86400  # a full day later — the same window acquire() itself expires
    assert limiter.budget_used_ratio == 0.0


def test_rate_limiter_budget_used_ratio_is_zero_with_no_daily_cap() -> None:
    fake = _FakeClock()
    limiter = _RateLimiter(rpm=None, rpd=None, clock=fake.clock, sleep=fake.sleep)
    limiter.acquire()
    assert limiter.budget_used_ratio == 0.0


def test_rate_limiter_with_none_limits_never_waits_or_raises() -> None:
    fake = _FakeClock()
    limiter = _RateLimiter(rpm=None, rpd=None, clock=fake.clock, sleep=fake.sleep)
    for _ in range(100):
        limiter.acquire()
    assert fake.slept_for == []


# ------------------------------------------------------------ RateLimitedProvider ---


def test_rate_limited_provider_raises_rate_limit_exceeded_on_429() -> None:
    request = httpx.Request("POST", "http://fake")

    def send(prompt: str) -> str:
        raise httpx.HTTPStatusError("429", request=request, response=httpx.Response(429, request=request))

    provider = RateLimitedProvider("fake", "fake-model", send, rpm=None, rpd=None)
    with pytest.raises(RateLimitExceeded):
        provider.call("prompt")


def test_rate_limited_provider_propagates_non_429_http_errors() -> None:
    request = httpx.Request("POST", "http://fake")

    def send(prompt: str) -> str:
        raise httpx.HTTPStatusError("500", request=request, response=httpx.Response(500, request=request))

    provider = RateLimitedProvider("fake", "fake-model", send, rpm=None, rpd=None)
    with pytest.raises(httpx.HTTPStatusError):
        provider.call("prompt")


# ------------------------------------------------------------------ CascadeClient ---


class _FakeProvider:
    """Stands in for RateLimitedProvider in CascadeClient tests — same
    name/model/call surface, without any real rate limiting or HTTP.
    """

    def __init__(self, name: str, model: str, result: str | None = None, error: Exception | None = None) -> None:
        self.name = name
        self.model = model
        self._result = result
        self._error = error
        self.calls = 0

    def call(self, prompt: str) -> str:
        self.calls += 1
        if self._error is not None:
            raise self._error
        assert self._result is not None, "_FakeProvider needs a result or an error"
        return self._result


def _local(responses: list[httpx.Response]) -> OllamaClient:
    client = OllamaClient(host="http://fake-ollama:11434", model="local-model")
    client._client = httpx.Client(transport=_MockTransport(responses))
    return client


def test_cascade_uses_first_provider_that_succeeds() -> None:
    p1 = _FakeProvider("p1", "p1-model", result='{"a": 1}')
    p2 = _FakeProvider("p2", "p2-model", result='{"a": 2}')
    cascade = CascadeClient([p1, p2], _local([]))

    result = cascade.generate_json("prompt")

    assert result == StructuredResponse(data={"a": 1})
    assert cascade.model == "p1-model"
    assert p2.calls == 0


def test_cascade_rotates_past_rate_limited_provider() -> None:
    p1 = _FakeProvider("p1", "p1-model", error=RateLimitExceeded("exhausted"))
    p2 = _FakeProvider("p2", "p2-model", result='{"a": 2}')
    cascade = CascadeClient([p1, p2], _local([]))

    result = cascade.generate_json("prompt")

    assert result == StructuredResponse(data={"a": 2})
    assert cascade.model == "p2-model"


def test_cascade_rotates_past_a_failing_provider() -> None:
    p1 = _FakeProvider("p1", "p1-model", error=httpx.ConnectError("refused"))
    p2 = _FakeProvider("p2", "p2-model", result='{"a": 2}')
    cascade = CascadeClient([p1, p2], _local([]))

    result = cascade.generate_json("prompt")

    assert result == StructuredResponse(data={"a": 2})


def test_cascade_rotates_past_a_non_json_response() -> None:
    p1 = _FakeProvider("p1", "p1-model", result="not json at all")
    p2 = _FakeProvider("p2", "p2-model", result='{"a": 2}')
    cascade = CascadeClient([p1, p2], _local([]))

    result = cascade.generate_json("prompt")

    assert result == StructuredResponse(data={"a": 2})


def test_cascade_falls_back_to_local_when_every_hosted_provider_is_unavailable() -> None:
    p1 = _FakeProvider("p1", "p1-model", error=RateLimitExceeded("exhausted"))
    p2 = _FakeProvider("p2", "p2-model", error=httpx.ConnectError("refused"))
    local_resp = httpx.Response(200, json={"response": '{"a": "local"}'})
    cascade = CascadeClient([p1, p2], _local([local_resp]))

    result = cascade.generate_json("prompt")

    assert result == StructuredResponse(data={"a": "local"})
    assert cascade.model == "local-model"


def test_cascade_raises_when_hosted_and_local_both_unavailable() -> None:
    p1 = _FakeProvider("p1", "p1-model", error=RateLimitExceeded("exhausted"))

    class _FailingTransport(httpx.BaseTransport):
        def handle_request(self, request: httpx.Request) -> httpx.Response:
            raise httpx.ConnectError("connection refused", request=request)

    local = OllamaClient(host="http://fake-ollama:11434")
    local._client = httpx.Client(transport=_FailingTransport())
    cascade = CascadeClient([p1], local)

    with pytest.raises(LLMUnavailableError):
        cascade.generate_json("prompt")


# ------------------------------------------------------------- hosted providers ---


def test_gemini_provider_extracts_text_from_candidates() -> None:
    resp = httpx.Response(
        200,
        json={"candidates": [{"content": {"parts": [{"text": '{"role_family": "swe.backend"}'}]}}]},
    )
    provider = GeminiProvider(api_key="fake-key", model="gemini-2.0-flash")
    provider._client = httpx.Client(transport=_MockTransport([resp]))
    assert provider.send("prompt") == '{"role_family": "swe.backend"}'


def test_openai_compatible_provider_extracts_message_content() -> None:
    resp = httpx.Response(
        200,
        json={"choices": [{"message": {"content": '{"same_role": true}'}}]},
    )
    provider = OpenAICompatibleProvider(base_url="https://api.groq.com/openai/v1", api_key="fake-key", model="m")
    provider._client = httpx.Client(transport=_MockTransport([resp]))
    assert provider.send("prompt") == '{"same_role": true}'
