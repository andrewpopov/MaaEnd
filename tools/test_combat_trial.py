import hashlib
import json
import tempfile
import unittest
from pathlib import Path

from combat_trial import PHASES, init_session, score


class CombatTrialTests(unittest.TestCase):
    def opportunity(self, ident='combo-1', **updates):
        row = dict(id=ident, phase='combos', kind='combo', slot=2,
                   open_ms=1000, close_ms=2200, reviewed=True, evidence=['video#1'])
        row.update(updates)
        return row

    def event(self, ident='input-1', opportunity_id='combo-1', **updates):
        row = dict(id=ident, opportunity_id=opportunity_id, kind='input', slot=2,
                   t_ms=1400, reviewed=True, evidence=['trace:1'], accepted=True)
        row.update(updates)
        return row

    def accepted_ten(self):
        return ([self.opportunity(str(i)) for i in range(10)],
                [self.event(str(i), str(i)) for i in range(10)])

    def test_init_manifest_and_phase_budget(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            profile = root / 'profile.json'
            profile.write_text(json.dumps(dict(id='fire', members=['one'],
                required_signals=['combo'], signals={})), encoding='utf-8')
            output = root / 'trial'
            manifest = json.loads(init_session(output, profile).read_text())
            self.assertEqual(sum(p['seconds'] for p in PHASES), 1200)
            self.assertEqual(manifest['maximum_emulator_seconds'], 1200)
            self.assertEqual(manifest['profile_sha256'], hashlib.sha256(profile.read_bytes()).hexdigest())
            self.assertFalse(manifest['game_inputs_sent'])
            self.assertFalse(manifest['readiness']['hud_signals_bound'])
            self.assertFalse(manifest['readiness']['live_validation_passed'])
            self.assertEqual((output / 'events.jsonl').read_text(), '')
            self.assertEqual((output / 'opportunities.jsonl').read_text(), '')
            self.assertFalse(json.loads((output / 'annotation-example.json').read_text())['event']['reviewed'])
            with self.assertRaises(FileExistsError):
                init_session(output, profile)

    def test_empty_is_insufficient(self):
        result = score([], [])
        self.assertEqual(result['combo_gate'], 'insufficient_evidence')
        self.assertIsNone(result['catch_rate'])
        self.assertFalse(result['full_rotation_ready'])

    def test_unknown_acceptance_is_never_success(self):
        result = score([self.opportunity()], [self.event(accepted=None)])
        self.assertEqual(result['combo_gate'], 'insufficient_evidence')
        self.assertEqual(result['caught_and_accepted'], 0)
        self.assertEqual(result['opportunities'][0]['unknown_acceptance'], 1)

    def test_ten_accepted_pass_combo_gate_only(self):
        result = score(*self.accepted_ten())
        self.assertEqual(result['combo_gate'], 'pass')
        self.assertEqual(result['catch_rate'], 1)
        self.assertEqual(result['latency_ms']['samples'], 10)
        self.assertEqual(result['latency_ms']['maximum'], 400)
        self.assertFalse(result['full_rotation_ready'])

    def test_wrong_owner_and_duplicate_inputs_fail(self):
        for changes in ({'slot': 3}, {}):
            with self.subTest(changes=changes):
                opportunities, events = self.accepted_ten()
                events.append(self.event('extra', '0', **changes))
                result = score(opportunities, events)
                self.assertEqual(result['combo_gate'], 'fail')
                self.assertGreater(result['wrong_or_duplicate_inputs'], 0)

    def test_invalid_prompt_windows(self):
        for updates in ({'close_ms': 1000}, {'close_ms': 999}, {'open_ms': -1},
                        {'open_ms': float('nan')}, {'close_ms': float('inf')},
                        {'open_ms': True}, {'close_ms': '2200'}):
            with self.subTest(updates=updates), self.assertRaises(ValueError):
                score([self.opportunity(**updates)], [])

    def test_invalid_input_events(self):
        for updates in ({'t_ms': float('nan')}, {'t_ms': float('inf')},
                        {'t_ms': -1}, {'t_ms': True}, {'slot': 5}):
            with self.subTest(updates=updates), self.assertRaises(ValueError):
                score([self.opportunity()], [self.event(**updates)])

    def test_duplicate_ids_rejected(self):
        with self.assertRaises(ValueError):
            score([self.opportunity(), self.opportunity()], [])
        with self.assertRaises(ValueError):
            score([self.opportunity()], [self.event(), self.event()])

    def test_unknown_opportunity_reference_rejected(self):
        with self.assertRaises(ValueError):
            score([self.opportunity()], [self.event(opportunity_id='missing')])

    def test_unreviewed_missing_evidence_and_other_kind_excluded(self):
        rows = [self.opportunity('unreviewed', reviewed=False),
                self.opportunity('no-evidence', evidence=[]),
                self.opportunity('other', kind='final-strike')]
        result = score(rows, [])
        self.assertEqual(result['reviewed_combo_opportunities'], 0)
        self.assertEqual(result['excluded_unreviewed_or_other'], 3)
        self.assertEqual(result['combo_gate'], 'insufficient_evidence')
        result = score([self.opportunity()], [self.event(reviewed=False)])
        self.assertEqual(result['caught_and_accepted'], 0)


if __name__ == '__main__':
    unittest.main()
