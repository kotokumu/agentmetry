import { afterEach, describe, expect, it, vi } from "vitest";
import { retentionClient } from "../api/retention-client";
import "./retention-settings";
import type { RetentionSettings } from "./retention-settings";

afterEach(() => { document.body.replaceChildren(); vi.restoreAllMocks(); });

const arrange = () => {
  vi.spyOn(retentionClient, "status").mockResolvedValue({ policy: { enabled: true, archiveDays: 30, deleteDays: 365, revision: 2n }, operations: [], cycles: [] });
  vi.spyOn(retentionClient, "capacity").mockResolvedValue({ totalAllocatedBytes: 2_400_020n, databaseBytes: 2_000_000n, databaseUnusedBytes: 100_000n, walBytes: 20n, archiveBytes: 400_000n, archiveAllocatedBytes: 400_000n, stagingAllocatedBytes: 0n, activeRawBytes: 500_000n, observationBytes: 200_000n, queryProjectionBytes: 300_000n, estimatedArchivePeakBytes: 100_000n, estimatedRestorePeakBytes: 800_000n, filesystemFreeBytes: 9_000_000n, unavailableReason: "", warning: "" });
  vi.spyOn(retentionClient, "segments").mockResolvedValue({ segments: [{ id: "abc", minReceivedAt: new Date("2026-01-01T00:00:00Z"), maxReceivedAt: new Date("2026-01-01T01:00:00Z"), exportCount: 4n, originalBytes: 1000n, storedBytes: 250n, payloadIntegrity: "intact", metadataIntegrity: "verifiable", integrityError: "", scheduleUnavailableReason: "policy disabled" }], nextPageToken: "" });
};

