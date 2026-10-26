import datetime as dt
import tempfile
import unittest
from pathlib import Path

from team_manager import build_upgrade_plan, recommend


class TeamManagementTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'proof.png').write_bytes(b'evidence')
        self.now = dt.datetime(2026, 10, 20, 18, tzinfo=dt.timezone.utc)
        self.proof = {'account_uid': '123', 'observed_at': self.now.isoformat(), 'evidence': ['proof.png'], 'reviewed': True}
        self.team = {'id': 'test', 'members': ['Arcane', 'Si', 'Tangtang', 'Xaihi']}
        self.roster = [{'name': n, 'level': 90, 'weapon_level': 90} for n in self.team['members']]
        self.state = {n: {'operator_level': 90, 'weapon_level': 90,
                         **{'skill_' + k: 9 for k in ['basic', 'battle', 'combo', 'ultimate']}}
                      for n in self.team['members']}
        self.state['Si']['weapon_level'] = 80
        self.request = {'team_id': 'test', 'account_uid': '123',
                        'targets': {'operator_level': 90, 'weapon_level': 90, 'skill_rank': 9},
                        'current_state': self.state, 'state_observed_at': self.now.isoformat(),
                        'state_evidence': ['proof.png'], 'gear_plans': [],
                        'upgrades': [{**self.proof, 'id': 'si-weapon', 'operator': 'Si',
                                      'kind': 'weapon_level', 'from': 80, 'to': 90,
                                      'costs': {'WEAPON_EXP': 100}}],
                        'inventory': {'WEAPON_EXP': {**self.proof, 'quantity': 40}},
                        'reserves': {'WEAPON_EXP': 20}}
        self.resources = {'WEAPON_EXP': {'inventory_expression': '{weapon_exp}'}}

    def plan(self):
        return build_upgrade_plan(self.team, self.roster, self.request, self.resources, self.root, self.now)

    def test_absolute_target_and_reserve(self):
        result = self.plan()
        self.assertTrue(result['farm_ready'])
        self.assertEqual(result['shortages'][0]['missing'], 80)
        self.assertEqual(result['farm_inventory_targets'], {'WEAPON_EXP': 120})
        self.assertEqual(result['farm_pipeline_override']['SupplyPlanInsufficient_WEAPON_EXP']['recognition']['param']['custom_recognition_param']['expression'], '{weapon_exp}<120')

    def test_unknown_stock_is_not_zero_and_live_scan_can_resolve_it(self):
        self.request['inventory'] = {}
        result = self.plan()
        self.assertIsNone(result['shortages'][0]['missing'])
        self.assertTrue(result['farm_ready'])

    def test_unverified_cost_blocks_farming(self):
        self.request['upgrades'][0]['costs'] = None
        self.assertFalse(self.plan()['farm_ready'])

    def test_account_mismatch_and_stale_costs_block(self):
        for change in [{'account_uid': 'other'}, {'observed_at': '2026-10-18T18:00:00+00:00'}]:
            self.request['upgrades'][0].update(change)
            self.assertFalse(self.plan()['farm_ready'])

    def test_shared_gear_and_arcane_wrong_form_block(self):
        self.request['gear_plans'] = [
            {**self.proof, 'operator': 'Arcane', 'physical_item_ids': ['a', 'b', 'c', 'd'],
             'attributes': {'intellect': 707, 'will': 335}, 'required_form': 'WILL'},
            {**self.proof, 'operator': 'Si', 'physical_item_ids': ['a', 'e', 'f', 'g']}]
        errors = self.plan()['blockers']
        self.assertTrue(any('Shared gear conflict' in e for e in errors))
        self.assertTrue(any('Arcane attributes' in e for e in errors))

    def test_omitted_or_overlapping_level_costs(self):
        self.request['upgrades'] = []
        self.assertFalse(self.plan()['farm_ready'])
        step = {**self.proof, 'operator': 'Si', 'kind': 'weapon_level', 'from': 80, 'to': 90, 'costs': {'WEAPON_EXP': 100}}
        self.request['upgrades'] = [{**step, 'id': 'one'}, {**step, 'id': 'two'}]
        with self.assertRaisesRegex(ValueError, 'Overlapping'):
            self.plan()

    def test_unsupported_material_needs_another_source(self):
        self.request['upgrades'][0]['costs'] = {'rare_boss_item': 2}
        self.assertFalse(self.plan()['farm_ready'])

    def test_supported_shortage_can_be_farmed_while_other_sources_are_listed(self):
        self.request['upgrades'][0]['costs']['MARK_OF_PERSEVERANCE'] = 1
        result = self.plan()
        self.assertTrue(result['farm_ready'])
        self.assertEqual(result['other_acquisition_needed'], ['MARK_OF_PERSEVERANCE'])

    def test_mastery_is_separate_from_normal_skill_rank(self):
        self.request['targets']['skill_rank'] = 10
        with self.assertRaises(ValueError):
            self.plan()
        self.request['targets']['skill_rank'] = 9
        self.request['targets']['mastery_rank'] = 1
        self.assertFalse(self.plan()['farm_ready'])
        self.assertTrue(any('mastery_battle' in error for error in self.plan()['blockers']))

    def test_owned_teams_first_and_name_normalization(self):
        teams = [{'id': 'missing', 'members': ['Absent'], 'guide_priority': 100}, self.team]
        result = recommend(teams, self.roster)
        self.assertEqual(result[0]['id'], 'test')
        self.assertEqual(result[1]['missing_members'], ['Absent'])


if __name__ == '__main__':
    unittest.main()
