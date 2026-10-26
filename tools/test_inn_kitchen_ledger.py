"""Scheduling and queue reconciliation regressions; no game inputs."""
import unittest
from collections import Counter
from pathlib import Path
from unittest.mock import patch
from inn_kitchen_ledger import KitchenLedger
from inn_service_trial import ServiceTrial


def kitchen(k=1, queue=None, active=None, seconds=0):
    return dict(phase="kitchen", kitchen=str(k), queue_verified=True,
        queue=queue or {}, active_dish=active, active_seconds=seconds,
        recipes=[dict(dish="fin_dumpling", seconds=18 if k==1 else 21, available=True,box=[596,191,150,55]),
                 dict(dish="shroom_dumpling", seconds=11 if k==1 else 13, available=True,box=[979,307,150,55])],
        seats=[dict(seat="1",dishes=["fin_dumpling","shroom_dumpling"])])


class LedgerTests(unittest.TestCase):
    def test_repeated_demand_can_queue_two_copies(self):
        ledger=KitchenLedger([]);view=kitchen();ledger.observe(1,view,10)
        plan=ledger.plan(1,view,Counter(fin_dumpling=4),Counter(),10)
        self.assertEqual([r["dish"] for r in plan],["fin_dumpling","fin_dumpling"])
        ledger.queued(1,plan,10)
        self.assertEqual(ledger.reserved(),{"fin_dumpling":2})

    def test_expiry_is_check_due_not_confirmed_counter_stock(self):
        jobs=[dict(kitchen=1,dish="fin_dumpling",queued=1,eta=20,picked=False)]
        ledger=KitchenLedger(jobs)
        self.assertEqual(ledger.state(21)["counts"]["awaiting_confirmation"],{"fin_dumpling":1})
        self.assertEqual(ledger.reserved(),{"fin_dumpling":1})
        self.assertNotIn("ready",jobs[0])

    def test_visible_countdown_retimes_waiting_copy_after_speed_change(self):
        jobs=[dict(kitchen=1,dish="fin_dumpling",queued=1,eta=30,picked=False),
              dict(kitchen=1,dish="shroom_dumpling",queued=2,eta=41,picked=False)]
        ledger=KitchenLedger(jobs)
        ledger.observe(1,kitchen(queue={"fin_dumpling":1,"shroom_dumpling":1},active="fin_dumpling",seconds=3),10)
        self.assertEqual([j["eta"] for j in jobs],[13,24])

    def test_new_copy_does_not_retime_older_same_dish_on_counter(self):
        jobs=[dict(kitchen=1,dish="fin_dumpling",queued=1,eta=19,picked=False),
              dict(kitchen=1,dish="fin_dumpling",queued=20,eta=38,picked=False)]
        ledger=KitchenLedger(jobs)
        ledger.observe(1,kitchen(queue={"fin_dumpling":1},active="fin_dumpling",seconds=8),25)
        self.assertTrue(jobs[0]["counter_check_due"])
        self.assertEqual(jobs[1]["eta"],33)

    def test_plan_uses_observed_times_and_reserves_across_both_kitchens(self):
        ledger=KitchenLedger([]);v=kitchen();ledger.observe(1,v,10)
        plan=ledger.plan(1,v,Counter(fin_dumpling=1,shroom_dumpling=1),Counter(),10)
        self.assertEqual([r["dish"] for r in plan],["shroom_dumpling","fin_dumpling"])
        ledger.queued(1,plan,10)
        ledger.observe(2,kitchen(2),11)
        self.assertEqual(ledger.plan(2,kitchen(2),Counter(fin_dumpling=1,shroom_dumpling=1),Counter(),11),[])
        self.assertEqual([j["eta"] for j in ledger.jobs],[21,39])

    def test_idle_kitchen_refills_without_twelve_second_cooldown(self):
        ledger=KitchenLedger([])
        self.assertTrue(ledger.refill_due(Counter(fin_dumpling=1),Counter(),1))
        ledger.refill_pending.add(1)
        self.assertTrue(ledger.refill_due(Counter(fin_dumpling=1),Counter(),2))
        self.assertFalse(ledger.refill_due(Counter(fin_dumpling=1),Counter(fin_dumpling=1),2))

    def test_unexpected_queue_adopted_once_without_inventing_fifo(self):
        ledger=KitchenLedger([]);v=kitchen(queue={"fin_dumpling":1,"shroom_dumpling":1},active="fin_dumpling",seconds=5)
        self.assertFalse(ledger.observe(1,v,10))
        self.assertEqual(len(ledger.jobs),2)
        ledger.observe(1,v,11)
        self.assertEqual(len(ledger.jobs),2)
        self.assertEqual(ledger.desyncs,1)
        self.assertTrue(ledger.kitchens["1"]["timing_uncertain"])

    def test_blaze_invalidates_estimates_preserving_reservations(self):
        ledger=KitchenLedger([dict(kitchen=1,dish="fin_dumpling",eta=30,picked=False)])
        ledger.invalidate_timers()
        self.assertEqual(ledger.reserved(),{"fin_dumpling":1})
        self.assertEqual(ledger.phase(ledger.jobs[0],10),"awaiting_confirmation")

    def test_persisted_cache_keeps_pending_refill_and_recipe_times(self):
        ledger=KitchenLedger([]);ledger.observe(1,kitchen(),10);ledger.refill_pending.add(2)
        copy=KitchenLedger([],ledger.state(12))
        self.assertEqual(copy.refill_pending,{2})
        self.assertEqual(copy.kitchens["1"]["recipes"][0]["seconds"],18)


