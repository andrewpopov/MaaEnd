"""Capture English Endfield operator pages through a connected MaaEnd instance.

Open an operator's Overview page, stop daily tasks, then run:
    .\\tools\\Capture-Roster.ps1
Resume an interrupted scan from any operator's Overview:
    .\\tools\\Capture-Roster.ps1 -Resume <capture-directory>

Requires the repository Python environment (Pillow and numpy), MaaEnd's HTTP API,
and the English PC UI at 1280x720. Navigation only: never equips or spends items.
"""

import argparse
import datetime as dt
import io
import json
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

import numpy as np
from PIL import Image

SCREENS = (
    "01-overview.png", "02-abilities.png", "03-weapon.png",
    "04-gear-summary.png", "05-armor.png", "06-gloves.png",
    "06-kit-1.png", "06-kit-2.png",
)
PAGE_TEXT = {
    "Overview": ("Attributes", [25, 240, 290, 80]),
    r"Ability\s*Matrix": (r"Level\s*Up", [210, 600, 230, 90]),
    "Weapons": ("Switch", [990, 625, 235, 75]),
    "Gear": ("Switch", [990, 625, 235, 75]),
    "inventory": ("Operator Stats.*Attributes", [530, 85, 335, 65]),
}


def now():
    return dt.datetime.now().astimezone().isoformat()


def signature(data):
    with Image.open(io.BytesIO(data)) as image:
        if image.size != (1280, 720):
            raise RuntimeError(f"Expected 1280x720 capture, received {image.size}")
        return np.asarray(image.convert("L").crop((65, 18, 330, 52))).copy()


def same_operator(first, second):
    return float(np.mean(np.abs(first.astype(float) - second.astype(float)) > 30)) < 0.003


class Archive:
    def __init__(self, root, resume=False):
        self.root = root.resolve()
        self.root.mkdir(parents=True, exist_ok=True)
        self.path = self.root / "manifest.json"
        if resume:
            self.data = json.loads(self.path.read_text(encoding="utf-8"))
        else:
            if self.path.exists():
                raise ValueError("Output already contains a scan; use --resume")
            self.data = {"version": 1, "started_at": now(), "operators": [],
                         "complete": False, "changes_applied": False}
        self.data.pop("error", None)
        self.signatures = []
        for record in self.data["operators"]:
            overview = self.root / record["directory"] / SCREENS[0]
            if not overview.is_file():
                raise ValueError(f"Missing operator identity screenshot: {overview}")
            self.signatures.append(signature(overview.read_bytes()))

    def checkpoint(self):
        self.data["updated_at"] = now()
        temporary = self.path.with_suffix(".tmp")
        temporary.write_text(json.dumps(self.data, indent=4), encoding="utf-8")
        temporary.replace(self.path)

    def find_or_add(self, data):
        current = signature(data)
        for index, previous in enumerate(self.signatures):
            if same_operator(current, previous):
                return self.data["operators"][index]
        index = len(self.signatures) + 1
        record = {"index": index, "directory": f"{index:03}", "screens": []}
        (self.root / record["directory"]).mkdir()
        self.data["operators"].append(record)
        self.signatures.append(current)
        self.save(record, SCREENS[0], data)
        return record

    def save(self, record, name, data):
        signature(data)  # Validate PNG and resolution before committing it.
        target = self.root / record["directory"] / name
        temporary = target.with_suffix(".tmp")
        temporary.write_bytes(data)
        temporary.replace(target)
        if name not in record["screens"]:
            record["screens"].append(name)
        self.checkpoint()

    def captured(self, record):
        return all((self.root / record["directory"] / name).is_file() for name in SCREENS)


class MaaClient:
    def __init__(self, base, instance=None):
        parsed = urllib.parse.urlparse(base)
        if parsed.scheme != "http" or parsed.hostname not in {"localhost", "127.0.0.1", "::1"}:
            raise ValueError("The API must be a local HTTP endpoint")
        self.base = base.rstrip("/")
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        instances = self.request("/maa/state")["instances"]
        candidates = [key for key, value in instances.items()
                      if value.get("connected") and value.get("resource_loaded")]
        if instance is None:
            if len(candidates) != 1:
                raise ValueError("Select a connected instance with --instance")
            instance = candidates[0]
        if instance not in instances or instance not in candidates:
            raise ValueError("Instance must already have a connected controller and loaded resources")
        self.instance = instance
        self.prefix = "/maa/instances/" + urllib.parse.quote(instance, safe="")
        self.task_id = "roster-capture-" + uuid.uuid4().hex
        self.owns_task = False
        self.require_idle()

    def request(self, path, payload=None, raw=False):
        data = None if payload is None else json.dumps(payload).encode("utf-8")
        request = urllib.request.Request(self.base + path, data=data,
                                        headers={"Content-Type": "application/json"})
        with self.http.open(request, timeout=10) as response:
            result = response.read()
        return result if raw else json.loads(result)

    def post(self, path, payload):
        return self.request(self.prefix + path, payload)

    def state(self):
        return self.request("/maa/state")["instances"][self.instance]

    def require_idle(self):
        if self.state()["is_running"]:
            raise RuntimeError("Instance is busy; stop its tasks before scanning")

    @staticmethod
    def guard(page):
        text, roi = PAGE_TEXT[page]
        if page == "inventory":
            return {"recognition": "OCR", "roi": roi, "expected": [text]}
        return {"recognition": "OCR", "roi": [0, 0, 460, 65],
                "expected": [r"^.+\s*/\s*" + page + "$" ]}

    def move(self, source, destination, point=None, key=None):
        self.require_idle()
        node = {**self.guard(source), "action": "ClickKey" if key is not None else ("Click" if point else "DoNothing"),
                "pre_delay": 0, "post_delay": 0, "rate_limit": 0,
                "pre_wait_freezes": {"time": 200, "target": [1237, 185, 43, 370]},
                "next": ["RosterCaptureReady"]}
        if key is not None:
            node["key"] = key
        elif point is not None:
            node["target"] = [point[0] - 2, point[1] - 2, 4, 4]
        text, roi = PAGE_TEXT[destination]
        ready = {"recognition": "And", "all_of": [self.guard(destination),
                 {"recognition": "OCR", "roi": roi, "expected": [text]}],
                 "action": "DoNothing", "pre_delay": 0, "post_delay": 0,
                 "pre_wait_freezes": {"time": 150, "target": roi}}
        override = {"RosterCaptureStep": node, "RosterCaptureReady": ready}
        self.post("/tasks/run", [{"entry": "RosterCaptureStep",
                  "pipeline_override": json.dumps(override), "selected_task_id": self.task_id}])
        self.owns_task = True
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            state = self.state()
            status = state["task_run_state"]["statuses"].get(self.task_id)
            if not state["is_running"] and status in {"succeeded", "failed", "stopped"}:
                self.owns_task = False
                if status != "succeeded":
                    raise RuntimeError(f"Navigation failed: {source} -> {destination}; progress saved")
                return
            time.sleep(0.15)  # Poll completion, never delay a game action blindly.
        raise TimeoutError(f"Navigation did not finish: {source} -> {destination}")

    def screenshot(self):
        # HTTP serves a shared cache; allow one subscription refresh after native readiness.
        time.sleep(0.28)
        return self.request(self.prefix + "/screenshot", raw=True)

    def cleanup(self):
        if self.owns_task:
            self.post("/tasks/stop", {})
            self.owns_task = False
        self.post("/screenshot/unsubscribe", {"subscriber_id": self.task_id})


