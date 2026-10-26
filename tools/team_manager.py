"""Team recommendations, upgrade queues and shortage farming over saved account data.

This module never equips, crafts or levels an operator. The farm command emits
an override for MaaEnd's existing SupplyPlan pipeline; the Go action dispatches it.
"""
import argparse
import datetime as dt
import html
import json
import re
from pathlib import Path


GEAR_STEPS = [
    'Open the operator, select Gear, then Gear Plan at the top right.',
    'Select a saved row to preview all four items, attributes and set effects. This does not equip it.',
    'Check the item portraits for gear borrowed from another operator. Reserve each physical item once per team.',
    'Choose Apply, handle any sharing confirmation, then reopen Gear and verify all four equipped pieces.',
    'For Arcane, verify final INT >= WILL for INT form, or WILL > INT for WILL form. Check ULT Efficiency separately.',
    'To save a new plan, first equip and verify the four desired pieces, then return to Gear Plan and select Current Gear.',
    'Enter a distinct name under Save As, choose Save, then reopen the named row and compare the four items and set effects.',
    'To update a saved plan, explicitly select its overwrite control and verify the destination name. Never use overwrite or delete while merely applying a plan.',
]


def load(path):
    return json.loads(Path(path).read_text(encoding='utf-8-sig'))


def key(name):
    value = re.sub(r'[^a-z0-9]', '', name.lower())
    return {'endmin': 'endministrator'}.get(value, value)


def quantity(value):
    if type(value) is not int or not 0 <= value <= 3000000000:
        raise ValueError('Material quantities must be integers between 0 and 3000000000')
    return value


def fresh(value, now):
    try:
        age = (now - dt.datetime.fromisoformat(value)).total_seconds()
        return 0 <= age <= 86400
    except (ValueError, TypeError):
        return False


def evidence_ok(record, uid, root, now):
    if record.get('account_uid') != uid or not fresh(record.get('observed_at'), now):
        return False
    files = record.get('evidence', [])
    return bool(files) and all(isinstance(p, str) and (root / p).is_file() for p in files)


def recommend(teams, roster):
    owned = {key(o['name']): o for o in roster}
    result = []
    for team in teams:
        members = [owned.get(key(n)) for n in team['members']]
        missing = [n for n, row in zip(team['members'], members) if row is None]
        levels = [min(row.get('level', 0) / 90, 1) for row in members if row]
        weapons = [min((row.get('weapon_level') or 0) / 90, 1) for row in members if row]
        readiness = round(100 * (sum(levels) + sum(weapons)) / 8, 1)
        result.append({**team, 'missing_members': missing, 'fully_owned': not missing,
                       'level_readiness_percent': readiness,
                       'equipment_readiness': 'Saved plans and team-specific thresholds require verification'})
    result.sort(key=lambda t: (not t['fully_owned'], -t.get('guide_priority', 0),
                               -t['level_readiness_percent'], t['id']))
    return result


def read_account(root):
    path = root / 'data/TeamManagement/roster.json'
    if path.is_file():
        return load(path)
    resume = load(root / 'data/AccountScans/ui_session.json')
    snapshot = load(resume['export'])
    uid = resume.get('uid_observed')
    if not uid:
        raise ValueError('Capture account identity before recommending teams')
    roster = []
    for record in snapshot['records']:
        data = record['data']
        if record['task_id'] == 'operators' and data.get('operator_name'):
            roster.append({'name': data['operator_name'], 'level': data.get('level') or 0,
                           'weapon_level': None, 'skill_ranks': {}})
    if not roster:
        raise ValueError('Capture the operator roster before recommending teams')
    return {'account_uid': uid, 'roster': roster,
            'as_of': snapshot['session']['created_at'],
            'verification': 'Partial scan; missing entries are unconfirmed, not proof of non-ownership'}


