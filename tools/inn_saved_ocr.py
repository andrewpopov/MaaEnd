"""Read a saved inn screenshot with MaaEnd OCR, without a live game controller."""
import argparse
import json
from pathlib import Path

import cv2
from maa.controller import CustomController
from maa.resource import Resource
from maa.tasker import Tasker
from maa.toolkit import Toolkit



class SavedImageController(CustomController):
    """Image-only Maa controller: every input and app operation is rejected."""
    def __init__(self, path):
        self.image = cv2.imread(str(path))
        if self.image is None or self.image.shape[:2] != (720, 1280):
            raise ValueError("Expected a saved 1280x720 screenshot")
        super().__init__()

    def connect(self):
        return True

    def request_uuid(self):
        return "inn-saved-image-only"

    def get_features(self):
        return 0

    def screencap(self):
        return self.image.copy()

    def reject_input(self, *args):
        return False

    start_app = stop_app = click = swipe = touch_down = touch_move = reject_input
    touch_up = click_key = input_text = key_down = key_up = reject_input


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--resource", type=Path, default=Path("install/resource"))
    parser.add_argument("--roi", nargs=4, type=int, default=[0, 0, 1280, 720])
    parser.add_argument("--only-rec", action="store_true")
    args = parser.parse_args()
    if not args.image.is_file():
        parser.error("A saved image is required")
    if args.output.resolve().drive.upper() == "C:":
        parser.error("Keep OCR evidence and debug images off C:")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    Toolkit.init_option(args.output.parent / "offline-ocr-runtime")
    resource = Resource()
    if not resource.post_bundle(args.resource.resolve()).wait().succeeded:
        raise RuntimeError("Cannot load installed OCR resources")
    controller = SavedImageController(args.image.resolve())
    if not controller.post_connection().wait().succeeded:
        raise RuntimeError("Cannot read the saved screenshot")
    tasker = Tasker()
    if not tasker.bind(resource, controller):
        raise RuntimeError("Cannot bind the offline image controller")
    job = tasker.post_task("InnSavedOCR", {"InnSavedOCR": {
        "recognition": "OCR", "roi": args.roi, "expected": [".*"], "only_rec": args.only_rec,
        "action": "DoNothing", "pre_delay": 0, "post_delay": 0,
    }}).wait()
    detail = job.get()
    if not job.succeeded or detail is None or not detail.nodes:
        raise RuntimeError("Saved-image OCR did not complete")
    result = {"image": str(args.image.resolve()), "live_controller": False,
              "ocr": detail.nodes[-1].recognition.raw_detail}
    args.output.write_text(json.dumps(result, indent=4, ensure_ascii=False) + "\n", encoding="utf8")
    print(json.dumps(result))


if __name__ == "__main__":
    main()
