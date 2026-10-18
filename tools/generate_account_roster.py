"""Generate the screen-guarded Endies operator capture routes (no spending)."""
import json
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/'assets/resource/pipeline/AccountScan/AccountRoster.json'
nodes={}
BASE={'pre_delay':0,'post_delay':0,'rate_limit':0}
def ocr(text=None,roi=(0,0,550,80)):
 p={'roi':list(roi),'threshold':.8}
 if text is not None:p['expected']=[text]
 return {'type':'OCR','param':p}
def both(*recognitions):return {'type':'And','param':{'all_of':[{'recognition':r} for r in recognitions]}}
def node(name,rec,action=None,next=None,freeze=None):
 n={**BASE,'recognition':rec}
 if action:n['action']=action
 if next:n['next']=next if isinstance(next,list) else [next]
 if freeze:n['pre_wait_freezes']={'time':250,'target':list(freeze)}
 nodes[name]=n
 return name
def state(name,step,rec=None,next=None,resume=None,**params):
 if resume is not None:params['next']=resume
 return node(name,rec or {'type':'DirectHit'},{'type':'Custom','param':{'custom_action':'AccountScanRosterStepAction','custom_action_param':{'step':step,**params}}},next)
def click(name,rec,xy,next):
 return node(name,rec,{'type':'Click','param':{'target':[xy[0]-2,xy[1]-2,4,4]}},next)
def snapshot(name,task,page,rec,next,talent_count=None):
 params={'task_id':task,'page':page}
 if talent_count is not None:params['talent_count']=talent_count
 return node(name,rec,{'type':'Custom','param':{'custom_action':'AccountScanCaptureAction','custom_action_param':params}},next,freeze=(920,80,285,520) if task!='skills' else (970,0,300,480))
def swipe(name,rec,begin,end,next):
 return node(name,rec,{'type':'Swipe','param':{'begin':list(begin),'end':list(end),'duration':600,'end_hold':500}},next)
HEADER=lambda page:ocr(r'^.+\s*/\s*'+page.replace('Ability','Al?bility').replace(' ',r'\s*')+r'$')
OV=both(HEADER('Overview'),ocr('Attributes',(80,200,300,210)))
GRID=both(ocr('Operators',(0,0,350,80)),ocr('Default',(0,600,330,90)))
MATRIX=both(HEADER('Ability Matrix'),ocr('Level Up',(160,570,350,140)),ocr('(?:RANK|ANK|RAN)',(310,135,260,155)))
DETAIL=both(HEADER('Ability Matrix'),ocr(r'^(?:Basic\s*Attack|Battle\s*Skill|Combo\s*Skill|Ultimate|Attribute\s*Increase|Talent|Base\s*Skill)$',(960,0,310,60)))
WEAPON=both(HEADER('Weapons'),ocr('Switch',(930,605,285,90)))
GEAR=both(HEADER('Gear'),ocr('Gear Bonus',(920,65,285,100)))
EQUIP=both(ocr('Equip',(0,0,300,80)),ocr('Operator Stats & Attributes',(500,0,420,100)))
for mode in ('operators','skills','equipped_weapons','equipped_gear'):
 state('AccountBatchEntry'+mode,'begin',next=['AccountBatchNeedOverview','AccountBatchRecoverDetail','AccountBatchRecoverMatrix','AccountBatchRecoverEquip','AccountBatchRecoverGear','AccountBatchRecoverWeapons','AccountBatchRecoverPotential','AccountBatchRecoverEssence','[JumpBack]SceneEnterMenuOperator'],task_id=mode)