def make_request(team, roster, uid, targets):
    owned = {key(o['name']): o for o in roster}
    upgrades = []
    for name in team['members']:
        row = owned.get(key(name), {})
        for kind, field in [('operator_level', 'level'), ('weapon_level', 'weapon_level')]:
            current = row.get(field)
            target = targets[kind]
            if current is None or current < target:
                upgrades.append({'id': key(name) + '-' + kind, 'operator': name,
                                 'kind': kind, 'from': current, 'to': target,
                                 'costs': None, 'account_uid': uid,
                                 'observed_at': None, 'evidence': []})
        # Skill targets are per skill; the guide priority is shown in the report.
        for skill in ['basic', 'battle', 'combo', 'ultimate']:
            current = row.get('skill_ranks', {}).get(skill)
            if current is None or current < targets['skill_rank']:
                upgrades.append({'id': key(name) + '-skill-' + skill, 'operator': name,
                                 'kind': 'skill_' + skill, 'from': current,
                                 'to': targets['skill_rank'], 'costs': None,
                                 'account_uid': uid, 'observed_at': None, 'evidence': []})
            mastery = targets.get('mastery_rank', 0)
            if mastery:
                current_mastery = row.get('mastery_ranks', {}).get(skill)
                if current_mastery is None or current_mastery < mastery:
                    upgrades.append({'id': key(name) + '-mastery-' + skill, 'operator': name,
                                     'kind': 'mastery_' + skill, 'from': current_mastery,
                                     'to': mastery, 'costs': None, 'account_uid': uid,
                                     'observed_at': None, 'evidence': []})
    return {'schema_version': 1, 'team_id': team['id'], 'account_uid': uid,
            'targets': targets, 'upgrades': upgrades, 'gear_plans': [],
            'inventory': {}, 'reserves': {}, 'current_state': {},
            'state_observed_at': None, 'state_evidence': []}


