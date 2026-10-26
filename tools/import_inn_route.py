"""Import a reviewed successful inn delivery from its images and movement journal.

The caller must inspect both captures to confirm the pickup, numbered table and
tray decrease. This tool never controls the game or invents/reverses movement.
"""

import argparse
import base64
import datetime as dt
import json
from pathlib import Path

from PIL import Image


BOXES = ((570, 150, 690, 190), (850, 170, 1010, 210))


def fingerprint(path):
    with Image.open(path) as source:
        return fingerprint_image(source)


def fingerprint_image(source):
    if source.size != (1280, 720):
        raise ValueError('Route evidence must be 1280x720')
    source = source.convert('RGB')
    result = []
    for side in range(2):
        best = None
        for expanded in (False, True):
            xs = [(570, 630, 690), (850, 910, 950)][side]
            ys, widths = (150, 170), (120,)
            if expanded:
                xs = [(500, 510, 570, 630, 690), (850, 910, 950)][side]
                ys, widths = (150, 170, 190), (80, 120)
            for left in xs:
                for top in ys:
                    for width in widths:
                        samples = sample_scene(source, (left, top, width, 40))
                        mask = architecture_mask(samples)
                        if sum(mask)*2 < len(mask):
                            continue
                        texture = sum(abs(samples[i] - samples[i - 3]) for i in range(3, len(samples)) if mask[i//3] and mask[(i-3)//3])
                        if best is None or texture > best[0]:
                            best = (texture, samples, [left, top, width, 40])
            if best is not None and best[0] >= sum(architecture_mask(best[1])) * 6:
                break
        if best is None:
            raise ValueError('No architectural crop with sufficient stable coverage')
        texture, samples, box = best
        if texture < sum(architecture_mask(samples)) * 6:
            raise ValueError('Uninformative scene anchor')
        result.append({'box': box, 'pixels': base64.b64encode(samples).decode(), 'architecture_only': True})
    return result


def sample_scene(source, box):
    left, top, width, height = box
    return bytes(channel for y in range(top, top + height, 8)
                 for x in range(left, left + width, 8)
                 for channel in source.getpixel((x, y)))


def architecture_mask(samples):
    return [10 <= r <= 235 and r >= g-3 and g >= b-3 and r-g <= 75 and g-b <= 65
            for r, g, b in zip(samples[0::3], samples[1::3], samples[2::3])]


def timestamp(value):
    return dt.datetime.fromisoformat(value)


def read_rows(path):
    return [json.loads(line) for line in path.read_text('utf-8').splitlines() if line.strip()]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--data', type=Path, required=True)
    parser.add_argument('--start-image', type=Path, required=True)
    parser.add_argument('--end-image', type=Path, required=True)
    parser.add_argument('--start-time', required=True)
    parser.add_argument('--end-time', required=True)
    parser.add_argument('--seat', choices=list('123456'), required=True)
    parser.add_argument('--dish', required=True)
    args = parser.parse_args()
    start, end = timestamp(args.start_time), timestamp(args.end_time)
    if not 0 < (end - start).total_seconds() <= 60:
        raise ValueError('Delivery must be a single bounded successful leg')
    actions = [row for row in read_rows(args.data / 'actions.jsonl')
               if start <= timestamp(row['at']) <= end]
    if len(actions) != 2 or [(row['kind'], row.get('dish')) for row in actions] != [
            ('pickup', args.dish), ('serve', args.dish)]:
        raise ValueError('The interval must contain only the confirmed pickup and matching serve')
    steps = [row['command'] for row in read_rows(args.data / 'navigation.jsonl')
             if start < timestamp(row['at']) < end]
    if not 1 <= len(steps) <= 24 or sum(s['Milliseconds'] for s in steps) > 12000:
        raise ValueError('Movement is empty or exceeds route limits')
    for s in steps:
        if (s['Kind'] == 'camera' or s['Goal'] != 'diner'
                or not 0 < s['Milliseconds'] <= 1500
                or not 100 <= s['X'] <= 290 or not 450 <= s['Y'] <= 640):
            raise ValueError('Only measured fixed-camera delivery movement can be imported')
    route_id = f"reviewed-table{args.seat}-{start.strftime('%Y%m%dT%H%M%S')}"
    route = {'version': 1, 'id': route_id, 'goal': 'diner', 'seat': args.seat,
             'start': fingerprint(args.start_image), 'end': fingerprint(args.end_image),
             'steps': steps, 'created_at': dt.datetime.now(dt.timezone.utc).isoformat(),
             'successful_replays': 0}
    target = args.data / 'fixed-routes.json'
    routes = json.loads(target.read_text('utf-8')) if target.exists() else []
    routes = [r for r in routes if r['id'] != route_id] + [route]
    temporary = target.with_suffix('.tmp')
    temporary.write_text(json.dumps(routes, indent=4) + '\n', 'utf-8')
    temporary.replace(target)
    with (args.data / 'route-imports.jsonl').open('a', encoding='utf-8') as out:
        out.write(json.dumps({'route_id': route_id, 'start_time': args.start_time,
                             'end_time': args.end_time, 'dish': args.dish, 'seat': args.seat,
                             'start_image': str(args.start_image.resolve()),
                             'end_image': str(args.end_image.resolve())}) + '\n')
    print(json.dumps({'route_id': route_id, 'segments': len(steps),
                      'movement_ms': sum(s['Milliseconds'] for s in steps),
                      'recorded_elapsed_seconds': (end - start).total_seconds(),
                      'live_replay_verified': False}))


if __name__ == '__main__':
    main()
