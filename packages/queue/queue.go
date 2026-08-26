// Package queue is the Go side of the Go<->Python handoff ADR-001 requires
// ("Go and Python never call each other synchronously") and ADR-003
// specifies (River, Postgres-backed, transactional enqueue). Go only ever
// inserts; it never runs a River worker for these two queues, since
// apps/brain's Python consumer (packages/riverpy) is what processes them —
// see that package's own comment for why it is hand-rolled rather than
// using the official (insert-only) `riverqueue` PyPI client.
//
// Topology is 2 queues, not ADR-003's speculative 8: normalize/classify
// Tier 0/dedup Stage 1/score stay synchronous in the Go collector exactly
// as P1 built them — moving already-fast, already-correct code onto a
// queue for its own sake adds nothing. Only the genuinely-Python-bound work
// is queued.
package queue

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// QueueEmbed computes and stores a job's embedding (apps/brain, via
// fastembed/bge-small-en-v1.5), then triggers Tier 1 role/seniority
// refinement for that job once the embedding is written.
const QueueEmbed = "embed"

// QueueBrainDeep covers dedup Stage 3 semantic adjudication, Tier 2 LLM
// classification escalation, and LLM explanation generation — grouped into
// one queue since all three need the job's embedding to already exist and
// volume is low enough that type-dispatch inside the Python consumer is
// simpler than three separate queues for it.
const QueueBrainDeep = "brain_deep"

// EmbedArgs is QueueEmbed's job payload.
type EmbedArgs struct {
	JobID string `json:"job_id"`
}

// Kind implements river.JobArgs.
func (EmbedArgs) Kind() string { return "embed" }

// EmbedResumeArgs is QueueEmbed's other job payload — no fields, since
// ADR-015 makes this a single-user system with exactly one resume row, so
// there is nothing to identify beyond "the resume changed, recompute its
// embedding." apps/brain's embed consumer dispatches on Kind() the same
// way brain_deep's consumer dispatches on BrainDeepArgs.Task, since both
// queues carry more than one payload shape.
type EmbedResumeArgs struct{}

// Kind implements river.JobArgs.
func (EmbedResumeArgs) Kind() string { return "embed_resume" }

// BrainDeepTask discriminates the tasks QueueBrainDeep's consumer
// dispatches on.
type BrainDeepTask string

const (
	// TaskDedupStage3 runs semantic + LLM adjudication for a job pair
	// Stage 2 flagged as structurally plausible but inconclusive.
	TaskDedupStage3 BrainDeepTask = "dedup_stage3"
	// TaskClassifyTier2 upgrades Tier 0's role-family guess with an
	// LLM-backed classification.
	TaskClassifyTier2 BrainDeepTask = "classify_tier2"
	// TaskExplain generates job_score.explanation — see the doc comment
	// on TaskSummarize below for how this differs from it.
	TaskExplain BrainDeepTask = "explain"
	// TaskSummarize generates job.ai_summary — a factual TL;DR of the
	// posting itself (what the company does, what the role is,
	// requirements, pay), job-level and the same for every user. Distinct
	// from TaskExplain's job_score.explanation, which is a personalized
	// "why this matches you" narrative — same LLM infrastructure, a
	// different output and a different owning table, not two names for
	// the same thing.
	TaskSummarize BrainDeepTask = "summarize"
)

// BrainDeepArgs is QueueBrainDeep's job payload. CandidateJobID is only
// meaningful for TaskDedupStage3 — the other job in the pair Stage 2
// (apps/collector/internal/dedup) found structurally plausible but
// couldn't confirm, left empty for every other task.
type BrainDeepArgs struct {
	JobID          string        `json:"job_id"`
	Task           BrainDeepTask `json:"task"`
	CandidateJobID string        `json:"candidate_job_id,omitempty"`
}

// Kind implements river.JobArgs.
func (BrainDeepArgs) Kind() string { return "brain_deep" }

// Client wraps a River client configured for insert-only use — Go never
// registers workers or calls Start() for these queues, per River's own
// documented insert-only pattern (initialize with no Queues/Workers, never
// call Start). apps/brain's hand-rolled consumer (packages/riverpy) is the
// only thing that ever transitions a row out of 'available'.
type Client struct {
	river *river.Client[pgx.Tx]
}

