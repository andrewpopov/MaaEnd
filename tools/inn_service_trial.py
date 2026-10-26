"""Bounded native-OCR inn service with camera setup before the service clock.

Pipeline owns every input and its fresh recognition. Native paired snapshots own
observations. This research driver records confirmed handoffs and recovery
outcomes; it does not promote production route counters.
"""
import argparse
import datetime as dt
from collections import Counter
import json
from pathlib import Path
import time
import subprocess
import sys
import base64
import cv2
from PIL import Image
from import_inn_route import sample_scene
from inn_route_status import inspect_endpoint
from inn_counter_heading import counter_heading

from inn_fixed_trial import Trial, button
from inn_native_reader import NativeInnReader, physical_boards
from inn_kitchen_ledger import KitchenLedger
from inn_waypoints import InnRoutes, pickup_step, counter_motion_observed, CounterSlots

NAMES={"hot_sour_soup":"Hot & Sour Soup","fin_dumpling":"Fin Dumpling",
       "tuber_fries":"Tuber Fries","salted_cheese_crackers":"Salted Cheese Crackers",
       "tonic_congee_greens":"Tonic Congee & Greens","shroom_dumpling":"Shroom Dumpling"}
ROIS={"hot_sour_soup":[450,170,370,110],"tuber_fries":[840,170,370,110],
      "fin_dumpling":[450,280,370,110],"salted_cheese_crackers":[840,280,370,110]}


def click(roi,text,next_node=None,point=None):
    if text=="End Session":roi=[90,42,120,27];text="End.*Session"
    node=button(roi,text,next_node,point)
    if text=="End.*Session":node["only_rec"]=True
    node.update(pre_delay=0,post_delay=0,rate_limit=0,next=[next_node] if next_node else [])
    return node


def walk(x,y,hold,next_node=None):
    direction={(195,471):"forward",(195,631):"backward",(115,551):"left",(275,551):"right"}[(x,y)]
    return {"recognition":"OCR","roi":[90,42,120,27],"expected":["End.*Session"],"only_rec":True,
        "action":{"type":"Custom","param":{"custom_action":"CharacterControllerJoystickAction",
        "custom_action_param":{"direction":direction,"duration":hold}}},
        "next":[next_node] if next_node else [],"timeout":2000}


def active_service_ready():
    # Kitchen fades leave recognizable text over an unusable transition frame.
    # Gate the service HUD and let its pixels settle before the paired scan.
    return {"recognition":"And","all_of":[
        {"recognition":"OCR","roi":[90,42,120,27],"expected":["End.*Session"],"only_rec":True},
        {"recognition":"OCR","roi":[55,330,210,50],"expected":["Current Income"],"only_rec":True}],
        "action":"DoNothing","next":[],"timeout":5000,"rate_limit":0,"pre_delay":0,"post_delay":0,
        "post_wait_freezes":{"target":[20,90,190,32],"time":150,"threshold":.995,"timeout":2000}}


def queue_ack(kitchen,count,next_node=None,expected=None):
    return {"recognition":"Custom","custom_recognition":"InnkeepingKitchenQueueCount",
        "custom_recognition_param":{"kitchen":str(kitchen),"count":count,**({"expected":dict(expected)} if expected is not None else {})},
        "action":"DoNothing","next":[next_node] if next_node else [],
        "timeout":3000,"pre_delay":0,"post_delay":0,"rate_limit":0}


def tray_ack(expected):
    return {"recognition":"And","all_of":[
        {"recognition":"OCR","roi":[90,42,120,27],"expected":["End.*Session"],"only_rec":True},
        {"recognition":"Custom","custom_recognition":"InnkeepingTrayContents",
         "custom_recognition_param":{"expected":dict(+Counter(expected))}}],
        "action":"DoNothing","next":[],"timeout":2500,"rate_limit":50,
        "pre_delay":0,"post_delay":0}



def service_receipt(view,target,dish,before,expected_tray):
    """Require matching inventory and target demand from the same fresh read."""
    if view.get("unknown") or view.get("phase") not in ("active","closing","closed_serving"):
        return False,False
    seat=next((s for s in (view.get("seats") or []) if s["seat"]==str(target)),None)
    target_ack=bool(seat and (dish in (seat.get("served") or []) or
        (seat["phase"]=="waiting" and Counter(seat.get("dishes") or [])[dish]<before[dish]) or
        (seat["phase"] in ("dining","bill_ready") and sum(before.values())==1)))
    return Counter(view.get("tray") or {})==Counter(expected_tray),target_ack


def pickup_destination(path):
    frame=cv2.imread(str(path))
    if frame is None:return -1
    counts=[]
    for slot in range(5):
        crop=frame[565:590,473+79*slot:493+79*slot]
        b,g,r=cv2.split(crop)
        counts.append(int(((r>200)&(g>180)&(b<120)).sum()))
    return counts.index(max(counts)) if max(counts)>10 else -1


