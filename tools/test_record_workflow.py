"""Verify capture subscription renewal and input-free recording."""
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from PIL import Image

import record_workflow


class WorkflowRecordingTests(unittest.TestCase):
    def test_renews_before_thirty_second_expiry_and_never_sends_inputs(self):
        clock = [0.0]
        calls = []
        frame = [0]
        class Reply:
            def __init__(self, data):
                self.data = data
            def __enter__(self):
                return self
            def __exit__(self, *args):
                pass
            def read(self):
                return self.data
        class HTTP:
            def open(self, request, timeout):
                if isinstance(request, str):
                    calls.append(('GET', request, clock[0]))
                    stream = io.BytesIO()
                    Image.new('RGB', (1280, 720), (frame[0], 0, 0)).save(stream, format='PNG')
                    frame[0] += 1
                    return Reply(stream.getvalue())
                calls.append(('POST', request.full_url, clock[0]))
                return Reply(b'{"ok":true}')
        def sleep(seconds):
            clock[0] += seconds
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / 'capture'
            with patch.object(record_workflow.urllib.request, 'build_opener', return_value=HTTP()), \
                 patch.object(record_workflow.time, 'monotonic', side_effect=lambda: clock[0]), \
                 patch.object(record_workflow.time, 'sleep', side_effect=sleep):
                status = record_workflow.record('http://127.0.0.1:12702/api', 'fixture', output, duration=36)
            renewals = [c[2] for c in calls if c[1].endswith('/subscribe')]
            self.assertEqual(renewals, [0, 10, 20, 30])
            self.assertEqual(status['status'], 'stopped')
            self.assertEqual(status['samples'], 72)
            self.assertEqual(status['unique_frames'], 72)
            self.assertFalse(status['game_inputs_sent'])
            self.assertTrue(all(c[1].endswith(('/screenshot', '/subscribe', '/unsubscribe')) for c in calls))
            self.assertEqual(len((output / 'frames.jsonl').read_text().splitlines()), 72)
            self.assertIsNone(json.loads((output / 'status.json').read_text())['stream_warning'])


if __name__ == '__main__':
    unittest.main()
