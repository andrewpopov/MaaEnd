"""Run a bounded inn Trial with fixed-camera paths and fresh native controls.

Pipeline owns clicks and each measured input; innframe supplies existing MaaEnd
HUD/inventory recognition. This research driver stops on a failed acknowledgement.
It never starts a real session or promotes route replay counters.
"""
import os
import argparse
import copy
import datetime as dt
import hashlib
import json
from pathlib import Path
import re
import subprocess
import time
import urllib.request

from inn_route_status import fresh_frame


def button(roi, text, next_node=None, point=None, timeout=3000):
    node={'recognition':'OCR','roi':roi,'expected':[text],
          'action':{'type':'LongPress','param':{'target':point or True,'duration':120,'contact':2}},'timeout':timeout}
    if next_node:node['next']=[next_node]
    return node


def walk(x,y,hold,next_node):
    return {'recognition':'OCR','roi':[15,15,205,65],'expected':['End Session'],
            'action':{'type':'Swipe','param':{'begin':[195,551],'end':[x,y],'duration':20,'end_hold':hold}},
            'next':[next_node],'timeout':2000}


def cycle_nodes(seat, dish):
    recipes={'salted_cheese_crackers':('Salted Cheese Crackers',[950,280,280,110],14),
             'fin_dumpling':('Fin Dumpling',[450,280,370,110],20),
             'tuber_fries':('Tuber Fries',[950,170,280,110],25),
             'hot_sour_soup':('Hot & Sour Soup',[450,170,370,110],37)}
    name,roi,seconds=recipes[dish]
    n={'FixedCookOpen':button([840,600,210,80],'Add Order','FixedKitchen1'),
       'FixedKitchen1':button([160,110,155,50],'Kitchen 1','FixedQueue'),
       'FixedQueue':button(roi,re.escape(name),'FixedQueueConfirmed'),
       'FixedQueueConfirmed':button([300,590,95,50],r'[0-9]+s','FixedAwaitFood',[1235,114,1,1]),
       'FixedAwaitFood':button([800,390,220,80],re.escape(name) if dish!='salted_cheese_crackers' else 'Salted Cheese','FixedLoadedExit',[850,425,1,1],(seconds+15)*1000),
       'FixedLoadedExit':{'recognition':'OCR','roi':[800,390,300,90],'expected':['Put Back on Counter'],
         'action':{'type':'Swipe','param':{'begin':[195,551],'end':[115,551],'duration':20,'end_hold':1050}},'next':['FixedNorth0'],'timeout':2000}}
    n['FixedQueueConfirmed']['next']=['FixedAwaitFood','FixedFoodCenter0','FixedFoodRight0','FixedFoodLeft0']
    n['FixedQueueConfirmed']['timeout']=(seconds+15)*1000
    for i in range(6):
        following=['FixedAwaitFood']+(['FixedFoodCenter'+str(i+1),'FixedFoodRight'+str(i+1),'FixedFoodLeft'+str(i+1)] if i<5 else [])
        n['FixedFoodCenter'+str(i)]={'recognition':'And','all_of':[
            {'recognition':'OCR','roi':[15,15,205,65],'expected':['End Session']},
            {'recognition':'Custom','custom_recognition':'InnkeepingReadyFoodOffset','custom_recognition_param':{'dish':dish,'side':'center'}}],
            'action':{'type':'Swipe','param':{'begin':[195,551],'end':[195,631],'duration':20,'end_hold':100}},
            'next':following,'timeout':2500}
        for side,x in [('Right',275),('Left',115)]:
            n['FixedFood'+side+str(i)]={'recognition':'And','all_of':[
                {'recognition':'OCR','roi':[15,15,205,65],'expected':['End Session']},
                {'recognition':'Custom','custom_recognition':'InnkeepingReadyFoodOffset','custom_recognition_param':{'dish':dish,'side':side.lower()}}],
                'action':{'type':'Swipe','param':{'begin':[195,551],'end':[x,551],'duration':20,'end_hold':200}},
                'next':following,'timeout':2500}
    row=(int(seat)-1)//2+1
    for i,hold in enumerate([700]+[1000]*(row-1)):
        n['FixedNorth'+str(i)]=walk(195,471,hold,'FixedNorth'+str(i+1) if i+1<row else 'FixedApproach')
    right=int(seat)%2==1
    n['FixedApproach']=walk(275 if right else 115,551,250 if right else 450,'FixedServe')
    n['FixedServe']={'recognition':'And','all_of':[
        {'recognition':'OCR','roi':[700,350,350,160],'expected':['Serve']},
        {'recognition':'ColorMatch','roi':[700,350,350,160],'method':40,'lower':[35,60,150],'upper':[85,255,255],'count':20,'connected':False}],
        'box_index':0,'action':{'type':'LongPress','param':{'target':True,'duration':120,'contact':2}},'next':['FixedExit'],'timeout':2500}
    n['FixedExit']=walk(115 if right else 275,551,250 if right else 400,'FixedSouth0')
    for i,hold in enumerate([1000]*(row-1)+[700]):
        n['FixedSouth'+str(i)]=walk(195,631,hold,'FixedSouth'+str(i+1) if i+1<row else 'FixedCounterApproach')
    n['FixedCounterApproach']=walk(275,551,900,'FixedCycleDone')
    n['FixedCycleDone']={'recognition':'OCR','roi':[15,15,205,65],'expected':['End Session'],'action':'DoNothing','timeout':2000}
    return n


