"""Regression checks for interrupted inn return movement; no game inputs."""
import unittest
from inn_service_trial import ServiceTrial


class ReturnResumeTests(unittest.TestCase):
    def trial(self):
        t=ServiceTrial.__new__(ServiceTrial)
        t.seat=1;t.branch=500;t.return_motion_done=False;t.cycles=0
        t.at_counter=False;t.lat=0;t.scene_version=2;t.origin_verified_version=-1
        t.trace=lambda *a,**k:None
        return t

    def test_fast_return_uses_bar_contact_without_alignment_scan(self):
        t=self.trial();t.fast_routes=True;moves=[]
        t.move=lambda steps,name,**kw:moves.append(steps)
        t.persist=lambda:None
        t.verify_origin=lambda:self.fail("Fast route uses timed return and endpoint guard")
        t.return_counter()
        self.assertEqual(moves[-1],[(195,631,700)])
        self.assertTrue(t.at_counter)
        self.assertIsNone(t.seat)

    def test_return_undoes_only_acknowledged_service_approach(self):
        t=self.trial();t.fast_routes=True;t.service_extra=150
        moves=[];t.move=lambda steps,name:moves.append(steps);t.persist=lambda:None
        t.return_counter()
        self.assertEqual(moves[0][0],(115,551,150))
        self.assertEqual(t.service_extra,0)

    def test_verification_failure_does_not_replay_completed_movement(self):
        t=self.trial();moves=[];checkpoints=[]
        t.move=lambda steps,name,**kw:moves.append(steps)
        t.persist=lambda:checkpoints.append(t.return_motion_done)
        def fail():raise RuntimeError("landmark missing")
        t.verify_origin=fail
        with self.assertRaises(RuntimeError):t.return_counter()
        self.assertEqual(checkpoints,[True])
        self.assertFalse(t.at_counter)
        self.assertEqual(t.cycles,0)
        t.verify_origin=lambda:None
        t.return_counter()
        self.assertEqual(len(moves),1)
        self.assertTrue(t.at_counter)
        self.assertIsNone(t.seat)
        self.assertEqual(t.cycles,1)
        self.assertFalse(t.return_motion_done)

    def test_failed_movement_is_not_checkpointed_as_completed(self):
        t=self.trial()
        def fail(*args):raise RuntimeError("native task failed")
        t.move=fail
        t.persist=lambda:self.fail("unconfirmed movement checkpointed")
        t.verify_origin=lambda:self.fail("unconfirmed movement verified")
        with self.assertRaises(RuntimeError):t.return_counter()
        self.assertFalse(t.return_motion_done)
        self.assertEqual(t.cycles,0)

    def test_zero_lateral_counter_flag_still_requires_current_landmarks(self):
        t=self.trial();checks=[]
        def verify():
            checks.append(t.scene_version)
            t.origin_verified_version=t.scene_version
        t.verify_origin=verify
        t.center_counter();t.center_counter()
        self.assertEqual(checks,[2])
        t.scene_version+=1;t.center_counter()
        self.assertEqual(checks,[2,3])

    def test_native_alignment_keeps_two_strict_checks_without_redundant_full_read(self):
        t=self.trial();t.origin={"id":"walls","start":[{"box":[550,190,80,40]},{"box":[760,190,80,40]}]}
        t.last_task_id=123;calls=[]
        t.execute=lambda entry,nodes,name,timeout:calls.append(nodes)
        t.snapshot=lambda:self.fail("Native endpoint already has fresh strict verification")
        t.verify_origin()
        nodes=calls[0]
        self.assertEqual(nodes["InnOriginAligned0"]["custom_recognition_param"]["mode"],"aligned")
        self.assertEqual(nodes["InnOriginConfirmed0"]["custom_recognition_param"]["mode"],"aligned")
        self.assertEqual(nodes["InnOriginAligned0"]["post_wait_freezes"]["target"],[550,190,80,40])
        self.assertEqual(nodes["InnOriginConfirmed0"]["next"],[])
        self.assertEqual(t.origin_verified_version,t.scene_version)

    def test_resume_failure_clears_stale_pause_flag_for_recovery(self):
        t=self.trial();t.pause_confirmed=True
        def fail(*args):raise RuntimeError("active screen not acknowledged")
        t.execute=fail
        with self.assertRaises(RuntimeError):t.resume_game()
        self.assertFalse(t.pause_confirmed)