state('AccountBatchNeedOverview','prepare',OV)
click('AccountBatchOpenRosterGrid',OV,(31,652),'AccountBatchOpenFilter')
click('AccountBatchOpenFilter',GRID,(362,646),'AccountBatchResetFilter')
FILTER=both(ocr('FILTER',(450,80,400,65)),ocr('Save',(1010,565,200,65)))
click('AccountBatchResetFilter',FILTER,(915,596),'AccountBatchSaveFilter')
click('AccountBatchSaveFilter',FILTER,(1120,596),'AccountBatchGridTop')
state('AccountBatchGridTop','grid_top',GRID)
nodes['AccountBatchGridTop']['pre_wait_freezes']={'time':300,'target':[42,80,325,520]}
swipe('AccountBatchRosterScrollTop',GRID,(200,180),(200,550),'AccountBatchGridTop')
click('AccountBatchSelectFirst',GRID,(90,145),'AccountBatchExitGrid')
click('AccountBatchExitGrid',both(ocr('Operators',(0,0,350,80)),ocr('Attributes',(920,260,330,250))),(1190,112),'AccountBatchNeedOverview')
nodes['AccountBatchExitGrid']['pre_wait_freezes']={'time':300,'target':[950,80,290,130]}
snapshot('AccountBatchSnapshotOverview','operators','overview',OV,'AccountBatchDispatch')
state('AccountBatchDispatch','dispatch')
state('AccountBatchOperatorDone','operator_done',OV)
state('AccountBatchAdvance','advance',OV)
node('AccountBatchSelectedPortrait',{'type':'TemplateMatch','param':{'roi':[0,75,121,520],'template':['LevelUpOperator/OperatorSelected.png'],'green_mask':True,'threshold':.9}})
click('AccountBatchClickNext',OV,(30,235),'AccountBatchWaitChanged')
node('AccountBatchChangedName',HEADER('Overview'))
node('AccountBatchWaitChanged',{'type':'And','param':{'all_of':['AccountBatchChangedName',{'recognition':ocr('Attributes',(80,200,300,210))}]}},next='AccountBatchSnapshotOverview',freeze=(70,15,400,65))
swipe('AccountBatchRailScroll',OV,(45,475),(45,320),'AccountBatchAdvanceAfterScroll')
state('AccountBatchAdvanceAfterScroll','advance_after_scroll',OV)
nodes['AccountBatchAdvanceAfterScroll']['pre_wait_freezes']={'time':300,'target':[0,80,80,510]}
state('AccountBatchFinish','finish',OV)
# Ability matrix, all four skill details and the twelve visible talent nodes.
click('AccountBatchOpenAbilities',OV,(1130,229),['AccountBatchEndminMatrixSnapshot','AccountBatchMatrixSnapshot'])
snapshot('AccountBatchMatrixSnapshot','skills','matrix',MATRIX,'AccountBatchOpenBasic',talent_count=12)
snapshot('AccountBatchEndminMatrixSnapshot','skills','matrix',both(MATRIX,ocr(r'^Endministrator\s*/')),'AccountBatchAlignEndmin',talent_count=8)
state('AccountBatchAlignEndmin','matrix_align',MATRIX,points=[[392,239],[583,414],[733,346],[872,278],[1005,218],[679,455],[847,414],[806,528],[987,522]])
state('AccountBatchOpenBasic','matrix_align',MATRIX,points=[[400,242],[579,414],[726,337],[862,270],[990,205],[671,423],[826,373],[792,456],[962,423],[697,517],[881,547],[986,498],[1181,546]])
node('AccountBatchBasicRank',ocr('(?:RANK|ANK|RAN)',(310,135,260,155)))
nodes['AccountBatchBasicRank']['recognition']['param'].update({'order_by':'Vertical','index':0,'threshold':.7})
click('AccountBatchClickBasic',MATRIX,(400,242),'AccountBatchSkillBasic')
nodes['AccountBatchClickBasic']['pre_wait_freezes']={'time':150,'target':[350,150,200,240]}
for name,cat,y,nxt in [('basic','Basic Attack',204,'AccountBatchOpenBattle'),('battle','Battle Skill',275,'AccountBatchOpenCombo'),('combo','Combo Skill',348,'AccountBatchOpenUltimate'),('ultimate','Ultimate',418,'AccountBatchSkillsBack')]:
 rec=both(HEADER('Ability Matrix'),ocr(cat.replace(' ',r'\s*'),(970,0,300,65)))
 snapshot('AccountBatchSkill'+name.title(),'skills',name,rec,nxt)
for cap,y,dst in [('Battle',275,'Battle'),('Combo',348,'Combo'),('Ultimate',418,'Ultimate')]:
 click('AccountBatchOpen'+cap,DETAIL,(225,y),'AccountBatchSkill'+dst)
click('AccountBatchSkillsBack',DETAIL,(1220,35),['AccountBatchTalentOpen01','AccountBatchSkillsBackRetry'])
click('AccountBatchSkillsBackRetry',DETAIL,(1220,35),'AccountBatchTalentOpen01')
talents=[(579,414),(726,337),(862,270),(990,205),(671,423),(826,373),(792,456),(962,423),(697,517),(881,547),(986,498),(1181,546)]
for i,point in enumerate(talents,1):
 suffix=str(i).zfill(2)
 click('AccountBatchTalentOpen'+suffix,MATRIX,point,'AccountBatchTalentRead'+suffix)
 nodes['AccountBatchTalentOpen'+suffix]['pre_wait_freezes']={'time':150,'target':[540,150,660,430]}
 snapshot('AccountBatchTalentRead'+suffix,'skills','talent-'+suffix,DETAIL,'AccountBatchTalentBack'+suffix)
 dest='AccountBatchTalentOpen'+str(i+1).zfill(2) if i<len(talents) else 'AccountBatchMatrixBack'
 click('AccountBatchTalentBack'+suffix,DETAIL,(1220,35),[dest,'AccountBatchTalentBackRetry'+suffix])
 click('AccountBatchTalentBackRetry'+suffix,DETAIL,(1220,35),dest)
