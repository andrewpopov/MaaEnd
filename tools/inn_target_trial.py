"""Supervised same-session inn table check; normal Go actions own all food inputs.

This research tool uses fresh counter icons and static room patches to restore
its actual departure pose. It does not certify or promote production routes.
"""
import argparse
import datetime as dt
import json
from pathlib import Path
import subprocess
import sys
import time
import urllib.request

import cv2
import numpy as np
from inn_live_segment import snapshot
from inn_room_landmarks import room_offset


def main():
    ap=argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--task-id',type=int,required=True)
    ap.add_argument('--seat',type=int,choices=range(1,7),required=True)
    ap.add_argument('--output',type=Path,required=True)
    ap.add_argument('--run',action='store_true')
    ap.add_argument('--pickup-only',action='store_true',help='Stop after native pickup; no departure or service movement')
    a=ap.parse_args()
    if not a.run or a.output.resolve().drive.upper()=='C:':ap.error('Explicit --run and image storage off C: required')
    a.output.mkdir(parents=True,exist_ok=False)
    root=Path.cwd();base='http://127.0.0.1:12701/api/maa/instances/innreact';http=urllib.request.build_opener(urllib.request.ProxyHandler({}))
    def post(path,body):
        return json.load(http.open(urllib.request.Request(base+path,data=json.dumps(body).encode(),headers={'Content-Type':'application/json'}),timeout=15))
    def state():return snapshot(root)
    def valid():
        live=json.load(http.open('http://127.0.0.1:12701/api/maa/state',timeout=8))['instances']
        if any(v['is_running'] for k,v in live.items() if k!='innreact'):raise RuntimeError('Another MaaEnd profile owns the game')
        own=live['innreact']
        if not own['is_running'] or a.task_id not in own['task_run_state']['pending_task_ids']:raise RuntimeError('The original task is no longer running')
    def trace(event,**kw):
        row=dict(at=dt.datetime.now(dt.timezone.utc).isoformat(),event=event,**kw)
        with (a.output/'events.jsonl').open('a') as f:f.write(json.dumps(row)+'\n')
        print(json.dumps(row),flush=True)
    sequence=0
    def move(label,moves):
        nonlocal sequence
        valid();sequence+=1;name=f'{sequence:02d}-{label}';p=a.output/(name+'.json')
        p.write_text(json.dumps([dict(Kind='walk',X=x,Y=y,Milliseconds=80,HoldMilliseconds=h,JoystickWarmup=True,Pipeline=True) for x,y,h in moves]))
        with (a.output/(name+'.log')).open('w') as f:
            subprocess.run([sys.executable,'tools/inn_live_segment.py','--run','--task-id',str(a.task_id),'--steps',str(p),'--output',str(a.output/name)],stdout=f,stderr=f,check=True)
        trace('segment_observed',name=name,view=state()['view'])
    frames=0
    def frame():
        nonlocal frames
        time.sleep(.12);frames+=1;p=a.output/f'frame-{frames:03d}.png';p.write_bytes(http.open(base+'/screenshot',timeout=8).read())
        im=cv2.imread(str(p))
        details=json.loads(subprocess.check_output([str(root/'.cache/innframe.exe'),'--image',str(p),'--data',str(root/'install/data/Innkeeping')],text=True))
        return im,details
    valid();post('/screenshot/subscribe',{'subscriber_id':'inn-target-trial','interval_ms':100})
    try:
        end=time.monotonic()+100
        while time.monotonic()<end:
            valid();s=state();v=s['view'];seat=next((t for t in v['seats'] if t['seat']==str(a.seat)),{})
            times=[dt.datetime.fromisoformat(j['expected_finish_at']) for j in (s.get('cooking') or []) if j.get('expected_finish_at') and j.get('dish') in (seat.get('dishes') or [])]
            if v['phase']=='active' and seat.get('dishes') and times and dt.datetime.now(dt.timezone.utc)>min(times):break
            time.sleep(.2)
        else:raise RuntimeError('No ready batch and known target; no movement')
        wanted=set(seat['dishes']);before=list(seat['dishes']);origin,_=frame();(a.output/'counter-origin.png').write_bytes(cv2.imencode('.png',origin)[1].tobytes())
        # Two independently textured static crops above diner bubbles.
        candidates=[]
        gray=cv2.cvtColor(origin,cv2.COLOR_BGR2GRAY)
        for y in (150,170,190):
            for x in range(500,1001,40):
                patch=gray[y:y+40,x:x+64]
                if patch.std()<14:continue
                scores=cv2.matchTemplate(gray[125:250,280:1200],patch,cv2.TM_CCOEFF_NORMED)
                _,best,_,loc=cv2.minMaxLoc(scores);ix,iy=loc
                scores[max(0,iy-30):iy+31,max(0,ix-50):ix+51]=-1
                if best>.98 and float(scores.max())<.90:candidates.append((float(patch.std()),x,y,patch.copy()))
        candidates.sort(reverse=True,key=lambda t:t[0]);refs=[]
        for cand in candidates:
            if not refs or abs(cand[1]-refs[0][1])>=160:refs.append(cand)
            if len(refs)==2:break
        if len(refs)!=2:raise RuntimeError('No two unique static departure landmarks; no movement')
        trace('departure_captured',seat=a.seat,requests=before,landmarks=[[x,y] for _,x,y,_ in refs])
        backed=False
        pickup_deadline=time.monotonic()+30
        for attempt in range(120):
            if time.monotonic()>pickup_deadline or sequence>=12:raise RuntimeError('Bounded pickup deadline reached; no repeated inputs')
            valid();s=state();v=s['view']
            if any(v['tray'].get(d,0)>0 for d in wanted):break
            if v.get('prompt')=='pickup':time.sleep(.2);continue
            im,detail=frame();goals=[g for g in detail['counter_foods'] if g['dish'] in wanted]
            if not goals:raise RuntimeError('Target food is not visibly localized; no searching or repeated route')
            g=min(goals,key=lambda g:abs(g['box'][0]+g['box'][2]/2-640));cx=g['box'][0]+g['box'][2]/2
            if 590<=cx<=690:
                if backed:raise RuntimeError('Aligned food still has no pickup acknowledgement')
                move('counter-depth',[(195,631,150)]);backed=True
            else:move('counter-align',[(275 if cx>640 else 115,551,min(350,max(100,round(abs(cx-640)*.8))))])
            time.sleep(.35)
        else:raise RuntimeError('Counter calibration bound reached')
        s=state()
        if not any(s['view']['tray'].get(d,0)>0 for d in wanted):raise RuntimeError('No matching food acquired')
        carried_before=dict(s['view']['tray'])
        trace('pickup_confirmed',tray=carried_before)
        if a.pickup_only:return
        previous=None
        for attempt in range(10):
            im,_=frame();g=cv2.cvtColor(im,cv2.COLOR_BGR2GRAY);offsets=[]
            for _,x,y,patch in refs:
                scores=cv2.matchTemplate(g[125:250,280:1200],patch,cv2.TM_CCOEFF_NORMED);_,score,_,loc=cv2.minMaxLoc(scores)
                if score<.90:
                    offsets=[];break
                offsets.append((loc[0]+280-x,loc[1]+125-y))
            if not offsets:
                measured=room_offset(origin,im);trace('architecture_match',**measured)
                if not measured['usable']:raise RuntimeError('Departure architecture uncertain; no guessed return')
                offsets=[(measured['dx'],measured['dy'])]*2
            if abs(offsets[0][0]-offsets[1][0])>15 or abs(offsets[0][1]-offsets[1][1])>8:raise RuntimeError('Static landmarks disagree on position')
            dx=sum(p[0] for p in offsets)/2;dy=sum(p[1] for p in offsets)/2
            trace('origin_offset',dx=dx,dy=dy)
            if abs(dy)>10:raise RuntimeError('Counter depth changed; departure pose not restored')
            if abs(dx)<=12:break
            if previous is not None and abs(dx)>previous+30:raise RuntimeError('Lateral correction moved away from departure')
            previous=abs(dx);move('restore-axis',[(115 if dx<0 else 275,551,min(250,max(80,round(abs(dx)*.7))))])
        else:raise RuntimeError('Departure restoration bound reached')
        row=(a.seat+1)//2;axis=[1050]+[1250]*(row-1)
        moves=[(195,471,h) for h in axis]+[(275 if a.seat%2 else 115,551,450)]
        move('table'+str(a.seat),moves)
        end=time.monotonic()+8
        while time.monotonic()<end:
            s=state();target=next((t for t in s['view']['seats'] if t['seat']==str(a.seat)),{})
            if not s['view'].get('unknown') and any(s['view']['tray'].get(d,0)<carried_before.get(d,0) for d in wanted) and (target.get('phase')=='dining' or (target.get('phase')=='waiting' and isinstance(target.get('dishes'),list) and any(target['dishes'].count(d)<before.count(d) for d in wanted))):
                frame();trace('target_service_confirmed',seat=a.seat,view=s['view']);return
            time.sleep(.1)
        frame();raise RuntimeError('No target-table service acknowledgement; inspect actual endpoint')
    finally:post('/screenshot/unsubscribe',{'subscriber_id':'inn-target-trial'})


if __name__=='__main__':main()
