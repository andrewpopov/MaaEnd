"""Save reviewed manual inn routes and their nearest recorded endpoint frames."""

import argparse
import datetime as dt
import json
from pathlib import Path

from import_inn_route import fingerprint, timestamp


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('calibration', type=Path)
    args = parser.parse_args()
    evidence = json.loads(args.calibration.read_text('utf-8'))
    frames = []
    for folder in evidence['recordings']:
        folder = Path(folder)
        for line in (folder / 'frames.jsonl').read_text('utf-8').splitlines():
            row = json.loads(line)
            frames.append((timestamp(row['captured_at']), folder / row['file']))

    def nearest(value):
        at = timestamp(value)
        # All endpoints were inspected while stationary. Never substitute a
        # distant image across a recording gap or a route movement boundary.
        distance, path = min((abs((time - at).total_seconds()), path) for time, path in frames)
        if distance > 2:
            raise ValueError(f'No timely endpoint frame for {value}')
        return path

    target = args.calibration.parent / 'fixed-routes.json'
    routes = json.loads(target.read_text('utf-8')) if target.exists() else []
    for i, row in enumerate(evidence['routes']):
        steps = [s['command'] for s in row['steps']]
        for step in steps:
            step['Pipeline'] = True
        if not 1 <= len(steps) <= 24 or sum(s['Milliseconds'] for s in steps) > 12000:
            raise ValueError('Invalid route movement budget')
        for s in steps:
            if (s['Kind'] == 'camera' or s['Goal'] != row['goal'] or
                    not 100 <= s['X'] <= 290 or not 450 <= s['Y'] <= 640 or
                    not 0 < s['Milliseconds'] <= 1500):
                raise ValueError('Only measured fixed-camera steps may be imported')
        first, last = nearest(row['start']), nearest(row['end'])
        route_id = f"live-{row['goal']}-table{row['seat']}-{i}-20261019"
        route = {'version': 1, 'id': route_id, 'goal': row['goal'], 'seat': row['seat'],
                 'start': fingerprint(first), 'end': fingerprint(last), 'steps': steps,
                 'created_at': dt.datetime.now(dt.timezone.utc).isoformat(),
                 'successful_replays': 0}
        routes = [r for r in routes if r['id'] != route_id] + [route]
        row['imported_route_id'] = route_id
        row['start_image'] = str(first)
        row['end_image'] = str(last)
    temporary = target.with_suffix('.tmp')
    temporary.write_text(json.dumps(routes, indent=4) + '\n', 'utf-8')
    temporary.replace(target)
    args.calibration.write_text(json.dumps(evidence, indent=4) + '\n', 'utf-8')
    print(json.dumps([{'id': r['id'], 'goal': r['goal'], 'seat': r['seat'],
                       'movement_ms': sum(s['Milliseconds'] for s in r['steps'])}
                      for r in routes]))


if __name__ == '__main__':
    main()