click('AccountBatchMatrixBack',MATRIX,(1220,35),'AccountBatchOperatorDone')
# Bounded right-pane scroll recording is shared by weapons and individual gear pieces.
swipe('AccountBatchRightScroll',{'type':'Or','param':{'any_of':[{'recognition':WEAPON},{'recognition':EQUIP}]}},(1090,550),(1090,430),'AccountBatchScrolledSnapshot')
# Custom step selects a guarded capture node for the active detail type.
state('AccountBatchScrolledSnapshot','scroll_capture')
state('AccountBatchScrollCheck','scroll_check')
snapshot('AccountBatchWeaponScrolled','equipped_weapons','scroll',WEAPON,'AccountBatchScrollCheck')
snapshot('AccountBatchGearScrolled','equipped_gear','scroll',EQUIP,'AccountBatchScrollCheck')
click('AccountBatchOpenWeapons',OV,(1250,350),'AccountBatchWeaponTop')
snapshot('AccountBatchWeaponTop','equipped_weapons','weapons',WEAPON,'AccountBatchWeaponScrollStart')
state('AccountBatchWeaponScrollStart','scroll_start',WEAPON,page='weapons',resume='AccountBatchOpenPotential')
click('AccountBatchOpenPotential',WEAPON,(1042,309),'AccountBatchPotentialRead')
POT=both(ocr(r'^.*\s*/\s*POTENTIAL$',(0,0,700,80)),ocr('Select Material',(960,600,300,100)))
snapshot('AccountBatchPotentialRead','equipped_weapons','potential',POT,'AccountBatchPotentialBack')
click('AccountBatchPotentialBack',POT,(1220,35),'AccountBatchOpenEssence')
click('AccountBatchOpenEssence',WEAPON,(1160,280),'AccountBatchEssenceRead')
ESS=both(ocr(r'^.*\s*/\s*ACTIVATION$',(0,0,700,80)),ocr('Activation Preview',(920,80,290,110)))
snapshot('AccountBatchEssenceRead','equipped_weapons','essence',ESS,'AccountBatchEssenceBack')
click('AccountBatchEssenceBack',ESS,(1238,35),'AccountBatchWeaponsBackOverview')
click('AccountBatchWeaponsBackOverview',WEAPON,(1250,230),'AccountBatchOperatorDone')
# The individual slots show an explicit Equip/Unequip state; never press either button.
click('AccountBatchOpenGear',OV,(1250,440),'AccountBatchGearSummary')
snapshot('AccountBatchGearSummary','equipped_gear','gear',GEAR,'AccountBatchOpenArmor')
click('AccountBatchOpenArmor',GEAR,(550,200),'AccountBatchGearReadArmor')
for slot,label,y,nxt in [('armor','Armor',110,'AccountBatchGearOpenGloves'),('gloves','Gloves',208,'AccountBatchGearOpenKit1'),('kit_1','Kit',302,'AccountBatchGearOpenKit2'),('kit_2','Kit',400,'AccountBatchGearBack')]:
 cap={'armor':'Armor','gloves':'Gloves','kit_1':'Kit1','kit_2':'Kit2'}[slot]
 rec=both(ocr('Equip',(0,0,300,80)),ocr('Operator Stats & Attributes',(500,0,420,100)),ocr(label,(920,75,285,85)))
 if slot in {'kit_1','kit_2'}:
  rec['param']['all_of'].append({'recognition':{'type':'ColorMatch','param':{'roi':[20,y-35,35,70],'method':4,'lower':[185,185,185],'upper':[255,255,255],'count':1500}}})
 snapshot('AccountBatchGearRead'+cap,'equipped_gear',slot,rec,'AccountBatchGearScroll'+cap)
 state('AccountBatchGearScroll'+cap,'scroll_start',rec,page=slot,resume=nxt)
 if slot!='armor':click('AccountBatchGearOpen'+cap,EQUIP,(38,y),'AccountBatchGearRead'+cap)
click('AccountBatchGearBack',EQUIP,(1220,35),'AccountBatchGearBackOverview')
click('AccountBatchGearBackOverview',GEAR,(1250,230),'AccountBatchOperatorDone')
# Recover known read-only panels before using the general world/menu route.
for label,guard,xy in [('Detail',DETAIL,(1220,35)),('Matrix',MATRIX,(1220,35)),('Equip',EQUIP,(1220,35)),('Gear',GEAR,(1250,230)),('Weapons',WEAPON,(1250,230)),('Potential',POT,(1220,35)),('Essence',ESS,(1238,35))]:
 click('AccountBatchRecover'+label,guard,xy,['AccountBatchNeedOverview','AccountBatchRecoverDetail','AccountBatchRecoverMatrix','AccountBatchRecoverEquip','AccountBatchRecoverGear','AccountBatchRecoverWeapons','AccountBatchRecoverPotential','AccountBatchRecoverEssence','[JumpBack]SceneEnterMenuOperator'])
for name,n in nodes.items():
 if 'Back' in name or name.startswith('AccountBatchRecover'):
  n['post_wait_freezes']={'time':500,'target':[170,80,1060,520]}
 if name.startswith('AccountBatchRecover'):n['max_hit']=4
OUT.write_text(json.dumps(nodes,indent=4)+'\n',encoding='utf-8')
print(f'Generated {len(nodes)} read-only scanner nodes: {OUT}')
