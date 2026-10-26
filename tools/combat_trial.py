"""Prepare and score bounded combat trials. No controller inputs or automatic arming.

init creates an explicit, unreviewed annotation scaffold. score accepts only
reviewed opportunities with evidence. record_workflow.py provides contextual
screenshots; calibrate_rotation.py provides actual MaaFramework signal replay.
"""
import argparse
import datetime as dt
import hashlib
import json
import math
import statistics
import subprocess
from pathlib import Path

PHASES = [
    {'id':'preflight','seconds':120,'goal':'Confirm candidate identity, team, encounter, recorder and stop/release behavior'},
    {'id':'manual-evidence','seconds':180,'goal':'Record ready/unready prompts, consumption, cooldowns, and selected-slot changes'},
    {'id':'calibration','seconds':180,'goal':'Review positives and hard negatives; calibrate and replay held-out clips; otherwise collect missing evidence'},
    {'id':'shadow','seconds':180,'goal':'Preview the selected variant, then one bounded controlled short rotation only if gates pass'},
    {'id':'repetitions','seconds':300,'goal':'Repeat the same short rotation two or three times; seek ten reviewed action opportunities'},
    {'id':'recovery','seconds':120,'goal':'Check stop/restart and one interruption; stop on wrong owner, false advancement, or held-input failure'},
    {'id':'flush','seconds':120,'goal':'Stop, verify attack release, flush artifacts and generate preliminary score; no last-minute patches'},
]


def read_jsonl(path):
    if not path.exists():
        return []
    return [json.loads(line) for line in path.read_text(encoding='utf-8-sig').splitlines() if line.strip()]


def write_json(path, data):
    path.write_text(json.dumps(data, indent=4, ensure_ascii=False)+'\n', encoding='utf-8')


def init_session(output, profile_path):
    profile_raw=profile_path.read_bytes()
    profile=json.loads(profile_raw.decode('utf-8-sig'))
    output.mkdir(parents=True,exist_ok=False)
    write_json(output/'session.json', {
        'schema_version':1, 'created_at':dt.datetime.now(dt.timezone.utc).isoformat(),
        'profile_id':profile['id'],'profile_sha256':hashlib.sha256(profile_raw).hexdigest(),
        'members':profile['members'],'maximum_emulator_seconds':1200,'phases':PHASES,
        'status':'prepared_not_run','game_inputs_sent':False,
        'evidence_requirements':['reviewed visible prompt onset/expiry and expected owner',
          'record actual input separately from resource acknowledgement and damage landing',
          'include interrupted, missed and wrong-owner cases; unknown is never success',
          'native video or same-frame trace required for timing; HTTP cache alone is insufficient'],
        'reuse':{'context_recorder':'tools/record_workflow.py',
                'signal_replay':'tools/calibrate_rotation.py','build_install':'tools/Update-Local.ps1'},
        'acceptance':{'minimum_reviewed_combo_opportunities':10,'minimum_catch_rate':0.9,
          'maximum_wrong_or_duplicate_inputs':0,'minimum_short_rotation_repetitions':3,
          'scope':'one team/layout/encounter; gate is preliminary, not unattended reliability',
          'stop_on':['wrong slot','out-of-order dispatch','unconfirmed advancement','held input after stop'],
          'latency':'report ready-to-dispatch and dispatch-to-resource-ack separately; no assumed game latency target'},
        'readiness':{'hud_signals_bound':all(profile.get('signals',{}).get(k) for k in profile['required_signals']),
                     'live_validation_passed':False},
    })
    (output/'opportunities.jsonl').write_text('',encoding='utf-8')
    (output/'events.jsonl').write_text('',encoding='utf-8')
    write_json(output/'annotation-example.json', {
        'opportunity':{'id':'replace-with-unique-id','phase':'combos','kind':'combo','slot':2,
          'open_ms':1000,'close_ms':2200,'reviewed':False,'evidence':['video.mp4#t=1.0,2.2']},
        'event':{'id':'replace-with-unique-event-id','opportunity_id':'replace-with-unique-id',
          'kind':'input','slot':2,'t_ms':1400,'reviewed':False,'evidence':['trace.jsonl:42'],
          'accepted':None,'landed':None},
        'note':'Examples are not test evidence. Times use the same explicitly aligned session clock.'})
    write_json(output/'bindings-template.json',{'profile_id':profile['id'],'signals':{},
       'note':'Populate real recognition parameters and positive/negative fixture paths for calibrate_rotation.py; empty bindings are not calibration.'})
    return output/'session.json'


