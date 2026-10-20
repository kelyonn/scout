import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import type { ApplicationState, ApplicationsResponse, JobFeedResponse } from "./types";

// Optimistic patches (docs/12-frontend-ux.md section 5: "Optimistic
// everything"). Each returns nothing; callers revert a failed mutation by
// invalidating the same keys, which refetches the server's truth.

export function patchJobState(queryClient: QueryClient, groupId: string, state: ApplicationState): void {
  queryClient.setQueriesData<InfiniteData<JobFeedResponse>>({ queryKey: ["jobs"] }, (old) => {
    if (!old || !Array.isArray(old.pages)) return old;
    return {
      ...old,
      pages: old.pages.map((p) => ({
        ...p,
        data: p.data.map((j) => (j.job_group_id === groupId ? { ...j, state } : j)),
      })),
    };
  });
}

export function patchApplicationState(queryClient: QueryClient, groupId: string, state: ApplicationState): void {
  queryClient.setQueryData<ApplicationsResponse>(["applications"], (old) => {
    if (!old) return old;
    return {
      ...old,
      data: old.data.map((a) =>
        a.job_group_id === groupId ? { ...a, state, state_changed_at: new Date().toISOString() } : a,
      ),
    };
  });
}