class RefillTests(unittest.TestCase):
    def driver(self):
        t=ServiceTrial.__new__(ServiceTrial);t.jobs=[];t.ledger=KitchenLedger(t.jobs)
        t.allowed_seats=set(range(1,7));t.trace=lambda *a,**k:None;t.persist=lambda:None
        return t

    def test_pickup_finishes_held_order_before_unrelated_single_order(self):
        t=self.driver();t.blocked_seats={}
        view={"seats":[{"seat":"1","dishes":["fin_dumpling","shroom_dumpling"]},
                       {"seat":"2","dishes":["salted_cheese_crackers"]}]}
        held=Counter(fin_dumpling=1)
        self.assertGreater(t.pickup_priority(view,held,"shroom_dumpling"),
                           t.pickup_priority(view,held,"salted_cheese_crackers"))

    def test_closing_and_zero_timer_do_not_start_cooking(self):
        for phase,seconds in (("closing",25),("closed_serving",0),("active",0)):
            self.assertFalse(ServiceTrial.can_refill({"phase":phase,"remaining_seconds":seconds}))
        self.assertTrue(ServiceTrial.can_refill({"phase":"active","remaining_seconds":150}))

    def test_two_recipe_batch_has_fresh_exact_ack_after_each_click(self):
        t=self.driver();calls=[];views=iter([kitchen(1),kitchen(2)])
        t.snapshot=lambda **kw:{"view":next(views)}
        t.execute=lambda entry,nodes,name:calls.append((entry,nodes))
        t.fill(initial=True)
        batches=[n for e,n in calls if e=="InnRefillBefore"]
        self.assertEqual(len(batches),1)
        nodes=batches[0]
        self.assertEqual(nodes["InnRefillBefore"]["next"],["InnRefillRecipe0"])
        self.assertEqual(nodes["InnRefillRecipe0"]["next"],["InnRefillAck0"])
        self.assertEqual(nodes["InnRefillAck0"]["next"],["InnRefillRecipe1"])
        self.assertEqual(nodes["InnRefillAck1"]["next"],[])
        self.assertEqual(nodes["InnRefillAck1"]["custom_recognition_param"]["expected"],{"shroom_dumpling":1,"fin_dumpling":1})
        self.assertEqual(len(t.jobs),2)

    def test_nearly_ready_kitchen_remembers_unvisited_refill(self):
        t=self.driver();t.execute=lambda *a:None
        t.snapshot=lambda **kw:{"view":kitchen(queue={"fin_dumpling":1},active="fin_dumpling",seconds=2)}
        t.fill()
        self.assertEqual(t.ledger.refill_pending,{1,2})

    def test_partial_batch_failure_reconciles_without_replaying_inputs(self):
        t=self.driver();calls=[];views=iter([kitchen(),kitchen(queue={"shroom_dumpling":1},active="shroom_dumpling",seconds=9)])
        t.snapshot=lambda **kw:{"view":next(views)}
        def execute(entry,nodes,name):
            calls.append(entry)
            if entry=="InnRefillBefore":raise RuntimeError("ack failed")
        t.execute=execute
        with self.assertRaises(RuntimeError):t.fill()
        self.assertEqual(calls.count("InnRefillBefore"),1)
        self.assertEqual(t.ledger.reserved(),{"shroom_dumpling":1})
        self.assertEqual(t.ledger.desyncs,1)




