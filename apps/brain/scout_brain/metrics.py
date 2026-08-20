"""Prometheus metrics — docs/16-observability.md section 4's AI catalog,
mirroring packages/metrics' own scope decision on the Go side: the subset
that feeds the Overview dashboard's "LLM budget burn" panel, not the full
catalog.

`scout_llm_cost_usd_total` from that catalog is deliberately not
implemented: every provider ADR-016's cascade calls is a free tier
(Gemini/Groq/OpenRouter's no-cost quotas, or local Ollama), so a real
dollar figure would always read zero — not inaccurate, just meaningless.
`scout_llm_budget_used_ratio` here instead reads each provider's actual
constraint directly: fraction of today's request-per-day allowance spent,
from the same `_RateLimiter` that enforces it (llm.py) rather than a
dollar figure this system was designed from ADR-014 onward not to need.
"""

from __future__ import annotations

from prometheus_client import Counter, Gauge, Histogram, start_http_server

LLM_CALLS_TOTAL = Counter(
    "scout_llm_calls_total",
    "LLM calls that returned a usable structured response, by task and the provider that answered.",
    ["task", "provider"],
)

LLM_LATENCY_SECONDS = Histogram(
    "scout_llm_latency_seconds",
    "LLM call latency, by task and provider.",
    ["task", "provider"],
)

LLM_ERRORS_TOTAL = Counter(
    "scout_llm_errors_total",
    "LLM call failures that caused CascadeClient to rotate to the next provider, by provider and error class.",
    ["provider", "error_class"],
)

LLM_BUDGET_USED_RATIO = Gauge(
    "scout_llm_budget_used_ratio",
    "Fraction of today's request-per-day allowance used, by provider.",
    ["provider"],
)


def start_metrics_server(port: int) -> None:
    """Starts prometheus_client's own background HTTP server — no manual
    handler needed, unlike the Go services (packages/metrics.Serve), since
    prometheus_client ships one. Runs in a daemon thread the process
    doesn't need to manage or shut down explicitly.
    """
    start_http_server(port)
