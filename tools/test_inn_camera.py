"""Camera calibration keeps verified yaw across a lateral-only correction."""
import unittest
from unittest.mock import patch
from inn_service_trial import ServiceTrial


class CameraEvidenceTests(unittest.TestCase):
    def test_occluded_counter_edge_does_not_undo_verified_yaw(self):
        trial=ServiceTrial.__new__(ServiceTrial)
        report={"view":{"phase":"active"},"image":"paired.png"}
        windows=iter([(550,700),(620,770)])
        trial.dining_window=lambda _:next(windows)
        trial.trace=lambda *a,**kw:None
        movements=[]
        trial.move=lambda steps,name:movements.extend(steps)
        trial.snapshot=lambda:report
        trial.execute=lambda *a: self.fail("Lateral motion must not trigger another camera turn")
        headings=[{"usable":True,"angle_degrees":0},{"usable":False}]
        with patch('inn_service_trial.cv2.imread',return_value=None),patch('inn_service_trial.counter_heading',side_effect=headings):
            self.assertIs(trial.normalize_camera(report),report)
        self.assertEqual(len(movements),1)
        self.assertGreaterEqual(movements[0][2],240)

    def test_round_end_returns_without_another_camera_input(self):
        trial=ServiceTrial.__new__(ServiceTrial)
        report={"view":{"phase":"ended"}}
        self.assertIs(trial.normalize_camera(report),report)


if __name__=='__main__':unittest.main()