def build_upgrade_plan(team, roster, request, resources, root, now=None):
    now = now or dt.datetime.now(dt.timezone.utc)
    blockers, totals, queue, used, ids, intervals = [], {}, [], {}, set(), {}
    uid = request.get('account_uid')
    if not uid:
        blockers.append('Verify account UID')
    if request.get('team_id') != team['id']:
        blockers.append('Request does not match the selected team')
    member_keys = {key(n) for n in team['members']}
    owned = {key(row['name']): row for row in roster}
    for name in team['members']:
        if key(name) not in owned:
            blockers.append('Operator is not in saved roster: ' + name)
    state = request.get('current_state', {})
    state_proof = {'account_uid': uid, 'observed_at': request.get('state_observed_at'),
                   'evidence': request.get('state_evidence')}
    if not evidence_ok(state_proof, uid, root, now):
        blockers.append('Capture fresh current operator, weapon and skill levels for this account')
    for step in request.get('upgrades', []):
        ident = step.get('id')
        if not ident or ident in ids:
            raise ValueError('Every upgrade must have a unique ID')
        ids.add(ident)
        name, kind = step.get('operator', ''), step.get('kind', '')
        if key(name) not in member_keys or kind not in {
                'operator_level', 'weapon_level', 'skill_basic', 'skill_battle',
                'skill_combo', 'skill_ultimate', 'mastery_basic', 'mastery_battle',
                'mastery_combo', 'mastery_ultimate', 'promotion', 'gear_craft', 'artifice'}:
            raise ValueError('Upgrade operator or kind is not valid for the selected team')
        before, after = step.get('from'), step.get('to')
        interval_key = (key(name), kind)
        if kind not in {'gear_craft', 'artifice', 'promotion'}:
            if type(before) is not int or type(after) is not int or after <= before:
                blockers.append('Read exact before/after levels: ' + ident)
            else:
                for left, right in intervals.get(interval_key, []):
                    if max(left, before) < min(right, after):
                        raise ValueError('Overlapping upgrade costs: ' + ident)
                intervals.setdefault(interval_key, []).append((before, after))
        costs = step.get('costs')
        valid = step.get('reviewed') is True and evidence_ok(step, uid, root, now) and isinstance(costs, dict) and bool(costs)
        if not valid:
            blockers.append('Capture exact fresh costs and evidence: ' + ident)
        else:
            for resource, amount in costs.items():
                totals[resource] = quantity(totals.get(resource, 0) + quantity(amount))
        queue.append({**step, 'state': 'costed' if valid else 'needs_cost_capture',
                      'execution': 'needs_verified_upgrade_navigation'})
    # A request may not silently omit an upgrade needed to reach its targets.
    for name in team['members']:
        current = state.get(name, {})
        for kind in ['operator_level', 'weapon_level', 'skill_basic', 'skill_battle', 'skill_combo', 'skill_ultimate',
                     'mastery_basic', 'mastery_battle', 'mastery_combo', 'mastery_ultimate']:
            mastery = kind.startswith('mastery_')
            target = request.get('targets', {}).get('mastery_rank', 0) if mastery else request.get('targets', {}).get('skill_rank' if kind.startswith('skill_') else kind)
            if mastery and target == 0:
                continue
            start = current.get(kind)
            if type(target) is not int or not 1 <= target <= (3 if mastery else 9 if kind.startswith('skill_') else 90):
                raise ValueError('Specify valid operator, weapon and skill targets')
            if mastery and current.get(kind.replace('mastery_', 'skill_')) != 9:
                blockers.append('Mastery requires normal skill rank 9: ' + name + '/' + kind)
            if type(start) is not int:
                blockers.append('Read current ' + kind + ': ' + name)
                continue
            cursor = start
            for left, right in sorted(intervals.get((key(name), kind), [])):
                if left != cursor:
                    blockers.append('Cost sequence differs from current state: ' + name + '/' + kind)
                    break
                cursor = right
            if cursor < target:
                blockers.append('Complete cost sequence to target: ' + name + '/' + kind)
            if cursor > max(start, target):
                blockers.append('Upgrade sequence exceeds selected target: ' + name + '/' + kind)
    for plan in request.get('gear_plans', []):
        name = plan.get('operator', '')
        if key(name) not in member_keys:
            raise ValueError('Gear plan operator is outside the selected team')
        item_ids = plan.get('physical_item_ids', [])
        if len(item_ids) != 4 or len(set(item_ids)) != 4:
            blockers.append('Verify all four distinct physical items: ' + name)
        for item in item_ids:
            if item in used:
                blockers.append('Shared gear conflict: ' + str(item) + ' / ' + used[item] + ' / ' + name)
            used[item] = name
        if not evidence_ok(plan, uid, root, now):
            blockers.append('Verify saved plan and item ownership: ' + name)
        if key(name) == 'arcane':
            attributes = plan.get('attributes', {})
            intellect, will = attributes.get('intellect'), attributes.get('will')
            form = plan.get('required_form')
            if (type(intellect) is not int or type(will) is not int or
                    form not in {'INT', 'WILL'} or
                    (form == 'INT' and intellect < will) or
                    (form == 'WILL' and will <= intellect)):
                blockers.append('Verify Arcane attributes match the selected INT/WILL role')
    shortages, targets, unsupported = [], {}, []
    for resource, needed in totals.items():
        reserve = quantity(request.get('reserves', {}).get(resource, 0))
        target = quantity(needed + reserve)
        inventory = request.get('inventory', {}).get(resource, {})
        balance = inventory.get('quantity') if evidence_ok(inventory, uid, root, now) else None
        if balance is not None:
            balance = quantity(balance)
        shortages.append({'resource': resource, 'required': needed, 'reserve': reserve,
                          'owned': balance, 'missing': None if balance is None else max(0, target - balance),
                          'inventory_target': target, 'farm_supported': resource in resources})
        if resource in resources and target:
            targets[resource] = target
        elif balance is None or balance < target:
            unsupported.append(resource)
    # SupplyPlan scans live stock. Its thresholds are total needed + reserves,
    # never the shortage count (which would underfarm when stock is already held).
    patch = {'SyncItemDataBegin': {'enabled': True},
             'SupplyPlanDispatch': {'next': ['SupplyPlanInsufficient_' + r for r in targets] + ['SupplyPlanAllTargetsMet']}}
    for resource, target in targets.items():
        expr = resources[resource]['inventory_expression']
        for node, comparison in [('SupplyPlanInsufficient_', '<'), ('SupplyPlanRewardItemMet_', '>=')]:
            patch[node + resource] = {'recognition': {'param': {'custom_recognition_param': {
                'expression': expr + comparison + str(target)}}}}
    blockers = list(dict.fromkeys(blockers))
    return {'schema_version': 1, 'team_id': team['id'], 'members': team['members'],
            'created_at': now.isoformat(), 'account_uid': uid, 'upgrades': queue,
            'gear_plans': request.get('gear_plans', []), 'gear_plan_steps': GEAR_STEPS,
            'resource_totals': totals, 'shortages': shortages, 'farm_inventory_targets': targets,
            'other_acquisition_needed': unsupported,
            'farm_ready': not blockers and bool(targets), 'blockers': blockers,
            'farm_pipeline_override': patch if not blockers and targets else None,
            'upgrade_execution_enabled': False, 'game_changes_applied': False}