def score(opportunities, events):
    ids=[row['id'] for row in opportunities]
    if len(ids)!=len(set(ids)):
        raise ValueError('Duplicate opportunity IDs')
    event_ids=[row['id'] for row in events]
    if len(event_ids)!=len(set(event_ids)):
        raise ValueError('Duplicate event IDs')
    reviewed=[]
    for row in opportunities:
        if row.get('reviewed') is not True or not row.get('evidence'):continue
        if row.get('kind')!='combo':continue
        if type(row.get('slot')) is not int or row.get('slot') not in (1,2,3,4):raise ValueError('Invalid slot')
        if not all(type(row.get(k)) in (int,float) and math.isfinite(row[k]) and row[k]>=0 for k in ('open_ms','close_ms')) or row['close_ms']<=row['open_ms']:
            raise ValueError('Invalid prompt window')
        reviewed.append(row)
    known=set(ids)
    if any(e.get('opportunity_id') not in known for e in events):
        raise ValueError('Event references unknown opportunity')
    results=[];latencies=[]
    for row in reviewed:
        attempts=[e for e in events if e.get('opportunity_id')==row['id'] and e.get('kind')=='input' and e.get('reviewed') is True and e.get('evidence')]
        for e in attempts:
            if type(e.get('t_ms')) not in (int,float) or not math.isfinite(e['t_ms']) or e['t_ms']<0 or type(e.get('slot')) is not int or e.get('slot') not in (1,2,3,4):
                raise ValueError('Invalid input event')
        correct=[e for e in attempts if e['slot']==row['slot'] and row['open_ms']<=e['t_ms']<=row['close_ms']]
        accepted=[e for e in correct if e.get('accepted') is True]
        if accepted:latencies.append(min(e['t_ms'] for e in accepted)-row['open_ms'])
        results.append({'id':row['id'],'caught_and_accepted':bool(accepted),
          'input_in_window':bool(correct),'missed':not accepted,
          'wrong_owner_inputs':sum(e['slot']!=row['slot'] for e in attempts),
          'duplicate_inputs':max(0,len(attempts)-1),
          'unknown_acceptance':sum(e.get('accepted') is None for e in attempts),
          'late_inputs':sum(e['t_ms']>row['close_ms'] for e in attempts)})
    total=len(results);caught=sum(r['caught_and_accepted'] for r in results)
    errors=sum(r['wrong_owner_inputs']+r['duplicate_inputs'] for r in results)
    return {'schema_version':1,'reviewed_combo_opportunities':total,'excluded_unreviewed_or_other':len(opportunities)-total,
       'caught_and_accepted':caught,'catch_rate':caught/total if total else None,
       'wrong_or_duplicate_inputs':errors,'latency_ms':{'samples':len(latencies),'maximum':max(latencies) if latencies else None,
        'median':statistics.median(latencies) if latencies else None,
        'p95_nearest_rank':sorted(latencies)[math.ceil(.95*len(latencies))-1] if latencies else None},
       'combo_gate':'insufficient_evidence' if total<10 else ('pass' if caught/total>=.9 and errors==0 else 'fail'),
       'full_rotation_ready':False,'note':'Combo gate alone does not validate skill order, Final Strike detection, interruption recovery, or unattended use.',
       'opportunities':results,'skill_timings':skill_timing_review(opportunities,events)}


