"use client";

import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchResumeStatus, uploadResume } from "@/lib/api";
import { ErrorNotice, PageHeader, Skeleton } from "@/components/ui";

// Mirrors apps/api/internal/resume's maxUploadBytes — checked here too so
// a 40 MB scan is refused before it's uploaded, not after.
const MAX_BYTES = 10 * 1024 * 1024;

function validate(file: File): string | null {
  const isPdf = file.type === "application/pdf" || file.name.toLowerCase().endsWith(".pdf");
  if (!isPdf) return "Only PDF files are supported. Export your resume as a PDF and try again.";
  if (file.size === 0) return "That file is empty.";
  if (file.size > MAX_BYTES) return "That PDF is over 10 MB. A resume is usually well under 1 MB — try exporting it again.";
  return null;
}

export default function ResumePage() {
  const queryClient = useQueryClient();
  const fileInput = useRef<HTMLInputElement>(null);
  const [dragOver, setDragOver] = useState(false);
  const [validationError, setValidationError] = useState<string | null>(null);

  const status = useQuery({ queryKey: ["resume-status"], queryFn: fetchResumeStatus });

  const upload = useMutation({
    mutationFn: uploadResume,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["resume-status"] });
    },
  });

  function handleFile(file: File | undefined) {
    if (!file || upload.isPending) return;
    const problem = validate(file);
    setValidationError(problem);
    if (problem) {
      upload.reset();
      return;
    }
    upload.mutate(file);
  }

  function open() {
    fileInput.current?.click();
  }

  return (
    <div className="max-w-2xl mx-auto page">
      <PageHeader
        title="Resume"
        subtitle="Match scores compare every posting against this. It stays on this machine and is never sent to a hosted model."
      />

      <div
        role="button"
        tabIndex={0}
        aria-label="Upload resume PDF"
        aria-disabled={upload.isPending || undefined}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            open();
          }
        }}
        onDragOver={(e) => {
          e.preventDefault();
          setDragOver(true);
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragOver(false);
          handleFile(e.dataTransfer.files[0]);
        }}
        onClick={open}
        className={`rounded-[var(--radius-card)] border-2 border-dashed p-8 sm:p-10 text-center cursor-pointer transition-colors duration-150 ${
          dragOver ? "border-accent-primary bg-accent-subtle" : "border-border-strong bg-bg-surface hover:border-text-muted"
        }`}
      >
        <input
          ref={fileInput}
          type="file"
          accept="application/pdf,.pdf"
          className="hidden"
          tabIndex={-1}
          onChange={(e) => {
            handleFile(e.target.files?.[0]);
            // Allow picking the same file again after a failure.
            e.target.value = "";
          }}
        />
        <p className="text-body text-text-secondary">
          {upload.isPending ? "Uploading and reading…" : "Drop a PDF here, or click to choose one"}
        </p>
        <p className="text-caption text-text-muted mt-1">PDF only, up to 10 MB. Replacing it overwrites the previous one.</p>
      </div>

      {validationError && (
        <p role="alert" className="mt-4 text-body text-danger rounded-[var(--radius-card)] border border-danger/30 bg-danger/10 p-4">
          {validationError}
        </p>
      )}

      {upload.isError && (
        <div className="mt-4">
          <ErrorNotice error={upload.error} fallback="Upload failed." />
        </div>
      )}

      {upload.isSuccess && (
        <div role="status" className="mt-4 text-body text-success rounded-[var(--radius-card)] border border-success/30 bg-success/10 p-4">
          Uploaded — {upload.data.skills.length} skill{upload.data.skills.length === 1 ? "" : "s"} recognised. Match scores update
          once the embedding is recomputed (usually within a minute).
          {upload.data.skills_added && upload.data.skills_added.length > 0 && (
            <p className="text-caption mt-1">New: {upload.data.skills_added.join(", ")}</p>
          )}
          {upload.data.skills_removed && upload.data.skills_removed.length > 0 && (
            <p className="text-caption mt-1 text-text-muted">No longer present: {upload.data.skills_removed.join(", ")}</p>
          )}
        </div>
      )}

      <section className="mt-8" aria-labelledby="resume-status">
        <h2 id="resume-status" className="text-heading text-text-primary mb-3">
          Current status
        </h2>
        {status.isPending && <Skeleton className="h-32" />}
        {status.isError && <ErrorNotice error={status.error} fallback="Couldn't load resume status." onRetry={() => void status.refetch()} />}
        {status.data && (
          <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4 space-y-3">
            <Row label="Resume loaded" value={status.data.has_resume ? "Yes" : "Not yet"} />
            {status.data.has_resume && (
              <>
                <Row label="Embedding" value={status.data.has_embedding ? "Ready" : "Being recomputed…"} />
                <Row label="Text length" value={`${status.data.raw_text_length.toLocaleString()} characters`} />
                {status.data.updated_at && <Row label="Last updated" value={new Date(status.data.updated_at).toLocaleString()} />}
              </>
            )}
            <div>
              <p className="text-label text-text-secondary mb-2">Skills recognised ({status.data.skills.length})</p>
              {status.data.skills.length === 0 ? (
                <p className="text-caption text-text-muted">
                  {status.data.has_resume
                    ? "None of the skills in Scout's vocabulary were found. Skill match will read as unknown until the resume mentions some."
                    : "Upload a resume to populate this."}
                </p>
              ) : (
                <div className="flex flex-wrap gap-1.5">
                  {status.data.skills.map((skill) => (
                    <span
                      key={skill}
                      className="text-micro px-1.5 py-0.5 rounded-[var(--radius-input)] bg-bg-elevated text-text-muted border border-border-subtle"
                    >
                      {skill.replace(/_/g, " ")}
                    </span>
                  ))}
                </div>
              )}
            </div>
          </div>
        )}
      </section>
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-3 text-body">
      <span className="text-text-secondary">{label}</span>
      <span className="text-text-primary font-mono tabular-nums text-right">{value}</span>
    </div>
  );
}
