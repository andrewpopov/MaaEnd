"""Import an inspected directed inn path from measured gesture evidence.

Imports never mark a path replayed. A successful MaaEnd route probe must verify
its endpoint; world-only probes cannot authorize loaded production travel.
"""
import argparse
import datetime as dt
import json
from pathlib import Path

from import_inn_route import fingerprint


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--journal', type=Path, required=True)
    parser.add_argument('--labels', nargs='+', required=True)
    parser.add_argument('--id', required=True)
    parser.add_argument('--from-waypoint', required=True)
    parser.add_argument('--to-waypoint', required=True)
    parser.add_argument('--seat', choices=list('123456'))
    parser.add_argument('--counter-hub', action='store_true')
    parser.add_argument('--stage', choices=['exit', 'lane', 'approach'])
    parser.add_argument('--lane', choices=['inner', 'outer-right', 'outer-left', 'front-cross'])
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    if args.from_waypoint == args.to_waypoint or (args.seat and args.counter_hub):
        parser.error('Distinct waypoints and an unambiguous destination are required')
    if (args.stage == 'lane') != bool(args.lane):
        parser.error('A lane stage requires its physical lane; other stages do not take a lane')
    rows = [json.loads(line) for line in args.journal.read_text('utf-8').splitlines() if line.strip()]
    selected = []
    for label in args.labels:
        matches = [row for row in rows if row['label'] == label]
        if len(matches) != 1 or matches[0]['status'] != 'Succeeded':
            raise ValueError(f'Expected one successful measured segment: {label}')
        selected.append(matches[0])
    if selected != sorted(selected, key=lambda row: row['at']):
        raise ValueError('Gesture labels must follow recorded order')
    indices = [rows.index(row) for row in selected]
    if indices != list(range(indices[0], indices[0]+len(indices))):
        raise ValueError('An intervening gesture would change the path')
    steps = [step for row in selected for step in row['steps']]
    duration = sum(step['Milliseconds']+step.get('HoldMilliseconds', 0) for step in steps)
    if not 1 <= len(steps) <= 24 or not 0 < duration <= 12000:
        raise ValueError('Path exceeds the bounded executor limits')
    if args.stage == 'lane':
        for step in steps:
            aligned = (step['Y'] == 551 and step['X'] in (115, 275)) if args.lane == 'front-cross' else (step['X'] == 195 and step['Y'] in (471, 631))
            if step['Kind'] != 'walk' or not aligned:
                raise ValueError('Lane travel must follow its recorded axis without camera pans or diagonal shortcuts')
    if args.stage in ('exit', 'approach') and duration > 1500:
        raise ValueError('A table exit or approach must be separate from long lane travel')
    routes = json.loads(args.output.read_text('utf-8-sig')) if args.output.exists() else []
    if any(route['id'] == args.id for route in routes):
        raise ValueError('Route ID already exists; inspect its replay evidence before replacing it')
    route = dict(version=1, id=args.id, goal='transit',
                 from_waypoint=args.from_waypoint, to_waypoint=args.to_waypoint,
                 start=fingerprint(selected[0]['before']), end=fingerprint(selected[-1]['after']),
                 steps=steps, created_at=dt.datetime.now(dt.timezone.utc).isoformat(),
                 successful_replays=0, inn_replays=0, loaded_replays=0, empty_replays=0,
                 transit_evidence=dict(start_image=selected[0]['before'], end_image=selected[-1]['after'],
                     journal=str(args.journal), task_ids=[tid for row in selected for tid in row['task_ids']]))
    if args.seat:
        route['seat'] = args.seat
    if args.stage:
        route['movement_stage'] = args.stage
    if args.lane:
        route['lane'] = args.lane
    if args.counter_hub:
        route['counter_hub'] = True
    routes.append(route)
    args.output.write_text(json.dumps(routes, indent=4)+'\n', encoding='utf-8')
    print(json.dumps(dict(id=args.id, segments=len(steps), gesture_ms=duration, replay_verified=False)))


if __name__ == '__main__':
    main()