def skill_timing_review(opportunities, events):
    """Measure reviewed skill windows; do not invent an ideal cast time."""
    results=[]
    def timestamp(row,key):
        value=row.get(key)
        if value is None:return None
        if type(value) not in (int,float) or not math.isfinite(value) or value<0:
            raise ValueError('Invalid timing field: '+key)
        return value
    for row in opportunities:
        if row.get('kind') not in ('skill','ultimate') or row.get('reviewed') is not True or not row.get('evidence'):continue
        if type(row.get('slot')) is not int or not 1<=row['slot']<=4:raise ValueError('Invalid skill slot')
        opened,closed=timestamp(row,'open_ms'),timestamp(row,'close_ms')
        if opened is None or closed is None or closed<=opened:raise ValueError('Invalid skill window')
        attempts=[e for e in events if e.get('opportunity_id')==row['id'] and e.get('kind')=='input' and e.get('reviewed') is True and e.get('evidence')]
        measurements=[]
        for e in attempts:
            sent=timestamp(e,'t_ms')
            if sent is None:raise ValueError('Missing skill input timestamp')
            stages={k:timestamp(e,k) for k in ('detected_ms','decision_ms','controller_complete_ms','ack_ms')}
            ordered=[v for v in (stages['detected_ms'],stages['decision_ms'],sent,stages['controller_complete_ms'],stages['ack_ms']) if v is not None]
            if ordered!=sorted(ordered):raise ValueError('Nonmonotonic timing stages')
            notes=[]
            if e.get('slot')!=row['slot']:notes.append('wrong_operator')
            if sent<opened:notes.append('before_eligible_window')
            if sent>closed:notes.append('missed_eligible_window')
            if e.get('accepted') is not True:notes.append('acceptance_unconfirmed')
            anchor=timestamp(row,'final_strike_ms')
            measurements.append({'event_id':e['id'],'eligible_to_input_ms':sent-opened,
               'remaining_window_ms':closed-sent,'final_strike_to_input_ms':sent-anchor if anchor is not None else None,
               'detect_to_decision_ms':stages['decision_ms']-stages['detected_ms'] if stages['decision_ms'] is not None and stages['detected_ms'] is not None else None,
               'decision_to_input_ms':sent-stages['decision_ms'] if stages['decision_ms'] is not None else None,
               'input_to_controller_complete_ms':stages['controller_complete_ms']-sent if stages['controller_complete_ms'] is not None else None,
               'input_to_resource_ack_ms':stages['ack_ms']-sent if stages['ack_ms'] is not None else None,
               'findings':notes})
        results.append({'id':row['id'],'kind':row['kind'],'slot':row['slot'],
           'missing_input':not attempts,'duplicate_inputs':max(0,len(attempts)-1),'measurements':measurements})
    return {'status':'reviewed_measurements' if results else 'insufficient_evidence','opportunities':results,
        'interpretation':'Eligible-window delay is not automatically avoidable: animation locks, attack-chain preservation and guide ordering may justify waiting. Resource acknowledgement is not damage landing.',
        'improvement_checks':['reduce measured detection and dispatch delay for missed windows',
          'compare casts before and after confirmed Final Strikes in matched runs',
          'check support setup before burst and avoid interrupting an unfinished attack chain',
          'compare hold versus timed clicks only after chain cadence is measured',
          'change one timing rule at a time; validate on a separate clip']}


def preflight(repo, candidate, installed, profile):
    def digest(path):
        return hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None
    raw=json.loads(profile.read_text(encoding='utf-8-sig'))
    revision=subprocess.run(['git','-C',str(repo),'rev-parse','HEAD'],capture_output=True,check=True).stdout.decode().strip()
    # Includes tracked diff plus untracked source contents; avoids falsely calling a dirty build clean.
    paths=subprocess.run(['git','-C',str(repo),'ls-files','--cached','--others','--exclude-standard'],capture_output=True,check=True).stdout.decode().splitlines()
    source=hashlib.sha256()
    for name in sorted(set(paths)):
        path=repo/name
        if path.is_file() and name.startswith(('agent/','assets/','tools/')):
            source.update(name.encode());source.update(path.read_bytes())
    candidate_hash,installed_hash=digest(candidate),digest(installed)
    missing=[k for k in raw['required_signals'] if not raw.get('signals',{}).get(k)]
    return {'source_revision':revision,'source_tree_sha256':source.hexdigest(),
      'candidate_sha256':candidate_hash,'installed_sha256':installed_hash,
      'candidate_matches_disk_install':candidate_hash is not None and candidate_hash==installed_hash,
      'running_process_identity_verified':False,'profile_sha256':digest(profile),'missing_signals':missing,
      'emulator_entry':'AdbAutoFightTest','input_mappings':'existing AutoFight controller Pipeline',
      'trace_environment_variable':'MAAEND_COMBAT_TRACE_DIR',
      'execution_ready':False,'note':'Disk match alone does not prove the running process or loaded resource version. Verify these before the emulator window.'}


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    subs=parser.add_subparsers(dest='command',required=True)
    init=subs.add_parser('init');init.add_argument('--output',type=Path,required=True);init.add_argument('--profile',type=Path,required=True)
    report=subs.add_parser('score');report.add_argument('--session',type=Path,required=True)
    pre=subs.add_parser('preflight');pre.add_argument('--repo',type=Path,required=True);pre.add_argument('--candidate',type=Path,required=True);pre.add_argument('--installed',type=Path,required=True);pre.add_argument('--profile',type=Path,required=True);pre.add_argument('--output',type=Path,required=True)
    args=parser.parse_args()
    if args.command=='preflight':
        result=preflight(args.repo,args.candidate,args.installed,args.profile);write_json(args.output,result);print(json.dumps(result))
    elif args.command=='init':print(init_session(args.output,args.profile))
    else:
        result=score(read_jsonl(args.session/'opportunities.jsonl'),read_jsonl(args.session/'events.jsonl'))
        write_json(args.session/'score.json',result);print(json.dumps(result))

if __name__=='__main__':main()
