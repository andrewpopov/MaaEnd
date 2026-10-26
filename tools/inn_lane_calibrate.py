"""Record individual middle-lane edges with a fresh HUD gate and tray checks.

This supervised calibration never starts a session, retries a failed edge, or
promotes a route. Keep screenshots on I:. Each endpoint still needs physical
review before the measured edge can enter the production graph.
"""
import argparse
import json
from pathlib import Path

from inn_fixed_trial import Trial
from inn_route_calibrate import validate_steps


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--steps", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--run", action="store_true")
    args = parser.parse_args()
    if not args.run:
        parser.error("Explicit --run is required")
    if args.output.resolve().drive.upper() == "C:":
        parser.error("Store calibration images off C:")
    steps = json.loads(args.steps.read_text("utf8"))
    validate_steps(steps)
    if any(s["Kind"] != "walk" or not ((s["X"] == 195 and s["Y"] in (471, 631)) or (s["Y"] == 551 and s["X"] in (115, 275))) for s in steps):
        parser.error("Use only cardinal middle-lane and interaction-spur inputs")
    trial = Trial(Path.cwd(), args.output.resolve())
    trial.post("/screenshot/subscribe", {"subscriber_id": "inn-lane-calibration", "interval_ms": 100})
    try:
        before = trial.inspect()
        if sum(before["tray"].values()) or before.get("unknown"):
            raise RuntimeError("This calibration requires a verified empty tray")
        for index, step in enumerate(steps):
            node = {
                "recognition": "OCR", "roi": [15, 15, 205, 65], "expected": ["End Session"],
                "pre_delay": 0, "post_delay": 0, "rate_limit": 0,
                "pre_wait_freezes": {"time": 250, "target": [510, 150, 600, 40], "threshold": .99, "timeout": 2000},
                "action": {"type": "Swipe", "param": {"begin": [195, 551], "end": [step["X"], step["Y"]], "duration": step["Milliseconds"], "end_hold": step.get("HoldMilliseconds", 0)}},
                "next": ["InnLaneEndpoint"], "timeout": 2000,
            }
            endpoint = {"recognition": "OCR", "roi": [15, 15, 205, 65], "expected": ["End Session"], "pre_wait_freezes": {"time": 250, "target": [510, 150, 600, 40], "threshold": .99, "timeout": 2000}, "action": "DoNothing", "pre_delay": 0, "post_delay": 0, "rate_limit": 0}
            trial.trace("edge_start", index=index, step=step, inventory_mode="empty")
            if not trial.run("InnLaneInput", {"InnLaneInput": node, "InnLaneEndpoint": endpoint}, "Empty middle-lane calibration edge " + str(index), 25):
                trial.trace("stopped", reason="Edge did not finish; no retry", index=index)
                return
            after = trial.inspect()
            if after["tray"] != before["tray"] or after.get("unknown"):
                raise RuntimeError("Inventory changed during calibration; no further input")
            trial.trace("edge_recorded", index=index, endpoint_verified=False)
            before = after
        trial.trace("finished", endpoint_verified=False, promotion_authorized=False)
    finally:
        trial.post("/screenshot/unsubscribe", {"subscriber_id": "inn-lane-calibration"})


if __name__ == "__main__":
    main()
