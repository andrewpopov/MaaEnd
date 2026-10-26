"""Counter heading is advisory; edge strength must not reject warm wood."""
import unittest
import cv2
import numpy as np
from inn_counter_heading import counter_heading


class HeadingTests(unittest.TestCase):
    def test_low_contrast_independent_borders_are_detected(self):
        frame=np.full((720,1280,3),110,dtype=np.uint8)
        cv2.rectangle(frame,(220,410),(1060,450),(135,135,135),-1)
        cv2.rectangle(frame,(220,510),(1060,550),(135,135,135),-1)
        result=counter_heading(frame)
        self.assertTrue(result['usable'],result)
        self.assertLess(abs(result['angle_degrees']),1)
        self.assertFalse(result['input_authorized'])

    def test_flat_screen_is_not_a_heading(self):
        self.assertFalse(counter_heading(np.zeros((720,1280,3),dtype=np.uint8))['usable'])

    def test_one_border_does_not_supply_independent_evidence(self):
        frame=np.full((720,1280,3),110,dtype=np.uint8)
        frame[470:,:, :]=135
        self.assertFalse(counter_heading(frame)['usable'])


if __name__=='__main__':unittest.main()
