"""Inspect current inn route anchor matches without sending game inputs."""

import argparse
import os
import base64
import datetime as dt
import io
import json
import math
import urllib.request
import time
from pathlib import Path

from PIL import Image
import numpy as np
from import_inn_route import BOXES, sample_scene, architecture_mask


def fresh_frame(http):
    api = os.environ.get('MAAEND_API_URL', 'http://127.0.0.1:12701/api').rstrip('/') + '/maa/instances/' + os.environ.get('MAAEND_INSTANCE_ID', 'innreact')
    previous = http.open(api + '/screenshot', timeout=8).read()
    request = urllib.request.Request(api + '/screenshot/subscribe',
        data=b'{"subscriber_id":"inn-route-status","interval_ms":100}',
        headers={'Content-Type': 'application/json'})
    http.open(request, timeout=5).read()
    try:
        deadline = time.monotonic() + 3
        while time.monotonic() < deadline:
            data = http.open(api + '/screenshot', timeout=8).read()
            if data != previous:
                return data
            time.sleep(.1)
        raise ValueError('No fresh changing game frame; calibration capture cancelled')
    finally:
        http.open(urllib.request.Request(api + '/screenshot/unsubscribe',
            data=b'{"subscriber_id":"inn-route-status"}',
            headers={'Content-Type': 'application/json'}), timeout=5).read()


def interval_geometry(offsets, interval):
    """Match only the measured corridor; never extend either endpoint."""
    try:
        a, b = interval['boundaries']
        evidence = interval['evidence']
        cross = interval['cross_track_pixels']
        agreement = interval['progress_difference']
        endpoint = interval.get('endpoint_tolerance_pixels',0)
        if (len(evidence) != 2 or any(not isinstance(v, str) or not v.strip() for v in evidence)
                or evidence[0].strip() == evidence[1].strip()
                or not math.isfinite(cross) or not 0 < cross <= 6
                or not math.isfinite(agreement) or not 0 < agreement <= .25
                or not math.isfinite(endpoint) or not 0 <= endpoint <= 6):
            return False
        progress = []
        if len(a) != 2 or len(b) != 2 or len(offsets) != 2:
            return False
        for point, first, last in zip(offsets, a, b):
            if any(len(v) != 2 for v in (point, first, last)):
                return False
            if any(type(v) is not int or abs(v) > 72 for v in first + last):
                return False
            dx, dy = last[0] - first[0], last[1] - first[1]
            if not 8 <= math.hypot(dx, dy) < 80:
                return False
            ex, ey = point[0] - first[0], point[1] - first[1]
            fraction = (ex*dx + ey*dy) / (dx*dx + dy*dy)
            if not 0 <= fraction <= 1:
                nearest = max(0,min(1,fraction))
                if endpoint == 0 or math.hypot(ex-nearest*dx,ey-nearest*dy) > endpoint:
                    return False
                fraction = nearest
            elif math.hypot(ex-fraction*dx,ey-fraction*dy) > cross:
                return False
            progress.append(fraction)
        return abs(progress[0] - progress[1]) <= agreement
    except (KeyError, TypeError, ValueError):
        return False


