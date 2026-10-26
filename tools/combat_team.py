from rotation_readiness import build_rotation_readiness, render_rotation_readiness
"""Build a team-wide gear preview and combat readiness report; never controls the game."""
import argparse
import copy
import datetime as dt
import hashlib
import html
import json
from pathlib import Path

from account_scan import DEFAULT_STORE, Store
from gear_workflow import build_plan, cost_map, verified_snapshot

PROFILE = Path(__file__).resolve().parents[1] / 'assets/data/CombatTeams/heat-si.json'


def fresh_reviewed(record, now):
    try:
        age = (now - dt.datetime.fromisoformat(record['observed_at'])).total_seconds()
        return record['reviewed'] and bool(record['evidence_ids']) and 0 <= age <= 86400
    except (KeyError, TypeError, ValueError):
        return False


def build_team_plan(snapshot, profile, spend_limits=None, requests=None, now=None):
    now = now or dt.datetime.now(dt.timezone.utc)
    members = profile['members']
    if len(members) != 4 or len(set(members)) != 4 or not 1 <= profile['driver_slot'] <= 4:
        raise ValueError('A team requires four unique operators and a valid driver slot')
    budget = cost_map(spend_limits or {})
    requests = requests or {}
    blockers, plans, totals, reserved = [], {}, {}, set()
    working = copy.deepcopy(snapshot)
    rows = [r for r in snapshot['records'] if fresh_reviewed(r, now)]
    def block(text):
        if text not in blockers:
            blockers.append(text)
    for name in members:
        levels = {r['data'].get('level') for r in rows
                  if r['task_id'] == 'operators' and r['data'].get('operator_name') == name}
        if len(levels) != 1 or not all(type(v) is int and v >= profile['minimum_level'] for v in levels):
            block('Verify level >= ' + str(profile['minimum_level']) + ': ' + name)
        request = copy.deepcopy(profile['gear_requests'][name])
        override = requests.get(name, {})
        if set(override) - {'gear', 'artifice_attempts', 'donor_allowlist', 'overwrite_plan_id'}:
            raise ValueError('Unsupported operator request override: ' + name)
        request.update(override)
        request['spend_limits'] = budget
        plan = build_plan(working, request, now)
        plans[name] = plan
        for message in plan['blockers']:
            block(name + ': ' + message)
        for resource, quantity in plan['resource_totals'].items():
            totals[resource] = totals.get(resource, 0) + quantity
        # A single unequipped physical item cannot satisfy two operators or become a donor later.
        used = {v['item_id'] for v in plan['bindings'].values() if 'item_id' in v}
        used |= {s['donor_item_id'] for s in plan['steps'] if s['kind'] == 'artifice_once'}
        reserved |= used
        working['records'] = [r for r in working['records']
                              if r['task_id'] != 'gear_inventory' or r['data'].get('item_id') not in reserved]
        equipped = [r['data'] for r in rows if r['task_id'] == 'equipped_gear'
                    and r['data'].get('operator_name') == name and r['data'].get('equipped') is True]
        for slot, target in request['gear'].items():
            matches = [g for g in equipped if g.get('slot') == slot]
            binding = plan['bindings'].get(slot, {})
            if (len(matches) != 1 or matches[0].get('item_name') != target['item_name']
                    or not binding.get('item_id') or matches[0].get('item_id') != binding['item_id']):
                block('Verify equipped target ' + name + '/' + slot)
    for resource, quantity in totals.items():
        balances = [r['data'].get('quantity') for r in rows if r['task_id'] == 'craft_materials'
                    and r['data'].get('item_name') == resource]
        if len(balances) != 1 or type(balances[0]) is not int or balances[0] < quantity:
            block('Team total exceeds verified balance: ' + resource)
        if budget.get(resource, 0) < quantity:
            block('Team total exceeds spend limit: ' + resource)
    formation = [r['data'] for r in rows if r['task_id'] == 'team_presets'
                 and r['data'].get('members') == members]
    if len(formation) != 1 or not any(t['task_id'] == 'team_presets' and t['status'] == 'reviewed' for t in snapshot['tasks']):
        block('Verify exact team slot order: ' + ' / '.join(members))
    missing = [s for s in profile['required_signals'] if not profile['signals'].get(s)]
    if missing:
        block('Calibrate combat HUD signals: ' + ', '.join(missing))
    return {'schema_version': 1, 'profile_id': profile['id'], 'created_at': now.isoformat(),
            'session_id': snapshot['session']['id'], 'members': members,
            'driver': members[profile['driver_slot'] - 1], 'status': 'blocked' if blockers else 'ready',
            'combat_ready': not blockers, 'game_changes_applied': False, 'gear_execution_enabled': False,
            'plans': plans, 'resource_totals': totals, 'spend_limits': budget,
            'reserved_physical_items': sorted(reserved), 'blockers': blockers,
            'missing_signals': missing, 'capture_tasks': profile['capture_tasks'],
            'investment_targets': profile['gear_policy'],
            'weapon_upgrade_status': 'Capture exact weapon, essence and observed upgrade costs before planning spending',
            'support_rotation': profile['rules']}


