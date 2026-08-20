from scout_brain.models import JobForEmbedding


def test_embedding_text_prefers_stripped_over_plain_description() -> None:
    job = JobForEmbedding(
        id="j1",
        normalized_title="software engineer intern",
        description_text="raw text with boilerplate",
        description_stripped="clean text",
    )
    assert job.embedding_text() == "software engineer intern\nclean text"


def test_embedding_text_falls_back_to_description_text_when_unstripped() -> None:
    job = JobForEmbedding(
        id="j1",
        normalized_title="software engineer intern",
        description_text="raw text, no stripping done yet",
        description_stripped=None,
    )
    assert job.embedding_text() == "software engineer intern\nraw text, no stripping done yet"


def test_embedding_text_handles_missing_description_entirely() -> None:
    job = JobForEmbedding(id="j1", normalized_title="software engineer intern")
    assert job.embedding_text() == "software engineer intern"
