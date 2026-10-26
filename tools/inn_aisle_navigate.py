"""Supervised middle-aisle movement with native physical-table OCR checkpoints.

This measures a route; it never grants production replay credit. The camera must
already face the dining room. Every pulse is admitted by fresh native phase and
physical-board recognition, and a missing or stalled checkpoint stops execution.
"""
import argparse
import json
import cv2
from pathlib import Path
import time

from inn_fixed_trial import Trial
from inn_native_reader import NativeInnReader
from inn_route_status import fresh_frame


def odd_board(report):
    candidates = [(seat, box) for seat, box in report["boards"].items()
                  if seat in ("1", "3", "5") and box[0] >= 450]
    return max(candidates, key=lambda item: item[1][3], default=None)


def center_command(board, left=False):
    x, _, w, _ = board
    center = x + w / 2
    depth_normalized = ((640-center) if left else (center-640)) / board[3]
    if depth_normalized < 4:
        return (275 if left else 115), 551, 120
    if depth_normalized > 8:
        return (115 if left else 275), 551, 120
    return None


class AisleTrial(Trial):
    def __init__(self, root, output, world=False):
        super().__init__(root, output)
        self.world = world
        self.reader = None

    def observe(self):
        node = {"recognition":"DirectHit","action":{"type":"Custom","param":{
            "custom_action":"InnkeepingAssistAction"}},"pre_delay":0,"post_delay":0,"rate_limit":0}
        if not self.run("InnNativeSnapshot",{"InnNativeSnapshot":node},"Read native inn checkpoint",15):
            raise RuntimeError("Native read-only checkpoint failed")
        state=self.get("/maa/state")["instances"]["innreact"]
        task_id=state["task_run_state"]["pending_task_ids"][0]
        native=None
        reports=self.root/"install/data/Innkeeping"
        for file in sorted(reports.glob("decision-*.tmp.json"),key=lambda p:p.stat().st_mtime,reverse=True):
            candidate=json.loads(file.read_text("utf8"))
            if candidate.get("task_id")==task_id:
                native=candidate
                break
        if not native or native.get("game_inputs_sent") or not Path(native["image"]).is_file():
            raise RuntimeError("No paired native checkpoint report for this task")
        path=Path(native["image"])
        if self.reader is None:
            self.reader=NativeInnReader(self.root,self.output,path)
        report=self.reader.inspect(path,native_report=native)
        phase = report["view"]["phase"]
        allowed = ("world",) if self.world else ("active", "closing", "closed_serving")
        if phase not in allowed:
            raise RuntimeError("Unexpected phase: " + phase)
        self.trace("aisle_observation", image=str(path), boards=report["boards"], view=report["view"])
        return report

    def pulse(self, report, command):
        selected = odd_board(report)
        if selected is None:
            selected = max(report["boards"].items(), key=lambda item: item[1][3], default=None)
        if selected is None:
            raise RuntimeError("No physical right-side table board; no movement sent")
        seat, box = selected
        x, y, w, h = box
        roi = [max(0, x-2), max(0, y-1), min(w+4,1280-max(0,x-2)), min(h+2,720-max(0,y-1))]
        guard = Path(report["image"]).with_name(Path(report["image"]).stem + "-guard.png")
        frame = cv2.imread(report["image"])
        cv2.imwrite(str(guard), frame[y:y+h,x:x+w])
        roi = [max(0,x-30),max(0,y-20),min(w+60,1280-max(0,x-30)),min(h+40,720-max(0,y-20))]
        phase = {"recognition": "OCR", "roi": [15, 10, 220, 65],
                 "expected": ["Explore" if self.world else "End Session"]}
        node = {"recognition": "And", "all_of": [phase,
            {"recognition": "Custom", "custom_recognition": "InnkeepingPhysicalBoard",
             "custom_recognition_param": {"number": seat,"box":box,"template":str(guard)}}],
            "action": {"type": "Swipe", "param": {"begin": [195,551],
                "end": list(command[:2]), "duration": 20, "end_hold": command[2]}},
            "timeout": 1800, "pre_delay": 0, "post_delay": 0, "rate_limit": 0,
            "post_wait_freezes": {"time": 250, "target": [510,150,600,40], "threshold": .99, "timeout": 2000}}
        if not self.run("InnAislePulse", {"InnAislePulse": node}, "Inn aisle checkpoint", 10):
            raise RuntimeError("Fresh physical-board movement guard failed")
        self.trace("aisle_pulse", command=command, board=seat, box=box, production_verified=False)

    def row(self, target):
        target = str(target if target % 2 else target-1)
        previous = None
        for _ in range(16):
            report = self.observe()
            selected=odd_board(report)
            if selected is None:
                selected=max(((seat,box) for seat,box in report["boards"].items() if seat in ("2","4","6")),key=lambda item:item[1][3],default=None)
            if selected is None:
                raise RuntimeError("Physical aisle board unreadable")
            seat,box=selected
            left=int(seat)%2==0
            command=center_command(box,left)
            destination=report["boards"].get(target) or report["boards"].get(str(int(target)+1))
            observed_row=(int(seat)-1)//2
            target_row=(int(target)-1)//2
            if command is None and observed_row != target_row:
                command = (195,471,900 if self.world else 1100) if observed_row<target_row else (195,631,900)
            elif command is None and destination:
                x,y,w,h = destination
                if 325 <= y <= 390 and h >= 25:
                    self.trace("aisle_row_arrived", target=target, box=destination)
                    return report
                command = (195,471,250) if y < 325 or h < 25 else (195,631,200)
            elif command is None:
                command = (195,471,450) if observed_row < target_row else (195,631,450)
            signature = (seat, tuple(box), command)
            if previous and previous[0] == seat and previous[2] == command:
                distance = sum(abs(a-b) for a,b in zip(previous[1], box))
                if distance < 4:
                    raise RuntimeError("Physical checkpoint did not move; stopped without repeating input")
            previous = signature
            self.pulse(report, command)
        raise RuntimeError("Aisle checkpoint did not converge")

    def approach(self, target):
        report = self.row(target)
        for _ in range(5):
            board = report["boards"].get(str(target))
            if board:
                x,y,w,h = board
                goal = (680,800) if target % 2 else (450,650)
                if goal[0] <= x+w/2 <= goal[1] and 310 <= y <= 410:
                    self.trace("table_branch_arrived", seat=target, box=board, serving_verified=False)
                    return report
            # Short branches leave the aisle only at the confirmed target row.
            direction = 275 if target % 2 else 115
            if board and ((target % 2 and board[0] < 680) or (not target % 2 and board[0] > 650)):
                direction = 115 if target % 2 else 275
            self.pulse(report, (direction,551,180))
            report = self.observe()
        raise RuntimeError("Table branch did not reach its physical board")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run", action="store_true")
    parser.add_argument("--prepared-camera", action="store_true")
    parser.add_argument("--world", action="store_true")
    parser.add_argument("--seat", type=int, choices=range(1,7), required=True)
    parser.add_argument("--row-only", action="store_true")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if not args.run or not args.prepared_camera:
        parser.error("Explicit --run and a verified --prepared-camera are required")
    t = AisleTrial(Path.cwd(), args.output.resolve(), args.world)
    if args.row_only:
        t.row(args.seat)
    else:
        t.approach(args.seat)


if __name__ == "__main__":
    main()
