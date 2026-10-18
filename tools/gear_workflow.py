"""Offline craft/equip/artifice planner. No controller input or spending.

Costs are explicit item-name -> integer maps. Unknown costs never mean free.
Plans describe one observed artifice attempt, never guaranteed level gains.
"""
import argparse
import datetime as dt
import hashlib
import json
from pathlib import Path

from account_scan import DEFAULT_STORE, Store


def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False).encode()).hexdigest()


def count(value):
    if type(value) is not int or value < 0:
        raise ValueError('Quantity/level must be a known nonnegative integer')
    return value


def cost_map(value):
    if not isinstance(value, dict) or any(not isinstance(k, str) or not k for k in value):
        raise ValueError('Cost must be an explicit resource-name to quantity object')
    return {name: count(amount) for name, amount in value.items()}


def build_plan(snapshot, request, now=None):
    """Pure preview over a reviewed export; caller verifies stored evidence files."""
    now = now or dt.datetime.now(dt.timezone.utc)
    blockers, steps, totals, selected = [], [], {}, set()
    session = snapshot['session']['id']
    result = {
        'schema_version': 1, 'session_id': session,
        'input_fingerprint': fingerprint({'scan': snapshot, 'request': request}),
        'created_at': now.isoformat(), 'operator': request.get('operator'),
        'loadout_name': request.get('loadout_name'), 'steps': steps,
        'resource_totals': totals, 'blockers': blockers,
        'execution_enabled': False, 'game_changes_applied': False,
        'navigation_status': 'emulator_screen_validation_pending',
        'scope': 'gear crafting, four-slot equipment, named loadout, single artifice attempts',
        'pending_paths': ['weapon leveling/promotion', 'skill upgrades', 'live game execution'],
    }
    def block(message):
        if message not in blockers:
            blockers.append(message)
    tasks = {t['task_id']: t['status'] for t in snapshot['tasks']}
    checked = set()
    def records(task):
        if task not in checked:
            checked.add(task)
            if tasks.get(task) != 'reviewed':
                block('Review complete scan coverage: ' + task)
        rows = [r for r in snapshot['records'] if r['task_id'] == task]
        valid = []
        for row in rows:
            try:
                age = (now - dt.datetime.fromisoformat(row['observed_at'])).total_seconds()
                if not row['reviewed'] or not row.get('evidence_ids') or not 0 <= age <= 86400:
                    raise ValueError()
            except (KeyError, TypeError, ValueError):
                block('Review fresh evidence: ' + task + '/' + row['entity'])
                continue
            valid.append(row['data'])
        return valid
    def step(kind, **details):
        item = {'id': 'step-' + str(len(steps) + 1), 'kind': kind,
                'state': 'planned', **details}
        steps.append(item)
        return item['id']
    def charge(costs):
        for name, amount in costs.items():
            totals[name] = totals.get(name, 0) + amount
    identity = records('account_identity')
    if len(identity) != 1 or any(not identity[0].get(k) for k in ('uid', 'server')):
        block('Verify exactly one account UID and server')
    else:
        result['account'] = {k: identity[0][k] for k in ('uid', 'server')}
    operators = records('operators')
    operator = request.get('operator')
    if not operator or not any(o.get('operator_name') == operator for o in operators):
        block('Verify operator ownership: ' + str(operator))
    inventory = records('gear_inventory')
    records('equipped_gear')
    plans = records('gear_plans')
    by_id = {}
    for item in inventory:
        item_id = item.get('item_id')
        if not item_id or item_id in by_id or type(item.get('quantity')) is not int or item.get('quantity') != 1:
            block('Gear inventory needs unique physical item IDs with quantity 1')
            continue
        by_id[item_id] = item
    wanted = request.get('gear', {})
    if set(wanted) != {'armor', 'gloves', 'kit_1', 'kit_2'}:
        block('Specify all four gear slots')
    bindings = {}
    crafted_surplus = {}
    for slot, target in wanted.items():
        if slot not in {'armor', 'gloves', 'kit_1', 'kit_2'} or not isinstance(target, dict):
            block('Invalid slot request: ' + slot)
            continue
        item_name = target.get('item_name')
        item_slot = 'kit' if slot.startswith('kit_') else slot
        matches = [i for i in by_id.values() if i.get('item_name') == item_name
                   and i.get('slot') == item_slot and i['item_id'] not in selected
                   and i.get('equipped_by') in ('', None, operator)
                   and 'equipped_by' in i
                   and (not target.get('item_id') or target['item_id'] == i['item_id'])]
        # Reuse this operator's current gear first, then require an unambiguous copy.
        own = [i for i in matches if i.get('equipped_by') == operator]
        if len(own) == 1:
            matches = own
        if len(matches) == 1:
            item = matches[0]
            selected.add(item['item_id'])
            bindings[slot] = {'item_id': item['item_id']}
        elif len(matches) > 1:
            block('Choose exact owned copy for ' + slot + ': ' + str(item_name))
        elif target.get('item_id'):
            block('Requested item unavailable: ' + target['item_id'])
        elif not item_name:
            block('Missing item name: ' + slot)
        else:
            pool_key = (item_name, item_slot, target.get('recipe_id'))
            pool = crafted_surplus.get(pool_key)
            if pool and pool['remaining']:
                bindings[slot] = {'crafted_by': pool['step_id'], 'output_index': pool['next_index']}
                pool['remaining'] -= 1
                pool['next_index'] += 1
                continue
            recipes = [r for r in records('gear_recipes')
                       if r.get('item_name') == item_name and r.get('slot') == item_slot
                       and r.get('unlocked') is True
                       and (not target.get('recipe_id') or r.get('recipe_id') == target['recipe_id'])]
            if len(recipes) != 1:
                block('Choose one verified unlocked recipe for ' + slot + ': ' + item_name)
                continue
            recipe = recipes[0]
            try:
                # One final piece per slot; surplus batch output is explicit, not auto-equipped.
                batch = count(recipe['batch_size'])
                if not batch or not recipe.get('recipe_id') or not recipe.get('technique'):
                    raise ValueError('Incomplete recipe identity/technique')
                costs = cost_map(recipe['materials'])
                for name, amount in cost_map(recipe['currency_cost']).items():
                    costs[name] = costs.get(name, 0) + amount
                charge(costs)
                craft_id = step('craft', recipe_id=recipe['recipe_id'], technique=recipe['technique'],
                                item_name=item_name, batches=1, output_quantity=batch, costs=costs,
                                verify='Observe inventory delta and bind a new physical item ID; stop on uncertain result')
                bindings[slot] = {'crafted_by': craft_id, 'output_index': 0}
                crafted_surplus[pool_key] = {'step_id': craft_id, 'remaining': batch - 1, 'next_index': 1}
            except (KeyError, ValueError, TypeError) as error:
                block('Read complete recipe costs for ' + item_name + ': ' + str(error))
    # Bind every final item before allowing donor selection.
    final_ids = set(selected)
    upgraded_targets = set()
    for upgrade in request.get('artifice_attempts', []):
        try:
            target_id, donor_id = upgrade['target_item_id'], upgrade['donor_item_id']
            target, donor = by_id[target_id], by_id[donor_id]
            if target_id not in final_ids or donor_id in selected or target_id in upgraded_targets:
                raise ValueError('Target must be in loadout; donor must not be a reserved final item')
            if donor_id not in request.get('donor_allowlist', []):
                raise ValueError('Donor is not explicitly designated')
            if donor.get('locked') is not False or donor.get('favorite') is not False:
                raise ValueError('Donor lock/favorite state is protected or unknown')
            if 'equipped_by' not in donor or donor['equipped_by']:
                raise ValueError('Donor is equipped or its equipment state is unknown')
            levels = donor.get('artifice_levels')
            if not isinstance(levels, dict) or not levels or any(count(v) > 0 for v in levels.values()):
                raise ValueError('Donor enhancement state is upgraded or unknown')
            if donor.get('slot') != target.get('slot'):
                raise ValueError('Donor slot does not match target')
            previews = [p for p in records('artificing') if p.get('target_item_id') == target_id
                        and p.get('donor_item_id') == donor_id and p.get('stat_line') == upgrade['stat_line']]
            if len(previews) != 1:
                raise ValueError('Need one reviewed target/donor/stat preview')
            preview = previews[0]
            current = count(preview['current_level'])
            maximum = count(preview['maximum_level'])
            if current >= maximum or target.get('artifice_levels', {}).get(upgrade['stat_line']) != current:
                raise ValueError('Enhancement level is maxed or differs from inventory')
            chance = preview['success_chance']
            if type(chance) not in (int, float) or not 0 <= chance <= 1:
                raise ValueError('Success chance must be a known fraction from 0 to 1')
            if preview.get('pity') is None or not preview.get('match_quality'):
                raise ValueError('Missing pity or match quality')
            costs = cost_map(preview['catalyst_cost'])
            for name, amount in cost_map(preview['other_costs']).items():
                costs[name] = costs.get(name, 0) + amount
            charge(costs)
            selected.add(donor_id)
            upgraded_targets.add(target_id)
            step('artifice_once', target_item_id=target_id, donor_item_id=donor_id,
                 stat_line=upgrade['stat_line'], expected_current_level=current,
                 success_chance=chance, pity=preview['pity'], costs=costs,
                 verify='Read actual level, pity and consumption; re-scan before another attempt')
        except (KeyError, TypeError, ValueError) as error:
            block('Artifice attempt blocked: ' + str(error))
    if not request.get('artifice_attempts'):
        result['upgrade_status'] = 'awaiting_explicit_target_stat_donor_and_cost_preview'
    else:
        result['upgrade_status'] = 'single_attempt_previews_only'
    # Resource reservation is shared across crafting and artificing.
    if totals:
        balances = {}
        for material in records('craft_materials'):
            try:
                name = material['item_name']
                if name in balances:
                    raise ValueError('Ambiguous duplicate resource balance')
                balances[name] = count(material['quantity'])
            except (KeyError, TypeError, ValueError) as error:
                block('Review resource balance: ' + str(error))
        budget = cost_map(request.get('spend_limits', {}))
        for name, amount in totals.items():
            if name not in balances or balances[name] < amount:
                block('Insufficient or unknown balance: ' + name)
            if name not in budget or budget[name] < amount:
                block('Set sufficient explicit spend limit: ' + name)
    for slot, binding in bindings.items():
        step('equip', operator=operator, slot=slot, binding=binding,
             verify='Reopen equipped slot and compare exact physical item ID')
    name = request.get('loadout_name')
    existing = [p for p in plans if p.get('operator_name') == operator and p.get('plan_name') == name]
    if not name:
        block('Specify loadout name')
    elif existing and (len(existing) != 1 or not request.get('overwrite_plan_id')
                       or request['overwrite_plan_id'] != existing[0].get('plan_id')):
        block('Choose explicit plan ID before overwriting existing loadout')
    if len(bindings) == 4:
        step('save_loadout', operator=operator, name=name,
             overwrite_plan_id=request.get('overwrite_plan_id'),
             verify='Reopen named loadout and verify all four item IDs and set effects')
    result['status'] = 'blocked' if blockers else 'preview_ready'
    result['bindings'] = bindings
    return result


