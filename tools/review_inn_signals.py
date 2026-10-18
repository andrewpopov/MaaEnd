"""Extract evidence-backed inn UI signals from an offline MaaEnd OCR review."""
import argparse
import json
import re
from pathlib import Path

PATTERNS = {
    'ordering': r'^Ordering$',
    'dining': r'^Dining$',
    'bill_ready': r'^Bill Ready$',
    'kitchen_1': r'^Kitchen 1$',
    'kitchen_2': r'^Kitchen 2$',
    'kitchen_queue_full': r'Cannot cook more.*Cooking Queue is already ful',
    'tray_movement_restriction': r'Cannot jump or dash/dodge while holding trays',
    'gourmand_excludes_entree': r"long as it.s not an entree",
    'helper_collecting_bills': r'is helping to collect the dining bills',
    'closing_soon': r'^Closing Soon$',
    'closed_to_new_patrons': r'Inn closed.*No new',
    'serve_remaining_orders': r'Quickly serve the dishes and drinks for more income',
    'session_ended': r'^Session Ended$',
    'auto_collect_remaining_income': r'Auto.collect remaining income',
    'sweet_and_salty_theme': r'^Sweet & Salty$',
}


def summarize(directory):
    folder = directory / 'ocr'
    rows = {}
    for line in (folder / 'results.jsonl').read_text('utf-8').splitlines():
        row = json.loads(line)
        if not row.get('error'):
            rows[row['sha256']] = row
    signals = {}
    for name, pattern in PATTERNS.items():
        observations = []
        for row in rows.values():
            for token in row['text']:
                if re.search(pattern, token['text'], re.I):
                    observations.append({'elapsed_seconds': row['elapsed_seconds'], 'file': row['file'],
                                         'text': token['text'], 'box': token['box'], 'score': token['score']})
        if observations:
            signals[name] = {'observation_count': len(observations),
                             'first': observations[0], 'last': observations[-1]}
    report = {'recording': directory.name, 'ocr_frames_reviewed': len(rows), 'signals': signals,
              'evidence_scope': 'Recorded English native-client screens, not input logs or validated navigation',
              'user_assessment': 'Demonstration was not optimal; do not copy its scheduling',
              'automation_implications': [
                  'Use live kitchen queue availability and recipe timers; do not repeat a full-queue action.',
                  'Track requested dish icons separately from OCR text; some order cards have no dish names.',
                  'Represent Gourmand requirements as positive and negative constraints.',
                  'Check helper collection state before scheduling a duplicate bill collection.',
                  'Separate normal operation, closing warning, post-close serving and session-ended states.',
                  'Plan delivery routes for carrying restrictions and observed tray capacity.',
                  'Keep pre-cooking limited to unreserved capacity and current demand; phase-based thresholds need more runs.'
              ],
              'unvalidated': ['dish icon classifier', 'physical movement and interaction mapping',
                              'exact kitchen and tray capacity rules across upgrades',
                              'how helper bill collection is triggered',
                              'optimal pre-cook quantities or round-phase thresholds',
                              'recovery from wrong dish, blocked path or full tray'],
              'live_automation_enabled': False}
    annotation = folder / 'visual-review.json'
    if annotation.exists():
        report['visually_checked'] = json.loads(annotation.read_text('utf-8'))
    path = folder / 'inn-review.json'
    path.write_text(json.dumps(report, indent=4, ensure_ascii=False) + '\n', 'utf-8')
    print(json.dumps({'output': str(path), 'frames': len(rows), 'signals': list(signals)}))
    return report


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    summarize(parser.parse_args().directory)