class NativeCycleTests(unittest.TestCase):
    def driver(self):
        t=RefillTests().driver()
        t.last={"view":{"seats":[dict(seat="1",dishes=["fin_dumpling","fin_dumpling","shroom_dumpling","shroom_dumpling"])]}}
        t.scene_version=0;t.refill_attempted=False
        for k in (1,2):t.ledger.observe(k,kitchen(k),0)
        t.persist=lambda:None
        return t

    def receipts(self,kitchens=(1,2)):
        import datetime
        return [dict(view=kitchen(k,queue={"fin_dumpling":1,"shroom_dumpling":1},active="shroom_dumpling",seconds=7),
                     captured_at=datetime.datetime.now(datetime.timezone.utc).isoformat(),image="paired.png") for k in kitchens]

    def test_warm_refill_opens_once_switches_chefs_and_closes_once(self):
        t=self.driver();calls=[]
        t.execute=lambda entry,nodes,name,timeout=30:calls.append((entry,nodes))
        t.native_receipts=lambda:self.receipts()
        self.assertTrue(t.fast_refill(Counter()))
        self.assertEqual(len(calls),1)
        entry,nodes=calls[0]
        self.assertEqual(entry,"InnFastOpen")
        self.assertEqual(nodes[entry]["next"],["InnFastSelect1"])
        for k in (1,2):
            suffix=str(k)
            self.assertEqual(nodes["InnFastBefore"+suffix]["next"],["InnFastRecipe"+suffix+"_0"])
            self.assertEqual(nodes["InnFastRecipe"+suffix+"_0"]["next"],["InnFastAck"+suffix+"_0"])
            self.assertEqual(nodes["InnFastAck"+suffix+"_0"]["next"],["InnFastRecipe"+suffix+"_1"])
            self.assertEqual(nodes["InnFastRecipe"+suffix+"_1"]["next"],["InnFastAck"+suffix+"_1"])
            self.assertEqual(nodes["InnFastAck"+suffix+"_1"]["next"],["InnFastReceipt"+suffix])
            self.assertEqual(nodes["InnFastAck"+suffix+"_1"]["custom_recognition_param"]["expected"],{"fin_dumpling":1,"shroom_dumpling":1})
        self.assertEqual(nodes["InnFastReceipt1"]["next"],["InnFastSelect2"])
        self.assertEqual(nodes["InnFastReceipt2"]["next"],["InnFastClose"])
        self.assertEqual(nodes["InnFastClosed"]["next"],[])
        self.assertEqual(t.ledger.reserved(),{"fin_dumpling":2,"shroom_dumpling":2})

    def test_both_chefs_do_not_reserve_the_same_demand(self):
        t=self.driver();calls=[]
        t.last["view"]["seats"][0]["dishes"]=["fin_dumpling","shroom_dumpling"]
        t.execute=lambda entry,nodes,name,timeout=30:calls.append(nodes)
        t.native_receipts=lambda:self.receipts((1,))
        self.assertTrue(t.fast_refill(Counter()))
        self.assertNotIn("InnFastSelect2",calls[0])
        self.assertEqual(t.ledger.reserved(),{"fin_dumpling":1,"shroom_dumpling":1})

    def test_failed_prediction_never_replays_or_reserves_unseen_work(self):
        t=self.driver();calls=[];t.native_receipts=lambda:[]
        def execute(entry,nodes,name,timeout=30):
            calls.append(entry);raise RuntimeError("predicted queue differs")
        t.execute=execute
        with self.assertRaises(RuntimeError):t.fast_refill(Counter())
        self.assertEqual(calls,["InnFastOpen"])
        self.assertEqual(t.jobs,[])
        self.assertTrue(all(t.ledger.kitchens[str(k)]['timing_uncertain'] for k in (1,2)))

    def test_partial_visit_retains_acknowledged_chef_without_replay(self):
        t=self.driver();calls=[]
        t.native_receipts=lambda:self.receipts((1,))
        def execute(entry,nodes,name,timeout=30):
            calls.append(entry);raise RuntimeError("second chef acknowledgement failed")
        t.execute=execute
        with self.assertRaises(RuntimeError):t.fast_refill(Counter())
        self.assertEqual(calls,["InnFastOpen"])
        self.assertEqual(t.ledger.reserved(),{"fin_dumpling":1,"shroom_dumpling":1})
        self.assertNotIn(1,t.ledger.refill_pending)
        self.assertIn(2,t.ledger.refill_pending)
        self.assertTrue(t.ledger.kitchens['2']['timing_uncertain'])