def verified_snapshot(store, session):
    path = store.export(session)
    snapshot = json.loads(Path(path).read_text('utf-8-sig'))
    evidence = {e['id']: e for e in snapshot['evidence']}
    for record in snapshot['records']:
        if not record['reviewed']:
            continue
        for evidence_id in record['evidence_ids']:
            item = evidence[evidence_id]
            if (item['task_id'], item['entity']) != (record['task_id'], record['entity']):
                raise ValueError('Evidence belongs to another task/entity')
            file = (store.root / item['file']).resolve()
            if not file.is_relative_to(store.root) or hashlib.sha256(file.read_bytes()).hexdigest() != item['sha256']:
                raise ValueError('Evidence missing or checksum changed: ' + evidence_id)
    return snapshot


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--store', type=Path, default=DEFAULT_STORE)
    parser.add_argument('--session', required=True)
    parser.add_argument('--request', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    store = Store(args.store)
    try:
        preview = build_plan(verified_snapshot(store, args.session), json.loads(args.request.read_text('utf-8-sig')))
    finally:
        store.close()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(preview, indent=4, ensure_ascii=False) + '\n', 'utf-8')
    print(json.dumps({'status': preview['status'], 'steps': len(preview['steps']),
                      'blockers': preview['blockers'], 'output': str(args.output)}))


if __name__ == '__main__':
    main()