describe("retention settings", () => {
  it("shows policy, capacity, raw-only archive inventory, and restore controls without manual deletion", async () => {
    arrange();
    const element = document.createElement("am-retention-settings") as RetentionSettings;
    document.body.append(element);
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("Data retention"));
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("abc"));
    const text = element.shadowRoot?.textContent ?? "";
    expect(text).toContain("cross-export compressed files");
    expect(text).toContain("Archive files");
    expect(text).toContain("Metadata");
    expect(text).toContain("policy disabled");
    expect(text).toContain("Restore a receive-time period");
    expect([...element.shadowRoot!.querySelectorAll("button")].map((button) => button.textContent)).not.toContain("Delete now");
  });

  it("persists enabled archive and deletion day cutoffs", async () => {
    arrange();
    const update = vi.spyOn(retentionClient, "updatePolicy").mockResolvedValue();
    const element = document.createElement("am-retention-settings") as RetentionSettings;
    document.body.append(element);
    await vi.waitFor(() => expect(element.shadowRoot?.querySelector<HTMLInputElement>("[name=archiveDays]")?.value).toBe("30"));
    element.shadowRoot?.querySelector<HTMLFormElement>("form")?.dispatchEvent(new SubmitEvent("submit", { bubbles: true, cancelable: true }));
    await vi.waitFor(() => expect(update).toHaveBeenCalledWith(true, 30, 365));
  });

  it("loads every inventory page and does not hide running operation status", async () => {
    arrange();
    vi.mocked(retentionClient.segments).mockReset()
      .mockResolvedValueOnce({ segments: [], nextPageToken: "next" })
      .mockResolvedValueOnce({ segments: [{ id: "second-page", minReceivedAt: new Date("2026-01-02T00:00:00Z"), maxReceivedAt: new Date("2026-01-02T01:00:00Z"), exportCount: 1n, originalBytes: 10n, storedBytes: 5n, payloadIntegrity: "corrupt", metadataIntegrity: "verifiable", integrityError: "digest mismatch", scheduleUnavailableReason: "" }], nextPageToken: "" });
    vi.mocked(retentionClient.status).mockResolvedValue({
      policy: { enabled: true, archiveDays: 30, deleteDays: 365, revision: 2n },
      operations: Array.from({ length: 11 }, (_, index) => ({ id: `op-${index}`, kind: "restore", status: "running", phase: "content_removed", affectedExports: 1n, affectedSegments: 1n, error: index === 10 ? "retrying cleanup" : "" })),
      cycles: [{ id: "cycle", status: "failed", cohortSize: 0n, cohortUnavailableReason: "cohort unavailable", error: "cohort unavailable", pendingChildren: 0n, runningChildren: 0n, completedChildren: 0n, failedChildren: 0n, cancelledChildren: 0n }],
    });
    const element = document.createElement("am-retention-settings") as RetentionSettings;
    document.body.append(element);
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("second-page"));
    const text = element.shadowRoot?.textContent ?? "";
    expect(text).toContain("digest mismatch");
    expect(text).toContain("retrying cleanup");
    expect(text).toContain("cohort unavailable");
  });

  it("keeps polling while a maintenance cycle is running without child operations", async () => {
    arrange();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.mocked(retentionClient.status)
      .mockResolvedValueOnce({
        policy: { enabled: true, archiveDays: 30, deleteDays: 365, revision: 2n },
        operations: [],
        cycles: [{ id: "cycle", status: "running", cohortSize: 0n, cohortUnavailableReason: "", error: "", pendingChildren: 0n, runningChildren: 0n, completedChildren: 0n, failedChildren: 0n, cancelledChildren: 0n }],
      })
      .mockResolvedValueOnce({
        policy: { enabled: true, archiveDays: 30, deleteDays: 365, revision: 2n },
        operations: [],
        cycles: [{ id: "cycle", status: "completed", cohortSize: 0n, cohortUnavailableReason: "", error: "", pendingChildren: 0n, runningChildren: 0n, completedChildren: 0n, failedChildren: 0n, cancelledChildren: 0n }],
      });
    const element = document.createElement("am-retention-settings") as RetentionSettings;
    document.body.append(element);
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("running"));
    await vi.advanceTimersByTimeAsync(1_000);
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("completed"));
    expect(retentionClient.status).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });

  it("submits segment and period restores with finite holds and exposes the durable result", async () => {
    arrange();
    const segmentRestore = vi.spyOn(retentionClient, "restoreSegment").mockResolvedValue({ operationId: "segment-op", accepted: true, result: "current", currentSegmentIds: ["abc"], activeMatches: 0n, archivedMatches: 4n, deletedMatches: 0n });
    const periodRestore = vi.spyOn(retentionClient, "restorePeriod").mockResolvedValue({ operationId: "period-op", accepted: true, result: "current", currentSegmentIds: [], activeMatches: 1n, archivedMatches: 2n, deletedMatches: 1n });
    const element = document.createElement("am-retention-settings") as RetentionSettings;
    document.body.append(element);
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("abc"));
    const buttons = [...element.shadowRoot!.querySelectorAll("button")];
    buttons.find((button) => button.textContent?.includes("Restore…"))?.click();
    await vi.waitFor(() => expect(element.shadowRoot?.querySelector("dialog")?.open).toBe(true));
    buttons.find((button) => button.textContent?.includes("Restore segment"))?.click();
    await vi.waitFor(() => expect(segmentRestore).toHaveBeenCalledWith("abc", 30));
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("segment-op"));

    const dateInputs = [...element.shadowRoot!.querySelectorAll<HTMLInputElement>('input[type="datetime-local"]')];
    dateInputs[0].value = "2026-01-01T00:00";
    dateInputs[0].dispatchEvent(new Event("input", { bubbles: true }));
    dateInputs[1].value = "2026-01-02T00:00";
    dateInputs[1].dispatchEvent(new Event("input", { bubbles: true }));
    [...element.shadowRoot!.querySelectorAll("button")].find((button) => button.textContent?.includes("Restore period"))?.click();
    await vi.waitFor(() => expect(periodRestore).toHaveBeenCalledWith(new Date("2026-01-01T00:00"), new Date("2026-01-02T00:00"), 30));
    await vi.waitFor(() => expect(element.shadowRoot?.textContent).toContain("period-op"));
    expect(element.shadowRoot?.textContent).toContain("Active 1");
    expect(element.shadowRoot?.textContent).toContain("Archived 2");
    expect(element.shadowRoot?.textContent).toContain("Deleted 1");
  });

  it("announces capacity warnings and validation errors through accessible status roles", async () => {
    arrange();
    vi.mocked(retentionClient.capacity).mockResolvedValue({ totalAllocatedBytes: 1n, databaseBytes: 1n, databaseUnusedBytes: 0n, walBytes: 0n, archiveBytes: 0n, archiveAllocatedBytes: 0n, stagingAllocatedBytes: 0n, activeRawBytes: 0n, observationBytes: 0n, queryProjectionBytes: 0n, estimatedArchivePeakBytes: 20n, estimatedRestorePeakBytes: 0n, filesystemFreeBytes: 10n, unavailableReason: "", warning: "estimated allocation exceeds availability" });
    const element = document.createElement("am-retention-settings") as RetentionSettings;
    document.body.append(element);
    await vi.waitFor(() => expect(element.shadowRoot?.querySelector('[role="status"]')?.textContent).toContain("exceeds"));
    const dateInputs = [...element.shadowRoot!.querySelectorAll<HTMLInputElement>('input[type="datetime-local"]')];
    dateInputs[0].value = "2026-01-02T00:00";
    dateInputs[0].dispatchEvent(new Event("input", { bubbles: true }));
    dateInputs[1].value = "2026-01-01T00:00";
    dateInputs[1].dispatchEvent(new Event("input", { bubbles: true }));
    [...element.shadowRoot!.querySelectorAll("button")].find((button) => button.textContent?.includes("Restore period"))?.click();
    await vi.waitFor(() => expect(element.shadowRoot?.querySelector('[role="alert"]')?.textContent).toContain("Start must be earlier than end"));
    expect(element.shadowRoot?.querySelector("section")?.getAttribute("aria-busy")).toBe("false");
  });
});
