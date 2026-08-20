"""Dispatcher for packages/queue's `brain_deep` queue — one queue, several
task types (packages/queue.BrainDeepTask), per the plan's scope decision
that volume doesn't justify a separate queue per task.
"""

from __future__ import annotations

import logging

from scout_riverpy import Job

from scout_brain.classify_tier2 import ClassifyTier2Consumer
from scout_brain.dedup_stage3 import DedupStage3Consumer
from scout_brain.explain import ExplainConsumer
from scout_brain.summarize import SummarizeConsumer

logger = logging.getLogger(__name__)


class BrainDeepConsumer:
    def __init__(
        self,
        dedup_stage3: DedupStage3Consumer,
        classify_tier2: ClassifyTier2Consumer,
        summarize: SummarizeConsumer,
        explain: ExplainConsumer,
    ) -> None:
        self._dedup_stage3 = dedup_stage3
        self._classify_tier2 = classify_tier2
        self._summarize = summarize
        self._explain = explain

    def handle(self, job: Job) -> None:
        task = job.args.get("task")
        if task == "dedup_stage3":
            self._dedup_stage3.handle(job)
        elif task == "classify_tier2":
            self._classify_tier2.handle(job)
        elif task == "summarize":
            self._summarize.handle(job)
        elif task == "explain":
            self._explain.handle(job)
        else:
            # An unrecognized task is a real bug (a new task type shipped
            # on the Go side without a matching Python handler) — raising
            # surfaces it via riverpy's retry/discard path rather than
            # silently dropping the job.
            raise ValueError(f"brain_deep: unknown task {task!r}")