class DeliveryRetryTests(unittest.TestCase):
    def trial(self,reports):
        from types import SimpleNamespace
        t=ServiceTrial.__new__(ServiceTrial)
        t.at_counter=False;t.seat=2;t.served=0;t.pending_serve=None
        t.ledger=SimpleNamespace(served={})
        t.trace=lambda *a,**k:None
        t.board=lambda *a:None
        t.snapshot=lambda **kw:next(reports)
        return t

    def report(self,held=True,prompt="serve",unknown=False):
        return {"image":"unused", "view":{"phase":"active","unknown":unknown,
            "seats":[{"seat":"2","phase":"waiting" if held else "dining",
                "dishes":["fin_dumpling"] if held else []}],
            "tray":{"fin_dumpling":1} if held else {},
            "tray_slots":["fin_dumpling" if held else ""],"prompt":prompt}}

    def test_delivery_tour_prefers_nearby_table_over_lowest_number(self):
        from collections import Counter
        t=self.trial(iter([]));t.at_counter=False;t.seat=6
        t.allowed_seats=set(range(1,7));t.blocked_seats={}
        view={"seats":[{"seat":"1","dishes":["fin_dumpling"]},
                       {"seat":"5","dishes":["fin_dumpling"]}]}
        self.assertEqual(t.delivery_target(view,Counter(fin_dumpling=1)),5)

    def test_delivery_tour_finishes_one_guest_with_both_dishes(self):
        from collections import Counter
        t=self.trial(iter([]));t.at_counter=True
        t.allowed_seats=set(range(1,7));t.blocked_seats={}
        view={"seats":[{"seat":"1","dishes":["fin_dumpling","tonic_congee_greens"]},
                       {"seat":"4","dishes":["fin_dumpling","shroom_dumpling"]}]}
        self.assertEqual(t.delivery_target(view,Counter(fin_dumpling=1,shroom_dumpling=1)),4)

    def test_known_hud_route_does_not_scan_before_pressing_serve(self):
        t=self.trial(iter([self.report(False)]));t.last=self.report()
        t.at_counter=True;events=[]
        t.travel=lambda target:events.append("travel")
        t.execute=lambda *a:events.append("serve")
        t.serve(2)
        self.assertEqual(events,["travel","serve"])
        self.assertEqual(t.served,1)

    def test_candidate_agent_waits_for_exact_remaining_inventory(self):
        t=self.trial(iter([self.report(False)]));t.last=self.report();t.native_tray_ack=True
        pipelines=[];t.execute=lambda entry,nodes,name:pipelines.append(nodes)
        t.serve(2)
        nodes=pipelines[0]
        self.assertEqual(nodes["InnServiceServe"]["next"],["InnServedContents"])
        ack=nodes["InnServedContents"]
        self.assertEqual(ack["all_of"][0]["expected"],["End.*Session"])
        self.assertEqual(ack["all_of"][1]["custom_recognition"],"InnkeepingTrayContents")
        self.assertEqual(ack["all_of"][1]["custom_recognition_param"],{"expected":{}})
        self.assertEqual(t.served,1)

    def test_legacy_agent_has_no_unregistered_recognition_dependency(self):
        t=self.trial(iter([self.report(False)]));t.last=self.report()
        pipelines=[];t.execute=lambda entry,nodes,name:pipelines.append(nodes)
        t.serve(2)
        self.assertNotIn("InnServedContents",pipelines[0])
        self.assertEqual(pipelines[0]["InnServiceServe"]["next"],[])

    def test_unchanged_delivery_retries_then_stops_on_acknowledgement(self):
        t=self.trial(iter([self.report(),self.report(),self.report(False)]));calls=[]
        t.execute=lambda *a:calls.append(a)
        t.serve(2)
        self.assertEqual(len(calls),2)
        self.assertEqual(t.served,1)
        self.assertIsNone(t.pending_serve)

    def test_inventory_receipt_before_diner_animation_observes_without_serving_again(self):
        partial=self.report();partial["view"]["tray"]={};partial["view"]["tray_slots"]=[""]
        confirmed=self.report(False)
        # Actual delayed service reads use a served badge before dining starts.
        confirmed["view"]["seats"][0].update(phase="served",dishes=None,served=["fin_dumpling"])
        t=self.trial(iter([partial,confirmed]));t.last=self.report();calls=[]
        t.execute=lambda *a:calls.append(a)
        t.serve(2)
        self.assertEqual(len(calls),1)
        self.assertEqual(t.served,1)
        self.assertIsNone(t.pending_serve)

    def test_diner_receipt_before_tray_animation_observes_without_serving_again(self):
        partial=self.report(False);partial["view"]["tray"]={"fin_dumpling":1};partial["view"]["tray_slots"]=["fin_dumpling"]
        t=self.trial(iter([partial,self.report(False)]));t.last=self.report();calls=[]
        t.execute=lambda *a:calls.append(a)
        t.serve(2)
        self.assertEqual(len(calls),1)
        self.assertEqual(t.served,1)

    def test_partial_receipt_is_not_success_if_second_observation_still_disagrees(self):
        partial=self.report();partial["view"]["tray"]={};partial["view"]["tray_slots"]=[""]
        t=self.trial(iter([partial,partial]));t.last=self.report();calls=[]
        t.execute=lambda *a:calls.append(a)
        with self.assertRaisesRegex(RuntimeError,"not both acknowledged"):t.serve(2)
        self.assertEqual(len(calls),1)
        self.assertEqual(t.served,0)
        self.assertIsNotNone(t.pending_serve)

    def test_action_timeout_still_accepts_observed_delivery(self):
        t=self.trial(iter([self.report(),self.report(False)]))
        def timeout(*a):raise RuntimeError("task observation timed out")
        t.execute=timeout
        t.serve(2)
        self.assertEqual(t.served,1)
        self.assertIsNone(t.pending_serve)

    def test_failed_fresh_serve_guard_does_not_repeat_wrong_pose(self):
        t=self.trial(iter([self.report(),self.report()]));calls=[]
        def fail(*args):
            calls.append(args)
            raise RuntimeError("green Serve guard failed")
        t.execute=fail
        with self.assertRaisesRegex(RuntimeError,"not both acknowledged"):
            t.serve(2)
        self.assertEqual(len(calls),1)

    def test_unchanged_delivery_has_three_attempt_limit(self):
        t=self.trial(iter([self.report() for _ in range(4)]));calls=[]
        t.execute=lambda *a:calls.append(a)
        with self.assertRaisesRegex(RuntimeError,"not both acknowledged"):t.serve(2)
        self.assertEqual(len(calls),3)
        self.assertEqual(t.served,0)

    def test_lost_prompt_or_uncertain_tray_prevents_retry(self):
        for report in (self.report(prompt=""),self.report(unknown=True)):
            t=self.trial(iter([self.report(),report]));calls=[]
            t.execute=lambda *a:calls.append(a)
            with self.assertRaises(RuntimeError):t.serve(2)
            self.assertEqual(len(calls),1)



