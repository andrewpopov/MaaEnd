"""Replay OCR/template combat signal definitions on labelled screenshot fixtures.

A binding JSON has profile_id and signals; each signal includes recognition,
positive image paths and negative image paths. The tool sends no game inputs.
It writes a bound profile and Pipeline nodes only when every replay passes.
"""
import argparse
import dataclasses
import hashlib
import json
from pathlib import Path


def validate_binding(binding):
    if not isinstance(binding,dict) or not isinstance(binding.get('signals'),dict) or not binding.get('signals'):raise ValueError('Signal bindings are required')
    for key,spec in binding['signals'].items():
        if not key or not spec.get('positive') or not spec.get('negative'):raise ValueError('Each signal needs positive and negative screenshots: '+key)
        recognition=spec.get('recognition',{})
        if recognition.get('type') not in ('OCR','TemplateMatch','ColorMatch','And','Or'):raise ValueError('Unsupported screenshot recognizer: '+key)
        if not isinstance(recognition.get('param'),dict):raise ValueError('Recognition parameters are required')


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--profile',type=Path,required=True)
    parser.add_argument('--bindings',type=Path,required=True)
    parser.add_argument('--resource',type=Path,required=True)
    parser.add_argument('--maafw',type=Path,required=True)
    parser.add_argument('--output',type=Path,required=True)
    args=parser.parse_args()
    binding=json.loads(args.bindings.read_text(encoding='utf-8'));validate_binding(binding)
    profile=json.loads(args.profile.read_text(encoding='utf-8'))
    if profile['id']!=binding['profile_id']:raise ValueError('Profile identity differs')
    if set(binding['signals'])-set(profile['required_signals']):raise ValueError('Undeclared signal binding')
    from maa.library import Library
    from maa.resource import Resource
    from maa.tasker import Tasker
    from maa.pipeline import JOCR,JTemplateMatch,JColorMatch,JAnd,JOr
    import numpy as np
    from PIL import Image
    Library.open(args.maafw);resource=Resource();resource.use_cpu()
    job=resource.post_bundle(args.resource).wait()
    if not job.succeeded:raise RuntimeError('Resource load failed')
    tasker=Tasker();Library.framework().MaaTaskerBindResource(tasker._handle,resource._handle)
    kinds={'OCR':JOCR,'TemplateMatch':JTemplateMatch,'ColorMatch':JColorMatch,'And':JAnd,'Or':JOr}
    nodes={};results=[]
    for key,spec in binding['signals'].items():
        rec=spec['recognition'];node='GuideRotation'+''.join(part.title() for part in (profile['id']+'_'+key).replace('-','_').split('_'))
        for label,expected in [('positive',True),('negative',False)]:
            for filename in spec[label]:
                path=(args.bindings.parent/filename).resolve();image=Image.open(path).convert('RGB')
                if image.size!=(1280,720):raise ValueError('Capture must be 1280x720: '+str(path))
                detail=tasker.post_recognition(rec['type'],kinds[rec['type']](**rec['param']),np.asarray(image)[:,:,::-1].copy()).wait().get()
                hit=detail.nodes[0].recognition.hit
                results.append({'signal':key,'image':str(path),'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'expected':expected,'hit':hit,'passed':hit==expected})
        nodes[node]={'recognition':rec};profile['signals'][key]=node
    args.output.mkdir(parents=True,exist_ok=True)
    (args.output/'replay.json').write_text(json.dumps(results,indent=4)+'\n')
    if not all(result['passed'] for result in results):raise RuntimeError('Calibration replay failed; no profile or nodes written')
    profile['calibration']={'source_bindings_sha256':hashlib.sha256(args.bindings.read_bytes()).hexdigest(),'fixture_count':len(results),'remaining_signals':[key for key in profile['required_signals'] if not profile['signals'].get(key)]}
    (args.output/(profile['id']+'.json')).write_text(json.dumps(profile,indent=4)+'\n')
    (args.output/'Calibration.json').write_text(json.dumps(nodes,indent=4)+'\n')
    print(json.dumps({'profile':profile['id'],'passed':len(results),'remaining_signals':len(profile['calibration']['remaining_signals'])}))


if __name__=='__main__':main()
