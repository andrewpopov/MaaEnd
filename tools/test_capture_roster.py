"""Regression checks for screenshot checkpoints and carousel failure handling."""
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from PIL import Image

import capture_roster as roster


def png(value):
    image = Image.new('RGB', (1280, 720), (value, value, value))
    stream = io.BytesIO()
    image.save(stream, format='PNG')
    return stream.getvalue()


class FakeClient:
    task_id = 'test'

    def __init__(self, frames):
        self.frames = iter(frames)

    def post(self, *args):
        pass

    def move(self, *args, **kwargs):
        pass

    def screenshot(self):
        return next(self.frames)


class CaptureTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def test_resume_preserves_completed_operator(self):
        archive = roster.Archive(self.root)
        record = archive.find_or_add(png(20))
        for name in roster.SCREENS:
            archive.save(record, name, png(20))
        resumed = roster.Archive(self.root, resume=True)
        existing = resumed.find_or_add(png(20))
        self.assertTrue(resumed.captured(existing))
        self.assertEqual(len(resumed.data['operators']), 1)

    def test_missing_file_is_not_treated_as_completed(self):
        archive = roster.Archive(self.root)
        record = archive.find_or_add(png(20))
        record['screens'] = list(roster.SCREENS)
        archive.checkpoint()
        resumed = roster.Archive(self.root, resume=True)
        self.assertFalse(resumed.captured(resumed.data['operators'][0]))

    def test_resolution_error_leaves_previous_checkpoint_intact(self):
        archive = roster.Archive(self.root)
        record = archive.find_or_add(png(20))
        before = archive.path.read_bytes()
        stream = io.BytesIO()
        Image.new('RGB', (640, 360)).save(stream, format='PNG')
        with self.assertRaises(RuntimeError):
            archive.save(record, roster.SCREENS[1], stream.getvalue())
        self.assertEqual(archive.path.read_bytes(), before)

    def test_stalled_navigation_never_marks_scan_complete(self):
        archive = roster.Archive(self.root)
        client = FakeClient([png(20), png(20)])
        with patch.object(roster, 'capture_operator'):
            with self.assertRaisesRegex(RuntimeError, 'did not change'):
                roster.scan(client, archive)
        self.assertFalse(archive.data['complete'])
        self.assertTrue((self.root / '001' / roster.SCREENS[0]).is_file())

    def test_wrap_completes_and_existing_operators_are_not_recaptured(self):
        archive = roster.Archive(self.root)
        for value in [20, 160]:
            record = archive.find_or_add(png(value))
            for name in roster.SCREENS:
                archive.save(record, name, png(value))
        client = FakeClient([png(20), png(160), png(20)])
        with patch.object(roster, 'capture_operator') as capture:
            roster.scan(client, archive)
            capture.assert_not_called()
        self.assertTrue(archive.data['complete'])

    def test_initial_page_check_does_not_click(self):
        client = roster.MaaClient.__new__(roster.MaaClient)
        client.task_id = 'test'
        client.owns_task = False
        client.require_idle = lambda: None
        requests = []
        client.post = lambda path, payload: requests.append(payload)
        client.state = lambda: {'is_running': False, 'task_run_state': {'statuses': {'test': 'succeeded'}}}
        client.move('Overview', 'Overview')
        pipeline = json.loads(requests[0][0]['pipeline_override'])
        self.assertEqual(pipeline['RosterCaptureStep']['action'], 'DoNothing')
        self.assertNotIn('target', pipeline['RosterCaptureStep'])

    def test_nonlocal_api_rejected_before_contact(self):
        with self.assertRaises(ValueError):
            roster.MaaClient('http://example.com/api')


if __name__ == '__main__':
    unittest.main()