def inspect_endpoint(image, route, endpoint, search_window=None):
    """Reproduce bounded landmark/interval gates without OCR or inputs."""
    interval = route.get(endpoint + '_interval')
    horizontal, vertical = 6, 4
    if search_window is not None:
        horizontal,vertical=search_window
        if not (0<=horizontal<=78 and 0<=vertical<=78):raise ValueError("Invalid diagnostic search window")
    if interval is not None:
        try:
            points = [p for boundary in interval['boundaries'] for p in boundary]
            horizontal = (max(abs(p[0]) for p in points) + math.ceil(max(interval['cross_track_pixels'],interval.get('endpoint_tolerance_pixels',0))) + 1)//2*2
            vertical = (max(abs(p[1]) for p in points) + math.ceil(max(interval['cross_track_pixels'],interval.get('endpoint_tolerance_pixels',0))) + 1)//2*2
            if not 0 <= horizontal <= 78 or not 0 <= vertical <= 78:
                raise ValueError('Invalid interval search bounds')
        except (KeyError, TypeError, ValueError, OverflowError):
            return {'id': route['id'], 'endpoint': endpoint, 'landmark_gate_matches': False, 'reason': 'invalid interval'}
    best = []
    for i, patch in enumerate(route[endpoint]):
        raw = base64.b64decode(patch['pixels'] if isinstance(patch, dict) else patch)
        box = patch['box'] if isinstance(patch, dict) else [BOXES[i][0], BOXES[i][1], BOXES[i][2]-BOXES[i][0], BOXES[i][3]-BOXES[i][1]]
        mask = np.asarray(architecture_mask(raw) if isinstance(patch, dict) and patch.get('architecture_only') else [True]*(len(raw)//3))
        expected = ((box[2]+7)//8)*((box[3]+7)//8)*3
        texture = sum(abs(raw[j]-raw[j-3]) for j in range(3,len(raw)) if mask[j//3] and mask[(j-3)//3])
        minimum_texture = int(mask.sum())*6 if isinstance(patch,dict) and patch.get('architecture_only') else expected*4
        if (box[0] < 500 or box[0]+box[2] > 1110 or box[1] < 140 or box[1]+box[3] > 540
                or not 80 <= box[2] <= 160 or box[3] != 40 or len(raw) != expected
                or int(mask.sum())*3 < expected//3 or texture < minimum_texture):
            return {'id':route['id'], 'endpoint':endpoint, 'landmark_gate_matches':False, 'reason':'invalid reference landmark'}
        reference = np.frombuffer(raw, dtype=np.uint8).astype(float).reshape(-1, 3)
        choice = None
        for dy in range(-vertical, vertical+1, 2):
            for dx in range(-horizontal, horizontal+1, 2):
                shifted = [box[0]+dx, box[1]+dy, box[2], box[3]]
                if shifted[0] < 0 or shifted[1] < 0 or shifted[0]+shifted[2] > 1280 or shifted[1]+shifted[3] > 720:
                    continue
                current = np.frombuffer(sample_scene(image, shifted), dtype=np.uint8).astype(float).reshape(-1, 3)
                delta = (current-reference)[mask]
                if not delta.size:
                    continue
                lighting = delta.mean(axis=0)
                residual = np.abs(delta-lighting)
                mean = float(residual.mean())
                changed = float(np.count_nonzero(residual > 24)/residual.size)
                accepted = bool(np.max(np.abs(lighting)) <= 25 and mean <= 12 and np.count_nonzero(residual > 24) <= residual.size//5 and residual.size >= len(raw)//3)
                candidate = {'accepted': accepted, 'mean_error': mean, 'changed_fraction': changed, 'offset': [dx,dy]}
                # Runtime retains the first minimum in dy-then-dx order.
                if choice is None or (accepted and not choice['accepted']) or (accepted == choice['accepted'] and mean < choice['mean_error']):
                    choice = candidate
        best.append(choice or {'accepted':False, 'mean_error':255, 'changed_fraction':1, 'offset':[0,0]})
    photometric = len(best) == 2 and all(p['accepted'] for p in best)
    geometry = interval is None or interval_geometry([p['offset'] for p in best], interval)
    return {'id':route['id'], 'endpoint':endpoint, 'patches':best,
            'interval_present':interval is not None, 'photometric_matches':photometric,
            'interval_geometry_matches':geometry, 'landmark_gate_matches':photometric and geometry,
            'numbered_board_check_required':route.get(endpoint+'_pose') is not None}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--capture', action='store_true')
    parser.add_argument('--image', type=Path, help='Inspect a saved image entirely offline')
    parser.add_argument('--routes', type=Path, default=Path('install/data/Innkeeping/fixed-routes.json'))
    parser.add_argument('--search', help='Inspect only this route')
    parser.add_argument('--endpoint', choices=['start','end'], default='start')
    args = parser.parse_args()
    if args.image and args.capture:
        parser.error('--capture applies only to a fresh live frame')
    if args.image:
        data = args.image.read_bytes()
    else:
        http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        data = fresh_frame(http)
    with Image.open(io.BytesIO(data)) as source:
        if source.size != (1280,720):
            parser.error('Image must be 1280x720')
        image = source.convert('RGB')
    routes = json.loads(args.routes.read_text('utf-8'))
    if args.search:
        routes = [r for r in routes if r['id'] == args.search]
        if len(routes) != 1:
            parser.error('Expected exactly one matching route')
    matches = [inspect_endpoint(image,r,end) for r in routes for end in ([args.endpoint] if args.search else ['start','end'])]
    matches.sort(key=lambda r: (not r['landmark_gate_matches'], max((p['mean_error'] for p in r.get('patches',[])), default=255)))
    result = {'at':dt.datetime.now(dt.timezone.utc).isoformat(), 'offline':bool(args.image),
              'input_authorized':False, 'interaction_verified':False, 'closest':matches[:5]}
    if args.capture:
        folder = Path('I:/MaaEndData/Images/Innkeeping/route-calibration-stills')
        folder.mkdir(parents=True, exist_ok=True)
        file = folder / (dt.datetime.now(dt.timezone.utc).strftime('%Y%m%d-%H%M%S-%f') + '.png')
        file.write_bytes(data)
        result['image'] = str(file)
    print(json.dumps(result))


if __name__ == '__main__':
    main()