class RecoveryTests(unittest.TestCase):
    def driver(self,view):
        t=RefillTests().driver();t.recoveries=0;t.pickup_deferred_until=0;t.blocked_seats={};t.delivery_failures=[]
        t.get=lambda path:{"instances":{"innreact":{"is_running":False}}}
        t.snapshot=lambda:{"view":view,"image":"native.png"}
        return t

    def test_queue_desync_adopts_actual_queue_and_closes_without_replaying(self):
        t=self.driver(kitchen(queue={"shroom_dumpling":1},active="shroom_dumpling",seconds=7));calls=[]
        t.execute=lambda entry,nodes,name:calls.append(entry)
        self.assertTrue(t.recover(RuntimeError("Native step failed: Native kitchen cycle 1")))
        self.assertEqual(calls,["InnRecoverClose"])
        self.assertEqual(t.ledger.reserved(),{"shroom_dumpling":1})

    def test_pickup_miss_defers_check_without_inventing_inventory(self):
        t=self.driver(dict(phase="active",tray={}))
        self.assertTrue(t.recover(RuntimeError("No useful pickup marker recognized")))
        self.assertGreater(t.pickup_deferred_until,0)
        self.assertEqual(t.jobs,[])

    def test_delayed_pickup_receipt_releases_its_stock_reservation(self):
        t=self.driver(dict(phase="active",tray={"fin_dumpling":1}))
        t.jobs.append(dict(dish="fin_dumpling",kitchen=1,picked=False))
        t.pending_pickup={"dish":"fin_dumpling","before":{}}
        self.assertTrue(t.recover(RuntimeError("Pickup did not add exactly the observed dish")))
        self.assertTrue(t.jobs[0]["picked"])
        self.assertIsNone(t.pending_pickup)
        self.assertEqual(t.ledger.reserved(),{})

    def test_delayed_diner_receipt_confirms_service_without_another_click(self):
        t=self.driver(dict(phase="active",tray={},seats=[dict(seat="3",phase="dining")]))
        t.served=0
        t.pending_serve={"target":3,"dish":"shroom_dumpling","expected_tray":{},"target_before":{"shroom_dumpling":1}}
        t.execute=lambda *a,**kw:self.fail("A completed transfer must not be clicked again")
        self.assertTrue(t.recover(RuntimeError("Selected tray dish and target-specific service were not both acknowledged")))
        self.assertEqual(t.served,1)
        self.assertIsNone(t.pending_serve)
        self.assertEqual(t.ledger.served,{"shroom_dumpling":1})

    def test_running_task_prevents_recovery_inputs(self):
        t=self.driver(dict(phase="active"))
        t.get=lambda path:{"instances":{"innreact":{"is_running":True}}}
        t.snapshot=lambda:self.fail("Read task would conflict with a running task")
        self.assertFalse(t.recover(RuntimeError("Observation deadline reached")))


