"use client";

import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchResumeStatus, uploadResume } from "@/lib/api";
import { Shell } from "@/components/Shell";

export default function ResumePage() {
  const queryClient = useQueryClient();
  const fileInput = useRef<HTMLInputElement>(null);
  const [dragOver, setDragOver] = useState(false);

  const status = useQuery({ queryKey: ["resume-status"], queryFn: fetchResumeStatus });

  const upload = useMutation({
    mutationFn: uploadResume,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["resume-status"] });
    },
  });

  function handleFile(file: File | undefined) {
    if (!file) return;
    if (file.type !== "application/pdf") {
      upload.reset();
      return;
    }
    upload.mutate(file);
  }

  return (
    <Shell>
      <div className="max-w-2xl mx-auto px-6 py-8">
        <h1 className="text-title text-text-primary mb-1">Resume</h1>
        <p className="text-caption text-text-muted mb-6">
          Drives resume_match — the semantic and keyword terms in your priority
          score. Replace it any time; the old one is fully overwritten, not
          merged.
        </p>

        <div
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
          onClick={() => fileInput.current?.click()}
          className={`rounded-[var(--radius-card)] border-2 border-dashed p-10 text-center cursor-pointer transition-colors duration-150 ${
            dragOver
              ? "border-accent-primary bg-accent-subtle"
              : "border-border-strong bg-bg-surface hover:border-text-muted"
          }`}
        >
          <input
            ref={fileInput}
            type="file"
            accept="application/pdf"
            className="hidden"
            onChange={(e) => handleFile(e.target.files?.[0])}
          />
          <p className="text-body text-text-secondary">
            {upload.isPending ? "Uploading and processing…" : "Drop a PDF here, or click to choose one"}
          </p>
        </div>

        {upload.isError && (
          <div className="mt-4 text-body text-danger rounded-[var(--radius-card)] border border-danger/30 bg-danger/10 p-4">
            Upload failed: {upload.error instanceof Error ? upload.error.message : "unknown error"}
          </div>
        )}

        {upload.isSuccess && (
          <div className="mt-4 text-body text-success rounded-[var(--radius-card)] border border-success/30 bg-success/10 p-4">
            Uploaded — {upload.data.skills.length} skills extracted, embedding
            queued for recompute.
            {upload.data.skills_added && upload.data.skills_added.length > 0 && (
              <p className="text-caption mt-1">
                New: {upload.data.skills_added.join(", ")}
              </p>
            )}
            {upload.data.skills_removed && upload.data.skills_removed.length > 0 && (
              <p className="text-caption mt-1 text-text-muted">
                No longer present: {upload.data.skills_removed.join(", ")}
              </p>
            )}
          </div>
        )}

        <div className="mt-8">
          <h2 className="text-heading text-text-primary mb-3">Current status</h2>
          {status.isLoading && <p className="text-body text-text-muted">Loading…</p>}
          {status.isError && (
            <p className="text-body text-danger">Failed to load resume status.</p>
          )}
          {status.data && (
            <div className="rounded-[var(--radius-card)] border border-border-subtle bg-bg-surface p-4 space-y-3">
              <Row label="Resume loaded" value={status.data.has_resume ? "Yes" : "No"} />
              {status.data.has_resume && (
                <>
                  <Row
                    label="Embedding"
                    value={status.data.has_embedding ? "Computed" : "Pending recompute"}
                  />
                  <Row label="Text length" value={`${status.data.raw_text_length} characters`} />
                  {status.data.updated_at && (
                    <Row
                      label="Last updated"
                      value={new Date(status.data.updated_at).toLocaleString()}
                    />
                  )}
                </>
              )}
              <div>
                <p className="text-label text-text-secondary mb-2">
                  Skills ({status.data.skills.length})
                </p>
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
              </div>
            </div>
          )}
        </div>
      </div>
    </Shell>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between text-body">
      <span className="text-text-secondary">{label}</span>
      <span className="text-text-primary font-mono tabular-nums">{value}</span>
    </div>
  );
}
