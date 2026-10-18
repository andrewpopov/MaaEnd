"""Behavioral checks for physical item reservations and combined spending."""
import copy
import tempfile
from pathlib import Path

from account_scan import Store
from test_account_scan import png
import datetime as dt
import unittest

from gear_workflow import build_plan, verified_snapshot

NOW = dt.datetime(2026, 10, 17, 20, tzinfo=dt.timezone.utc)


class GearWorkflowTests(unittest.TestCase):
    def setUp(self):
        self.scan = {'session': {'id': 'test-session'}, 'tasks': [], 'records': []}
        self.request = {'operator': 'Si', 'loadout_name': 'Si - Test',
                        'gear': {s: {'item_name': s} for s in ('armor', 'gloves', 'kit_1', 'kit_2')},
                        'spend_limits': {'ore': 20, 'credits': 20}}
        self.add('account_identity', {'uid': 'test', 'server': 'test'})
        self.add('operators', {'operator_name': 'Si'})
        self.add('equipped_gear', {'operator_name': 'Si'})
        self.add('gear_plans', {'operator_name': 'Si', 'plan_name': 'Existing', 'plan_id': 'old'})
        for slot in self.request['gear']:
            self.add('gear_inventory', {'item_id': slot, 'item_name': slot,
                     'slot': 'kit' if slot.startswith('kit_') else slot, 'quantity': 1,
                     'equipped_by': None, 'locked': False, 'favorite': False,
                     'artifice_levels': {'int': 0}})

    def add(self, task, data):
        if not any(t['task_id'] == task for t in self.scan['tasks']):
            self.scan['tasks'].append({'task_id': task, 'status': 'reviewed'})
        self.scan['records'].append({'task_id': task, 'entity': str(len(self.scan['records'])),
                                    'reviewed': True, 'observed_at': NOW.isoformat(),
                                    'evidence_ids': ['fixture'], 'data': data})

    def plan(self):
        return build_plan(self.scan, self.request, NOW)

    def craft_armor(self, materials=None):
        self.scan['records'] = [r for r in self.scan['records'] if r['data'].get('item_id') != 'armor']
        self.add('gear_recipes', {'recipe_id': 'recipe', 'item_name': 'armor', 'slot': 'armor',
                 'unlocked': True, 'technique': 'observed', 'batch_size': 1,
                 'materials': {'ore': 3} if materials is None else materials, 'currency_cost': {'credits': 2}})
        self.add('craft_materials', {'item_name': 'ore', 'quantity': 20})
        self.add('craft_materials', {'item_name': 'credits', 'quantity': 20})

    def artifice(self, target='gloves', donor='donor'):
        self.add('gear_inventory', {'item_id': donor, 'item_name': 'donor', 'slot': 'gloves',
                 'quantity': 1, 'equipped_by': None, 'locked': False, 'favorite': False,
                 'artifice_levels': {'int': 0}})
        self.add('artificing', {'target_item_id': target, 'donor_item_id': donor, 'stat_line': 'int',
                 'current_level': 0, 'maximum_level': 3, 'success_chance': .5, 'pity': 0,
                 'match_quality': 'observed', 'catalyst_cost': {'ore': 4}, 'other_costs': {'credits': 1}})
        self.request.setdefault('artifice_attempts', []).append(
            {'target_item_id': target, 'donor_item_id': donor, 'stat_line': 'int'})
        self.request.setdefault('donor_allowlist', []).append(donor)

    def test_reuses_owned_and_keeps_execution_disabled(self):
        plan = self.plan()
        self.assertEqual(plan['status'], 'preview_ready')
        self.assertEqual([s['kind'] for s in plan['steps']], ['equip'] * 4 + ['save_loadout'])
        self.assertFalse(plan['execution_enabled'])

    def test_same_copy_cannot_fill_two_kits(self):
        self.request['gear']['kit_2'] = {'item_name': 'kit_1', 'item_id': 'kit_1'}
        self.assertEqual(self.plan()['status'], 'blocked')

    def test_ambiguous_owned_copies_require_id(self):
        item = copy.deepcopy(self.scan['records'][4]['data'])
        item['item_id'] = 'duplicate'
        self.add('gear_inventory', item)
        self.assertTrue(any('exact owned copy' in b for b in self.plan()['blockers']))

    def test_unknown_recipe_cost_is_not_free(self):
        self.craft_armor()
        next(r['data'] for r in self.scan['records'] if r['task_id'] == 'gear_recipes')['materials'] = None
        self.assertEqual(self.plan()['status'], 'blocked')
        self.assertFalse(any(s['kind'] == 'craft' for s in self.plan()['steps']))

    def test_combined_budget_craft_plus_artifice(self):
        self.craft_armor()
        self.artifice()
        self.request['spend_limits']['ore'] = 6
        plan = self.plan()
        self.assertEqual(plan['resource_totals'], {'ore': 7, 'credits': 3})
        self.assertTrue(any('spend limit: ore' in b for b in plan['blockers']))

    def test_locked_donor_blocked(self):
        self.craft_armor()
        self.artifice()
        next(r['data'] for r in self.scan['records'] if r['data'].get('item_id') == 'donor')['locked'] = True
        self.assertTrue(any('lock/favorite' in b for b in self.plan()['blockers']))

    def test_final_piece_cannot_be_donor(self):
        self.request['artifice_attempts'] = [{'target_item_id': 'armor',
            'donor_item_id': 'gloves', 'stat_line': 'int'}]
        self.request['donor_allowlist'] = ['gloves']
        self.assertTrue(any('reserved final item' in b for b in self.plan()['blockers']))

    def test_repeat_attempt_needs_new_preview(self):
        self.craft_armor()
        self.artifice()
        self.artifice(donor='donor2')
        self.assertEqual(sum(s['kind'] == 'artifice_once' for s in self.plan()['steps']), 1)
        self.assertEqual(self.plan()['status'], 'blocked')

    def test_stale_or_unreviewed_scan_blocks(self):
        self.scan['records'][0]['observed_at'] = (NOW - dt.timedelta(days=2)).isoformat()
        self.assertEqual(self.plan()['status'], 'blocked')
        self.scan['tasks'][1]['status'] = 'partial'
        self.assertIn('Review complete scan coverage: operators', self.plan()['blockers'])

    def test_existing_named_loadout_requires_exact_overwrite_id(self):
        self.request['loadout_name'] = 'Existing'
        self.assertEqual(self.plan()['status'], 'blocked')
        self.request['overwrite_plan_id'] = 'old'
        self.assertEqual(self.plan()['status'], 'preview_ready')

    def test_batch_surplus_is_reserved_before_another_craft(self):
        self.scan['records'] = [r for r in self.scan['records'] if r['data'].get('slot') != 'kit']
        for slot in ('kit_1', 'kit_2'):
            self.request['gear'][slot] = {'item_name': 'beacon'}
        self.add('gear_recipes', {'recipe_id': 'beacon', 'item_name': 'beacon', 'slot': 'kit',
                 'unlocked': True, 'technique': 'observed', 'batch_size': 2,
                 'materials': {'ore': 3}, 'currency_cost': {}})
        self.add('craft_materials', {'item_name': 'ore', 'quantity': 3})
        plan = self.plan()
        self.assertEqual(plan['status'], 'preview_ready')
        self.assertEqual(sum(s['kind'] == 'craft' for s in plan['steps']), 1)
        self.assertNotEqual(plan['bindings']['kit_1'], plan['bindings']['kit_2'])

    def test_evidence_checksum_verified_before_planning(self):
        with tempfile.TemporaryDirectory() as directory:
            store = Store(Path(directory))
            try:
                sid = store.create('test')
                eid = store.image(sid, 'account_identity', 'account', 'profile', png())
                store.record(sid, 'account_identity', 'account',
                             {'uid': 'test', 'server': 'test'}, [eid], True)
                snapshot = verified_snapshot(store, sid)
                evidence = snapshot['evidence'][0]
                (store.root / evidence['file']).write_bytes(b'changed')
                with self.assertRaisesRegex(ValueError, 'checksum'):
                    verified_snapshot(store, sid)
            finally:
                store.close()


if __name__ == '__main__':
    unittest.main()
