import unittest
from combat_trial import skill_timing_review
class SkillTimingTests(unittest.TestCase):
 def fixture(self):
  return ([dict(id='s',kind='skill',slot=2,open_ms=100,close_ms=500,final_strike_ms=90,reviewed=True,evidence=['clip'])],
   [dict(id='e',opportunity_id='s',kind='input',slot=2,t_ms=200,detected_ms=130,decision_ms=160,controller_complete_ms=220,ack_ms=300,accepted=True,reviewed=True,evidence=['trace'])])
 def test_measured_stages_and_strike_alignment(self):
  rows,events=self.fixture();m=skill_timing_review(rows,events)['opportunities'][0]['measurements'][0]
  self.assertEqual(m['detect_to_decision_ms'],30);self.assertEqual(m['decision_to_input_ms'],40)
  self.assertEqual(m['input_to_resource_ack_ms'],100);self.assertEqual(m['final_strike_to_input_ms'],110)
 def test_unknown_evidence_is_not_success(self):
  self.assertEqual(skill_timing_review([],[])['status'],'insufficient_evidence')
  rows,events=self.fixture();events[0]['accepted']=None
  self.assertIn('acceptance_unconfirmed',skill_timing_review(rows,events)['opportunities'][0]['measurements'][0]['findings'])
 def test_late_cast_and_nonmonotonic_stages(self):
  rows,events=self.fixture();rows[0]['close_ms']=150
  self.assertIn('missed_eligible_window',skill_timing_review(rows,events)['opportunities'][0]['measurements'][0]['findings'])
  events[0]['detected_ms']=250
  with self.assertRaises(ValueError):skill_timing_review(rows,events)
if __name__=='__main__':unittest.main()