def capture_operator(client, archive, record):
    def save(name):
        archive.save(record, name, client.screenshot())

    client.move("Overview", r"Ability\s*Matrix", point=(1160, 252))
    save(SCREENS[1])
    client.move(r"Ability\s*Matrix", "Overview", key=27)
    client.move("Overview", "Weapons", point=(1250, 350))
    save(SCREENS[2])
    client.move("Weapons", "Gear", point=(1250, 440))
    save(SCREENS[3])
    client.move("Gear", "inventory", point=(1110, 662))
    save(SCREENS[4])
    for name, x in zip(SCREENS[5:], (160, 242, 326)):
        client.move("inventory", "inventory", point=(x, 27))
        save(name)
    client.move("inventory", "Gear", key=27)
    client.move("Gear", "Overview", point=(1250, 258))
    print(f"Captured operator {record['index']}: {len(SCREENS)} screenshots", flush=True)


def scan(client, archive, start_page="Overview", limit=80):
    client.post("/screenshot/subscribe", {"subscriber_id": client.task_id, "interval_ms": 250})
    if start_page != "Overview":
        client.move(start_page, "Overview", point=(1250, 258))
    else:
        # Check page content with no input before saving the initial operator.
        client.move("Overview", "Overview")
    visited = set()
    previous = None
    first = None
    for _ in range(limit + 1):
        data = client.screenshot()
        record = archive.find_or_add(data)
        identity = record["index"]
        if identity == previous:
            raise RuntimeError("Operator did not change after E; stopped without marking the scan complete")
        if identity in visited:
            if identity != first:
                raise RuntimeError("Unexpected carousel jump; scan remains incomplete")
            archive.data["complete"] = all(archive.captured(r) for r in archive.data["operators"])
            archive.data["stop_reason"] = "operator carousel returned to the starting operator"
            return
        if first is None:
            first = identity
        visited.add(identity)
        if len(visited) > limit:
            raise RuntimeError("Operator safety limit reached")
        if not archive.captured(record):
            capture_operator(client, archive, record)
        previous = identity
        client.move("Overview", "Overview", key=69)
    raise RuntimeError("Carousel did not return to the starting operator")

def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--api-url", default="http://127.0.0.1:12702/api")
    parser.add_argument("--instance")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--resume", type=Path)
    parser.add_argument("--start-page", choices=("Overview", "Gear", "Weapons"), default="Overview")
    parser.add_argument("--max-operators", type=int, default=80)
    args = parser.parse_args(argv)
    if args.output and args.resume:
        parser.error("Use --output or --resume, not both")
    if args.max_operators < 1:
        parser.error("--max-operators must be positive")
    root = args.resume or args.output or Path(__file__).resolve().parents[1] / "install" / "data" / "RosterScans" / dt.datetime.now().strftime("%Y%m%d-%H%M%S")
    archive = Archive(root, resume=bool(args.resume))
    client = None
    try:
        client = MaaClient(args.api_url, args.instance)
        archive.data["complete"] = False
        archive.data["finished_at"] = None
        archive.checkpoint()
        print(f"Saving screenshots to {archive.root}", flush=True)
        scan(client, archive, args.start_page, args.max_operators)
        return 0
    except (Exception, KeyboardInterrupt) as error:
        archive.data["error"] = str(error) or "Interrupted by user"
        print(f"Stopped: {archive.data['error']}\nResume: Capture-Roster.ps1 -Resume \"{archive.root}\"", flush=True)
        return 1
    finally:
        if client:
            try:
                client.cleanup()
            except Exception as error:
                print(f"Controller cleanup failed: {error}", flush=True)
        archive.data["finished_at"] = now()
        archive.checkpoint()


if __name__ == "__main__":
    raise SystemExit(main())
