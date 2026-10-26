"""Measure visible counter-edge heading. Read-only; never authorizes movement."""
import argparse
import json
import math
from pathlib import Path

import cv2
import numpy as np


def counter_heading(image):
    if image is None or image.shape[:2] != (720, 1280):
        raise ValueError('Expected a native 1280x720 inn frame')
    gray = cv2.cvtColor(image[440:585, 150:1130], cv2.COLOR_BGR2GRAY)
    lines = None
    # Warm wood borders can have little luminance contrast. Preserve the
    # original detector first, then recover those borders on the same image;
    # the independent-edge and angular-consistency checks below still apply.
    for low, high in ((60, 140), (20, 60)):
        edges = cv2.Canny(gray, low, high)
        lines = cv2.HoughLinesP(edges, 1, np.pi / 1800, 60,
                                minLineLength=350, maxLineGap=40)
        if lines is not None:
            break
    candidates = []
    for line in [] if lines is None else lines.reshape(-1, 4):
        x1, y1, x2, y2 = map(int, line)
        if x2 < x1:
            x1, y1, x2, y2 = x2, y2, x1, y1
        angle = math.degrees(math.atan2(y2-y1, x2-x1))
        if abs(angle) <= 15:
            candidates.append((angle, math.hypot(x2-x1, y2-y1),
                               [x1+150, y1+440, x2+150, y2+440]))
    if not candidates:
        return dict(usable=False, reason='No long counter edges', input_authorized=False)
    angles = np.array([v[0] for v in candidates])
    center = float(np.median(angles))
    consistent = [v for v in candidates if abs(v[0]-center) <= 2]
    if not consistent:
        return dict(usable=False,reason='No consistent counter edges',input_authorized=False)
    # Separate physical edge heights: repeated Hough samples are not new evidence.
    heights = sorted((v[2][1]+v[2][3])/2 for v in consistent)
    levels = []
    for height in heights:
        if not levels or height-levels[-1] >= 8:
            levels.append(height)
    usable = len(levels) >= 2 and len(consistent) >= 2
    return dict(usable=usable, angle_degrees=float(np.median([v[0] for v in consistent])),
                independent_edge_levels=len(levels), edges=[v[2] for v in consistent],
                reason='Counter identity and camera response still require verification',
                input_authorized=False)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('images', nargs='+', type=Path)
    args = parser.parse_args()
    for path in args.images:
        print(json.dumps(dict(image=str(path), **counter_heading(cv2.imread(str(path))))))


if __name__ == '__main__':
    main()


