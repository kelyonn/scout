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


def main() -> None:
    config = load()

    # docs/16-observability.md — Python's port, not SCOUT_METRICS_ADDR, since
    # prometheus_client.start_http_server takes a bare port rather than a
    # host:port string the Go services' own SCOUT_METRICS_ADDR convention uses.
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

    # Public-data tasks (dedup adjudication, Tier 2 classification, posting
    # summaries) get the full ADR-016 cascade — hosted providers first,
    # local Ollama once they're exhausted or unconfigured. explain is
    # deliberately excluded: it reads job_score's resume-derived match
    # data, and AGENTS.md rule 9 requires that to stay local, so it builds
    # its own OllamaClient below rather than sharing this one.
    llm = build_cascade_client(config.llm, config.ollama_host)
    explain_llm = OllamaClient(config.ollama_host, model=config.llm.local_model)

    embed_conn = psycopg.connect(config.database_url, autocommit=False)
    embed_consumer = EmbedConsumer(embed_conn, embedder, role_index)

    brain_deep_conn = psycopg.connect(config.database_url, autocommit=False)
    dedup_stage3 = DedupStage3Consumer(brain_deep_conn, llm)
    classify_tier2 = ClassifyTier2Consumer(brain_deep_conn, llm)
    summarize = SummarizeConsumer(brain_deep_conn, llm)
    explain = ExplainConsumer(brain_deep_conn, explain_llm)
    brain_deep_consumer = BrainDeepConsumer(
        dedup_stage3, classify_tier2, summarize, explain
    )

    brain_deep_thread = threading.Thread(
        target=run_worker,
        args=(
            config.database_url,
            "brain_deep",
            "brain-worker-1",
            brain_deep_consumer.handle,
        ),
        daemon=True,
    )
    brain_deep_thread.start()
    logger.info("brain_deep consumer started")

    logger.info("embed consumer starting (blocks)")
    run_worker(config.database_url, "embed", "brain-worker-1", embed_consumer.handle)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        sys.exit(0)
