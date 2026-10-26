"""Measure an inn input sequence and save exact inputs plus before/after frames.

This is a supervised calibration tool, not a route validator. The first input
requires End Session (Explore with --world). Movement is submitted once, never
retried. Import reviewed evidence with import_inn_transit.py, then use MaaEnd's
route probe to verify its endpoints before allowing production replay.
"""
import argparse
import os
import datetime as dt
import json
import re
import time
import urllib.request
from pathlib import Path

from inn_route_status import fresh_frame

API_ROOT = os.environ.get('MAAEND_API_URL', 'http://127.0.0.1:12701/api').rstrip('/')
API = API_ROOT + '/maa/instances/innreact'
STATE = API_ROOT + '/maa/state'


def validate_steps(steps):
    if not 1 <= len(steps) <= 24:
        raise ValueError('Specify one move or a bounded burst of 1..24 inputs')
    total = 0
    for step in steps:
        kind = step['Kind']
        x, y = step['X'], step['Y']
        duration, hold = step['Milliseconds'], step.get('HoldMilliseconds', 0)
        if not all(type(v) is int for v in (x, y, duration, hold)):
            raise ValueError('Coordinates and times must be integers')
        if not 0 < duration <= 1500 or not 0 <= hold or duration + hold > 1500:
            raise ValueError('Each calibration input is bounded to 1500 ms')
        if kind == 'camera':
            origin = step.get('Origin', [1000, 350])
            if (not 500 <= x <= 1150 or not 150 <= y <= 500 or hold != 0
                    or len(origin) != 2 or not 500 <= origin[0] <= 1150
                    or not 180 <= origin[1] <= 500):
                raise ValueError('Camera input must stay inside the measured camera region')
        elif kind != 'walk' or not 100 <= x <= 290 or not 450 <= y <= 640:
            raise ValueError('Movement must use the measured joystick region')
        warmup = step.get('JoystickWarmup', False)
        if type(warmup) is not bool or (warmup and (kind != 'walk' or duration < 80 or duration + hold + 51 > 1500)):
            raise ValueError('Joystick warmup requires a walking drag of at least 80 ms')
        total += duration + hold + (51 if warmup else 0)
    if total > 12000:
        raise ValueError('Calibration sequence exceeds 12 seconds')


def swipe_param(step):
    begin = step.get('Origin', [1000, 350]) if step['Kind'] == 'camera' else [195, 551]
    end = [step['X'], step['Y']]
    duration, hold = step['Milliseconds'], step.get('HoldMilliseconds', 0)
    if step.get('JoystickWarmup', False):
        # Let the game observe the neutral touch before moving the finger.
        # This is a measured input gesture, not a UI settling delay.
        return {'begin': begin, 'end': [begin, end], 'duration': [1, duration], 'end_hold': [50, hold]}
    return {'begin': begin, 'end': end, 'duration': duration, 'end_hold': hold}


