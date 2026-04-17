import { describe, it, expect, beforeEach } from "vitest";
import {
  backgroundTasksByQuery,
  incBg,
  decBg,
  totalBgTasks,
} from "../index.js";

describe("background task counters", () => {
  beforeEach(() => {
    backgroundTasksByQuery.clear();
  });

  it("reports zero when nothing is active", () => {
    expect(totalBgTasks()).toBe(0);
  });

  it("counts a single task_started", () => {
    incBg("q1");
    expect(totalBgTasks()).toBe(1);
  });

  it("sums counters across concurrent queries", () => {
    incBg("__regular");
    incBg("task-1");
    incBg("task-1");
    incBg("task-2");
    expect(totalBgTasks()).toBe(4);
  });

  it("decBg on one key does not affect other keys", () => {
    incBg("__regular");
    incBg("task-1");
    incBg("task-1");
    decBg("task-1");
    expect(backgroundTasksByQuery.get("__regular")).toBe(1);
    expect(backgroundTasksByQuery.get("task-1")).toBe(1);
    expect(totalBgTasks()).toBe(2);
  });

  it("decBg removes the entry when count reaches zero", () => {
    incBg("q1");
    decBg("q1");
    expect(backgroundTasksByQuery.has("q1")).toBe(false);
    expect(totalBgTasks()).toBe(0);
  });

  it("decBg clamps at zero and never goes negative", () => {
    decBg("nonexistent");
    decBg("nonexistent");
    expect(backgroundTasksByQuery.has("nonexistent")).toBe(false);
    expect(totalBgTasks()).toBe(0);
  });

  it("deleting a key in finally drops leaked counters regardless of value", () => {
    // Simulates the SDK emitting task_started without a matching
    // task_notification — the entry would otherwise leak forever.
    incBg("q1");
    incBg("q1");
    incBg("q1");
    expect(backgroundTasksByQuery.get("q1")).toBe(3);

    // finally block: drop everything scoped to this query
    backgroundTasksByQuery.delete("q1");

    expect(totalBgTasks()).toBe(0);
  });

  it("a leaked query cannot keep another query's counter alive", () => {
    // Query A leaks a task_started. Query B completes cleanly.
    incBg("queryA");
    incBg("queryB");
    decBg("queryB");

    // Query B's finally runs
    backgroundTasksByQuery.delete("queryB");
    expect(totalBgTasks()).toBe(1); // only A's leak remains

    // Query A's finally runs
    backgroundTasksByQuery.delete("queryA");
    expect(totalBgTasks()).toBe(0);
  });
});