class AutomaticPreflightEntryTests(unittest.TestCase):
    def test_run_no_longer_requires_manual_camera_preparation_flag(self):
        from unittest.mock import patch
        import inn_service_trial
        events=[]
        class FakeTrial:
            def __init__(self,root,output,agent_binary=None):
                self.pause_confirmed=False
            def play(self,**kwargs):events.append(("play",kwargs))
            def persist(self):events.append(("persist",{}))
        with patch("inn_service_trial.ServiceTrial",FakeTrial), patch(
                "sys.argv",["inn_service_trial.py","--run","--output","unused-output"]):
            inn_service_trial.main()
        self.assertEqual(events,[("play",{"resume":False,"saved_state":None}),("persist",{})])


class WorldPreflightTests(unittest.TestCase):
    def test_preflight_graph_never_reaches_open_service_nodes(self):
        t=ServiceTrial.__new__(ServiceTrial)
        t.trace=lambda *a,**k:None
        events=[];captured=[]
        t.execute=lambda entry,nodes,*args:(events.append("preflight"),captured.append((entry,nodes)))
        report={"image":"world-frame","view":{"phase":"world"}}
        t.snapshot=lambda:report
        def align(r,world=False):
            self.assertTrue(world);events.append("align_world");return r
        t.normalize_camera=align
        saved={
            "InnCalWorld":{"action":"Click","next":["OpenService"]},
            "InnkeepingEntranceLedger":{"next":["OpenService"]},
            "InnkeepingTravelToManor":{"next":["InnkeepingEntranceLedger"]},
            "InnCalResultContinue":{"next":["OpenService"]},
            "InnCalFinish":{"next":["OpenService"]},
            "InnScoreDismiss":{"next":["OpenService"]},
            "OpenService":{"action":"Click","next":[]}}
        t.prepare_world_camera(saved)
        entry,nodes=captured[0];pending=[entry];seen=set()
        while pending:
            name=pending.pop()
            if name in seen:continue
            seen.add(name)
            if name=="[JumpBack]SceneAnyEnterWorld":continue
            pending.extend(nodes[name].get("next",[]))
        self.assertNotIn("OpenService",seen)
        self.assertIn("[JumpBack]SceneAnyEnterWorld",seen)
        self.assertEqual(nodes["InnPreflightWorld"]["all_of"],["InWorld"])
        self.assertEqual(nodes["InnPreflightLedger"]["action"],"DoNothing")
        self.assertEqual(nodes["InnkeepingEntranceLedger"]["next"],[])
        self.assertEqual(events,["preflight","align_world"])
        self.assertEqual(saved["InnCalWorld"]["action"],"Click")