def choose_order(view):
    seconds={'salted_cheese_crackers':14,'fin_dumpling':20,'tuber_fries':25,'hot_sour_soup':37}
    candidates=[]
    for seat in view['seats']:
        if seat['phase']!='waiting':continue
        for dish in seat.get('dishes') or []:
            if dish in seconds:candidates.append((int(seat['seat']),seconds[dish],dish))
    if not candidates:return None
    seat,_,dish=min(candidates)
    return str(seat),dish


def delivery_confirmed(view, seat, dish, before_count):
    target=next((s for s in view['seats'] if s['seat']==seat),None)
    return (not view.get('unknown') and not sum(view['tray'].values()) and target is not None
            and (target.get('dishes') or []).count(dish)<before_count)


class Trial:
    def __init__(self,root,output):
        self.root=root;self.output=output;output.mkdir(parents=True,exist_ok=False)
        self.http=urllib.request.build_opener(urllib.request.ProxyHandler({}))
        self.api=os.environ.get('MAAEND_API_URL','http://127.0.0.1:12701/api').rstrip('/')
        self.instance_id=os.environ.get('MAAEND_INSTANCE_ID','innreact')
        self.instance=self.api+'/maa/instances/'+self.instance_id
        self.base=json.loads((root/'.cache/inn-reactive-trial.json').read_text('utf8'))
        self.last_hash=None;self.last_image=None
    def get(self,path):return json.load(self.http.open(self.api+path,timeout=10))
    def post(self,path,body):
        request=urllib.request.Request(self.instance+path,data=json.dumps(body).encode(),headers={'Content-Type':'application/json'})
        return json.load(self.http.open(request,timeout=60 if path=="/tasks/start" else 15))
    def trace(self,event,**data):
        record=dict(at=dt.datetime.now(dt.timezone.utc).isoformat(),event=event,**data)
        with (self.output/'events.jsonl').open('a',encoding='utf8') as f:f.write(json.dumps(record)+'\n')
        print(json.dumps(record),flush=True)
    def capture(self):
        raw=self.http.open(self.instance+'/screenshot',timeout=10).read();digest=hashlib.sha256(raw).hexdigest()
        if digest!=self.last_hash:
            path=self.output/(dt.datetime.now(dt.timezone.utc).strftime('%Y%m%d-%H%M%S-%f')+'.png');path.write_bytes(raw)
            self.last_hash=digest;self.last_image=path
            with (self.output/'frames.jsonl').open('a',encoding='utf8') as f:f.write(json.dumps(dict(at=dt.datetime.now(dt.timezone.utc).isoformat(),image=str(path)))+'\n')
        return self.last_image
    def inspect(self):
        raw=fresh_frame(self.http);path=self.output/(dt.datetime.now(dt.timezone.utc).strftime('%Y%m%d-%H%M%S-%f')+'-inspect.png');path.write_bytes(raw)
        result=subprocess.run([str(self.root/'.cache/innframe.exe'),'--image',str(path),'--data',str(self.root/'install/data/Innkeeping')],capture_output=True,text=True,check=True)
        view=json.loads(result.stdout)['view'];self.trace('view',view=view,image=str(path));return view
    def run(self,entry,nodes,name,timeout=100):
        state=self.get('/maa/state')
        if any(v['is_running'] for v in state['instances'].values()):raise RuntimeError('MaaEnd already running; no new inputs submitted')
        payload=copy.deepcopy(self.base);payload['tasks'][0].update(entry=entry,pipeline_override=json.dumps(nodes),task_name=name)
        task=self.post('/tasks/start',payload)['taskIds'][0];self.last_task_id=task;self.trace('task_started',task=task,entry=entry,name=name)
        (self.output/(str(task)+'-pipeline.json')).write_text(json.dumps(nodes,indent=4)+'\n',encoding='utf8')
        deadline=time.monotonic()+timeout
        while time.monotonic()<deadline:
            time.sleep(getattr(self,"poll_interval",.5));self.capture();state=self.get('/maa/state')['instances'][self.instance_id]
            current=state.get('task_run_state',{})
            if task in current.get('pending_task_ids',[]) and not state['is_running']:
                status=current.get('overall_status');self.trace('task_finished',task=task,status=status);return status=='Succeeded'
        raise RuntimeError('Observation deadline reached: inspect the same task; no automatic restart')


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run',action='store_true',help='Submit the bounded Trial workflow to Endies')
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--duration',type=int,default=350)
    args=parser.parse_args()
    if not args.run:parser.error('Explicit --run is required')
    if args.output.resolve().drive.upper()=='C:':parser.error('Store captures off C:')
    root=Path.cwd();trial=Trial(root,args.output.resolve())
    saved=json.loads((root/'.cache/inn-fixed-table1-proven.json').read_text('utf8'))['all_prototype_nodes']
    setup=copy.deepcopy(saved)
    setup['FixedTrialDispatch']={'recognition':'DirectHit','next':['InnCalResultContinue','InnCalFinish','InnCalWorld'],'timeout':3000}
    setup['InnCalFinish']['next']=['InnCalWorld']
    # Live return-to-world loading exceeded the old ten-second recognition window.
    setup['InnCalFinish']['timeout']=30000
    setup['InnCalFinish']['pre_wait_freezes']={'time':800,'target':[1030,600,230,100],'timeout':2500}
    setup['InnCalSettled']['next']=['FixedInitialCounterApproach']
    setup['FixedInitialCounterApproach']=walk(275,551,700,'FixedInitialCounterReady')
    setup['FixedInitialCounterReady']={'recognition':'OCR','roi':[15,15,205,65],'expected':['End Session'],'action':'DoNothing','timeout':2000}
    trial.post('/screenshot/subscribe',{'subscriber_id':'fixed-trial-driver','interval_ms':100})
    try:
        if not trial.run('FixedTrialDispatch',setup,'Start fixed-camera Trial',70):return
        began=time.monotonic();completed=[]
        while time.monotonic()-began<args.duration:
            view=trial.inspect()
            if sum(view['tray'].values()) or view.get('unknown'):
                trial.trace('stopped',reason='Unexpected held or unidentified inventory');break
            order=choose_order(view)
            if not order:time.sleep(1);continue
            seat,dish=order;before=next(s for s in view['seats'] if s['seat']==seat)['dishes'].count(dish)
            trial.trace('selected',seat=seat,dish=dish)
            if not trial.run('FixedCookOpen',cycle_nodes(seat,dish),f'Fixed aisle: {dish} to Table {seat}',100):break
            after=trial.inspect()
            if not delivery_confirmed(after,seat,dish,before):
                trial.trace('stopped',reason='Delivery acknowledgement did not match selected table and tray');break
            completed.append(dict(seat=seat,dish=dish));trial.trace('delivery_verified',seat=seat,dish=dish,count=len(completed))
        trial.trace('finished',deliveries=completed,real_three_star_verified=False)
    finally:trial.post('/screenshot/unsubscribe',{'subscriber_id':'fixed-trial-driver'})


if __name__=='__main__':main()
