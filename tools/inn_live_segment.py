"""Replay a bounded middle-lane segment inside one existing reactive Trial.

Keeps the same task and Go session. No pickup or Serve is injected. Normal
observation is restored by PipelineOverride after the last measured movement.
"""
import argparse
import datetime as dt
import json
from pathlib import Path
import time
import uuid

from inn_fixed_trial import Trial
from inn_route_calibrate import validate_steps, swipe_param


def snapshot(root):
    deadline = time.monotonic() + 1
    while True:
        try:
            return json.loads((root / "install/data/Innkeeping/latest-state.json").read_text("utf8"))
        except (PermissionError, json.JSONDecodeError):
            # Windows can briefly deny a reader while the atomic state is replaced.
            # Retry only the read; never resubmit a game input.
            if time.monotonic() >= deadline:
                raise
            time.sleep(.02)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--task-id", type=int, required=True)
    parser.add_argument("--steps", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--start-prompt")
    parser.add_argument("--allow-camera", action="store_true", help="Allow supervised camera normalization; never changes route replay credit")
    parser.add_argument("--run", action="store_true")
    args = parser.parse_args()
    if not args.run or args.output.resolve().drive.upper() == "C:":
        parser.error("Explicit --run and image output off C: are required")
    steps = json.loads(args.steps.read_text("utf8"))
    validate_steps(steps)
    for step in steps:
        if step["Kind"] == "camera" and args.allow_camera:
            continue
        if step["Kind"] != "walk" or not ((step["X"] == 195 and step["Y"] in (471, 631)) or (step["Y"] == 551 and step["X"] in (115, 275))):
            parser.error("Only cardinal middle-lane and interaction-spur movements are allowed")
    root = Path.cwd()
    t = Trial(root, args.output.resolve())
    state = t.get("/maa/state")["instances"]["innreact"]
    if not state["is_running"] or args.task_id not in state["task_run_state"]["pending_task_ids"]:
        raise RuntimeError("The specified task is not running; no input sent")
    pause = t.get("/maa/instances/innreact/tasks/pause")
    if pause["requested"] or pause["paused"]:
        raise RuntimeError("Task was already paused; its pause is preserved")
    t.post("/tasks/pause", {"paused": True})
    deadline = time.monotonic() + 5
    while not t.get("/maa/instances/innreact/tasks/pause")["paused"]:
        if time.monotonic() >= deadline:
            raise RuntimeError("Pause not acknowledged; no pipeline or game input submitted")
        time.sleep(.1)
    before = snapshot(root)
    source = json.loads((root / "assets/resource/pipeline/Innkeeping/Innkeeping.json").read_text("utf8"))
    prefix = "InnLiveSegment" + uuid.uuid4().hex[:10]
    patch = {"InnkeepingReactiveObserve": {"recognition": "DirectHit", "action": "DoNothing", "next": [prefix + "0"], "pre_delay": 0, "post_delay": 0, "rate_limit": 0}}
    for i, step in enumerate(steps):
        node = {"recognition": "OCR", "roi": [15, 15, 205, 65], "expected": ["End Session"],
                "action": {"type": "Swipe", "param": swipe_param(step)},
                "pre_delay": 0, "post_delay": 0, "rate_limit": 0, "timeout": 2500,
                "next": [prefix + str(i + 1) if i + 1 < len(steps) else prefix + "Restore"]}
        if i == 0 and args.start_prompt:
            node["recognition"] = "And"
            node["all_of"] = [{"recognition": "OCR", "roi": [15, 15, 205, 65], "expected": ["End Session"]},
                              {"recognition": "OCR", "roi": [720, 390, 480, 80], "expected": [args.start_prompt]}]
        patch[prefix + str(i)] = node
    patch[prefix + "Restore"] = {"recognition": "DirectHit", "action": {"type": "Custom", "param": {"custom_action": "PipelineOverride", "custom_action_param": {"patch": {"InnkeepingReactiveObserve": source["InnkeepingReactiveObserve"]}, "allow_next": True}}}, "next": ["InnkeepingReactiveObserve"], "pre_delay": 0, "post_delay": 0, "rate_limit": 0}
    (t.output / "pipeline.json").write_text(json.dumps(patch, indent=4) + "\n")
    reply = t.post(f"/tasks/{args.task_id}/pipeline", {"pipelineOverride": json.dumps(patch)})
    if not reply.get("success"):
        raise RuntimeError("Pipeline patch rejected; task remains paused for inspection")
    log_path = root / "install/debug/maafw.log"
    log_offset = log_path.stat().st_size
    log_tail = ""
    resumed = dt.datetime.now(dt.timezone.utc)
    t.trace("segment_submitted", task=args.task_id, steps=steps, before=before, endpoint_verified=False)
    t.post("/screenshot/subscribe", {"subscriber_id": prefix, "interval_ms": 100})
    try:
        t.post("/tasks/pause", {"paused": False})
        deadline = time.monotonic() + sum(s["Milliseconds"] + s.get("HoldMilliseconds", 0) for s in steps) / 1000 + 20
        while time.monotonic() < deadline:
            t.capture()
            live = t.get("/maa/state")["instances"]["innreact"]
            after = snapshot(root)
            with log_path.open("rb") as log:
                if log_path.stat().st_size < log_offset:
                    log_offset = 0
                log.seek(log_offset)
                log_tail = (log_tail + log.read().decode("utf8", errors="replace"))[-100000:]
                log_offset = log.tell()
            restored = any("Node.Action.Succeeded" in line and ('"name":"' + prefix + 'Restore"') in line for line in log_tail.splitlines())
            if restored and dt.datetime.fromisoformat(after["at"]) > resumed:
                t.trace("observation_restored", task=args.task_id, after=after, endpoint_verified=False)
                t.inspect()
                return
            if not live["is_running"]:
                t.trace("task_terminal", task=args.task_id, state=live["task_run_state"])
                return
            time.sleep(.2)
        raise RuntimeError("Observation deadline reached; inspect the same task, do not repeat the movement")
    finally:
        t.post("/screenshot/unsubscribe", {"subscriber_id": prefix})


if __name__ == "__main__":
    main()
