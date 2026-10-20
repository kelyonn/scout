"""apps/brain's entrypoint: one long-running process consuming both of
packages/queue's queues concurrently (threads — this is I/O-bound work:
DB queries, Ollama HTTP calls, releasing the GIL exactly when it matters).
Run via `uv run python -m scout_brain.worker` (also how
infra/compose/local.yml's brain service starts it).
"""

from __future__ import annotations

import logging
import os
import sys
import threading
import time
from collections.abc import Callable

import psycopg
from scout_riverpy import run_worker

from scout_brain.brain_deep_consumer import BrainDeepConsumer
from scout_brain.classify_tier2 import ClassifyTier2Consumer
from scout_brain.config import load
from scout_brain.dedup_stage3 import DedupStage3Consumer
from scout_brain.embed_consumer import EmbedConsumer
from scout_brain.embeddings import Embedder
from scout_brain.explain import ExplainConsumer
from scout_brain.llm import OllamaClient, build_cascade_client
from scout_brain.logging_scrub import ScrubbingFilter
from scout_brain.metrics import start_metrics_server
from scout_brain.resume_embed import embed_pending_resumes
from scout_brain.role_taxonomy import RoleExemplarIndex
from scout_brain.summarize import SummarizeConsumer

logging.basicConfig(
    level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s"
)
# docs/16-observability.md section 6's scrubbing middleware requirement —
# attached to the root logger so it runs before every handler, catching
# every scout_brain.* logger, not just this module's.
logging.getLogger().addFilter(ScrubbingFilter())
logger = logging.getLogger(__name__)


def _drain_stop(database_url: str, max_seconds: float) -> Callable[[], bool]:
    """Stop condition for `--once` (ADR-019): True once both queues have had
    nothing claimable or running for two consecutive checks, or the time
    budget ends. Checking both queues matters because embed jobs enqueue
    brain_deep jobs; stopping on one queue alone would strand the other's work
    until the next scheduled run.
    """
    deadline = time.monotonic() + max_seconds
    idle_checks = 0
    lock = threading.Lock()
    conn = psycopg.connect(database_url, autocommit=True)

    def stop() -> bool:
        nonlocal idle_checks
        with lock:
            if time.monotonic() >= deadline:
                return True
            row = conn.execute(
                """
                SELECT count(*) FROM river_job
                WHERE queue IN ('embed', 'brain_deep')
                  AND (state = 'running'
                       OR (state IN ('available', 'retryable') AND scheduled_at <= now()))
                """
            ).fetchone()
            pending = row[0] if row else 0
            idle_checks = idle_checks + 1 if pending == 0 else 0
            return idle_checks >= 2

    return stop


def main() -> None:
    once = "--once" in sys.argv[1:]
    config = load()

    # docs/16-observability.md — Python's port, not SCOUT_METRICS_ADDR, since
    # prometheus_client.start_http_server takes a bare port rather than a
    # host:port string the Go services' own SCOUT_METRICS_ADDR convention uses.
    if not once:
        start_metrics_server(int(os.environ.get("SCOUT_METRICS_PORT", "9090")))

    logger.info("scout-brain starting, loading embedding model...")
    embedder = Embedder()
    logger.info("embedding model loaded")

    logger.info("embedding role-family exemplars for Tier 1 classification...")
    role_index = RoleExemplarIndex(embedder)
    logger.info("role exemplar index ready")

    startup_conn = psycopg.connect(config.database_url, autocommit=False)
    embed_pending_resumes(startup_conn, embedder)
    startup_conn.close()

    # Public-data bulk tasks (dedup adjudication, Tier 2 classification)
    # get the full ADR-016 cascade — hosted providers first, local Ollama
    # once they're exhausted or unconfigured. explain and summarize both
    # build their own local-only OllamaClient below instead of sharing
    # this one, for two different reasons: explain reads job_score's
    # resume-derived match data (AGENTS.md rule 9 requires that stay
    # local, regardless of speed); summarize is on a real interactive
    # path (apps/api's job detail handler enqueues it the instant a user
    # opens a job) and the shared cascade's rate limiter blocks on hosted
    # providers' rpm windows shared with whatever bulk dedup/classify
    # traffic is running — see summarize.py's own comment and
    # packages/queue's bulkBrainDeepPriority for the queue-ordering half
    # of this fix (2026-08-24). Both reuse one OllamaClient instance:
    # brain_deep's tasks all run in the same single worker thread below,
    # so there's no concurrency reason for two.
    llm = build_cascade_client(config.llm, config.ollama_host)
    local_llm = OllamaClient(config.ollama_host, model=config.llm.local_model)

    embed_conn = psycopg.connect(config.database_url, autocommit=False)
    embed_consumer = EmbedConsumer(embed_conn, embedder, role_index)

    brain_deep_conn = psycopg.connect(config.database_url, autocommit=False)
    dedup_stage3 = DedupStage3Consumer(brain_deep_conn, llm)
    classify_tier2 = ClassifyTier2Consumer(brain_deep_conn, llm)
    summarize = SummarizeConsumer(brain_deep_conn, local_llm)
    explain = ExplainConsumer(brain_deep_conn, local_llm)
    brain_deep_consumer = BrainDeepConsumer(
        dedup_stage3, classify_tier2, summarize, explain
    )

    stop = (
        _drain_stop(
            config.database_url, float(os.environ.get("SCOUT_ONCE_MAX_SECONDS", "900"))
        )
        if once
        else None
    )

    brain_deep_thread = threading.Thread(
        target=run_worker,
        args=(
            config.database_url,
            "brain_deep",
            "brain-worker-1",
            brain_deep_consumer.handle,
        ),
        kwargs={"stop": stop},
        daemon=not once,
    )
    brain_deep_thread.start()
    logger.info("brain_deep consumer started")

    logger.info("embed consumer starting (blocks)")
    run_worker(
        config.database_url,
        "embed",
        "brain-worker-1",
        embed_consumer.handle,
        stop=stop,
    )
    if once:
        brain_deep_thread.join()
        logger.info("queues drained, exiting")


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        sys.exit(0)