def render_report(report):
    esc = lambda value: html.escape(str(value))
    parts = ['<!doctype html><meta charset="utf-8"><title>Team management</title>',
             '<style>body{font:16px system-ui;max-width:1000px;margin:32px auto;padding:0 20px;line-height:1.5}li{margin:8px 0}details{margin:16px 0}code{overflow-wrap:anywhere}</style>',
             '<h1>Team management</h1><p>Recommendations are best fits from the saved guides. Level readiness is not a damage ranking. Historical account data needs verification before spending.</p>']
    for team in report['recommendations']:
        parts.append('<details><summary>' + esc(team['name']) + ' — ' + esc(' / '.join(team['members'])) + '</summary><p>' + esc('Missing: ' + ', '.join(team['missing_members']) if team['missing_members'] else 'All members found in saved roster') + '</p><p>Level readiness: ' + esc(team['level_readiness_percent']) + '%</p>')
        parts.extend('<a href="' + esc(url) + '">Build guide</a> ' for url in team.get('sources', []))
        parts.append('</details>')
    if report.get('plan'):
        plan = report['plan']
        parts += ['<h2>Selected team upgrade plan</h2><p>' + esc(' / '.join(plan['members'])) + '</p>',
                  '<p>Character upgrades require verified game navigation; this tool prepares the queue and farming targets.</p>', '<h3>Materials</h3><ul>']
        parts.extend('<li>' + esc(s['resource']) + ': need ' + esc(s['required']) + ', reserved ' + esc(s['reserve']) + ', owned ' + esc(s['owned'] if s['owned'] is not None else 'unknown') + ', missing ' + esc(s['missing'] if s['missing'] is not None else 'unknown') + '</li>' for s in plan['shortages'])
        parts += ['</ul><h3>Upgrade queue</h3><ul>']
        parts.extend('<li>' + esc(s['operator'] + ': ' + s['kind'] + ' ' + str(s['from']) + ' → ' + str(s['to']) + ' — ' + s['state']) + '</li>' for s in plan['upgrades'])
        parts += ['</ul><h3>Checks needed</h3><ul>']
        parts.extend('<li>' + esc(b) + '</li>' for b in plan['blockers'])
        parts.extend('<li>Another acquisition source is needed: ' + esc(r) + '</li>' for r in plan['other_acquisition_needed'])
        parts += ['</ul><p>Request file: <code>' + esc(report['request_file']) + '</code></p>']
        parts += ['<h3>Member build references</h3>']
        for name, build in report.get('selected_builds', {}).items():
            parts += ['<details><summary>' + esc(name) + '</summary><p>Guide alternatives; ownership and team-specific thresholds require checking.</p><ul>']
            parts.extend('<li>Weapon: ' + esc(w['name']) + ' (Potential ' + esc(w.get('weapon_potential', 'unknown')) + ')</li>' for w in build.get('weapon_recommendations', []))
            parts.extend('<li>Gear: ' + esc(g.get('set', '')) + ' — ' + esc(' / '.join(g.get('equipment', {}).values())) + '</li>' for g in build.get('gear_loadouts', []))
            parts.extend('<li>Skill priority: ' + esc(p.get('order', p)) + '</li>' for p in build.get('skill_upgrade_priorities', []))
            parts += ['</ul></details>']
    parts += ['<h2>Use and save gear plans</h2><ol>']
    parts.extend('<li>' + esc(step) + '</li>' for step in GEAR_STEPS)
    parts += ['</ol>']
    return '\n'.join(parts)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path.cwd())
    parser.add_argument('--mode', choices=['recommend', 'plan', 'farm'], default='recommend')
    parser.add_argument('--team', default='moba-si-dps')
    parser.add_argument('--level', type=int, default=90)
    parser.add_argument('--weapon', type=int, default=90)
    parser.add_argument('--skill', type=int, default=9)
    parser.add_argument('--mastery', type=int, default=0)
    args = parser.parse_args()
    root = args.root.resolve()
    library = load(root / 'data/TeamManagement/library.json')
    account = read_account(root)
    result = {'recommendations': recommend(library['teams'], account['roster']),
              'account_as_of': account.get('as_of'), 'mode': args.mode}
    folder = root / 'data/TeamManagement'
    folder.mkdir(parents=True, exist_ok=True)
    if args.mode != 'recommend':
        team = next((t for t in library['teams'] if t['id'] == args.team), None)
        if team is None or not re.fullmatch(r'[a-z0-9-]+', args.team):
            raise ValueError('Choose a team from the saved guide catalog')
        targets = {'operator_level': args.level, 'weapon_level': args.weapon, 'skill_rank': args.skill, 'mastery_rank': args.mastery}
        if not 1 <= args.level <= 90 or not 1 <= args.weapon <= 90 or not 1 <= args.skill <= 9 or not 0 <= args.mastery <= 3:
            raise ValueError('Upgrade targets are outside supported ranges')
        request_path = folder / 'requests' / (args.team + '.json')
        request_path.parent.mkdir(parents=True, exist_ok=True)
        if not request_path.exists():
            request_path.write_text(json.dumps(make_request(team, account['roster'], account['account_uid'], targets), indent=4) + '\n', encoding='utf-8')
        request = load(request_path)
        request.setdefault('targets', {}).setdefault('mastery_rank', 0)
        if request.get('account_uid') != account['account_uid']:
            raise ValueError('Build request belongs to another account')
        if request.get('targets') != targets:
            raise ValueError('Saved request has different targets; review its costs before changing targets')
        result['plan'] = build_upgrade_plan(team, account['roster'], request, library['farm_resources'], root)
        result['selected_builds'] = {name: next((b for n, b in library['characters'].items() if key(n) == key(name)), {}) for name in team['members']}
        result['request_file'] = str(request_path)
    output = folder / ('recommendations.json' if args.mode == 'recommend' else args.team + '-plan.json')
    output.write_text(json.dumps(result, indent=4, ensure_ascii=False) + '\n', encoding='utf-8')
    output.with_suffix('.html').write_text(render_report(result), encoding='utf-8')
    print(json.dumps({'report': str(output.with_suffix('.html')), 'mode': args.mode,
                      'farm_ready': result.get('plan', {}).get('farm_ready', False),
                      'blockers': len(result.get('plan', {}).get('blockers', [])),
                      'pipeline_override': result.get('plan', {}).get('farm_pipeline_override')}))


if __name__ == '__main__':
    main()
