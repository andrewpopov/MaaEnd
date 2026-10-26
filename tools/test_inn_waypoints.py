"""Topology/safety contracts for named inn movement, without game inputs."""
import unittest
from inn_waypoints import InnRoutes, pickup_step, counter_motion_observed, CounterSlots


class WaypointTests(unittest.TestCase):
    def test_every_table_pair_stays_in_middle_lane(self):
        routes = InnRoutes()
        for source in range(1, 7):
            for target in range(1, 7):
                legs = routes.goToTable(target, f"table{source}")
                if source == target:
                    self.assertEqual(legs, [])
                    continue
                self.assertEqual(legs[0].source, f"table{source}")
                self.assertEqual(legs[-1].target, f"table{target}")
                self.assertTrue(all(leg.target.startswith("lane") for leg in legs[:-1]))
                for a, b in zip(legs, legs[1:]):
                    self.assertEqual(a.target, b.source)

    def test_return_uses_same_waypoints_in_reverse_with_directional_timings(self):
        routes = InnRoutes()
        reverse = {"forward": "backward", "backward": "forward", "left": "right", "right": "left"}
        for table in range(1, 7):
            outbound = routes.goToTable(table)
            inbound = routes.recenter(f"table{table}")
            self.assertEqual([(leg.source, leg.target) for leg in inbound],
                             [(leg.target, leg.source) for leg in reversed(outbound)])
            self.assertEqual([leg.direction for leg in inbound],
                             [reverse[leg.direction] for leg in reversed(outbound)])
        self.assertEqual(routes.goToTable(1)[0].milliseconds, 700)
        self.assertEqual(routes.recenter("table1")[-1].milliseconds, 900)

    def test_measured_short_branches_apply_in_both_directions(self):
        routes=InnRoutes(branch_ms=400)
        for table in range(1,7):
            self.assertEqual(routes.goToTable(table)[-1].milliseconds,400)
            self.assertEqual(routes.recenter(f"table{table}")[0].milliseconds,400)
        for invalid in (0,99,501,True):
            with self.assertRaises(ValueError):InnRoutes(branch_ms=invalid)

    def test_counter_slots_require_calibration_and_do_not_cross_tables(self):
        with self.assertRaises(ValueError):InnRoutes().counterSpot(1)
        routes = InnRoutes({1: -360, 2: 540})
        legs = routes.counterSpot(2, "pickup1")
        self.assertEqual([leg.target for leg in legs], ["counter", "pickup2"])
        self.assertEqual(sum(leg.milliseconds for leg in legs), 900)
        self.assertEqual(routes.goToTable(6, "pickup1")[0].target, "counter")

    def test_unknown_pose_and_invalid_table_never_produce_motion(self):
        routes = InnRoutes()
        with self.assertRaises(ValueError):routes.recenter("lost")
        for invalid in (0, 7, True, "2"):
            with self.assertRaises(ValueError):routes.goToTable(invalid)

    def test_long_calibrated_counter_leg_respects_native_pulse_limit(self):
        routes = InnRoutes({1: 3000})
        pulses = routes.pulses(routes.counterSpot(1))
        self.assertEqual(sum(p[2] for p in pulses), 3000)
        self.assertTrue(all(0 < p[2] <= 1400 for p in pulses))


class CounterSlotTests(unittest.TestCase):
    def test_slot_pose_is_independent_of_changing_dish(self):
        slots=CounterSlots()
        slots.observe_pickup(115,"salted_cheese_crackers")
        slots.observe_pickup(124,"shroom_dumpling")
        self.assertEqual(len(slots.poses),1)
        self.assertEqual(slots.poses[0]["offset"],120)
        self.assertEqual(slots.poses[0]["last_dish"],"shroom_dumpling")

    def test_direct_route_uses_measured_pose_only(self):
        slots=CounterSlots()
        self.assertIsNone(slots.direct_step([890,310,90,90],0))
        slots.observe_pickup(440,"shroom_dumpling")
        self.assertEqual(slots.direct_step([890,310,90,90],0),((275,551,440),440,"right"))
        self.assertIsNone(slots.direct_step([1100,310,90,90],0))
        restored=CounterSlots(slots.poses)
        self.assertEqual(restored.poses,slots.poses)


class CounterSteeringTests(unittest.TestCase):
    def test_ineffective_or_wrong_direction_touch_does_not_advance_position(self):
        before=[{"dish":"shroom_dumpling","box":[520,310,96,96]}]
        wrong=[{"dish":"shroom_dumpling","box":[554,310,96,96]}]
        moved=[{"dish":"shroom_dumpling","box":[340,310,96,96]}]
        self.assertFalse(counter_motion_observed(before,before,420))
        self.assertFalse(counter_motion_observed(before,wrong,420))
        self.assertTrue(counter_motion_observed(before,moved,420))
        self.assertFalse(counter_motion_observed(before,[],420))
        self.assertFalse(counter_motion_observed(before+before,moved,420))

    def test_side_saturation_never_becomes_a_backward_step(self):
        self.assertIsNone(pickup_step([1100,300,100,100],1400))
        self.assertIsNone(pickup_step([300,300,100,100],-1400))

    def test_centered_food_approaches_counter_without_lateral_drift(self):
        pulse,delta,side=pickup_step([600,300,100,100],800)
        self.assertEqual((pulse,delta,side),((195,631,200),0,"center"))

    def test_large_visible_offset_uses_one_bounded_correction(self):
        pulse,delta,side=pickup_step([1000,300,100,100],0)
        self.assertEqual(pulse,(275,551,500))
        self.assertEqual(delta,500)
        self.assertEqual(side,"right")

if __name__ == "__main__":unittest.main()