def make_nodes(steps, world=False):
    validate_steps(steps)
    freeze = {'time': 250, 'target': [510, 150, 600, 40],
              'threshold': .99, 'timeout': 2000}
    nodes = {}
    for index, step in enumerate(steps):
        node = {'recognition': {'type': 'OCR', 'param': {
                    'roi': [30, 15, 220, 45] if world else [15, 15, 205, 65],
                    'expected': ['Explore'] if world else ['End Session']}}, 'pre_delay': 0,
                'post_delay': 0, 'rate_limit': 0,
                'action': {'type': 'Swipe', 'param': swipe_param(step)},
                'next': ['InnCalibration' + str(index + 1)]}
        if index == 0:
            node['recognition'] = {'type': 'OCR', 'param': {
                'roi': [30, 15, 220, 45] if world else [15, 15, 205, 65],
                'expected': ['Explore'] if world else ['End Session']}}
            node['pre_wait_freezes'] = freeze
            node['timeout'] = 2000
        nodes['InnCalibration' + str(index)] = node
    nodes['InnCalibration' + str(len(steps))] = {
        'recognition': {'type': 'DirectHit'}, 'pre_wait_freezes': freeze,
        'action': {'type': 'DoNothing'}, 'pre_delay': 0, 'post_delay': 0, 'rate_limit': 0}
    return nodes


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument('--move', nargs=2, type=int)
    source.add_argument('--burst', type=Path)
    parser.add_argument('--hold', type=int)
    parser.add_argument('--duration', type=int, default=20)
    parser.add_argument('--camera', action='store_true')
    parser.add_argument('--world', action='store_true')
    parser.add_argument('--origin', nargs=2, type=int)
    parser.add_argument('--label', required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--payload', type=Path, default=Path('.cache/inn-reactive-trial.json'))
    args = parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9_-]+', args.label):
        parser.error('Use a simple label containing letters, numbers, underscores or hyphens')
    if args.burst and (args.camera or args.origin or args.hold is not None):
        parser.error('Burst files already specify camera origin and hold times')
    if args.origin and not args.camera:
        parser.error('--origin applies only to camera movements')
    steps = json.loads(args.burst.read_text('utf8')) if args.burst else [{
        'Kind': 'camera' if args.camera else 'walk',
        'X': args.move[0], 'Y': args.move[1], 'Milliseconds': args.duration,
        'HoldMilliseconds': args.hold if args.hold is not None else (0 if args.camera else 80),
        'Pipeline': True}]
    if args.origin:
        steps[0]['Origin'] = args.origin
    nodes = make_nodes(steps, args.world)
    folder = args.output.resolve()
    if folder.drive.upper() == 'C:':
        parser.error('Store image captures off C:')
    journal = folder / 'gestures.jsonl'
    if journal.exists() and any(json.loads(line)['label'] == args.label
                                for line in journal.read_text('utf8').splitlines() if line.strip()):
        parser.error('Label already exists; preserve evidence with a new label')
    http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    def state():
        return json.load(http.open(STATE, timeout=8))['instances']
    if any(item['is_running'] for item in state().values()):
        raise RuntimeError('MaaEnd is busy; no input sent')
    payload = json.loads(args.payload.read_text('utf8'))
    payload['tasks'] = [dict(payload['tasks'][0], entry='InnCalibration0',
                            pipeline_override=json.dumps(nodes),
                            task_name='Inn route calibration: ' + args.label)]
    folder.mkdir(parents=True, exist_ok=True)
    stamp = dt.datetime.now(dt.timezone.utc).strftime('%Y%m%d-%H%M%S-%f')
    stem = stamp + '-' + args.label
    before, after = folder / (stem + '-before.png'), folder / (stem + '-after.png')
    before.write_bytes(fresh_frame(http))
    record = {'at': dt.datetime.now(dt.timezone.utc).isoformat(), 'label': args.label,
              'steps': steps, 'before': str(before), 'after': str(after),
              'status': 'SubmissionPending', 'world': args.world,
              'endpoint_verified': False}
    evidence = folder / (stem + '.json')
    evidence.write_text(json.dumps(record, indent=4) + '\n', encoding='utf8')
    request = urllib.request.Request(API + '/tasks/start', data=json.dumps(payload).encode(),
                                     headers={'Content-Type': 'application/json'})
    result = json.load(http.open(request, timeout=10))
    if not result.get('taskIds'):
        raise RuntimeError(result)
    record['task_ids'] = result['taskIds']
    record['status'] = 'Running'
    evidence.write_text(json.dumps(record, indent=4) + '\n', encoding='utf8')
    deadline = time.monotonic() + 25
    while time.monotonic() < deadline:
        live = state()['innreact']
        if not live['is_running']:
            record['status'] = live['task_run_state']['overall_status']
            break
        time.sleep(.1)
    else:
        raise RuntimeError('Calibration has not completed; do not repeat input. Inspect ' + str(evidence))
    after.write_bytes(fresh_frame(http))
    record['completed_at'] = dt.datetime.now(dt.timezone.utc).isoformat()
    evidence.write_text(json.dumps(record, indent=4) + '\n', encoding='utf8')
    with journal.open('a', encoding='utf8') as stream:
        stream.write(json.dumps(record) + '\n')
    print(json.dumps(record), flush=True)
    if record['status'] != 'Succeeded':
        raise SystemExit(1)


if __name__ == '__main__':
    main()
