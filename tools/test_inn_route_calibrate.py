"""Regression checks for supervised movement input boundaries."""
import unittest

from inn_route_calibrate import make_nodes, validate_steps


class InnRouteCalibrationTests(unittest.TestCase):
    def test_camera_replay_preserves_origin_and_exact_timing(self):
        steps = [{'Kind': 'camera', 'X': 650, 'Y': 230, 'Milliseconds': 250,
                  'HoldMilliseconds': 0, 'Origin': [1050, 230]},
                 {'Kind': 'walk', 'X': 195, 'Y': 471, 'Milliseconds': 20,
                  'HoldMilliseconds': 700}]
        nodes = make_nodes(steps)
        self.assertEqual(nodes['InnCalibration0']['action']['param'], {
            'begin': [1050, 230], 'end': [650, 230], 'duration': 250, 'end_hold': 0})
        self.assertEqual(nodes['InnCalibration1']['action']['param']['end_hold'], 700)
        self.assertEqual(nodes['InnCalibration0']['recognition']['param']['expected'], ['End Session'])
        self.assertEqual(nodes['InnCalibration2']['action']['type'], 'DoNothing')

    def test_world_input_requires_positive_explore_hud(self):
        nodes = make_nodes([{'Kind': 'walk', 'X': 275, 'Y': 551,
                             'Milliseconds': 20, 'HoldMilliseconds': 300}], world=True)
        self.assertEqual(nodes['InnCalibration0']['recognition']['param'], {
            'roi': [30, 15, 220, 45], 'expected': ['Explore']})

    def test_invalid_inputs_never_compile_to_game_actions(self):
        base = {'Kind': 'walk', 'X': 195, 'Y': 471, 'Milliseconds': 20, 'HoldMilliseconds': 700}
        for change in [{'Kind': 'click'}, {'X': 600}, {'Milliseconds': 0},
                       {'Milliseconds': 1000, 'HoldMilliseconds': 1000}, {'X': 195.0}]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                make_nodes([dict(base, **change)])
        with self.assertRaises(ValueError):
            make_nodes([])
        with self.assertRaises(ValueError):
            make_nodes([base] * 24)

    def test_neutral_touch_is_encoded_in_one_bounded_gesture(self):
        step = {'Kind': 'walk', 'X': 275, 'Y': 551, 'Milliseconds': 80,
                'HoldMilliseconds': 400, 'JoystickWarmup': True}
        param = make_nodes([step])['InnCalibration0']['action']['param']
        self.assertEqual(param['end'], [[195, 551], [275, 551]])
        self.assertEqual(param['duration'], [1, 80])
        self.assertEqual(param['end_hold'], [50, 400])
        for change in [{'Milliseconds': 20}, {'JoystickWarmup': 1}, {'Kind': 'camera', 'X': 650, 'Y': 230, 'HoldMilliseconds': 0}]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                make_nodes([dict(step, **change)])


    def test_camera_cannot_hold_or_start_in_action_controls(self):
        base = {'Kind': 'camera', 'X': 650, 'Y': 230, 'Milliseconds': 250, 'HoldMilliseconds': 0}
        for change in [{'HoldMilliseconds': 80}, {'Origin': [1100, 600]}, {'Y': 600}]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                validate_steps([dict(base, **change)])


if __name__ == '__main__':
    unittest.main()