class ServiceTrial(Trial):
    def __init__(self,root,output,agent_binary=None):
        super().__init__(root,output)
        self.native_tray_ack=agent_binary is not None
        self.agent_exec="agent/go-service"
        if agent_binary is not None:
            agent_binary=Path(agent_binary).resolve()
            if not agent_binary.is_file():raise ValueError("Missing native agent binary: "+str(agent_binary))
            self.agent_exec=str(agent_binary)
            self.base["agent_configs"]=[{"child_exec":self.agent_exec}]
        self.reader=None
        self.lat=0
        self.at_counter=True
        self.seat=None
        self.jobs=[]
        self.ledger=KitchenLedger(self.jobs)
        self.last=None
        self.served=0
        self.last_fill=0
        self.branch=0
        self.service_extra=0
        self.scene_version=0
        self.last_report_version=-1
        self.last_report_at=0
        self.origin=None
        self.pause_confirmed=False
        self.paused_at=0
        self.cycles=0
        self.max_cycles=0
        self.allowed_seats=set(range(1,7))
        self.origin_verified_version=-1
        self.return_motion_done=False
        self.real_run=False
        self.poll_interval=.1
        self.refill_attempted=False
        self.startup_kitchen_opened=False
        self.started_session_here=False
        self.pickup_deferred_until=0
        self.blocked_seats={}
        self.recoveries=0
        self.delivery_failures=[]
        self.pending_pickup=None
        self.pending_serve=None
        self.fast_routes=False
        self.settle_moves=False
        self.table_branch_ms=500
        self.counter_slots=CounterSlots()
        self.counter_position_known=True

    def capture(self):
        # Native paired snapshots are the evidence used by this driver.
        # Fetching the cached preview on every completion poll duplicates work.
        return self.last_image

    def snapshot(self,focus=None):
        started=time.monotonic()
        node={"recognition":"DirectHit","action":{"type":"Custom","param":{
            "custom_action":"InnkeepingAssistAction"}},"next":[],"pre_delay":0,"post_delay":0,"rate_limit":0}
        if focus:
            node["action"]["param"]["custom_action_param"]={"focus":focus}
        succeeded=self.run("InnServiceRead",{"InnServiceRead":node},"Native inn observation",60)
        task=self.last_task_id
        for file in sorted((self.root/"install/data/Innkeeping").glob("decision-"+str(task)+"-*.json"),key=lambda p:p.stat().st_mtime,reverse=True):
            report=json.loads(file.read_text("utf8"))
            if report.get("task_id")==task:
                self.last=report
                self.last_report_version=self.scene_version
                self.last_report_at=time.monotonic()
                if report["view"].get("phase") in ("active","closing","closed_serving"):
                    self.ledger.tray=dict(report["view"].get("tray") or {})
                self.trace("native_state",task=task,image=report["image"],view=report["view"],focus=focus,seconds=time.monotonic()-started,timings=report.get("timings_seconds"))
                return report
        raise RuntimeError("No paired report for native task" if succeeded else "Native observation failed")

    def execute(self,entry,nodes,name,timeout=30):
        try:
            if not self.run(entry,nodes,name,timeout):
                raise RuntimeError("Native step failed: "+name)
        finally:
            # A failed acknowledgement may follow a successful input.
            self.scene_version+=1

    def move(self,steps,name,world=False):
        nodes={}
        for i,(x,y,hold) in enumerate(steps):
            nodes["InnServiceWalk"+str(i)]=walk(x,y,hold,"InnServiceWalk"+str(i+1) if i+1<len(steps) else None)
            nodes["InnServiceWalk"+str(i)].update(pre_delay=0,post_delay=0,rate_limit=0)
            if world:
                nodes["InnServiceWalk"+str(i)].update(roi=[20,10,220,55],expected=["Explore"])
            if self.settle_moves:
                nodes["InnServiceWalk"+str(i)]["post_wait_freezes"]={"target":[910,170,120,40],"time":150,"threshold":.99,"timeout":1500}
            if i+1==len(steps):nodes["InnServiceWalk"+str(i)]["next"]=[]
        self.execute("InnServiceWalk0",nodes,name)

    def demand(self,view):
        return Counter(d for seat in view.get("seats") or [] if int(seat["seat"]) in self.allowed_seats for d in seat.get("dishes") or [] if d in NAMES)

    def native_receipts(self):
        folder=self.root/"install/data/Innkeeping"
        return [json.loads(p.read_text("utf8")) for p in folder.glob("decision-"+str(self.last_task_id)+"-*.json")]

    def fast_refill(self,carried):
        now=time.monotonic()
        need=self.demand((self.last or {}).get("view",{}))
        order=self.ledger.order(now)
        if any(self.ledger.kitchens[str(k)].get("timing_uncertain") for k in order):
            return False
        # Reserve the whole visit in a private planning ledger. Chef two must
        # see chef one's planned dishes without recording unacknowledged stock.
        planning=KitchenLedger([dict(job) for job in self.jobs],self.ledger.state(now))
        batches=[]
        for kitchen in order:
            queue=Counter(j["dish"] for j in self.ledger.cooking(kitchen,now))
            recipes=planning.plan(kitchen,{"queue":dict(queue)},need,carried,now)
            if recipes:
                batches.append((kitchen,queue,recipes))
                planning.queued(kitchen,recipes,now)
            else:
                self.ledger.refill_pending.discard(kitchen)
        if not batches:
            self.last_fill=now;self.refill_attempted=True
            self.persist()
            return True
        self.ledger.refill_pending.update(k for k,_,_ in batches)
        nodes={
            "InnFastOpen":click([840,600,210,80],"Add Order","InnFastSelect"+str(batches[0][0]),[930,625,1,1]),
            "InnFastClose":click([70,110,520,50],"Kitchen","InnFastClosed",[1235,114,1,1]),
            "InnFastClosed":active_service_ready()}
        for position,(kitchen,queue,recipes) in enumerate(batches):
            suffix=str(kitchen)
            first="InnFastRecipe"+suffix+"_0"
            nodes["InnFastSelect"+suffix]=click([180 if kitchen==1 else 450,120,110,30],"Kitchen "+suffix,"InnFastBefore"+suffix)
            nodes["InnFastBefore"+suffix]=queue_ack(kitchen,sum(queue.values()),first,queue)
            following="InnFastSelect"+str(batches[position+1][0]) if position+1<len(batches) else "InnFastClose"
            receipt="InnFastReceipt"+suffix
            nodes[receipt]={"recognition":"DirectHit","action":{"type":"Custom","param":{"custom_action":"InnkeepingAssistAction","custom_action_param":{"focus":"kitchen"}}},"next":[following],"rate_limit":0,"pre_delay":0,"post_delay":0}
            for index,recipe in enumerate(recipes):
                box=recipe["box"]
                roi=[450 if box[0]<840 else 840,170+116*round((box[1]-191)/116),370,110]
                recipe_node="InnFastRecipe"+suffix+"_"+str(index)
                ack="InnFastAck"+suffix+"_"+str(index)
                nodes[recipe_node]=click(roi,NAMES[recipe["dish"]],ack)
                queue[recipe["dish"]]+=1
                nodes[ack]=queue_ack(kitchen,sum(queue.values()),"InnFastRecipe"+suffix+"_"+str(index+1) if index+1<len(recipes) else receipt,queue)
        issued=time.monotonic()
        failure=None
        try:
            self.execute("InnFastOpen",nodes,"Native kitchen cycle both chefs",45)
        except RuntimeError as error:
            failure=error
        # Each chef has its own paired receipt. A partial visit is reconciled
        # once; neither missing receipts nor failed acknowledgements replay input.
        receipts=self.native_receipts()
        for kitchen,_,recipes in batches:
            reports=[r for r in receipts if r["view"].get("kitchen")==str(kitchen)]
            if len(reports)!=1:
                self.ledger.kitchens[str(kitchen)]["timing_uncertain"]=True
                failure=failure or RuntimeError("Native refill has no unique cooking receipt for kitchen "+str(kitchen))
                continue
            report=reports[0]
            self.ledger.queued(kitchen,recipes,issued)
            captured=dt.datetime.fromisoformat(report["captured_at"].replace("Z","+00:00")).timestamp()
            observed=time.monotonic()-max(0,time.time()-captured)
            self.ledger.observe(kitchen,report["view"],observed)
            self.ledger.refill_pending.discard(kitchen)
            self.trace("native_kitchen_cycle",kitchen=kitchen,seconds=time.monotonic()-issued,dishes=[r["dish"] for r in recipes],receipt=report["image"])
        self.persist()
        if failure:
            self.trace("native_refill_desync",error=str(failure),receipts=receipts)
            raise failure
        self.last_fill=time.monotonic();self.refill_attempted=True
        return True

    def fill(self,initial=False,carried=None):
        if not initial and all(self.ledger.kitchens.get(str(k),{}).get("recipes") for k in (1,2)):
            tray=Counter(carried if carried is not None else self.ledger.tray)
            if self.fast_refill(tray):return
        opened={"recognition":"And","all_of":[
            {"recognition":"OCR","roi":[190,125,100,25],"expected":["Kitchen.*1"],"only_rec":True},
            {"recognition":"OCR","roi":[460,125,105,25],"expected":["Kitchen.*2"],"only_rec":True}],
            "action":"DoNothing","next":[],"timeout":2000,"rate_limit":0,"pre_delay":0,"post_delay":0}
        if not getattr(self,"startup_kitchen_opened",False):
            self.execute("InnServiceOpen",{"InnServiceOpen":click([840,600,210,80],"Add Order","InnKitchenOpened",[930,625,1,1]),"InnKitchenOpened":opened},"Open and verify kitchen")
        self.startup_kitchen_opened=False
        order=self.ledger.order(time.monotonic())
        self.ledger.refill_pending.update(order)
        for kitchen in order:
            self.execute("InnServiceKitchen",{"InnServiceKitchen":click([180 if kitchen==1 else 450,120,110,30],"Kitchen "+str(kitchen))},"Select kitchen "+str(kitchen))
            warm=bool(self.ledger.kitchens.get(str(kitchen),{}).get("recipes"))
            report=self.snapshot(focus="kitchen" if warm else None);view=report["view"]
            if warm and (not view.get("queue_verified") or view.get("unknown")):
                report=self.snapshot();view=report["view"]
            now=time.monotonic()
            synced=self.ledger.observe(kitchen,view,now)
            self.trace("kitchen_checkin",kitchen=kitchen,synced=synced,state=self.ledger.state(now))
            if initial and sum((view.get("queue") or {}).values()):
                raise RuntimeError("Initial kitchen has untracked work; resume rejected")
            # Leave promptly for food due now, but remember the unfinished refill.
            if 0<view.get("active_seconds",0)<=4:
                break
            need=self.demand(view)
            tray=Counter(carried if carried is not None else view.get("tray") or {})
            recipes=self.ledger.plan(kitchen,view,need,tray,now,initial)
            queue=Counter(view.get("queue") or {})
            nodes={"InnRefillBefore":queue_ack(kitchen,sum(queue.values()),"InnRefillRecipe0" if recipes else None,queue)}
            for index,recipe in enumerate(recipes):
                box=recipe["box"]
                roi=[450 if box[0]<840 else 840,170+116*round((box[1]-191)/116),370,110]
                recipe_name="InnRefillRecipe"+str(index)
                ack_name="InnRefillAck"+str(index)
                nodes[recipe_name]=click(roi,NAMES[recipe["dish"]],ack_name)
                queue[recipe["dish"]]+=1
                nodes[ack_name]=queue_ack(kitchen,sum(queue.values()),"InnRefillRecipe"+str(index+1) if index+1<len(recipes) else None,queue)
            if recipes:
                issued=time.monotonic()
                try:
                    # One native task, with a fresh queue acknowledgement per click.
                    self.execute("InnRefillBefore",nodes,"Batch refill kitchen "+str(kitchen))
                except RuntimeError:
                    # A partial batch is never replayed. Rebuild from its actual queue.
                    observed=self.snapshot()
                    self.ledger.observe(kitchen,observed["view"],time.monotonic())
                    self.persist()
                    raise
                self.ledger.queued(kitchen,recipes,issued)
                self.trace("kitchen_batch_queued",kitchen=kitchen,dishes=[r["dish"] for r in recipes],seconds=time.monotonic()-issued)
            self.ledger.refill_pending.discard(kitchen)
        self.execute("InnServiceClose",{"InnServiceClose":click([70,110,520,50],"Kitchen","InnServiceClosed",[1235,114,1,1]),"InnServiceClosed":active_service_ready()},"Close kitchen and verify stable service HUD")
        self.last_fill=time.monotonic()
        self.refill_attempted=True
        self.persist()

    def lateral(self,duration):
        while duration:
            chunk=max(-1400,min(1400,duration))
            self.move([(275 if chunk>0 else 115,551,abs(chunk))],"Counter lateral pickup approach")
            self.lat+=chunk;duration-=chunk

    def deliverable(self,view,tray):
        return any(int(seat["seat"]) in self.allowed_seats
                   and self.blocked_seats.get(int(seat["seat"]),0)<=time.monotonic()
                   and any(tray[d]>0 for d in seat.get("dishes") or [])
                   for seat in view.get("seats") or [])

    @staticmethod
    def can_refill(view):
        return view.get("phase")=="active" and view.get("remaining_seconds",0)>30

    def pickup_priority(self,view,tray,dish):
        after=tray+Counter({dish:1})
        ranks=[]
        for seat in view.get("seats") or []:
            number=int(seat["seat"])
            if number not in self.allowed_seats or self.blocked_seats.get(number,0)>time.monotonic():continue
            need=Counter(seat.get("dishes") or [])
            if need[dish]<=tray[dish]:continue
            usable=sum(min(n,after[d]) for d,n in need.items())
            missing=sum(max(0,n-after[d]) for d,n in need.items())
            ranks.append((missing==0,usable,-missing))
        return max(ranks,default=(False,0,-99))

    def delivery_target(self,view,tray):
        routes=InnRoutes(branch_ms=getattr(self,"table_branch_ms",500))
        source="counter" if self.at_counter else "table"+str(self.seat)
        seats=[s for s in view.get("seats") or [] if int(s["seat"]) in self.allowed_seats
               and self.blocked_seats.get(int(s["seat"]),0)<=time.monotonic()]
        def cost(a,b):return sum(leg.milliseconds for leg in routes.between(a,b))
        choices=[]
        for first in seats:
            need=Counter(first.get("dishes") or [])
            handed=need & tray
            if not handed:continue
            first_node="table"+first["seat"]
            remaining=tray-handed
            completed=int(handed==need)
            best=(completed,sum(handed.values()),-cost(source,first_node)-cost(first_node,"counter"),-cost(source,first_node))
            for second in seats:
                if second is first:continue
                second_need=Counter(second.get("dishes") or [])
                second_handed=second_need & remaining
                if not second_handed:continue
                second_node="table"+second["seat"]
                score=(completed+int(second_handed==second_need),sum(handed.values())+sum(second_handed.values()),
                       -cost(source,first_node)-cost(first_node,second_node)-cost(second_node,"counter"),-cost(source,first_node))
                best=max(best,score)
            choices.append((best,-int(first["seat"])))
        return -max(choices)[1] if choices else None

    def nearby_counter_stock(self,report,tray,need):
        view=report["view"]
        dish=view.get("prompt_dish")
        if view.get("prompt")=="pickup" and dish in NAMES and need[dish]>tray[dish]:return True
        return any(m["box"][1]>=260 and m["box"][3]>=60
                   and abs(m["box"][0]+m["box"][2]/2-670)<=350
                   and need[m["dish"]]>tray[m["dish"]]
                   for m in report.get("counter_foods",[]) or [])

    def pickup_observation(self):
        if not getattr(self,"native_tray_ack",False):return self.snapshot()
        report=self.snapshot(focus="service")
        view=report["view"]
        # Fresh guest/tray/interaction evidence is sufficient to depart with food.
        # If demand vanished or recognition is uncertain, recover full counter
        # context before choosing another pickup or returning to the kitchens.
        if view["phase"] in ("active","closing","closed_serving") and (
                view.get("unknown") or not self.deliverable(view,Counter(view.get("tray") or {}))):
            return self.snapshot()
        return report

    def collect(self,report=None):
        report=report or self.snapshot();view=report["view"]
        need=self.demand(view)
        held=Counter(view.get("tray") or {})
        if sum(held.values())>=2:return report
        if self.deliverable(view,held) and not self.nearby_counter_stock(report,held,need):return report
        optional_approaches=0
        if time.monotonic()<self.pickup_deferred_until:return report
        visible_ready=view.get("prompt")=="pickup" and view.get("prompt_dish") in NAMES and need[view["prompt_dish"]]>Counter(view.get("tray") or {})[view["prompt_dish"]]
        if not visible_ready and not self.nearby_counter_stock(report,held,need) and not any(need[j["dish"]] for j in self.jobs if not j["picked"]):return report
        soonest=min((j["eta"] for j in self.jobs if not j["picked"] and need[j["dish"]]),default=time.monotonic())
        if not visible_ready and soonest>time.monotonic():
            time.sleep(min(2,soonest-time.monotonic()))
            return report
        for _ in range(8):
            if view["phase"] not in ("active","closing","closed_serving"):return report
            tray=Counter(view.get("tray") or {})
            if sum(tray.values())>=2:return report
            dish=view.get("prompt_dish")
            visible_pickup=view.get("prompt")=="pickup" and dish in NAMES and need[dish]>tray[dish]
            alternatives=[m["dish"] for m in report.get("counter_foods",[]) or []
                          if m["box"][1]>=260 and m["box"][3]>=60 and need[m["dish"]]>tray[m["dish"]]
                          and abs(m["box"][0]+m["box"][2]/2-670)<=350]
            # A requested dish already in interaction range costs no movement.
            # Do not abandon it for a distant icon with a better theoretical rank.
            if self.deliverable(view,tray) and not visible_pickup and (optional_approaches or not self.nearby_counter_stock(report,tray,need)):return report
            if visible_pickup:
                before=sum(tray.values())
                empty=next((i for i,d in enumerate(view["tray_slots"]) if not d),None)
                if empty is None:return report
                before_destination=pickup_destination(report["image"])
                pickup=click([720,390,480,80],("Salted Cheese" if dish=="salted_cheese_crackers" else "Tonic Congee" if dish=="tonic_congee_greens" else NAMES[dish]))
                self.pending_pickup={"dish":dish,"before":dict(tray)}
                pickup_nodes={"InnServicePickup":pickup}
                if getattr(self,"native_tray_ack",False):
                    pickup["next"]=["InnPickupContents"]
                    pickup_nodes["InnPickupContents"]=tray_ack(tray+Counter({dish:1}))
                else:
                    pickup["post_wait_freezes"]={"target":[445,585,160,80],"time":150,"threshold":.995,"timeout":1500}
                try:
                    self.execute("InnServicePickup",pickup_nodes,"Pick up "+dish)
                except RuntimeError as error:
                    # A timed-out acknowledgement can follow a successful input.
                    # Preserve the existing observed-destination recovery below.
                    self.trace("pickup_ack_checkin",dish=dish,error=str(error))
                report=self.pickup_observation();view=report["view"]
                after_destination=pickup_destination(report["image"])
                if Counter(view.get("tray") or {})==tray and after_destination==empty and before_destination!=empty and view.get("prompt")=="pickup" and view.get("prompt_dish")==dish:
                    # The first interaction visibly changed the pickup destination.
                    # A second input is authorized only by that specific UI transition.
                    again={**pickup,"recognition":"And","all_of":[
                        {"recognition":"OCR","roi":[720,390,480,80],"expected":pickup["expected"]},
                        {"recognition":"ColorMatch","roi":[473+79*empty,565,20,25],"method":40,"lower":[20,110,180],"upper":[40,255,255],"count":10,"connected":False}],"box_index":0}
                    again.pop("roi",None);again.pop("expected",None)
                    self.trace("pickup_destination_changed",slot=empty,dish=dish,image=report["image"])
                    again_nodes={"InnServicePickupAck":again}
                    if "InnPickupContents" in pickup_nodes:
                        again_nodes["InnPickupContents"]=pickup_nodes["InnPickupContents"]
                    self.execute("InnServicePickupAck",again_nodes,"Collect after acknowledged pickup-destination change")
                    report=self.pickup_observation();view=report["view"]
                if Counter(view.get("tray") or {})!=tray+Counter({dish:1}):
                    raise RuntimeError("Pickup did not add exactly the observed dish")
                job=next((j for j in self.jobs if not j["picked"] and j["dish"]==dish),None)
                if job:job["picked"]=True
                self.pending_pickup=None
                if getattr(self,"fast_routes",False) and self.counter_position_known:
                    pose=self.counter_slots.observe_pickup(self.lat,dish)
                    self.trace("counter_slot_learned",pose=pose,image=report["image"])
                if self.deliverable(view,Counter(view.get("tray") or {})):
                    return report
                next_dish=view.get("prompt_dish")
                if self.nearby_counter_stock(report,Counter(view.get("tray") or {}),need):continue
                return report
            # Native observation already owns this image and icon library.
            # Compatibility fallback is only for an older installed service.
            if "counter_foods" in report:
                foods=report["counter_foods"] or []
            else:
                tokens=self.output/"counter-feedback.tokens.json"
                tokens.write_text(json.dumps(report["tokens"]),encoding="utf8")
                result=subprocess.run([str(self.root/".cache/innframe.exe"),"--image",report["image"],"--tokens",str(tokens),"--data",str(self.root/"install/data/Innkeeping")],capture_output=True,text=True,check=True)
                foods=json.loads(result.stdout)["counter_foods"] or []
            stock=[m for m in foods if m["box"][1]>=260 and m["box"][3]>=60]
            markers=[m for m in stock if need[m["dish"]]>tray[m["dish"]]]
            if tray:
                markers=[m for m in markers if abs(m["box"][0]+m["box"][2]/2-670)<=350]
            if not markers and stock and not tray:
                # Ready stock beyond the screen can be occluded by the right
                # HUD. Follow an observed counter landmark to reveal it; never
                # pick an unrequested dish merely to clear that landmark.
                markers=[m for m in stock if m["box"][0]+m["box"][2]/2>760 or m["box"][0]+m["box"][2]/2<580]
                if markers:self.trace("counter_stock_reveal",image=report["image"],markers=markers)
            if markers:
                marker=max(markers,key=lambda m:(self.pickup_priority(view,tray,m["dish"]),m["box"][3]));x,y,w,h=marker["box"]
                center=x+w/2
                self.trace("counter_marker",dish=marker["dish"],box=marker["box"],lat=self.lat)
                correction=self.counter_slots.direct_step(marker["box"],self.lat) if getattr(self,"fast_routes",False) else None
                if correction:self.trace("counter_slot_direct_route",dish=marker["dish"],offset=self.lat+correction[1])
                else:correction=pickup_step(marker["box"],self.lat)
                if correction is None:
                    self.trace("pickup_marker_deferred",dish=marker["dish"],box=marker["box"],lat=self.lat)
                    self.pickup_deferred_until=time.monotonic()+10
                    return report
                pulse,delta,side=correction
                node=walk(*pulse)
                node.update(recognition="And",all_of=[
                    {"recognition":"OCR","roi":[90,42,120,27],"expected":["End.*Session"],"only_rec":True},
                    {"recognition":"Custom","custom_recognition":"InnkeepingReadyFoodOffset",
                     "custom_recognition_param":{"dish":marker["dish"],"side":side}}])
                node.pop("roi",None);node.pop("expected",None)
                self.execute("InnCounterApproach",{"InnCounterApproach":node},"Approach freshly verified counter food")
                before_foods=report.get("counter_foods") or []
                before_prompt=report["view"].get("prompt_dish")
                report=self.snapshot();view=report["view"]
                arrived=(delta and view.get("prompt")=="pickup" and view.get("prompt_dish")==marker["dish"]
                         and before_prompt!=marker["dish"])
                if arrived or counter_motion_observed(before_foods,report.get("counter_foods") or [],delta):
                    self.lat+=delta
                else:
                    self.counter_position_known=False
                    self.trace("counter_motion_unconfirmed",intended=delta,lat=self.lat,image=report["image"])
                if tray:optional_approaches+=1
            elif any(min(n,tray[d]) for d,n in need.items()):return report
            else:raise RuntimeError("No useful pickup marker recognized; blind lateral search is disabled")
        raise RuntimeError("Useful dish pickup was not verified within eight corrections")

    def verify_origin(self,world=False):
        if self.origin is None:raise RuntimeError("No observed counter reference")
        nodes={}
        phase_guard={"recognition":"OCR","roi":[20,10,220,55] if world else [90,42,120,27],"expected":["Explore"] if world else ["End.*Session"],"only_rec":True}
        # Wait on the actual reference wall, not the unrelated courtyard band.
        stable_box=list(self.origin["start"][0]["box"])
        def recognition(mode):
            return {"recognition":"Custom","custom_recognition":"InnkeepingCounterOrigin",
                    "custom_recognition_param":{"reference":self.origin["start"],"mode":mode}}
        def choices(i):
            return ["InnOriginAligned"+str(i)]+["InnOrigin"+mode+str(i) for mode in modes] if i<8 else ["InnOriginAligned8"]
        modes=["backward","right_large","left_large","right_coarse","left_coarse","right_fine","left_fine"]
        nodes["InnOriginBegin"]=dict(phase_guard,action="DoNothing",next=choices(0),timeout=1500)
        for i in range(9):
            nodes["InnOriginAligned"+str(i)]={**recognition("aligned"),"action":"DoNothing","next":["InnOriginConfirmed"+str(i)]+(["InnOrigin"+mode+str(i) for mode in modes] if i<8 else []),"post_wait_freezes":{"target":stable_box,"time":350,"threshold":.995,"timeout":1500},"timeout":1500,"rate_limit":0,"pre_delay":0,"post_delay":0}
            nodes["InnOriginConfirmed"+str(i)]={**recognition("aligned"),"action":"DoNothing","next":[],"timeout":1500,"rate_limit":0,"pre_delay":0,"post_delay":0}
            if i==0:nodes["InnOriginAligned0"]["custom_recognition_param"]["capture_failure"]=True
            if i==8:continue
            for mode in modes:
                duration=320 if mode.endswith("large") else 240 if mode.endswith("coarse") else 160
                node=walk(195,631,300) if mode=="backward" else walk(275 if mode.startswith("right") else 115,551,duration)
                node.update(recognition="And",all_of=[recognition(mode),phase_guard],next=choices(i+1),rate_limit=0,pre_delay=0,post_delay=0)
                node.pop("roi",None);node.pop("expected",None)
                node["post_wait_freezes"]={"target":stable_box,"time":120,"threshold":.995,"timeout":1000}
                nodes["InnOrigin"+mode+str(i)]=node
        self.execute("InnOriginBegin",nodes,"Native visual counter-origin alignment",25)
        # The native task terminates only after strict alignment, stabilization,
        # and a second fresh strict match. It is the arrival authority. A second
        # full OCR round trip adds latency without a stronger movement gate.
        self.trace("counter_origin_native_verified",task=self.last_task_id,
                   reference=self.origin["id"],strict_matches=2)
        self.origin_verified_version=self.scene_version
        self.counter_position_known=True

    def center_counter(self):
        if self.lat:
            self.lateral(-self.lat)
        if getattr(self,"fast_routes",False):
            # This experiment uses the fixed route estimate and the counter's
            # collision boundary. Fresh Serve/Pickup still authorizes handoffs.
            self.move([(195,631,300)],"Set counter depth before direct delivery route")
            self.trace("counter_pose_estimated",lat=self.lat,arrival_verified=False)
        elif self.origin_verified_version!=self.scene_version:
            self.verify_origin()

    def travel(self,target):
        if not self.at_counter and getattr(self,"service_extra",0):
            self.move([(115 if self.seat%2 else 275,551,self.service_extra)],
                      "Leave measured service-range correction")
            self.service_extra=0
        if self.at_counter:self.center_counter()
        source="counter" if self.at_counter else "table"+str(self.seat)
        routes=InnRoutes(branch_ms=getattr(self,"table_branch_ms",500))
        legs=routes.goToTable(target,source)
        if legs:
            self.trace("waypoint_route",source=source,target="table"+str(target),
                       via=[leg.target for leg in legs],arrival_verified=False)
            self.move(routes.pulses(legs),"Middle aisle to table "+str(target))
        self.branch=500
        self.at_counter=False;self.seat=target

    def board(self,report,target):
        # The paired native read already contains confidently detected plaques.
        # Do not load a second OCR runtime just to reread the same screenshot.
        box=physical_boards(report.get("tokens") or []).get(str(target))
        if box:
            # Table 3's measured service pose puts its plaque at 569,266;
            # the common front-row range incorrectly rejected that valid pose.
            if target==3 and 540<=box[0]<=640 and 250<=box[1]<=340 and box[3]>=28:
                return box
            if box[3]>=28 and 310<=box[1]<=430 and ((target%2 and 600<=box[0]<=1000) or (not target%2 and 300<=box[0]<=700)):
                return box
        return None

    def serve(self,target):
        # The top HUD and tray from the last pickup/check-in select the intent.
        # Run the calibrated route immediately; the endpoint's fresh green Serve
        # recognition is the interaction authority, not another scenery scan.
        report=getattr(self,"last",None) or self.snapshot(focus="service")
        view=report["view"]
        if self.at_counter or self.seat!=target:self.travel(target)
        box=None
        for _ in range(3):
            seat=next(s for s in view["seats"] if s["seat"]==str(target))
            needed=seat.get("dishes") or []
            slot=next((i for i,d in enumerate(view["tray_slots"]) if d and d in needed),None)
            if slot is None:break
            guard=None
            if box:
                frame=cv2.imread(report["image"]);x,y,w,h=box
                guard=self.output/("serve-table"+str(target)+"-"+str(time.time_ns())+".png")
                cv2.imwrite(str(guard),frame[y:y+h,x:x+w])
            select=click([15,15,205,65],"End Session","InnServiceServe",[483+79*slot,625,1,1])
            serve={"recognition":"And","all_of":[
                {"recognition":"Custom","custom_recognition":"InnkeepingPhysicalBoard","custom_recognition_param":{"number":str(target),"box":box,"template":str(guard)}},
                {"recognition":"OCR","roi":[720,390,480,80],"expected":["Serve"]},
                {"recognition":"ColorMatch","roi":[807,410,70,30],"method":40,"lower":[25,70,70],"upper":[85,255,255],"count":20,"connected":False}],
                "box_index":1,"action":{"type":"LongPress","param":{"target":True,"duration":120,"contact":2}},"next":[],"timeout":2500,"rate_limit":0}
            if box is None:
                # Green is the game's current-diner eligibility confirmation.
                # A new OCR+color check happens after selecting the held slot.
                serve["all_of"][0]={"recognition":"OCR","roi":[90,42,120,27],"expected":["End.*Session"],"only_rec":True}
            dish=view["tray_slots"][slot]
            expected_tray=Counter(view["tray"]);expected_tray[dish]-=1
            expected_tray=+expected_tray
            target_before=Counter(needed)
            serve_nodes={"InnServiceSlot":select,"InnServiceServe":serve}
            if getattr(self,"native_tray_ack",False):
                serve["next"]=["InnServedContents"]
                serve_nodes["InnServedContents"]=tray_ack(expected_tray)
            self.pending_serve={"target":target,"dish":dish,"expected_tray":dict(expected_tray),"target_before":dict(target_before)}
            original_tray=Counter(view["tray"])
            for attempt in range(3):
                action_succeeded=False
                try:
                    self.execute("InnServiceSlot",serve_nodes,"Serve verified table "+str(target))
                    action_succeeded=True
                except RuntimeError as exc:
                    self.trace("delivery_action_checkin",seat=target,error=str(exc))
                report=self.snapshot(focus="service");view=report["view"]
                if view["phase"]=="ended":return report
                current_seat=next((s for s in view.get("seats",[]) if s["seat"]==str(target)),None)
                unchanged=(action_succeeded and not view.get("unknown") and Counter(view.get("tray") or {})==original_tray
                    and current_seat and current_seat["phase"]=="waiting"
                    and Counter(current_seat.get("dishes") or [])==target_before
                    and view.get("prompt")=="serve"
                    and slot<len(view.get("tray_slots") or []) and view["tray_slots"][slot]==dish)
                if unchanged and attempt<2:
                    self.trace("delivery_retry",seat=target,dish=dish,attempt=attempt+1,image=report["image"])
                    continue
                break
            receipt=service_receipt(view,target,dish,target_before,expected_tray)
            if not all(receipt) and any(receipt):
                # Inventory and the diner HUD animate independently. A partial
                # receipt authorizes one observation, never another Serve click.
                self.trace("delivery_receipt_pending",seat=target,dish=dish,image=report["image"])
                report=self.snapshot(focus="service");view=report["view"]
                if view["phase"]=="ended":return report
                receipt=service_receipt(view,target,dish,target_before,expected_tray)
            if not all(receipt):
                raise RuntimeError("Selected tray dish and target-specific service were not both acknowledged")
            self.pending_serve=None
            self.served+=1
            self.ledger.served[dish]=self.ledger.served.get(dish,0)+1;self.trace("native_served",seat=target,count=self.served,image=report["image"])
            box=self.board(report,target) or box
        return report

    def recover_counter_contact(self,report):
        # Inverse branch motion can finish short of the bar. The wide wooden
        # counter face distinguishes contact from the open middle aisle.
        for attempt in range(4):
            if report["view"]["phase"] not in ("active","closing","closed_serving"):
                return False
            frame=cv2.imread(report["image"])
            b,g,r=cv2.split(frame[530:565,250:1000])
            wooden=(b<100)&(r.astype("int16")>g.astype("int16")+5)&(g.astype("int16")>b.astype("int16")+5)
            coverage=(wooden.mean(axis=1)>.8).sum()
            self.trace("counter_contact_check",image=report["image"],wooden_rows=int(coverage),attempt=attempt)
            if coverage>=20:return True
            if attempt==3:return False
            self.move([(195,631,700)],"Recover short counter return through middle aisle")
            report=self.snapshot()
        return False

    def return_counter(self):
        routes=InnRoutes(branch_ms=getattr(self,"table_branch_ms",500))
        steps=routes.pulses(routes.recenter("table"+str(self.seat)))
        if getattr(self,"service_extra",0):
            steps.insert(0,(115 if self.seat%2 else 275,551,self.service_extra))
        if not self.return_motion_done:
            self.move(steps,"Return via middle aisle to counter")
            self.return_motion_done=True
            self.trace("return_motion_completed",seat=self.seat)
            self.persist()
        if getattr(self,"fast_routes",False):
            self.move([(195,631,700)],"Finish direct counter return against the bar")
            self.trace("counter_pose_estimated",arrival_verified=False)
        else:self.verify_origin()
        self.at_counter=True;self.seat=None;self.lat=0
        self.service_extra=0
        self.return_motion_done=False
        self.cycles+=1

    def prepare_world_camera(self,saved):
        # Reach the ledger and align before spending patrons or starting the clock.
        # Each menu close uses the actual screen header; no generic escape chain.
        nodes=dict(saved)
        ledger=dict(saved["InnCalWorld"],action="DoNothing",next=[])
        nodes["InnPreflightLedger"]=ledger
        nodes["InnPreflightWorld"]={"recognition":"And","all_of":["InWorld"],
            "action":"DoNothing","next":["InnkeepingTravelToManor"],
            "pre_delay":0,"post_delay":0,"rate_limit":0}
        nodes["InnkeepingEntranceLedger"]=dict(saved["InnkeepingEntranceLedger"],next=[])
        nodes["InnCalResultContinue"]=dict(saved["InnCalResultContinue"],next=["InnCalFinish"])
        nodes["InnCalFinish"]=dict(saved["InnCalFinish"],next=["InnPreflightDispatch"],timeout=30000)
        nodes["InnScoreDismiss"]=dict(saved["InnScoreDismiss"],next=["InnPreflightDispatch"])
        for name,roi,text in (
                ("RegionalDevelopment",[35,15,340,50],"Regional Development"),
                ("Theme",[30,15,260,50],"THEME.*SETTINGS"),
                ("Settings",[30,15,260,50],"IN.*SETTINGS"),
                ("Menu",[72,123,155,28],"[Mm]enu.*REQ")):
            nodes["InnPreflightClose"+name]=click(roi,text,"InnPreflightDispatch",[1220,32,1,1])
        nodes["InnPreflightDispatch"]={"recognition":"DirectHit","action":"DoNothing",
            "next":["InnPreflightLedger","InnScoreDismiss","InnPreflightCloseRegionalDevelopment","InnPreflightCloseTheme",
                    "InnPreflightCloseSettings","InnPreflightCloseMenu","InnCalResultContinue",
                    "InnCalFinish","InnPreflightWorld","[JumpBack]SceneAnyEnterWorld"],
            "timeout":30000,"pre_delay":0,"post_delay":0,"rate_limit":0}
        self.execute("InnPreflightDispatch",nodes,"Prepare inn camera before opening service",180)
        aligned=self.normalize_camera(self.snapshot(),world=True)
        self.trace("world_camera_prepared",image=aligned["image"])

    def start(self):
        saved=json.loads((self.root/".cache/inn-fixed-table1-proven.json").read_text("utf8"))["all_prototype_nodes"]
        saved["InnCalTheme"]={"recognition":"And","all_of":[{"recognition":"OCR","roi":[30,15,260,50],"expected":["THEME.*SETTINGS"],"only_rec":False},{"recognition":"OCR","roi":[150,470,400,110],"expected":["Cereal.*Flour"]}],"box_index":1,"action":{"type":"LongPress","param":{"target":True,"duration":120,"contact":2}},"next":["InnCalThemeChosen"],"pre_delay":0,"post_delay":0,"rate_limit":0}
        saved["InnCalThemeChosen"]={"recognition":"OCR","roi":[590,90,400,70],"expected":["Cereal.*Flour"],"action":"DoNothing","next":["InnCalThemeMode"],"timeout":3000,"rate_limit":0}
        saved["InnCalThemeMode"]=click([1038,631,180,34] if self.real_run else [823,629,100,34],"Open the Inn" if self.real_run else "Trial Run","InnCalSettings")
        saved["InnCalThemeMode"]["only_rec"]=True
        for key in ("InnCalActive","InnCalSettled"):
            saved[key].update(recognition="OCR",roi=[55,330,210,50],expected=["Current Income"],only_rec=False)
        saved["InnCalSettled"].pop("pre_wait_freezes",None)
        saved["InnCalSettled"]["next"]=["InnStartupKitchenOpen"]
        saved["InnStartupKitchenOpen"]=click([840,600,210,80],"Add Order","InnStartupKitchenReady",[930,625,1,1])
        saved["InnStartupKitchenReady"]={"recognition":"And","all_of":[
            {"recognition":"OCR","roi":[190,125,100,25],"expected":["Kitchen.*1"],"only_rec":True},
            {"recognition":"OCR","roi":[460,125,105,25],"expected":["Kitchen.*2"],"only_rec":True}],
            "action":"DoNothing","next":[],"timeout":5000,"pre_delay":0,"post_delay":0,"rate_limit":0}
        settings_header={"recognition":"OCR","roi":[30,15,260,50],"expected":["IN.*SETTINGS"],"only_rec":False}
        settings_button={"recognition":"OCR","roi":[1070,630,130,32],"expected":["Set.*Menu"],"only_rec":True}
        saved["InnCalSettings"]={"recognition":"And","all_of":[settings_header,settings_button],"box_index":1,"action":{"type":"LongPress","param":{"target":True,"duration":120,"contact":2}},"next":["InnCalMenu"],"timeout":8000,"pre_delay":0,"post_delay":0,"rate_limit":0}
        menu_header={"recognition":"OCR","roi":[72,123,155,28],"expected":["[Mm]enu.*REQ"],"only_rec":True}
        cereal_bonus={"recognition":"OCR","roi":[75,470,370,45],"expected":["Streak.*12"],"only_rec":True}
        start_button={"recognition":"OCR","roi":[1028,624,170,30],"expected":["Open the Inn"],"only_rec":True}
        saved["InnCalMenu"]={"recognition":"And","all_of":[menu_header,cereal_bonus],"action":{"type":"Custom","param":{"custom_action":"InnkeepingMenuAction"}},"pre_wait_freezes":{"target":[450,145,790,365],"time":250,"threshold":.995,"timeout":2500},"pre_delay":0,"post_delay":0,"rate_limit":0}
        saved["InnkeepingStartDispatch"]={"recognition":"DirectHit","action":"DoNothing","next":["InnCalMenu"],"rate_limit":0,"pre_delay":0,"post_delay":0}
        saved["InnkeepingMenuStart"]={"recognition":"And","all_of":[menu_header,cereal_bonus,start_button],"box_index":2,"action":{"type":"LongPress","param":{"target":True,"duration":120,"contact":2}},"next":["InnCalActive"],"timeout":10000,"rate_limit":0,"pre_delay":0,"post_delay":0}
        saved["InnCalWrongMenu"]={**menu_header,"action":{"type":"LongPress","param":{"target":[1220,32,1,1],"duration":120,"contact":2}},"next":["InnCalSettingsBack"],"timeout":2000,"rate_limit":0,"pre_delay":0,"post_delay":0}
        saved["InnCalSettingsBack"]={**settings_header,"action":{"type":"LongPress","param":{"target":[1220,32,1,1],"duration":120,"contact":2}},"next":["InnCalTheme"],"timeout":2000,"pre_delay":0,"post_delay":0,"rate_limit":0}
        # Restart from the shared teleport checkpoint instead of navigating
        # from an arbitrary post-result indoor position.
        entrance=json.loads((self.root/"assets/resource/pipeline/Innkeeping/Entrance.json").read_text("utf8"))
        saved.update(entrance)
        saved["InnkeepingEntranceLedger"]["next"]=["InnCalWorld"]
        saved["InnCalSeekLedger"]={"recognition":"OCR","roi":[30,15,220,45],"expected":["Explore"],"action":"DoNothing","next":["InnkeepingTravelToManor"],"pre_delay":0,"post_delay":0,"rate_limit":0}
        # Open the Inn can first reveal the prior session's unclaimed result.
        # Recognize that transition instead of assuming Theme Settings appears.
        saved["InnCalWorld"]["next"]=["InnCalResultContinue","InnCalTheme"]
        saved["InnCalFinish"].update(next=["InnCalTheme","InnCalWorld","InnCalSeekLedger"],timeout=30000)
        saved["InnScoreDismiss"]=click([690,365,290,45],"Score REQ for Star","InnServiceStartDispatch",[640,150,1,1])
        saved["InnServiceStartDispatch"]={"recognition":"DirectHit","next":["InnScoreDismiss","InnCalResultContinue","InnCalFinish","InnCalTheme","InnCalSettings","InnCalWrongMenu","InnCalSettingsBack","InnCalWorld","InnCalSeekLedger"],"rate_limit":0}
        previous_agents=self.base.get("agent_configs")
        self.base["agent_configs"]=[{"child_exec":getattr(self,"agent_exec","agent/go-service")},
            {"child_exec":sys.executable,"child_args":[str(self.root/"tools/run_cpp_agent.py")]}]
        try:
            self.prepare_world_camera(saved)
            self.execute("InnServiceStartDispatch",saved,"Start real inn run" if self.real_run else "Start bounded inn Trial",180)
        finally:
            self.base["agent_configs"]=previous_agents
        self.startup_kitchen_opened=True
        self.started_session_here=True

    def persist(self):
        state={"counter_position_known":self.counter_position_known,"counter_slots":self.counter_slots.poses,"fast_routes":self.fast_routes,"trial":not self.real_run,"lat":self.lat,"at_counter":self.at_counter,"seat":self.seat,
               "return_motion_done":self.return_motion_done,"origin":self.origin,"pause_confirmed":self.pause_confirmed,"paused_at":self.paused_at,"cycles":self.cycles,"branch":self.branch,"jobs":self.jobs,"served":self.served,"last_fill":self.last_fill,
               "kitchen_ledger":self.ledger.state(time.monotonic()),
               "service_extra":getattr(self,"service_extra",0),"delivery_failures":self.delivery_failures,"pending_pickup":self.pending_pickup,"pending_serve":self.pending_serve,
               "remaining_seconds":(self.last or {}).get("view",{}).get("remaining_seconds",0)}
        (self.output/"resume-state.json").write_text(json.dumps(state,indent=2)+"\n",encoding="utf8")

    def pause_game(self):
        modal={"recognition":"And","all_of":[
            {"recognition":"OCR","roi":[450,450,180,80],"expected":["Cancel"]},
            {"recognition":"OCR","roi":[700,450,210,80],"expected":["Confirm"]}],
            "action":"DoNothing","next":[],"timeout":3000,"pre_delay":0,"post_delay":0,"rate_limit":0}
        nodes={"InnPauseDispatch":{"recognition":"DirectHit","action":"DoNothing","timeout":5000,"next":["InnAlreadyPaused","InnCloseBeforePause","InnCalibrationPause"],"pre_delay":0,"post_delay":0,"rate_limit":0},
          "InnAlreadyPaused":modal,
          "InnCloseBeforePause":click([70,110,520,50],"Kitchen","InnCalibrationPause",[1235,114,1,1]),
          "InnCalibrationPause":click([15,15,205,65],"End Session","InnCalibrationPaused",[150,53,1,1]),
          "InnCalibrationPaused":modal}
        self.execute("InnPauseDispatch",nodes,"Pause the in-game session clock")
        self.pause_confirmed=True;self.paused_at=time.monotonic()
        self.trace("game_clock_paused",remaining=(self.last or {}).get("view",{}).get("remaining_seconds"))

    def resume_game(self):
        # Once Cancel is submitted, a stale pause flag must never suppress recovery.
        self.pause_confirmed=False
        active={"recognition":"OCR","roi":[90,42,120,27],"expected":["End.*Session"],"only_rec":True,"action":"DoNothing","next":[],"timeout":3000,"rate_limit":0,"pre_delay":0,"post_delay":0}
        self.execute("InnCalibrationResume",{"InnCalibrationResume":click([450,450,180,80],"Cancel","InnResumeActive"),"InnResumeActive":active},"Resume the same paused session")
        elapsed=time.monotonic()-self.paused_at
        for job in self.jobs:job["eta"]+=elapsed
        self.last_fill+=elapsed
        self.trace("game_clock_resumed",paused_seconds=elapsed)

    def dining_window(self,report):
        frame=cv2.imread(report["image"],cv2.IMREAD_GRAYSCALE)
        template=cv2.imread(str(self.root/"assets/resource/image/Innkeeping/CourtyardStatue.png"),cv2.IMREAD_GRAYSCALE)
        if frame is None or template is None:return None
        best=None
        # The statue identifies the central courtyard. Bright side windows and
        # white HUD panels must never establish the route origin.
        for scale in (.8,.9,1.,1.1,1.2,1.3):
            resized=cv2.resize(template,None,fx=scale,fy=scale)
            response=cv2.matchTemplate(frame[165:310],resized,cv2.TM_CCOEFF_NORMED)
            _,score,_,point=cv2.minMaxLoc(response)
            if best is None or score>best[0]:best=(score,point[0]+resized.shape[1]/2,scale)
        if best[0]<.67:return None
        center=best[1]
        return (int(center-65*best[2]),int(center+85*best[2]))

    def normalize_camera(self,report,world=False):
        yaw_verified=False
        last_lateral=None
        heading_recheck=False
        for attempt in range(11):
            if report["view"]["phase"]=="ended":return report
            window=self.dining_window(report)
            heading=counter_heading(cv2.imread(report["image"]))
            self.trace("camera_check",image=report["image"],window=window,heading=heading)
            if window is None and last_lateral is not None:
                # Camera collision/zoom can hide a landmark after translation.
                # It does not authorize yaw search from the new, unknown pose.
                x,duration=last_lateral
                self.move([(275 if x==115 else 115,551,duration)],
                          "Undo lateral correction after dining landmark loss",world=world)
                self.last=self.snapshot()
                self.trace("camera_lateral_rejected",image=self.last["image"])
                raise RuntimeError("Dining landmark lost after lateral correction; camera search stopped")
            yaw_verified=heading["usable"] and abs(heading["angle_degrees"]-2)<=.5
            if window and not heading["usable"]:
                if heading_recheck:
                    raise RuntimeError("Dining heading remains unconfirmed; movement stopped")
                heading_recheck=True
                report=self.snapshot()
                continue
            heading_recheck=False
            if window and yaw_verified:
                center=(window[0]+window[1])/2
                if abs(center-700)<=30:return report
                duration=min(500,max(240,int(abs(center-700)/.25)))
                x=115 if center<700 else 275
                self.move([(x,551,duration)],"Center player on the verified courtyard aisle",world=world)
                last_lateral=(x,duration)
                report=self.snapshot()
                continue
            if window and heading.get("angle_degrees") is not None:
                error=heading["angle_degrees"]-2
                delta=int(max(-100,min(100,error*25)))
                if abs(delta)<20:delta=20 if delta>=0 else -20
            else:delta=150
            if attempt==10:raise RuntimeError("Dining camera did not converge within ten measured corrections")
            node=click([20,10,220,55],"Explore") if world else click([15,15,205,65],"End Session")
            node["action"]={"type":"Swipe","param":{"begin":[1100 if delta<0 else 800,200],"end":[1100+delta if delta<0 else 800+delta,200],"duration":600 if abs(delta)>100 else 160,"contact":1}}
            node["post_wait_freezes"]={"target":[450,150,450,100],"time":250,"threshold":.995,"timeout":2000}
            last_lateral=None
            self.execute("InnNormalizeCamera",{"InnNormalizeCamera":node},"Normalize camera on static dining architecture")
            yaw_verified=False
            report=self.snapshot()
        raise RuntimeError("Dining camera alignment exhausted its measured corrections")

    def initialize_origin(self,report):
        window=self.dining_window(report)
        if not window or window[0]<520:raise RuntimeError("Dining doorway not available for two independent landmarks")
        # Keep both references outside the bright, animated courtyard. The
        # previous crops overlapped sky/statue pixels and rejected valid returns.
        boxes=[(max(500,window[0]-80),190,80,40),(window[1]+16,190,80,40)]
        with Image.open(report["image"]) as frame:
            frame=frame.convert("RGB")
            self.origin={"id":"counter-doorway-pillars","start":[{"box":list(box),"pixels":base64.b64encode(sample_scene(frame,box)).decode()} for box in boxes]}
            if not inspect_endpoint(frame,self.origin,"start")["landmark_gate_matches"]:
                raise RuntimeError("Doorway references lack independently matchable texture")
        (self.output/"counter-origin.json").write_text(json.dumps(dict(self.origin,image=report["image"]),indent=2)+"\n",encoding="utf8")

    def maybe_blaze(self,report):
        full=any(t["score"]>=.95 and t["text"].strip()=="100/100" and 1080<=t["box"][0]<=1200 and 320<=t["box"][1]<=380 for t in report["tokens"])
        if not full:return
        node=click([1080,320,130,60],"^100/100$",point=[1215,325,1,1])
        self.execute("InnServiceBlaze",{"InnServiceBlaze":node},"Activate observed full Stove Fire")
        self.ledger.invalidate_timers()
        self.trace("full_blaze_clicked",image=report["image"])

    def finish_results(self,report):
        deadline=time.monotonic()+35
        while time.monotonic()<deadline:
            words=" ".join(t["text"].lower() for t in report["tokens"])
            if "revenue" in words and "highest streak" in words:
                score=[t for t in report["tokens"] if 210<=t["box"][0]<=650 and 605<=t["box"][1]<=680]
                self.trace("round_result",served=self.served,recoveries=self.recoveries,trial=not self.real_run,image=report["image"],score_tokens=score,tokens=report["tokens"],real_three_star_verified=False)
                (self.output/"result.json").write_text(json.dumps(report,indent=2)+"\n",encoding="utf8")
                return
            report=self.snapshot()
        self.trace("result_pending",image=report["image"])

    def correct_service_range(self,report,target):
        box=self.board(report,target)
        extra=getattr(self,"service_extra",0)
        if box is None or extra>=300:
            return False
        frame=cv2.imread(report["image"])
        if frame is None:return False
        x,y,w,h=box
        template=self.output/("table-range-"+str(target)+"-"+str(time.time_ns())+".png")
        if not cv2.imwrite(str(template),frame[y:y+h,x:x+w]):return False
        node=walk(275 if target%2 else 115,551,150)
        node.update(recognition="And",all_of=[
            {"recognition":"OCR","roi":[90,42,120,27],"expected":["End.*Session"],"only_rec":True},
            {"recognition":"Custom","custom_recognition":"InnkeepingPhysicalBoard",
             "custom_recognition_param":{"number":str(target),"box":box,"template":str(template)}}])
        node.pop("roi",None);node.pop("expected",None)
        try:
            self.execute("InnServiceRange",{"InnServiceRange":node},
                         "Approach freshly confirmed table "+str(target))
        except RuntimeError as error:
            self.trace("service_range_rejected",seat=target,error=str(error),image=report["image"])
            return False
        self.service_extra=extra+150
        self.trace("service_range_corrected",seat=target,extra=self.service_extra,image=report["image"])
        return True

    def recover(self,error):
        message=str(error)
        state=self.get("/maa/state")["instances"]
        if any(v["is_running"] for v in state.values()):
            # A timeout is not permission to submit a competing task.
            return False
        report=self.snapshot();view=report["view"]
        self.recoveries+=1
        self.trace("recovery_checkin",error=message,recovery=self.recoveries,image=report["image"],phase=view["phase"])
        if view["phase"]=="unknown":
            # Session-end animation can temporarily hide every phase label.
            # Keep observing within play's deadline; never send game inputs here.
            return True
        if view["phase"]=="ended":return True
        if view["phase"]=="kitchen":
            kitchen=int(view["kitchen"])
            self.ledger.observe(kitchen,view,time.monotonic())
            self.ledger.refill_pending.add(kitchen)
            self.execute("InnRecoverClose",{"InnRecoverClose":click([70,110,520,50],"Kitchen",point=[1235,114,1,1])},"Recover from observed kitchen queue")
            self.refill_attempted=True
            return True
        if view["phase"] not in ("active","closing","closed_serving"):
            return False
        pending=getattr(self,"pending_pickup",None)
        if pending and not view.get("unknown"):
            expected=Counter(pending["before"])+Counter({pending["dish"]:1})
            if Counter(view.get("tray") or {})==expected:
                job=next((j for j in self.jobs if not j["picked"] and j["dish"]==pending["dish"]),None)
                if job:job["picked"]=True
                self.trace("delayed_pickup_verified",dish=pending["dish"],image=report["image"])
                self.pending_pickup=None
        pending=getattr(self,"pending_serve",None)
        if pending and not view.get("unknown"):
            dish=pending["dish"]
            before=Counter(pending["target_before"])
            if all(service_receipt(view,pending["target"],dish,before,pending["expected_tray"])):
                self.served+=1
                self.ledger.served[dish]=self.ledger.served.get(dish,0)+1
                self.trace("delayed_service_verified",seat=pending["target"],dish=dish,count=self.served,image=report["image"])
                self.pending_serve=None
                return True
        if "Pickup" in message or "pickup" in message or "counter marker" in message or "counter food" in message:
            # A failed pickup never grants inventory. Re-read later and deliver
            # food already in hand rather than stopping the round.
            self.pickup_deferred_until=time.monotonic()+3
            return True
        if "kitchen" in message.lower() or "refill" in message.lower():
            # Menu never opened: next visit starts from observed world state.
            self.refill_attempted=True
            return True
        if ("pose" in message or "Serve" in message or "table" in message or "service" in message) and self.seat is not None:
            target=self.seat
            if self.correct_service_range(report,target):
                return True
            self.blocked_seats[target]=time.monotonic()+30
            failure={"seat":target,"reason":message,"image":report["image"],"tray":view.get("tray") or {},"observed_at":time.time()}
            self.delivery_failures.append(failure)
            self.trace("delivery_skipped",**failure)
            # Retain the observed table pose; the next selection takes the
            # existing inter-table aisle route instead of forcing a counter trip.
            return True
        if "origin" in message or "alignment" in message:
            if self.seat is not None:
                self.blocked_seats[self.seat]=time.monotonic()+20
            if not self.at_counter:
                if self.return_motion_done and not self.recover_counter_contact(report):return False
                self.return_counter()
                return True
            nearby=inspect_endpoint(Image.open(report["image"]).convert("RGB"),self.origin,"start",(78,12))
            if nearby.get("landmark_gate_matches"):
                self.verify_origin()
                return True
            # The static courtyard center remains observable after a pickup
            # pulse takes the reference outside its narrow correction window.
            # Reacquire that known anchor rather than terminate the round.
            aligned=self.normalize_camera(report)
            if aligned["view"]["phase"]=="ended":
                self.finish_results(aligned)
                return True
            self.move([(195,631,300)],"Re-establish counter contact after visual re-centering")
            self.lat=0
            self.verify_origin()
            return True
        return False

    def play(self,resume=False,saved_state=None):
        if saved_state:
            self.real_run=not saved_state.get("trial",True)
            self.fast_routes=saved_state.get("fast_routes",getattr(self,"fast_routes",False))
            self.counter_slots=CounterSlots(saved_state.get("counter_slots"))
            self.counter_position_known=saved_state.get("counter_position_known",True)
            self.origin=saved_state.get("origin")
            self.pause_confirmed=saved_state.get("pause_confirmed",False)
            self.paused_at=saved_state.get("paused_at",0)
            self.cycles=saved_state.get("cycles",0)
            self.return_motion_done=saved_state.get("return_motion_done",False)
            for key in ("lat","at_counter","seat","branch","jobs","served","last_fill"):
                setattr(self,key,saved_state[key])
            self.service_extra=saved_state.get("service_extra",0)
            self.delivery_failures=saved_state.get("delivery_failures",[])
            self.pending_pickup=saved_state.get("pending_pickup")
            self.pending_serve=saved_state.get("pending_serve")
            for failed in self.delivery_failures[-6:]:
                if time.time()-failed.get("observed_at",failed.get("at",0))<60:
                    self.blocked_seats[int(failed["seat"])]=time.monotonic()+30
            self.ledger=KitchenLedger(self.jobs,saved_state.get("kitchen_ledger"))
            if self.pause_confirmed:self.resume_game()
            view=self.snapshot()["view"]
            if view["phase"] not in ("active","closing","closed_serving"):
                raise RuntimeError("Saved Trial is no longer active")
            previous=saved_state.get("remaining_seconds",0)
            if previous and view.get("remaining_seconds",0)>previous+1:
                raise RuntimeError("Timer increased; saved state belongs to another round")
        elif resume:
            view=self.snapshot()["view"]
            if view["phase"]!="active" or sum((view.get("tray") or {}).values()):
                raise RuntimeError("Only an active, empty-tray startup Trial can be resumed")
        else:self.start()
        if not saved_state or self.origin is None:
            # Cook immediately; camera calibration must not leave both kitchens idle.
            if not self.jobs:
                try:self.fill(initial=True)
                except (RuntimeError,ValueError) as error:
                    if not self.recover(error):raise
            camera_report=self.snapshot()
            if self.started_session_here and not self.dining_window(camera_report):
                # A previous round can leave the dining-facing camera intact.
                # Apply the measured half-turn only when that landmark is absent.
                nodes={}
                for index in range(2):
                    node=click([15,15,205,65],"End Session","InnStartupTurn1" if index==0 else None)
                    node["action"]={"type":"Swipe","param":{"begin":[800,200],"end":[1100,200],"duration":600,"contact":1}}
                    node["post_wait_freezes"]={"target":[450,150,450,100],"time":250,"threshold":.995,"timeout":2000}
                    nodes["InnStartupTurn"+str(index)]=node
                self.execute("InnStartupTurn0",nodes,"Turn from startup kitchen view toward dining")
                camera_report=self.snapshot()
            self.started_session_here=False
            aligned=self.normalize_camera(camera_report)
            if aligned["view"]["phase"]=="ended":
                self.finish_results(aligned)
                return
            self.move([(195,631,300)],"Establish repeatable counter contact before route capture")
            self.initialize_origin(self.snapshot())
        if self.return_motion_done:
            try:self.return_counter()
            except RuntimeError as error:
                if not self.recover(error):raise
        deadline=time.monotonic()+350
        while time.monotonic()<deadline:
            try:
                report=self.last if self.last is not None and self.last_report_version==self.scene_version and time.monotonic()-self.last_report_at<=.5 else self.snapshot()
                view=report["view"]
                if view["phase"]=="ended":
                    self.finish_results(report)
                    return
                if view["phase"] not in ("active","closing","closed_serving"):raise RuntimeError("Unexpected inn phase")
                self.maybe_blaze(report)
                tray=Counter(view.get("tray") or {})
                # Do not delay a usable carried dish for another pickup. Counter
                # recognition and approach retries can otherwise consume the round.
                target=self.delivery_target(view,tray)
                if target is not None:
                    self.serve(target)
                    self.refill_attempted=False
                    continue
                if self.at_counter and sum(tray.values())<2:
                    need=self.demand(view)
                    ready_counter=any(not j["picked"] and need[j["dish"]]>tray[j["dish"]] and j["eta"]<=time.monotonic() for j in self.jobs)
                    visible_ready=view.get("prompt")=="pickup" and need[view.get("prompt_dish","")]>tray[view.get("prompt_dish","")]
                    if ready_counter or visible_ready:
                        collected=self.collect(report)
                        if Counter(collected["view"].get("tray") or {})!=tray:continue
                        report=collected;view=report["view"]
                tray=Counter(view.get("tray") or {})
                target=self.delivery_target(view,tray)
                if target is not None:
                    # Loaded food always leaves now. Choose the complete two-item
                    # tour by meal completion and travel cost, not table number.
                    self.serve(target)
                    self.refill_attempted=False
                    continue
                if not self.at_counter:
                    self.return_counter()
                    # Table observations cannot decide counter stock. Check the
                    # returned counter before spending time opening kitchens.
                    continue
                if self.max_cycles and self.cycles>=self.max_cycles:
                    self.pause_game()
                    self.trace("cycle_gate_passed",cycles=self.cycles,served=self.served)
                    return
                need=self.demand(view)
                # Keep both kitchens fed; no blanket twelve-second refill cooldown.
                now=time.monotonic()
                ready_soon=any(not j["picked"] and need[j["dish"]]>tray[j["dish"]]
                               and now<j["eta"]<=now+3 for j in self.jobs)
                if not tray and self.can_refill(view) and not ready_soon and self.ledger.refill_due(need,tray,now):
                    self.fill(carried=tray)
                self.collect(self.last if self.last is not None and self.last_report_version==self.scene_version and time.monotonic()-self.last_report_at<=.5 else None)
                time.sleep(.1)
            except (RuntimeError,ValueError) as error:
                if not self.recover(error):raise
                self.persist()
        raise RuntimeError("Bounded round exceeded its observation deadline")


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run",action="store_true")
    parser.add_argument("--settle-moves",action="store_true",help="Verify camera settling between measured movement pulses")
    parser.add_argument("--fast-routes",action="store_true",help="Test direct timed routes with endpoint interaction guards")
    parser.add_argument("--agent-binary",type=Path,help="Use a rebuilt native agent with tray-content acknowledgements")
    parser.add_argument("--real-run",action="store_true",help="Select Real Run explicitly at Theme Settings")
    parser.add_argument("--pause-on-stop",action="store_true")
    parser.add_argument("--max-cycles",type=int,default=0)
    parser.add_argument("--seats",nargs="+",type=int,choices=range(1,7),default=list(range(1,7)))
    parser.add_argument("--prepared-camera",action="store_true",help="Compatibility option; startup verifies the camera before service")
    parser.add_argument("--resume-state",type=Path,help="Resume this stopped Trial without restarting it")
    parser.add_argument("--resume-active",action="store_true",help="Resume only this just-started Trial after a startup recognition failure")
    parser.add_argument("--output",type=Path,required=True)
    args=parser.parse_args()
    if not args.run:parser.error("Explicit --run is required")
    trial=ServiceTrial(Path.cwd(),args.output.resolve(),agent_binary=args.agent_binary)
    trial.settle_moves=args.settle_moves
    trial.fast_routes=args.fast_routes
    trial.real_run=args.real_run
    trial.allowed_seats=set(args.seats)
    trial.max_cycles=args.max_cycles
    state=json.loads(args.resume_state.read_text("utf8")) if args.resume_state else None
    try:trial.play(resume=args.resume_active,saved_state=state)
    except BaseException:
        if args.pause_on_stop and not trial.pause_confirmed:
            try:trial.pause_game()
            except Exception as pause_error:trial.trace("pause_failed",reason=str(pause_error))
        raise
    finally:trial.persist()


if __name__=="__main__":main()
