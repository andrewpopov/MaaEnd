"""Team-wide material and item reservations, plus activation readiness boundaries."""
import copy
import datetime as dt
import unittest

from combat_team import build_team_plan

NOW = dt.datetime(2026, 10, 18, 18, tzinfo=dt.timezone.utc)


class TeamPlanTests(unittest.TestCase):
    def setUp(self):
        self.names = ['Laevatain', 'Si', 'Camille', 'Wulfgard']
        self.profile = {'id': 'heat-si', 'members': self.names, 'driver_slot': 1, 'minimum_level': 80,
                        'required_signals': [], 'signals': {}, 'rules': [], 'capture_tasks': [],
                        'gear_policy': {}, 'gear_requests': {}}
        self.scan = {'session': {'id': 'test'}, 'records': [], 'tasks': []}
        self.add('account_identity', {'uid': 'test', 'server': 'test'})
        self.add('gear_plans', {'operator_name': 'Si', 'plan_name': 'Existing', 'plan_id': 'old'})
        self.add('team_presets', {'team_id': 'heat', 'members': self.names})
        for name in self.names:
            self.add('operators', {'operator_name': name, 'level': 90})
            gear = {}
            for slot in ['armor', 'gloves', 'kit_1', 'kit_2']:
                item = name + '-' + slot
                gear[slot] = {'item_name': item}
                self.add('gear_inventory', {'item_id': item, 'item_name': item,
                         'slot': 'kit' if slot.startswith('kit_') else slot, 'quantity': 1,
                         'equipped_by': name, 'locked': False, 'favorite': False, 'artifice_levels': {'int': 0}})
                self.add('equipped_gear', {'operator_name': name, 'slot': slot, 'item_name': item,
                                          'item_id': item, 'equipped': True})
            self.profile['gear_requests'][name] = {'operator': name, 'loadout_name': 'Heat ' + name, 'gear': gear}

    def add(self, task, data):
        if not any(t['task_id'] == task for t in self.scan['tasks']):
            self.scan['tasks'].append({'task_id': task, 'status': 'reviewed'})
        self.scan['records'].append({'task_id': task, 'entity': str(len(self.scan['records'])),
            'data': data, 'reviewed': True, 'evidence_ids': ['fixture'], 'observed_at': NOW.isoformat()})

    def plan(self, budget=None):
        return build_team_plan(self.scan, self.profile, budget, now=NOW)

    def test_ready_requires_reviewed_equipped_team(self):
        plan = self.plan()
        self.assertTrue(plan['combat_ready'], plan['blockers'])
        self.assertFalse(plan['game_changes_applied'])
        self.assertFalse(plan['gear_execution_enabled'])

    def test_below_80_is_excluded(self):
        next(r for r in self.scan['records'] if r['task_id'] == 'operators')['data']['level'] = 79
        self.assertFalse(self.plan()['combat_ready'])

    def test_crafting_budgets_are_combined_for_all_four(self):
        self.scan['records'] = [r for r in self.scan['records'] if not
                               (r['task_id'] == 'gear_inventory' and r['data']['slot'] == 'armor')]
        for name in self.names:
            self.add('gear_recipes', {'recipe_id': name, 'item_name': name+'-armor', 'slot': 'armor',
                     'unlocked': True, 'technique': 'observed', 'batch_size': 1,
                     'materials': {'ore': 3}, 'currency_cost': {}})
        self.add('craft_materials', {'item_name': 'ore', 'quantity': 20})
        plan = self.plan({'ore': 10})
        self.assertEqual(plan['resource_totals']['ore'], 12)
        self.assertIn('Team total exceeds spend limit: ore', plan['blockers'])

    def test_one_unequipped_copy_is_not_shared(self):
        self.add('gear_inventory', {'item_id': 'shared', 'item_name': 'Shared', 'slot': 'kit',
                 'quantity': 1, 'equipped_by': None})
        for name in self.names[:2]:
            self.profile['gear_requests'][name]['gear']['kit_1'] = {'item_name': 'Shared'}
        plan = self.plan()
        self.assertEqual(plan['plans']['Laevatain']['bindings']['kit_1']['item_id'], 'shared')
        self.assertNotIn('kit_1', plan['plans']['Si']['bindings'])

    def test_missing_or_unreviewed_calibration_blocks(self):
        self.profile['required_signals'] = ['si_phase']
        self.assertFalse(self.plan()['combat_ready'])
        self.profile['signals']['si_phase'] = 'verified_node'
        next(r for r in self.scan['records'] if r['task_id'] == 'operators')['reviewed'] = False
        self.assertFalse(self.plan()['combat_ready'])

    def test_wrong_formation_and_stale_records_block(self):
        next(r for r in self.scan['records'] if r['task_id'] == 'team_presets')['data']['members'] = list(reversed(self.names))
        self.assertFalse(self.plan()['combat_ready'])
        stale = copy.deepcopy(self.scan)
        for r in stale['records']: r['observed_at'] = (NOW-dt.timedelta(days=2)).isoformat()
        self.assertFalse(build_team_plan(stale, self.profile, now=NOW)['combat_ready'])

    def test_same_item_name_does_not_prove_equipped_copy(self):
        next(r for r in self.scan['records'] if r['task_id'] == 'equipped_gear')['data']['item_id'] = 'different-copy'
        self.assertFalse(self.plan()['combat_ready'])


if __name__ == '__main__':
    unittest.main()