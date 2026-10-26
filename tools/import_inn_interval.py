"""Author a transit interval from two explicitly reviewed safe boundary photos.

Offline only. Clearance review certifies the physical segment between the photos;
landmark matching cannot itself prove obstacle clearance or camera equivalence.
"""
import argparse
import base64
import json
import math
import os
from pathlib import Path
import tempfile

import numpy as np
from PIL import Image

from import_inn_route import architecture_mask, sample_scene


def read_image(path):
    with Image.open(path) as source:
        if source.size != (1280, 720):
            raise ValueError('Evidence must be 1280x720')
        return source.convert('RGB')


def decode_scene(scene):
    if not isinstance(scene, list) or len(scene) != 2:
        raise ValueError('Two stored architectural landmarks are required')
    decoded = []
    for patch in scene:
        box = patch['box']
        if len(box) != 4 or any(type(v) is not int for v in box):
            raise ValueError('Invalid landmark box')
        x, y, w, h = box
        if not (500 <= x and x+w <= 1110 and 140 <= y and y+h <= 540 and 80 <= w <= 160 and h == 40) or patch.get('architecture_only') is not True:
            raise ValueError('Invalid architectural landmark')
        raw = base64.b64decode(patch['pixels'], validate=True)
        expected = ((w+7)//8)*5*3
        if len(raw) != expected:
            raise ValueError('Invalid fingerprint length')
        mask = architecture_mask(raw)
        texture = sum(abs(raw[j]-raw[j-3]) for j in range(3, len(raw)) if mask[j//3] and mask[(j-3)//3])
        if sum(mask)*3 < expected//3 or not any(mask) or texture < sum(mask)*6:
            raise ValueError('Insufficient landmark coverage or texture')
        decoded.append((box, raw, np.asarray(mask)))
    a, b = (p[0] for p in decoded)
    if a[0] < b[0]+b[2] and b[0] < a[0]+a[2] and a[1] < b[1]+b[3] and b[1] < a[1]+a[3]:
        raise ValueError('Landmarks must be independent nonoverlapping crops')
    return decoded


def match_score(current, raw, mask):
    # Same per-channel lighting correction and channel-count thresholds as Go.
    reference = np.frombuffer(raw, dtype=np.uint8).reshape(-1, 3).astype(float)
    differences = current.reshape(-1, 3).astype(float)[mask] - reference[mask]
    lighting = differences.mean(axis=0)
    if np.any(np.abs(lighting) > 25):
        return None
    residual = np.abs(differences-lighting)
    count = residual.size
    if count < len(raw)//3 or residual.sum() > count*12 or np.count_nonzero(residual > 24) > count//5:
        return None
    return float(residual.mean())


def locate(source, decoded):
    pixels = np.asarray(source)
    offsets = []
    for box, raw, mask in decoded:
        x, y, w, h = box
        matches = []
        for dy in range(-72, 73, 2):
            for dx in range(-72, 73, 2):
                sample = pixels[y+dy:y+dy+h:8, x+dx:x+dx+w:8]
                score = match_score(sample, raw, mask)
                if score is not None:
                    matches.append((score, dx, dy))
        if not matches:
            raise ValueError('Boundary landmark not found within measured search bounds')
        # Stable tie ordering matches Go's dy-then-dx iteration.
        matches.sort(key=lambda m: m[0])
        best = matches[0]
        if any(math.hypot(dx-best[1], dy-best[2]) > 4 and score <= best[0]+1 for score, dx, dy in matches[1:]):
            raise ValueError('Ambiguous landmark: competing separated locations')
        offsets.append([best[1], best[2]])
    return offsets


def make_interval(scene, boundary_images, reference_image=None):
    paths = [Path(p).resolve(strict=True) for p in boundary_images]
    if len(paths) != 2 or os.path.samefile(paths[0], paths[1]):
        raise ValueError('Two distinct boundary evidence files are required')
    decoded = decode_scene(scene)
    if reference_image is not None:
        reference = read_image(reference_image)
        if any(sample_scene(reference, box) != raw for box, raw, _ in decoded):
            raise ValueError('Reference image does not reproduce stored fingerprints')
    boundaries = [locate(read_image(path), decoded) for path in paths]
    for i in range(2):
        a, b = boundaries[0][i], boundaries[1][i]
        if not 8 <= math.hypot(b[0]-a[0], b[1]-a[1]) < 80:
            raise ValueError('Each landmark must have distinct measured boundaries 8..<80px apart')
    return {'boundaries': boundaries, 'evidence': [str(p) for p in paths],
            'cross_track_pixels': 4, 'progress_difference': .2}


def import_interval(routes_path, route_id, endpoint, boundary_images, *, reviewed_clearance=False, reference_image=None, dry_run=False):
    if not reviewed_clearance:
        raise ValueError('Explicit --reviewed-clearance is required')
    if endpoint not in ('start', 'end'):
        raise ValueError('Invalid endpoint')
    routes_path = Path(routes_path)
    original = routes_path.read_bytes()
    routes = json.loads(original)
    if not isinstance(routes, list):
        raise ValueError('Expected route list')
    selected = [r for r in routes if r.get('id') == route_id]
    if len(selected) != 1 or selected[0].get('goal') != 'transit' or selected[0].get('version') != 1:
        raise ValueError('Exactly one version 1 transit route must match')
    route = selected[0]
    interval = make_interval(route[endpoint], boundary_images, reference_image)
    key = endpoint+'_interval'
    changed = route.get(key) != interval
    if changed:
        route[key] = interval
        for field in ('successful_replays', 'inn_replays', 'empty_replays', 'loaded_replays'):
            route[field] = 0
    if changed and not dry_run:
        temporary = None
        try:
            with tempfile.NamedTemporaryFile(mode='w', encoding='utf-8', dir=routes_path.parent, prefix=routes_path.name+'.', suffix='.tmp', delete=False) as stream:
                temporary = Path(stream.name)
                json.dump(routes, stream, indent=4)
                stream.write('\n')
                stream.flush()
                os.fsync(stream.fileno())
            if routes_path.read_bytes() != original:
                raise ValueError('Routes changed during import; refusing to overwrite')
            os.replace(temporary, routes_path)
        finally:
            if temporary is not None:
                temporary.unlink(missing_ok=True)
    return {'route_id': route_id, 'endpoint': endpoint, 'interval': interval, 'changed': changed, 'dry_run': dry_run}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--routes', type=Path, required=True)
    parser.add_argument('--route', required=True)
    parser.add_argument('--endpoint', choices=('start','end'), required=True)
    parser.add_argument('--reference-image', type=Path)
    parser.add_argument('--boundary-images', type=Path, nargs=2, required=True)
    parser.add_argument('--reviewed-clearance', action='store_true', required=True)
    parser.add_argument('--dry-run', action='store_true')
    args = parser.parse_args()
    print(json.dumps(import_interval(args.routes, args.route, args.endpoint, args.boundary_images,
        reviewed_clearance=args.reviewed_clearance, reference_image=args.reference_image, dry_run=args.dry_run), indent=2))


if __name__ == '__main__':
    main()