class DeliverySkipTests(unittest.TestCase):
    def test_unserviceable_table_is_logged_and_does_not_force_counter_return(self):
        t=RecoveryTests().driver(dict(phase="active",tray={"shroom_dumpling":1}))
        t.seat=3;t.at_counter=False
        t.return_counter=lambda:self.fail("Should select the next diner before returning")
        self.assertTrue(t.recover(RuntimeError("Target service pose was not verified")))
        self.assertEqual(t.seat,3)
        self.assertEqual(t.delivery_failures[0]["seat"],3)
        self.assertEqual(t.delivery_failures[0]["tray"],{"shroom_dumpling":1})
        self.assertGreater(t.blocked_seats[3],0)


class DispatchPriorityTests(unittest.TestCase):
    def test_partial_table_order_leaves_counter_without_searching_other_dishes(self):
        t=RefillTests().driver();t.allowed_seats=set(range(1,7));t.blocked_seats={}
        view=dict(phase="active",tray={"tonic_congee_greens":1},
                  seats=[dict(seat="2",dishes=["tonic_congee_greens","salted_cheese_crackers"])])
        report={"view":view}
        t.snapshot=lambda:self.fail("Carried congee should dispatch without another OCR")
        self.assertIs(t.collect(report),report)

    def test_ready_second_dish_is_loaded_without_waiting_for_another_cook(self):
        from unittest.mock import patch
        t=RefillTests().driver();t.blocked_seats={};t.pickup_deferred_until=0
        seats=[dict(seat="2",dishes=["tonic_congee_greens","salted_cheese_crackers"])]
        view=dict(phase="active",tray={"tonic_congee_greens":1},tray_slots=["tonic_congee_greens","","","",""],seats=seats,prompt="pickup",prompt_dish="salted_cheese_crackers")
        report={"view":view,"image":"paired.png"}
        after={"view":dict(view,tray={"tonic_congee_greens":1,"salted_cheese_crackers":1},tray_slots=["tonic_congee_greens","salted_cheese_crackers","","",""],prompt="put_back"),"image":"paired.png"}
        calls=[];t.execute=lambda *a:calls.append(a[0]);t.snapshot=lambda:after
        with patch('inn_service_trial.pickup_destination',return_value=-1):
            self.assertIs(t.collect(report),after)
        self.assertEqual(calls,["InnServicePickup"])

    def test_second_dish_search_stops_after_one_observed_approach(self):
        t=RefillTests().driver();t.blocked_seats={};t.pickup_deferred_until=0;t.lat=0
        view=dict(phase="active",tray={"tonic_congee_greens":1},seats=[dict(seat="2",dishes=["tonic_congee_greens","salted_cheese_crackers"])],prompt="put_back")
        report={"view":view,"image":"paired.png","counter_foods":[dict(dish="salted_cheese_crackers",box=[900,300,90,90])]}
        calls=[];t.execute=lambda *a:calls.append(a[0]);t.snapshot=lambda:report
        self.assertIs(t.collect(report),report)
        self.assertEqual(calls,["InnCounterApproach"])

    def test_blocked_table_does_not_claim_deliverable_food(self):
        import time
        t=RefillTests().driver();t.allowed_seats={2};t.blocked_seats={2:time.monotonic()+30}
        self.assertFalse(t.deliverable(dict(seats=[dict(seat="2",dishes=["shroom_dumpling"])]),Counter(shroom_dumpling=1)))

if __name__=="__main__":unittest.main()