class ServiceRangeRecoveryTests(unittest.TestCase):
    def test_unknown_table_never_authorizes_inward_movement(self):
        t=ServiceTrial.__new__(ServiceTrial)
        t.board=lambda *args:None
        t.execute=lambda *args:self.fail("Unidentified table must not authorize movement")
        self.assertFalse(t.correct_service_range({"image":"unused"},4))

    def test_rejected_plaque_guard_does_not_stop_other_deliveries(self):
        from pathlib import Path
        from unittest.mock import patch
        import numpy as np
        t=ServiceTrial.__new__(ServiceTrial);t.output=Path(".")
        t.board=lambda *args:[413,328,32,46];t.trace=lambda *a,**k:None
        def reject(*args):raise RuntimeError("fresh plaque no longer matches")
        t.execute=reject
        with patch("inn_service_trial.cv2.imread",return_value=np.zeros((720,1280,3),dtype=np.uint8)), patch(
                "inn_service_trial.cv2.imwrite",return_value=True):
            self.assertFalse(t.correct_service_range({"image":"unused"},4))
        self.assertEqual(getattr(t,"service_extra",0),0)

    def test_range_correction_requires_fresh_plaque_and_is_bounded(self):
        from pathlib import Path
        from unittest.mock import patch
        import numpy as np
        t=ServiceTrial.__new__(ServiceTrial)
        t.output=Path(".");t.board=lambda *args:[413,328,32,46]
        t.trace=lambda *a,**k:None
        calls=[];t.execute=lambda entry,nodes,name:calls.append(nodes[entry])
        with patch("inn_service_trial.cv2.imread",return_value=np.zeros((720,1280,3),dtype=np.uint8)), patch(
                "inn_service_trial.cv2.imwrite",return_value=True):
            self.assertTrue(t.correct_service_range({"image":"unused"},4))
            self.assertTrue(t.correct_service_range({"image":"unused"},4))
            self.assertFalse(t.correct_service_range({"image":"unused"},4))
        self.assertEqual(len(calls),2)
        self.assertEqual(t.service_extra,300)
        self.assertEqual(calls[0]["all_of"][1]["custom_recognition_param"]["number"],"4")
        self.assertEqual(calls[0]["action"]["param"]["custom_action_param"],{"direction":"left","duration":150})


class CameraCollisionRecoveryTests(unittest.TestCase):
    def test_world_preflight_rotates_without_active_round_guard(self):
        from unittest.mock import patch
        t=ServiceTrial.__new__(ServiceTrial)
        t.trace=lambda *a,**k:None
        t.dining_window=lambda report:(625,775)
        report={"image":"unused","view":{"phase":"world"}}
        t.snapshot=lambda:report
        nodes=[]
        t.execute=lambda entry,pipeline,name:nodes.append(pipeline[entry])
        headings=iter([{"usable":True,"angle_degrees":0},
                       {"usable":True,"angle_degrees":2}])
        with patch("inn_service_trial.cv2.imread",return_value=None), patch(
                "inn_service_trial.counter_heading",side_effect=lambda frame:next(headings)):
            self.assertIs(t.normalize_camera(report,world=True),report)
        self.assertEqual(len(nodes),1)
        self.assertEqual(nodes[0]["expected"],["Explore"])
        self.assertEqual(nodes[0]["action"]["param"]["end"],[1050,200])

    def test_unreadable_heading_cannot_reuse_pre_movement_alignment(self):
        from unittest.mock import patch
        t=ServiceTrial.__new__(ServiceTrial)
        t.trace=lambda *a,**k:None
        t.dining_window=lambda report:(500,650)
        report={"image":"unused","view":{"phase":"world"}}
        t.snapshot=lambda:report
        moves=[];t.move=lambda steps,name,**kw:moves.append(steps)
        t.execute=lambda *args:self.fail("Unconfirmed heading must not authorize rotation")
        headings=iter([{"usable":True,"angle_degrees":2},
                       {"usable":False},{"usable":False}])
        with patch("inn_service_trial.cv2.imread",return_value=None), patch(
                "inn_service_trial.counter_heading",side_effect=lambda frame:next(headings)):
            with self.assertRaisesRegex(RuntimeError,"heading remains unconfirmed"):
                t.normalize_camera(report,world=True)
        self.assertEqual(moves,[[(115,551,500)]])

    def test_lost_landmark_after_translation_undoes_step_without_yaw_search(self):
        from unittest.mock import patch
        t=ServiceTrial.__new__(ServiceTrial)
        t.trace=lambda *a,**k:None
        windows=iter([(500,650),None])
        t.dining_window=lambda report:next(windows)
        report={"image":"unused","view":{"phase":"active"}}
        t.snapshot=lambda:report
        moves=[]
        t.move=lambda steps,name,**kw:moves.append(steps)
        t.execute=lambda *a:self.fail("Lost landmark must not authorize camera rotation")
        with patch("inn_service_trial.cv2.imread",return_value=None), patch(
                "inn_service_trial.counter_heading",return_value={"usable":True,"angle_degrees":2}):
            with self.assertRaisesRegex(RuntimeError,"landmark lost after lateral"):
                t.normalize_camera(report)
        self.assertEqual(moves,[[(115,551,500)],[(275,551,500)]])


