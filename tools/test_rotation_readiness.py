import copy,datetime as dt,unittest
from rotation_readiness import build_rotation_readiness
NOW=dt.datetime(2026,10,20,tzinfo=dt.timezone.utc)
def fixture():
 members=['Arcane','Perlica','Xaihi','Akekuri']
 def row(task,name,data):return {'task_id':task,'data':dict(operator_name=name,**data),'reviewed':True,'evidence_ids':['verified-image'],'observed_at':NOW.isoformat()}
 rows=[row('account_identity','',{'uid':'123','server':'test'}),row('team_presets','',{'members':members})]
 for name in members:
  rows += [row('operators',name,{'level':90,'attributes':{'intellect':500,'will':400},'ultimate_gain_efficiency_percent':55}),row('skills',name,{'skill_ranks':{'battle':9}})]
 profile={'id':'test','members':members,'minimum_level':80,'required_signals':['identity'],'signals':{'identity':'node'},'rotation':{},'build_requirements':[{'operator':'Arcane','field':'attributes.intellect','gte_field':'attributes.will'},{'operator':'Xaihi','field':'ultimate_gain_efficiency_percent','minimum':49.69}]}
 return {'session':{'id':'s'},'records':rows},profile
class ReadinessTests(unittest.TestCase):
 def test_matching_reviewed_build_is_ready(self):
  s,p=fixture();self.assertTrue(build_rotation_readiness(s,p,NOW)['combat_ready'])
 def test_wrong_arcane_form_blocks_rotation(self):
  s,p=fixture();s['records'][2]['data']['attributes']['will']=600
  self.assertFalse(build_rotation_readiness(s,p,NOW)['combat_ready'])
 def test_unknown_energy_is_not_zero_or_ready(self):
  s,p=fixture()
  for row in s['records']:row['data'].pop('ultimate_gain_efficiency_percent',None)
  self.assertTrue(any('efficiency' in x for x in build_rotation_readiness(s,p,NOW)['blockers']))
 def test_other_team_order_and_unreviewed_skills_block(self):
  s,p=fixture();s['records'][1]['data']['members']=list(reversed(p['members']));s['records'][3]['reviewed']=False
  report=build_rotation_readiness(s,p,NOW)
  self.assertTrue(any('slot order' in x for x in report['blockers']));self.assertTrue(any('skill ranks' in x for x in report['blockers']))
 def test_uncalibrated_signals_and_stale_records_block(self):
  s,p=fixture();p['signals']={}
  self.assertFalse(build_rotation_readiness(s,p,NOW)['combat_ready'])
  p['signals']={'identity':'node'}
  self.assertFalse(build_rotation_readiness(s,p,NOW+dt.timedelta(days=2))['combat_ready'])
 def test_weapon_level_does_not_override_operator_stats(self):
  s,p=fixture();weapon=copy.deepcopy(s['records'][2]);weapon['task_id']='equipped_weapons';weapon['data']['level']=20
  weapon['data']['attributes']['will']=999;s['records'].append(weapon)
  self.assertTrue(build_rotation_readiness(s,p,NOW)['combat_ready'])
if __name__=='__main__':unittest.main()