// New constructs an insert-only River client against pool.
func New(pool *pgxpool.Pool) (*Client, error) {
	riverClient, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		return nil, fmt.Errorf("queue: construct river client: %w", err)
	}
	return &Client{river: riverClient}, nil
}

// EnqueueEmbed enqueues an embed job for jobID, transactionally with tx —
// the whole point of River per ADR-003: the job is enqueued if and only if
// tx commits.
func (c *Client) EnqueueEmbed(ctx context.Context, tx pgx.Tx, jobID string) error {
	_, err := c.river.InsertTx(ctx, tx, EmbedArgs{JobID: jobID}, &river.InsertOpts{Queue: QueueEmbed})
	if err != nil {
		return fmt.Errorf("queue: enqueue embed: %w", err)
	}
	return nil
}

// EnqueueEmbedResume enqueues an embed_resume job, transactionally with tx
// — apps/api's resume upload handler calls this in the same transaction
// that writes the new resume.raw_text, so a job is only ever queued for a
// write that actually committed.
func (c *Client) EnqueueEmbedResume(ctx context.Context, tx pgx.Tx) error {
	_, err := c.river.InsertTx(ctx, tx, EmbedResumeArgs{}, &river.InsertOpts{Queue: QueueEmbed})
	if err != nil {
		return fmt.Errorf("queue: enqueue embed_resume: %w", err)
	}
	return nil
}

// bulkBrainDeepPriority is river's lowest priority (1 highest, 4 lowest —
// infra/migrations/000010_river.up.sql's priority_in_range check), used
// for every BrainDeepTask except TaskSummarize. TaskDedupStage3,
// TaskClassifyTier2, and TaskExplain all run at ingest time, in bulk, for
// every job the collector ever writes, with nobody waiting on any single
// one of them — TaskSummarize is the one task on a real interactive path
// (apps/api's job detail handler enqueues it the instant a user opens a
// job, and the user is looking at a loading state until it completes).
// Before this, every brain_deep job shared river's default priority (1),
// so a freshly enqueued TaskSummarize landed at the back of whatever bulk
// backlog a recent live run had just produced, ordered by scheduled_at
// alone — confirmed live 2026-08-24: 28,836 available brain_deep jobs
// after one 10-minute live run, a single-threaded consumer, and a real
// summary request stuck behind all of them despite the LLM call itself
// (a local Ollama call, ~2.4s measured) being fast. Demoting the bulk
// tasks here means a fresh TaskSummarize is claimed on the consumer's very
// next fetch regardless of backlog size — river_job_prioritized_fetching_index
// sorts on priority before scheduled_at — without a second queue or a
// second consumer thread.
const bulkBrainDeepPriority = 4

// EnqueueBrainDeep enqueues a brain_deep job for jobID/task, transactionally
// with tx. For TaskDedupStage3, use EnqueueDedupStage3 instead — that task
// needs the candidate job id this method has no way to carry.
func (c *Client) EnqueueBrainDeep(ctx context.Context, tx pgx.Tx, jobID string, task BrainDeepTask) error {
	opts := &river.InsertOpts{Queue: QueueBrainDeep}
	if task != TaskSummarize {
		opts.Priority = bulkBrainDeepPriority
	}
	_, err := c.river.InsertTx(ctx, tx, BrainDeepArgs{JobID: jobID, Task: task}, opts)
	if err != nil {
		return fmt.Errorf("queue: enqueue brain_deep: %w", err)
	}
	return nil
}

// EnqueueDedupStage3 enqueues a TaskDedupStage3 brain_deep job comparing
// jobID against candidateJobID — the pair Stage 2 found structurally
// plausible but couldn't confirm (apps/collector/internal/dedup's
// Stage2Result.Stage3CandidateJobID). Always bulk — see
// bulkBrainDeepPriority's own comment.
func (c *Client) EnqueueDedupStage3(ctx context.Context, tx pgx.Tx, jobID, candidateJobID string) error {
	_, err := c.river.InsertTx(ctx, tx, BrainDeepArgs{
		JobID: jobID, Task: TaskDedupStage3, CandidateJobID: candidateJobID,
	}, &river.InsertOpts{Queue: QueueBrainDeep, Priority: bulkBrainDeepPriority})
	if err != nil {
		return fmt.Errorf("queue: enqueue dedup stage3: %w", err)
	}
	return nil
}