class PickupObservationTests(unittest.TestCase):
    def test_verified_usable_pickup_departs_without_full_stock_scan(self):
        from collections import Counter
        t=ServiceTrial.__new__(ServiceTrial);t.native_tray_ack=True
        t.allowed_seats={2};t.blocked_seats={}
        report={"view":{"phase":"active","seats":[{"seat":"2","dishes":["fin_dumpling"]}],
                        "tray":{"fin_dumpling":1}}}
        scans=[]
        t.snapshot=lambda **kw:(scans.append(kw),report)[1]
        self.assertIs(t.pickup_observation(),report)
        self.assertEqual(scans,[{"focus":"service"}])
        self.assertTrue(t.deliverable(report["view"],Counter(report["view"]["tray"])))

    def test_departed_guest_restores_counter_context(self):
        t=ServiceTrial.__new__(ServiceTrial);t.native_tray_ack=True
        t.allowed_seats={2};t.blocked_seats={}
        focused={"view":{"phase":"active","seats":[{"seat":"2","dishes":[]}],
                         "tray":{"fin_dumpling":1}}}
        complete={**focused,"counter_foods":[{"dish":"shroom_dumpling"}]}
        scans=[]
        def snapshot(**kw):
            scans.append(kw);return focused if kw else complete
        t.snapshot=snapshot
        self.assertIs(t.pickup_observation(),complete)
        self.assertEqual(scans,[{"focus":"service"},{}])

    def test_uncertain_tray_restores_full_observation(self):
        t=ServiceTrial.__new__(ServiceTrial);t.native_tray_ack=True
        focused={"view":{"phase":"active","unknown":["tray"]}}
        complete={"view":{"phase":"active","tray":{}}}
        reports=iter([focused,complete]);t.snapshot=lambda **kw:next(reports)
        self.assertIs(t.pickup_observation(),complete)


class PickupDeliveryPriorityTests(unittest.TestCase):
    def test_requested_dish_in_reach_beats_distant_complete_meal(self):
        from unittest.mock import patch
        from collections import Counter
        t=ServiceTrial.__new__(ServiceTrial)
        t.allowed_seats={1,3};t.blocked_seats={};t.pickup_deferred_until=0
        t.jobs=[];t.fast_routes=False;t.pending_pickup=None
        t.trace=lambda *a,**k:None
        seats=[{"seat":"1","dishes":["tonic_congee_greens","tonic_congee_greens"]},
               {"seat":"3","dishes":["shroom_dumpling"]}]
        before={"image":"unused","view":{"phase":"active","seats":seats,
            "tray":{},"tray_slots":["", ""],"prompt":"pickup",
            "prompt_dish":"tonic_congee_greens"},
            "counter_foods":[{"dish":"shroom_dumpling","box":[320,308,84,90]}]}
        after={**before,"view":{**before["view"],
            "tray":{"tonic_congee_greens":1},"tray_slots":["tonic_congee_greens",""]}}
        calls=[]
        t.execute=lambda entry,*args:calls.append(entry)
        t.snapshot=lambda:after
        with patch("inn_service_trial.pickup_destination",return_value=0):
            self.assertIs(t.collect(before),after)
        self.assertEqual(calls,["InnServicePickup"])
        self.assertTrue(t.deliverable(after["view"],Counter(after["view"]["tray"])))


if __name__=="__main__":unittest.main()
