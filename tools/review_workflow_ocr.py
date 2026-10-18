"""Offline MaaEnd OCR review of recorded PNG frames. No live controller binding."""
import argparse
import hashlib
import json
import re
from pathlib import Path

import numpy as np
from PIL import Image


def review(directory, install):
    from maa.library import Library
    from maa.pipeline import JOCR
    from maa.resource import Resource
    from maa.tasker import Tasker

    directory = directory.resolve()
    samples = [json.loads(line) for line in (directory / 'frames.jsonl').read_text('utf-8').splitlines()]
    frames = {}
    for sample in samples:
        frames.setdefault(sample['sha256'], sample)
    output = directory / 'ocr'
    output.mkdir(exist_ok=True)
    target = output / 'results.jsonl'
    completed = {}
    if target.exists():
        for line in target.read_text('utf-8').splitlines():
            row = json.loads(line)
            if not row.get('error'):
                completed[row['sha256']] = row
    Library.open((install / 'maafw').resolve())
    resource = Resource()
    resource.use_cpu()
    if not resource.post_bundle((install / 'resource').resolve()).wait().succeeded:
        raise RuntimeError('MaaEnd resource load failed')
    tasker = Tasker()
    # Python bind() also requires a controller. Bind only resources via its native API;
    # post_recognition supplies archived images and needs no input-capable controller.
    if not Library.framework().MaaTaskerBindResource(tasker._handle, resource._handle):
        raise RuntimeError('Offline resource binding failed')
    with target.open('a', encoding='utf-8', buffering=1) as log:
        for index, (digest, sample) in enumerate(frames.items(), 1):
            if digest in completed:
                continue
            path = (directory / sample['file']).resolve()
            result = {**sample, 'ocr_engine': 'MaaFramework OCR', 'source': 'archived_png'}
            try:
                if not path.is_relative_to(directory) or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
                    raise ValueError('Screenshot integrity check failed')
                with Image.open(path) as image:
                    if image.size != (1280, 720):
                        raise ValueError('Expected 1280x720 capture')
                    bgr = np.asarray(image.convert('RGB'))[:, :, ::-1].copy()
                job = tasker.post_recognition('OCR', JOCR(roi=(0, 0, 1280, 720)), bgr).wait()
                detail = job.get()
                if not detail or not detail.nodes or not detail.nodes[0].recognition:
                    raise RuntimeError('Missing recognition detail')
                result['raw'] = detail.nodes[0].recognition.raw_detail
                result['text'] = [r for r in result['raw'].get('all', []) if r.get('score', 0) >= .8
                                  and not re.search(r'UID\s*:', r.get('text', ''), re.I)]
                completed[digest] = result
            except Exception as error:
                result['error'] = str(error)
            log.write(json.dumps(result, ensure_ascii=False) + '\n')
            if index % 25 == 0 or index == len(frames):
                print(json.dumps({'processed': index, 'total': len(frames), 'successful': len(completed)}), flush=True)
    timeline = []
    for digest, sample in frames.items():
        result = completed.get(digest)
        if not result:
            continue
        texts = [r['text'] for r in sorted(result['text'], key=lambda r: (r['box'][1], r['box'][0]))]
        timeline.append({'elapsed_seconds': sample['elapsed_seconds'], 'file': sample['file'],
                         'sha256': digest, 'texts': texts})
    (output / 'timeline.json').write_text(json.dumps(timeline, indent=4, ensure_ascii=False) + '\n', 'utf-8')
    transcript = []
    for row in timeline:
        transcript.append(f"[{row['elapsed_seconds']:.1f}s] {row['file']}\n" + '\n'.join(row['texts']))
    (output / 'transcript.txt').write_text('\n\n'.join(transcript) + '\n', 'utf-8')
    summary = {'samples': len(samples), 'unique_frames': len(frames), 'ocr_succeeded': len(completed),
               'ocr_failed': len(frames) - len(completed), 'confidence_threshold_for_transcript': .8,
               'raw_results_retained': True, 'live_controller_bound': False, 'game_inputs_sent': False,
               'models': {name: hashlib.sha256((install / 'resource/model/ocr' / name).read_bytes()).hexdigest()
                          for name in ('det.onnx', 'rec.onnx', 'keys.txt')}}
    (output / 'summary.json').write_text(json.dumps(summary, indent=4) + '\n', 'utf-8')
    print(json.dumps(summary), flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    parser.add_argument('--install', type=Path, default=Path(__file__).resolve().parents[1] / 'install')
    args = parser.parse_args()
    review(args.directory, args.install)
