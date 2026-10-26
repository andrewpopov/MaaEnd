"""Verify motion measurements cannot silently accept flat or repeated landmarks."""
import unittest
import json
import tempfile
from pathlib import Path

import numpy as np
from PIL import Image

from inn_motion_lab import analyze_pair, locate, probe_nodes, trace_probe


class MotionLabTests(unittest.TestCase):
    def test_trace_distinguishes_motion_before_terminal_and_residual_drift(self):
        rng = np.random.default_rng(19)
        patch = rng.integers(20, 220, size=(34, 30, 3), dtype=np.uint8)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root/'frames').mkdir()
            rows = []
            for i, (at, dy) in enumerate([(0.8, 0), (1.3, 8), (2.7, 12)]):
                pixels = np.zeros((720, 1280, 3), dtype=np.uint8)
                pixels[320+dy:354+dy, 550:580] = patch
                relative = f'frames/{i}.png'
                Image.fromarray(pixels).save(root/relative)
                rows.append({'elapsed_seconds': at, 'file': relative})
            (root/'frames.jsonl').write_text('\n'.join(json.dumps(row) for row in rows), 'utf8')
            (root/'probe.json').write_text(json.dumps({'submitted_seconds': 1.0, 'terminal_seconds': 1.5,
                'gesture': {'mode': 'fixture'}, 'capture_source': 'fixture'}), 'utf8')
            landmarks = root/'landmarks.json'
            landmarks.write_text(json.dumps([{'name': 'board', 'depth': 'near', 'box': [550, 320, 30, 34]}]), 'utf8')
            metric = trace_probe(root, landmarks)['metrics'][0]
            self.assertAlmostEqual(metric['first_observed_motion_after_submit_seconds'], .3)
            self.assertEqual(metric['terminal_displacement_pixels'], [0, 8])
            self.assertEqual(metric['settled_displacement_pixels'], [0, 12])
            self.assertAlmostEqual(metric['residual_after_terminal_pixels'], 4)

    def test_tracks_recorded_physical_table_after_foreground_drift(self):
        fixtures = Path(__file__).resolve().parents[1]/'agent/go-service/innkeeping/testdata'
        before = Image.open(fixtures/'inn-seat1-template-source.png').convert('RGB')
        after = Image.open(fixtures/'inn-seat1-drift.png').convert('RGB')
        result = locate(before, after, [582, 315, 18, 34])
        self.assertTrue(result['matched'], result)
        self.assertGreater(result['displacement'][0], 80)
        self.assertLess(result['displacement'][0], 120)

    def test_tracks_near_far_parallax_without_claiming_world_distance(self):
        rng = np.random.default_rng(17)
        source = np.zeros((720, 1280, 3), dtype=np.uint8)
        target = source.copy()
        landmarks = [dict(name='board', depth='near', box=[550, 320, 30, 34]),
                     dict(name='door', depth='far', box=[850, 170, 32, 36])]
        for item, displacement in zip(landmarks, [(100, 4), (28, 2)]):
            x, y, w, h = item['box']
            patch = rng.integers(20, 220, size=(h, w, 3), dtype=np.uint8)
            source[y:y+h, x:x+w] = patch
            dx, dy = displacement
            target[y+dy:y+dy+h, x+dx:x+dx+w] = patch+10
        result = analyze_pair(Image.fromarray(source), Image.fromarray(target), landmarks)
        self.assertEqual(result['median_displacement_pixels'], {'near': [100, 4], 'far': [28, 2]})
        self.assertGreater(result['near_far_difference_pixels'], 72)
        self.assertFalse(result['world_distance_known'])

    def test_rejects_blank_and_ambiguous_repeated_landmark(self):
        blank = Image.new('RGB', (1280, 720), 'gray')
        self.assertFalse(locate(blank, blank, [550, 320, 30, 34])['matched'])
        rng = np.random.default_rng(8)
        patch = rng.integers(0, 240, size=(34, 30, 3), dtype=np.uint8)
        before = np.zeros((720, 1280, 3), dtype=np.uint8)
        after = before.copy()
        before[320:354, 550:580] = patch
        after[320:354, 550:580] = patch
        after[320:354, 590:620] = patch
        self.assertFalse(locate(Image.fromarray(before), Image.fromarray(after), [550, 320, 30, 34])['matched'])

    def test_probe_is_one_gesture_after_two_hud_gates(self):
        for mode in ['press', 'swipe', 'ramp']:
            nodes = probe_nodes(0, -18, 350, mode)
            self.assertEqual(len(nodes), 2)
            gate = nodes['InnMotionLabTheme']
            self.assertEqual(gate['action']['type'], 'DoNothing')
            action = nodes['InnMotionLabGesture']['action']
            self.assertEqual(action['param']['contact'], 8)
            self.assertNotIn('next', nodes['InnMotionLabGesture'])
            self.assertEqual(nodes['InnMotionLabGesture']['recognition']['param']['expected'], ['End Session'])
        for vector, duration in [((0, 0), 350), ((100, 0), 350), ((0, 18), 5000)]:
            with self.assertRaises(ValueError):
                probe_nodes(*vector, duration, 'press')


if __name__ == '__main__':
    unittest.main()
