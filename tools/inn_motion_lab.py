# /// script
# requires-python = ">=3.11"
# dependencies = ["numpy", "Pillow", "opencv-python-headless", "maafw>=5.13.0b4,<6"]
# ///
"""Measure inn movement with bounded MaaEnd probes and independently tracked landmarks.

Pair analysis is input-free. Probe sends exactly one gesture after recognizing the
inn HUD; it never starts a session, repeats a failed input, or reverses a route.
All measurements are image pixels, not world metres. Near/far landmarks must be
chosen on physical furniture or architecture, outside HUD, people and bubbles.
"""
from __future__ import annotations

import argparse
import copy
import hashlib
import io
import json
import math
import subprocess
from pathlib import Path
import threading
import time
import urllib.request
import urllib.parse
import uuid

import numpy as np
import cv2
from PIL import Image, ImageDraw


def gray(image):
    return np.asarray(image.convert('L'), dtype=np.float32)


def locate(reference, current, box, radius=(120, 40)):
    """Find a distinctive patch, rejecting flat, ambiguous and inconsistent matches."""
    x, y, w, h = map(int, box)
    if reference.size != (1280, 720) or current.size != reference.size:
        raise ValueError('Expected matching 1280x720 frames')
    if not (12 <= w <= 80 and 12 <= h <= 80 and 0 <= x <= 1280-w and 0 <= y <= 720-h):
        raise ValueError('Landmark must be a 12..80 pixel physical patch inside the frame')

    def search(source, target, source_box, center):
        sx, sy, sw, sh = source_box
        patch = gray(source.crop((sx, sy, sx+sw, sy+sh)))
        if float(patch.std()) < 8:
            return None
        cx, cy = center
        left, top = max(0, cx-radius[0]), max(0, cy-radius[1])
        right, bottom = min(1280, cx+radius[0]+sw), min(720, cy+radius[1]+sh)
        region = gray(target.crop((left, top, right, bottom)))
        candidates = []
        for scale in np.linspace(.8, 1.25, 10):
            width, height = round(sw*scale), round(sh*scale)
            template = cv2.resize(patch, (width, height))
            if width > region.shape[1] or height > region.shape[0]:
                continue
            scores = cv2.matchTemplate(region, template, cv2.TM_CCOEFF_NORMED)
            # Keep the best spatially separated alternatives at every scale.
            for _ in range(2):
                _, best, _, (ix, iy) = cv2.minMaxLoc(scores)
                candidates.append((best, left+ix, top+iy, width, height))
                scores[max(0, iy-height//2):iy+height//2+1,
                       max(0, ix-width//2):ix+width//2+1] = -1
        if not candidates:
            return None
        candidates.sort(reverse=True)
        score, x, y, w, h = candidates[0]
        alternatives = [c[0] for c in candidates[1:]
                        if math.hypot(c[1]+c[3]/2-x-w/2, c[2]+c[4]/2-y-h/2) > max(8, min(w, h)/2)]
        runner_up = max(alternatives, default=-1)
        if score < .88 or score-runner_up < .04:
            return None
        return {'box': [x, y, w, h], 'score': round(score, 4),
                'distinctiveness': round(score-runner_up, 4)}

    found = search(reference, current, (x, y, w, h), (x, y))
    if found is None:
        return {'matched': False}
    back = search(current, reference, found['box'], (x, y))
    if back is None or math.hypot(back['box'][0]-x, back['box'][1]-y) > 3:
        return {'matched': False, 'reason': 'forward_backward_inconsistent'}
    return dict(found, matched=True, displacement=[found['box'][0]+(found['box'][2]-w)/2-x, found['box'][1]+(found['box'][3]-h)/2-y])


def analyze_pair(before, after, landmarks):
    if not landmarks or len({item['name'] for item in landmarks}) != len(landmarks):
        raise ValueError('Choose at least one uniquely named physical landmark')
    rows = []
    for item in landmarks:
        if item.get('depth') not in {'near', 'far'}:
            raise ValueError('Each reviewed landmark needs depth near or far')
        rows.append(dict(item, **locate(before, after, item['box'])))
    groups = {}
    for depth in ['near', 'far']:
        points = [r['displacement'] for r in rows if r['matched'] and r['depth'] == depth]
        if points:
            groups[depth] = np.median(points, axis=0).tolist()
    parallax = None
    if len(groups) == 2:
        parallax = float(np.linalg.norm(np.subtract(groups['near'], groups['far'])))
    return {'landmarks': rows, 'median_displacement_pixels': groups,
            'near_far_difference_pixels': parallax, 'world_distance_known': False}


def feature_tracks(before, after, landmarks):
    """Match physical scene features without assuming a single screen translation.

    Mutual SIFT matching rejects descriptor ambiguity. Different depths are kept
    separate: fitting one homography across the entire room would hide parallax.
    This is diagnostic tracking, not authorization to move or proof of arrival.
    """
    if before.size != (1280, 720) or after.size != before.size:
        raise ValueError('Expected matching 1280x720 frames')
    a, b = gray(before).astype(np.uint8), gray(after).astype(np.uint8)
    mask = np.zeros((720, 1280), dtype=np.uint8)
    for item in landmarks:
        x, y, w, h = item['box']
        mask[y:y+h, x:x+w] = 255
    target_mask = np.zeros_like(mask)
    target_mask[145:550, 230:1100] = 255
    target_mask[145:455, :450] = 0
    target_mask[390:460, 700:1100] = 0
    detector = cv2.SIFT_create(nfeatures=2000, contrastThreshold=.025)
    ka, da = detector.detectAndCompute(a, mask)
    kb, db = detector.detectAndCompute(b, target_mask)
    if da is None or db is None or len(da) < 2 or len(db) < 2:
        return []
    matcher = cv2.BFMatcher(cv2.NORM_L2)
    def distinctive(source, target):
        return {m.queryIdx: m for pair in matcher.knnMatch(source, target, k=2)
                if len(pair) == 2 for m, n in [pair] if m.distance < .7*n.distance}
    forward, backward = distinctive(da, db), distinctive(db, da)
    tracks = []
    occupied = set()
    for i, match in forward.items():
        if match.trainIdx not in backward or backward[match.trainIdx].trainIdx != i:
            continue
        p, q = ka[i].pt, kb[match.trainIdx].pt
        if math.hypot(q[0]-p[0], q[1]-p[1]) > 200:
            continue
        cell = (int(p[0])//12, int(p[1])//12)
        if cell in occupied:
            continue
        occupied.add(cell)
        landmark = next((item for item in landmarks if item['box'][0] <= p[0] < item['box'][0]+item['box'][2] and item['box'][1] <= p[1] < item['box'][1]+item['box'][3]), None)
        if landmark is None:
            continue
        tracks.append({'landmark': landmark['name'], 'depth': landmark['depth'], 'before': list(p), 'after': list(q),
                       'displacement': [q[0]-p[0], q[1]-p[1]],
                       'descriptor_distance': float(match.distance)})
    return tracks


def save_pair(before_path, after_path, landmarks_path, output):
    output.mkdir(parents=True, exist_ok=False)
    before = Image.open(before_path).convert('RGB')
    after = Image.open(after_path).convert('RGB')
    report = analyze_pair(before, after, json.loads(landmarks_path.read_text('utf8')))
    report['feature_tracks'] = feature_tracks(before, after, report['landmarks'])
    report.update(before=str(before_path.resolve()), after=str(after_path.resolve()))
    (output/'analysis.json').write_text(json.dumps(report, indent=4)+'\n', 'utf8')
    overlay = after.copy()
    draw = ImageDraw.Draw(overlay)
    for track in report['feature_tracks']:
        draw.line((*track['before'], *track['after']), fill='cyan', width=1)
        x, y = track['after']
        draw.ellipse((x-2, y-2, x+2, y+2), fill='cyan')
    for row in report['landmarks']:
        if not row['matched']:
            continue
        x, y, w, h = row['box']
        draw.rectangle((x, y, x+w, y+h), outline='lime', width=2)
        draw.text((x, y+h+2), row['name']+': '+str(row['displacement']), fill='lime')
    overlay.save(output/'tracked-landmarks.png')
    return report


def trace_probe(recording, landmarks_path):
    """Measure observed response and residual camera movement from a probe clip."""
    probe = json.loads((recording/'probe.json').read_text('utf8'))
    rows = [json.loads(line) for line in (recording/'frames.jsonl').read_text('utf8').splitlines()]
    submitted, terminal = probe['submitted_seconds'], probe['terminal_seconds']
    prior = [r for r in rows if r['elapsed_seconds'] < submitted]
    if not prior:
        raise ValueError('No pre-submission reference frame')
    reference_path = recording/prior[-1]['file']
    reference = Image.open(reference_path).convert('RGB')
    landmarks = json.loads(landmarks_path.read_text('utf8'))
    frames, cache = [], {}
    for row in rows:
        if row['file'] not in cache:
            with Image.open(recording/row['file']) as frame:
                cache[row['file']] = analyze_pair(reference, frame.convert('RGB'), landmarks)
        frames.append(dict(elapsed_seconds=row['elapsed_seconds'], analysis=cache[row['file']]))
    metrics = []
    for landmark in landmarks:
        samples = [(f['elapsed_seconds'], next(r for r in f['analysis']['landmarks']
                    if r['name'] == landmark['name'])) for f in frames]
        samples = [(t, r) for t, r in samples if r['matched']]
        onset = next((t-submitted for t, r in samples if t >= submitted
                      and math.hypot(*r['displacement']) > 3), None)
        at_terminal = [(t, r) for t, r in samples if t <= terminal]
        settled = [(t, r) for t, r in samples if t > terminal+1]
        metric = {'name': landmark['name'], 'depth': landmark['depth'],
                  'match_fraction': len(samples)/max(1, len(frames)),
                  'first_observed_motion_after_submit_seconds': onset}
        if at_terminal and settled:
            start, end = at_terminal[-1][1]['displacement'], settled[-1][1]['displacement']
            metric.update(terminal_displacement_pixels=start, settled_displacement_pixels=end,
                          residual_after_terminal_pixels=float(np.linalg.norm(np.subtract(end, start))))
        metrics.append(metric)
    report = {'reference': str(reference_path.resolve()), 'gesture': probe['gesture'],
              'metrics': metrics, 'world_distance_known': False,
              'capture_source': probe.get('capture_source'),
              'timing_note': 'Observed from captured frames; includes HUD gating and capture latency, not exact touch onset',
              'frames': frames}
    (recording/'motion-analysis.json').write_text(json.dumps(report, indent=4)+'\n', 'utf8')
    return {k: v for k, v in report.items() if k != 'frames'}


def probe_nodes(dx, dy, hold_ms, mode):
    if not (8 <= math.hypot(dx, dy) <= 72 and 80 <= hold_ms <= 800):
        raise ValueError('Use an 8..72 pixel vector and 80..800 ms hold')
    target = [198+dx, 552+dy, 1, 1]
    if mode == 'press':
        action = {'type': 'LongPress', 'param': {'target': target, 'duration': hold_ms, 'contact': 8}}
    elif mode in {'swipe', 'ramp'}:
        action = {'type': 'Swipe', 'param': {'begin': [198, 552, 1, 1], 'end': target,
                  'duration': 20 if mode == 'swipe' else 80, 'end_hold': hold_ms, 'contact': 8}}
    else:
        raise ValueError('Unknown input mode')
    def node(recognition, action, next_nodes=None):
        return dict(recognition=recognition, action=action, pre_delay=0, post_delay=0,
                    rate_limit=0, timeout=2000, **({'next': next_nodes} if next_nodes else {}))
    return {
        'InnMotionLabTheme': node({'type': 'OCR', 'param': {'roi': [20, 135, 470, 65],
            'expected': ['Appetizing Start']}}, {'type': 'DoNothing'}, ['InnMotionLabGesture']),
        'InnMotionLabGesture': node({'type': 'OCR', 'param': {'roi': [15, 15, 205, 65],
            'expected': ['End Session']}}, action),
    }


def run_probe(api, instance, payload_path, output, dx, dy, hold_ms, mode, adb=None, serial=None,
              maafw=None, capture_config=None):
    parsed = urllib.parse.urlparse(api)
    if parsed.scheme != 'http' or parsed.hostname not in {'127.0.0.1', 'localhost', '::1'}:
        raise ValueError('Use the local MaaEnd API')
    nodes = probe_nodes(dx, dy, hold_ms, mode)
    if bool(adb) != bool(serial):
        raise ValueError('Direct emulator capture needs both --adb and --serial')
    if bool(maafw) != bool(capture_config) or (maafw and not adb):
        raise ValueError('Native capture needs --maafw, --capture-config, --adb and --serial')
    http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    base = api.rstrip('/')
    prefix = base+'/maa/instances/'+urllib.parse.quote(instance, safe='')
    def get(path):
        with http.open(base+path, timeout=8) as reply:
            return json.load(reply)
    def post(path, data):
        request = urllib.request.Request(prefix+path, data=json.dumps(data).encode(),
                                        headers={'Content-Type': 'application/json'})
        with http.open(request, timeout=8) as reply:
            return json.load(reply)
    state = get('/maa/state')['instances']
    if instance not in state or not state[instance]['connected'] or any(s['is_running'] for s in state.values()):
        raise RuntimeError('Controller unavailable or MaaEnd busy; no input sent')
    payload = copy.deepcopy(json.loads(payload_path.read_text('utf8')))
    controller = json.loads(payload['pi_envs']['PI_CONTROLLER'])
    if controller.get('type', '').lower() != 'adb':
        raise ValueError('This calibration is for the ADB joystick only')
    native = None
    if maafw:
        from maa.library import Library
        from maa.controller import AdbController
        from maa.define import MaaAdbInputMethodEnum, MaaAdbScreencapMethodEnum
        Library.open(maafw)
        native = AdbController(adb, serial, screencap_methods=MaaAdbScreencapMethodEnum.EmulatorExtras,
            input_methods=MaaAdbInputMethodEnum.Null, config=json.loads(capture_config.read_text('utf8')))
        if not native.post_connection().wait().status.succeeded:
            raise RuntimeError('Native capture connection failed; no input task submitted')
    payload['tasks'] = [dict(payload['tasks'][0], entry='InnMotionLabTheme',
                       pipeline_override=json.dumps(nodes), task_name='Inn movement lab: single '+mode)]
    output.mkdir(parents=True, exist_ok=False)
    (output/'frames').mkdir()
    subscriber = 'inn-motion-lab-'+uuid.uuid4().hex
    stop = threading.Event()
    rows, errors = [], []
    origin = time.perf_counter()
    def capture():
        previous = None
        with (output/'frames.jsonl').open('w', encoding='utf8', buffering=1) as journal:
            while not stop.is_set():
                tick = time.perf_counter()
                try:
                    if native:
                        job = native.post_screencap().wait()
                        if not job.status.succeeded:
                            raise RuntimeError('Native screenshot failed')
                        ok, encoded = cv2.imencode('.png', native.cached_image,
                                                   [cv2.IMWRITE_PNG_COMPRESSION, 1])
                        if not ok:
                            raise RuntimeError('PNG encoding failed')
                        data = encoded.tobytes()
                    elif adb:
                        result = subprocess.run([str(adb), '-s', serial, 'exec-out', 'screencap', '-p'],
                                                capture_output=True, timeout=3, check=True,
                                                creationflags=getattr(subprocess, 'CREATE_NO_WINDOW', 0))
                        data = result.stdout
                    else:
                        with http.open(prefix+'/screenshot', timeout=3) as reply:
                            data = reply.read()
                    digest = hashlib.sha256(data).hexdigest()
                    frame = Image.open(io.BytesIO(data))
                    if frame.size != (1280, 720):
                        raise ValueError('Unexpected screenshot dimensions; no implicit rescaling')
                    relative = 'frames/'+digest+'.png'
                    if digest != previous:
                        (output/relative).write_bytes(data)
                    row = {'elapsed_seconds': time.perf_counter()-origin, 'file': relative,
                           'changed': digest != previous, 'request_seconds': time.perf_counter()-tick}
                    journal.write(json.dumps(row)+'\n')
                    rows.append(row)
                    previous = digest
                except Exception as error:
                    errors.append(str(error))
                    stop.set()
                stop.wait(max(0, .05-(time.perf_counter()-tick)))
    worker = None
    report = {'gesture': {'vector': [dx, dy], 'hold_ms': hold_ms, 'mode': mode},
              'input_sent': False, 'world_distance_known': False}
    try:
        post('/screenshot/subscribe', {'subscriber_id': subscriber, 'interval_ms': 50})
        worker = threading.Thread(target=capture, daemon=True)
        worker.start()
        # Capture warmup is a measurement window, not a production movement delay.
        deadline = time.perf_counter()+(8 if adb and not native else 2)
        while len(rows) < 3 and not errors and time.perf_counter() < deadline:
            stop.wait(.05)
        if len(rows) < 3 or len({r['file'] for r in rows}) < 2 or errors:
            raise RuntimeError('Fresh capture unavailable; no task submitted')
        report['submitted_seconds'] = time.perf_counter()-origin
        started = post('/tasks/start', payload)
        report['task_ids'] = started.get('taskIds', [])
        if not report['task_ids']:
            raise RuntimeError('Task not accepted; no retry')
        report['input_sent'] = None  # Task acceptance does not prove the HUD gate passed.
        deadline = time.perf_counter()+12
        while time.perf_counter() < deadline:
            current = get('/maa/state')['instances'][instance]
            observed_ids = current.get('task_run_state', {}).get('pending_task_ids', [])
            if set(report['task_ids']).issubset(observed_ids) and not current['is_running']:
                report['task_status'] = current['task_run_state']['overall_status']
                report['terminal_seconds'] = time.perf_counter()-origin
                report['input_sent'] = True if report['task_status'] == 'Succeeded' else None
                break
            stop.wait(.1)
        else:
            report['task_status'] = 'observation_timeout'
            raise RuntimeError('Task still has no terminal observation; inspect same task, do not restart')
        # Measure residual camera/animation movement after input has finished.
        stop.wait(1.5)
        return report
    finally:
        stop.set()
        if worker:
            worker.join(4)
        try:
            post('/screenshot/unsubscribe', {'subscriber_id': subscriber})
        finally:
            report.update(samples=len(rows), unique_frames=len({r['file'] for r in rows}), errors=errors)
            submitted, terminal = report.get('submitted_seconds'), report.get('terminal_seconds')
            if submitted is not None and terminal is not None:
                active = [r for r in rows if submitted <= r['elapsed_seconds'] <= terminal]
                report['unique_frames_while_task_active'] = len({r['file'] for r in active})
            report['capture_source'] = ('MaaFramework native emulator capture, input disabled' if native else 'direct adb screencap' if adb else
                'MaaEnd screenshot cache; sampling rate is not a fresh-frame guarantee')
            report['serial'] = serial
            if len(rows) > 1:
                times = [r['elapsed_seconds'] for r in rows]
                report['median_sample_gap_seconds'] = float(np.median(np.diff(times)))
            (output/'probe.json').write_text(json.dumps(report, indent=4)+'\n', 'utf8')


def connect_native(api, instance, adb, serial, capture_config, install):
    """Apply a verified native capture configuration to an idle MaaEnd instance."""
    parsed = urllib.parse.urlparse(api)
    if parsed.scheme != 'http' or parsed.hostname not in {'127.0.0.1', 'localhost', '::1'}:
        raise ValueError('Use the local MaaEnd API')
    config = json.loads(capture_config.read_text('utf8'))
    mumu = config['extras']['mumu']
    # Bind the native display to the explicitly named emulator, not just an index.
    root = Path(mumu['path'])
    configs = list((root/'vms').glob(f"*-{int(mumu['index'])}/configs/vm_config.json"))
    port = serial.rsplit(':', 1)[-1]
    def has_port(value):
        if isinstance(value, dict):
            return str(value.get('host_port')) == port or any(has_port(v) for v in value.values())
        return isinstance(value, list) and any(has_port(v) for v in value)
    if len(configs) != 1 or not has_port(json.loads(configs[0].read_text('utf8'))):
        raise ValueError('MuMu instance index does not match the requested ADB serial')
    if not Path(mumu['lib']).is_file() or not adb.is_file():
        raise ValueError('Capture library or ADB executable missing')
    paths = [install/'resource', install/'resource_adb']
    if not all(p.is_dir() for p in paths):
        raise ValueError('Both installed resource bundles must exist')
    http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    base = api.rstrip('/')
    with http.open(base+'/maa/state', timeout=8) as reply:
        state = json.load(reply)['instances']
    if instance not in state or any(v['is_running'] for v in state.values()):
        raise RuntimeError('MaaEnd is busy or the requested profile is absent')
    prefix = base+'/maa/instances/'+urllib.parse.quote(instance, safe='')
    def post(path, body):
        request = urllib.request.Request(prefix+path, data=json.dumps(body).encode(),
                                        headers={'Content-Type': 'application/json'})
        with http.open(request, timeout=15) as reply:
            return json.load(reply)
    post('/agent/stop', {})
    connection = {'type': 'Adb', 'adb_path': str(adb), 'address': serial,
                  'screencap_methods': '64', 'input_methods': str((1 << 64)-9),
                  'config': json.dumps(config), 'display_short_side': 720}
    connected = post('/connect', connection)
    loaded = post('/resource/load', {'paths': [str(p.resolve()) for p in paths]})
    return {'connection': connected, 'resources': loaded,
            'profile': instance, 'serial': serial,
            'scope': 'Current MaaEnd runtime connection; GUI rediscovery may replace this configuration'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    pair = sub.add_parser('pair', help='Input-free near/far landmark displacement measurement')
    pair.add_argument('--before', type=Path, required=True)
    pair.add_argument('--after', type=Path, required=True)
    pair.add_argument('--landmarks', type=Path, required=True)
    pair.add_argument('--output', type=Path, required=True)
    probe = sub.add_parser('probe', help='One bounded gesture, gated by the current inn HUD')
    probe.add_argument('--api', default='http://127.0.0.1:12701/api')
    probe.add_argument('--instance', default='innreact')
    probe.add_argument('--payload', type=Path, required=True)
    probe.add_argument('--vector', type=int, nargs=2, required=True)
    probe.add_argument('--hold-ms', type=int, required=True)
    probe.add_argument('--mode', choices=['press', 'swipe', 'ramp'], required=True)
    probe.add_argument('--output', type=Path, required=True)
    probe.add_argument('--adb', type=Path, help='ADB executable for fresh capture independent of MaaEnd task cache')
    probe.add_argument('--serial', help='Explicit emulator serial for read-only direct screenshots')
    probe.add_argument('--maafw', type=Path, help='MaaFramework library directory for native emulator capture')
    probe.add_argument('--capture-config', type=Path, help='Reviewed native capture configuration JSON')
    trace = sub.add_parser('trace', help='Analyze response onset and post-input settling in a recorded probe')
    trace.add_argument('--recording', type=Path, required=True)
    trace.add_argument('--landmarks', type=Path, required=True)
    connect = sub.add_parser('connect-native', help='Use verified MuMu native capture in an idle MaaEnd profile')
    connect.add_argument('--api', default='http://127.0.0.1:12701/api')
    connect.add_argument('--instance', default='innreact')
    connect.add_argument('--adb', type=Path, required=True)
    connect.add_argument('--serial', required=True)
    connect.add_argument('--capture-config', type=Path, required=True)
    connect.add_argument('--install', type=Path, required=True)
    args = parser.parse_args()
    if args.command == 'pair':
        result = save_pair(args.before, args.after, args.landmarks, args.output)
    elif args.command == 'trace':
        result = trace_probe(args.recording, args.landmarks)
    elif args.command == 'connect-native':
        result = connect_native(args.api, args.instance, args.adb, args.serial,
                                args.capture_config, args.install)
    else:
        result = run_probe(args.api, args.instance, args.payload, args.output,
                           *args.vector, args.hold_ms, args.mode, args.adb, args.serial,
                           args.maafw, args.capture_config)
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
