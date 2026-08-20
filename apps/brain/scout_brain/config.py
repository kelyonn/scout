"""Environment configuration. Fails fast on a missing required variable —
same posture apps/collector/cmd and apps/notifier/cmd already take, rather
than defaulting into a state that looks like it started but cannot do
anything.
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field
from pathlib import Path

import yaml

# Same value packages/queue and embedding_version columns should agree on —
# bumping the model changes what "comparable" embeddings means, so this is
# the version stamp written alongside every embedding.
EMBEDDING_VERSION = "bge-small-en-v1.5"

EMBEDDING_MODEL = "BAAI/bge-small-en-v1.5"

DEFAULT_LOCAL_MODEL = "qwen2.5:3b-instruct"

# apps/brain/scout_brain/config.py -> apps/brain/scout_brain -> apps/brain ->
# apps -> repo root.
_REPO_ROOT = Path(__file__).resolve().parents[3]
DEFAULT_LLM_PROVIDERS_PATH = _REPO_ROOT / "infra" / "config" / "llm_providers.yaml"


@dataclass(frozen=True)
class ProviderConfig:
    """One infra/config/llm_providers.yaml entry with its API key already
    resolved from the environment. Only providers whose api_key_env is
    actually set on this host appear here — see load_llm_config.
    """

    name: str
    kind: str  # "gemini" | "openai_compatible"
    api_key: str
    model: str
    base_url: str | None
    rpm: int | None
    rpd: int | None


@dataclass(frozen=True)
class LLMConfig:
    providers: list[ProviderConfig] = field(default_factory=list)
    local_model: str = DEFAULT_LOCAL_MODEL


@dataclass(frozen=True)
class Config:
    database_url: str
    ollama_host: str
    llm: LLMConfig


def load_llm_config(path: Path = DEFAULT_LLM_PROVIDERS_PATH) -> LLMConfig:
    """Reads infra/config/llm_providers.yaml and resolves each provider's
    API key from its api_key_env. A provider with no key set in the
    environment is silently omitted — not an error, since a dev host with
    zero hosted-provider signups should just run the local-only cascade
    ADR-016 always allows, exactly as this codebase behaved before the
    cascade existed. A missing config file behaves the same way: local
    Ollama only.
    """
    if not path.exists():
        return LLMConfig()

    raw = yaml.safe_load(path.read_text()) or {}
    providers: list[ProviderConfig] = []
    for entry in raw.get("providers", []):
        api_key = os.environ.get(entry["api_key_env"], "")
        if not api_key:
            continue
        providers.append(
            ProviderConfig(
                name=entry["name"],
                kind=entry["kind"],
                api_key=api_key,
                model=entry["model"],
                base_url=entry.get("base_url"),
                rpm=entry.get("rpm"),
                rpd=entry.get("rpd"),
            )
        )

    local_model = ((raw.get("local") or {}).get("model")) or DEFAULT_LOCAL_MODEL
    return LLMConfig(providers=providers, local_model=local_model)


def load() -> Config:
    database_url = os.environ.get("SCOUT_DATABASE_URL")
    if not database_url:
        raise RuntimeError("SCOUT_DATABASE_URL is not set")
    ollama_host = os.environ.get("SCOUT_BRAIN_OLLAMA_HOST", "http://localhost:11434")
    return Config(
        database_url=database_url, ollama_host=ollama_host, llm=load_llm_config()
    )