def render_report(report, profile):
    esc = lambda s: html.escape(str(s))
    parts = ['<!doctype html><meta charset="utf-8"><title>Heat Si team readiness</title>',
             '<h1>' + esc(profile['name']) + '</h1>', '<p>On-field carry: ' + esc(report['driver']) +
             '. Combat readiness: ' + esc(report['status']) + '. No game changes applied.</p>',
             '<h2>Preparation order</h2><ol><li>Fill Si’s four empty slots from verified owned gear or costed recipes.</li><li>Review Si’s weapon at level 74 against the level-90 target; capture upgrade costs.</li><li>Verify the other three operators’ exact gear, essences, and support energy requirements.</li><li>Capture and validate combat states before enabling the team policy.</li></ol>',
             '<details><summary>Detailed readiness checks (' + str(len(report['blockers'])) + ' outstanding)</summary><ul>']
    parts += ['<li>' + esc(b) + '</li>' for b in report['blockers']]
    parts += ['</ul></details>']
    if report.get('previous_visual_review'):
        parts += ['<h2>Previous visual review (not readiness proof)</h2><ul>']
        parts += ['<li>' + esc(str(r)) + '</li>' for r in report['previous_visual_review']['operators']]
        parts += ['</ul>']
    parts += ['<h2>Gear and character targets</h2>']
    for member in profile['characters']:
        name = member['name']; target = profile['gear_requests'][name]
        parts += ['<h3>' + esc(name) + '</h3><p>' + esc(member['role']) + '</p>',
                  '<p>Weapon guide target: ' + esc(member['guide_target_weapon']) +
                  '. Ownership and essence still require verification.</p><ul>']
        parts += ['<li>' + esc(slot + ': ' + item['item_name']) + '</li>' for slot, item in target['gear'].items()]
        parts += ['</ul><p>Skill investment: ' + esc(member['skill_investment']) + '</p>',
                  '<a href="' + esc(member['source_url']) + '">Character guide</a>']
    parts += ['<h2>Conditional combat rules</h2><ol>']
    parts += ['<li>' + esc(r['id'] + ': ' + r['action'] + ' slot ' + str(r['slot']) +
                           '; requires ' + ', '.join(r['requires'])) + '</li>' for r in profile['rules']]
    parts += ['</ol><p>This is an original policy for the user-selected lineup, not an exact published timeline or a measured optimum.</p>']
    return '\n'.join(parts)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--profile', type=Path, default=PROFILE)
    parser.add_argument('--store', type=Path, default=DEFAULT_STORE)
    parser.add_argument('--session')
    parser.add_argument('--request', type=Path)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    profile_bytes = args.profile.read_bytes()
    profile = json.loads(profile_bytes.decode('utf-8-sig'))
    session = args.session or json.loads((args.store / 'ui_session.json').read_text('utf-8-sig'))['session_id']
    store = Store(args.store)
    try:
        snapshot = verified_snapshot(store, session)
    finally:
        store.close()
    request = json.loads(args.request.read_text('utf-8-sig')) if args.request else {}
    report = build_rotation_readiness(snapshot, profile) if profile.get('rotation') else build_team_plan(snapshot, profile, request.get('spend_limits'), request.get('operators'))
    report['profile_sha256'] = hashlib.sha256(profile_bytes).hexdigest()
    review = args.store / 'analysis' / 'build-review-20261018.json'
    if review.is_file():
        prior = json.loads(review.read_text('utf-8-sig'))
        report['previous_visual_review'] = {'as_of': prior['as_of'], 'not_execution_evidence': True,
            'operators': [{k: r[k] for k in ('name', 'level', 'weapon_level', 'gear_slots_present')}
                          for r in prior['roster'] if r['name'] in profile['members']]}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=4) + '\n', encoding='utf8')
    args.output.with_suffix('.html').write_text((render_rotation_readiness(report, profile) if profile.get('rotation') else render_report(report, profile)), encoding='utf8')
    print(json.dumps({'status': report['status'], 'combat_ready': report['combat_ready'],
                      'blockers': len(report['blockers']), 'output': str(args.output)}))


if __name__ == '__main__':
    main()