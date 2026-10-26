"""Record a manual workflow using MaaEnd's screenshot-only HTTP subscription.

No tasks, clicks, keys or controller changes are submitted. Create STOP in the
output directory to finish. Identical frames share one PNG; all sample times
remain in frames.jsonl. OCR can be performed on saved images independently.
"""
import argparse
import datetime as dt
import hashlib
import io
import json
import time
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

from PIL import Image


def now():
    return dt.datetime.now(dt.timezone.utc).isoformat()


def record(base, instance, output, duration=1800, interval=.5, max_bytes=1024**3):
    url = urllib.parse.urlparse(base)
    if url.scheme != 'http' or url.hostname not in {'127.0.0.1', 'localhost', '::1'}:
        raise ValueError('Use only the local MaaEnd API')
    if duration <= 0 or interval < .25 or max_bytes <= 0:
        raise ValueError('Invalid recording bounds')
    output.mkdir(parents=True, exist_ok=False)
    (output / 'frames').mkdir()
    http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    prefix = base.rstrip('/') + '/maa/instances/' + urllib.parse.quote(instance, safe='')
    subscriber = 'manual-workflow-' + uuid.uuid4().hex
    status = {'started_at': now(), 'instance': instance, 'source': prefix + '/screenshot',
              'status': 'starting', 'samples': 0, 'unique_frames': 0, 'bytes': 0,
              'input_capture': False, 'audio_capture': False, 'game_inputs_sent': False,
              'ocr_status': 'pending_offline_processing', 'cache_freshness': 'not_independently_verified',
              'interval_seconds': interval, 'maximum_seconds': duration}
    def save():
        # A Windows reader may briefly open status.json without FILE_SHARE_DELETE.
        # Frame capture must not fail merely because the status rename is locked.
        status.pop('status_write_warning', None)
        temp = output / 'status.tmp'
        temp.write_text(json.dumps(status, indent=4) + '\n', 'utf-8')
        try:
            temp.replace(output / 'status.json')
        except PermissionError:
            status['status_write_warning'] = 'status.json temporarily locked by a reader'
            if status['status'] in {'stopped', 'failed'}:
                (output / 'status-final.json').write_text(json.dumps(status, indent=4) + '\n', 'utf-8')
    def post(path):
        req = urllib.request.Request(prefix + path, data=json.dumps(
            {'subscriber_id': subscriber, 'interval_ms': int(interval * 1000)}).encode(),
            headers={'Content-Type': 'application/json'})
        with http.open(req, timeout=5) as response:
            response.read()
    save()
    try:
        post('/screenshot/subscribe')
        start = time.monotonic()
        renewed_at = start
        last_change = start
        last_digest = None
        errors = 0
        status['status'] = 'recording'
        with (output / 'frames.jsonl').open('a', encoding='utf-8', buffering=1) as log:
            while time.monotonic() - start < duration and not (output / 'STOP').exists():
                tick = time.monotonic()
                try:
                    # MXU expires subscriptions after 30s; screenshot GET does not renew them.
                    if tick - renewed_at >= 10:
                        post('/screenshot/subscribe')
                        renewed_at = time.monotonic()
                        status['last_subscription_renewal_at'] = now()
                    with http.open(prefix + '/screenshot', timeout=5) as response:
                        data = response.read()
                    with Image.open(io.BytesIO(data)) as image:
                        dimensions = image.size
                        if image.format != 'PNG':
                            raise ValueError('Expected lossless PNG screenshot')
                        image.verify()
                    digest = hashlib.sha256(data).hexdigest()
                    if digest != last_digest:
                        last_change = tick
                        status['last_changed_at'] = now()
                        last_digest = digest
                    status['unchanged_seconds'] = round(tick - last_change, 1)
                    status['stream_warning'] = 'unchanged_frame_for_15_seconds' if tick - last_change >= 15 else None
                    relative = 'frames/' + digest + '.png'
                    target = output / relative
                    if not target.exists():
                        if status['bytes'] + len(data) > max_bytes:
                            status['stop_reason'] = 'storage_limit'
                            break
                        target.write_bytes(data)
                        status['bytes'] += len(data)
                        status['unique_frames'] += 1
                    log.write(json.dumps({'captured_at': now(), 'elapsed_seconds': round(tick-start, 3),
                                          'file': relative, 'sha256': digest, 'size': dimensions}) + '\n')
                    status['samples'] += 1
                    status['last_sample_at'] = now()
                    errors = 0
                except Exception as error:
                    errors += 1
                    status['last_error'] = str(error)
                    if errors >= 3:
                        raise RuntimeError('Screenshot stream failed three times; recording stopped') from error
                save()
                time.sleep(max(0, interval - (time.monotonic() - tick)))
        status['status'] = 'stopped'
        status.setdefault('stop_reason', 'stop_file' if (output / 'STOP').exists() else 'duration_limit')
    except Exception as error:
        status['status'] = 'failed'
        status['error'] = str(error)
    finally:
        try:
            post('/screenshot/unsubscribe')
        except Exception as error:
            status['unsubscribe_error'] = str(error)
        status['finished_at'] = now()
        save()
    return status


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--api-url', required=True)
    parser.add_argument('--instance', required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--duration', type=float, default=1800)
    args = parser.parse_args()
    print(json.dumps(record(args.api_url, args.instance, args.output, args.duration)))
