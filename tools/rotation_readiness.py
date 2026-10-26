"""Evidence-based prerequisites for sourced combat rotations; no game inputs."""
import datetime as dt
import html


def build_rotation_readiness(snapshot, profile, now=None):
    now=now or dt.datetime.now(dt.timezone.utc)
    blockers=[]
    def block(message):
        if message not in blockers:blockers.append(message)
    rows=[]
    for row in snapshot.get('records',[]):
        try:
            age=(now-dt.datetime.fromisoformat(row['observed_at'])).total_seconds()
            if row.get('reviewed') is True and row.get('evidence_ids') and 0<=age<=86400:rows.append(row)
        except (KeyError,ValueError,TypeError):pass
    members=profile['members']
    if len(members)!=4 or len(set(members))!=4:raise ValueError('Four unique team members are required')
    if not any(r['task_id']=='account_identity' and r['data'].get('uid') and r['data'].get('server') for r in rows):block('Verify current account UID and server')
    if not any(r['task_id']=='team_presets' and r['data'].get('members')==members for r in rows):block('Verify exact team slot order: '+' / '.join(members))
    def values(name,field):
        found=[]
        for row in rows:
            data=row['data']
            if row['task_id']!='operators' or data.get('operator_name')!=name:continue
            for key in field.split('.'):
                if not isinstance(data,dict) or key not in data:break
                data=data[key]
            else:
                if data not in found:found.append(data)
        return found
    for name in members:
        level=values(name,'level')
        if len(level)!=1 or type(level[0]) is not int or level[0]<profile.get('minimum_level',1):block('Verify operator level: '+name)
        if not any(r['task_id']=='skills' and r['data'].get('operator_name')==name for r in rows):block('Review skill ranks and unlocked talents: '+name)
    for requirement in profile.get('build_requirements',[]):
        name,field=requirement['operator'],requirement['field'];observed=values(name,field)
        if len(observed)!=1:
            block('Verify '+name+'/'+field);continue
        value=observed[0]
        if 'equals' in requirement:
            if value!=requirement['equals']:block('Guide build mismatch: '+name+'/'+field)
            continue
        if type(value) not in (int,float):block('Verify numeric stat: '+name+'/'+field);continue
        if 'minimum' in requirement and value<requirement['minimum']:block('Guide breakpoint not met: '+name+'/'+field)
        for relation in ['gte_field','gt_field']:
            if relation not in requirement:continue
            other=values(name,requirement[relation])
            if len(other)!=1 or type(other[0]) not in (int,float):block('Verify '+name+'/'+requirement[relation]);continue
            if not (value>=other[0] if relation=='gte_field' else value>other[0]):block('Arcane attribute form differs from guide: '+name)
    missing=[key for key in profile['required_signals'] if not profile['signals'].get(key)]
    if missing:block('Calibrate HUD signals: '+', '.join(missing))
    return {'schema_version':2,'profile_id':profile['id'],'created_at':now.isoformat(),'session_id':snapshot['session']['id'],
            'members':members,'status':'blocked' if blockers else 'ready','combat_ready':not blockers,
            'blockers':blockers,'missing_signals':missing,'rotation':profile['rotation'],
            'guide_requirements':profile.get('build_requirements',[]),'game_changes_applied':False}


def render_rotation_readiness(report, profile):
    esc=lambda value:html.escape(str(value))
    parts=['<!doctype html><meta charset="utf-8"><title>Guide rotation readiness</title>',
           '<h1>'+esc(profile['name'])+'</h1>','<p>'+esc(report['status'])+'</p>',
           '<p><a href="'+esc(profile['rotation']['source_url'])+'">Build guide</a> — '+esc(profile['rotation']['source_updated'])+'</p>',
           '<p>The same rotation serves RTA and automated combat. Source timings are conditional on build and observed combat state.</p>',
           '<ul>']
    parts += ['<li>'+esc(message)+'</li>' for message in report['blockers']]
    parts += ['</ul><ol>']
    parts += ['<li>'+esc(step['id'])+'; wait for '+esc(', '.join(step['requires']))+'</li>' for step in profile['rotation']['steps']]
    parts += ['</ol><p>Unconfirmed inputs pause the rotation; no blind skill retries. Preview mode proposes rotation actions without casting them.</p>']
    return '\n'.join(parts)
