"""Checks for account isolation, evidence integrity, and honest scan completion."""
import io
import json
import tempfile
import unittest
from pathlib import Path

from PIL import Image

from account_scan import Store, ingest_ui, capture_complete, capture_ready, observed_fields, required_capture_pages


def png(value=10):
    stream=io.BytesIO()
    Image.new('RGB',(1280,720),(value,value,value)).save(stream,format='PNG')
    return stream.getvalue()


class ScanStoreTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.store=Store(Path(self.temp.name))
        self.addCleanup(self.store.close)
        self.sid=self.store.create('endies')

    def identity(self):
        eid=self.store.image(self.sid,'account_identity','account','profile',png())
        self.store.record(self.sid,'account_identity','account',{'uid':'test-uid','server':'test-server'},[eid],True)
        self.store.complete(self.sid,'account_identity','UID and server read from account profile')
        return eid

    def test_weapon_level_reads_current_value_from_live_level_fraction(self):
        # Native OCR from Laevatain's 1280x720 Weapons page includes the cap.
        for text, expected in [('90/90',90),('80 / 90',80),('80',80)]:
            with self.subTest(text=text):
                tokens=[{'text':text,'score':0.982139,'box':[918,150,114,53]}]
                self.assertEqual(observed_fields('equipped_weapons','weapons',tokens)['level'],expected)

    def test_weapon_level_does_not_read_unrelated_or_impossible_numbers(self):
        for text, box in [('90/80',[918,150,114,53]),('116',[950,400,90,40])]:
            with self.subTest(text=text):
                self.assertNotIn('level',observed_fields('equipped_weapons','weapons',[{'text':text,'score':1,'box':box}]))

    def test_ui_ocr_rejects_world_screen_without_creating_record(self):
        image = Path(self.temp.name) / 'world.png'
        image.write_bytes(png())
        result = ingest_ui(self.store, 'operators', image, [
            {'text': 'Explore', 'score': .99, 'box': [0, 0, 20, 20]},
            {'text': 'UID: 123456789', 'score': .99, 'box': [0, 690, 100, 20]}])
        self.assertFalse(result['accepted'])
        self.assertEqual(self.store.db.execute('SELECT count(*) FROM records').fetchone()[0], 0)

    def test_ui_ocr_saves_readable_unreviewed_draft(self):
        image = Path(self.temp.name) / 'overview.png'
        image.write_bytes(png())
        result = ingest_ui(self.store, 'operators', image, [
            {'text': 'Rossi / Overview', 'score': .99, 'box': [0, 0, 240, 30]},
            {'text': 'Attributes', 'score': .99, 'box': [0, 200, 100, 20]}])
        self.assertTrue(result['accepted'])
        record = self.store.db.execute('SELECT * FROM records WHERE session_id=?', (result['session_id'],)).fetchone()
        self.assertFalse(record['reviewed'])
        self.assertIn('Rossi', json.loads(record['data_json'])['recognized_text'][0]['text'])
        self.assertTrue((self.store.root / 'exports' / (result['session_id'] + '-ocr.txt')).exists())
        self.assertEqual(self.store.task(result['session_id'], 'operators')['status'], 'partial')

    def test_overview_buttons_do_not_count_as_ability_page(self):
        image = Path(self.temp.name) / 'overview.png'
        image.write_bytes(png())
        result = ingest_ui(self.store, 'skills', image, [
            {'text': 'Endministrator/Overview', 'score': .99, 'box': [0, 20, 300, 30]},
            {'text': 'Ability Matrix', 'score': .99, 'box': [1060, 210, 150, 30]},
            {'text': 'Level Up', 'score': .99, 'box': [960, 210, 100, 30]}])
        self.assertFalse(result['accepted'])

    def batch_tokens(self, name='Rossi', page='Overview'):
        return [
            {'text':name+'/'+page,'score':.99,'box':[20,20,350,30]},
            {'text':'Attributes','score':.99,'box':[90,240,150,25]},
            {'text':'90','score':.99,'box':[950,110,90,70]},
            {'text':'UID: 123456789','score':.99,'box':[60,695,150,15]}]

    def test_batch_binds_operator_name_and_completion_checks_real_pages(self):
        image=Path(self.temp.name)/'page.png';image.write_bytes(png())
        result=ingest_ui(self.store,'operators',image,self.batch_tokens(),'Rossi','123456789','overview')
        row=self.store.db.execute('SELECT entity,data_json FROM records WHERE session_id=?',(result['session_id'],)).fetchone()
        self.assertEqual(row['entity'],'Rossi')
        self.assertEqual(json.loads(row['data_json'])['level'],90)
        done=capture_complete(self.store,result['session_id'],'operators',['Rossi'])
        self.assertEqual(done['operator_count'],1)
        pointer=json.loads((self.store.root/'ui_session.json').read_text(encoding='utf-8'))
        self.assertEqual(pointer['status'],'captured_pending_review')
        self.assertEqual(pointer['last_completed_task'],'operators')
        self.assertEqual(self.store.task(result['session_id'],'operators')['status'],'captured')
        with self.assertRaisesRegex(ValueError,'Missing captured pages'):
            capture_complete(self.store,result['session_id'],'skills',['Rossi'])

    def test_resume_requires_same_entity_complete_pages_and_recent_evidence(self):
        image=Path(self.temp.name)/'page.png';image.write_bytes(png())
        result=ingest_ui(self.store,'operators',image,self.batch_tokens(),'Rossi','123456789','overview')
        sid=result['session_id']
        self.assertTrue(capture_ready(self.store,sid,'operators','Rossi'))
        self.assertFalse(capture_ready(self.store,sid,'skills','Rossi'))
        self.assertFalse(capture_ready(self.store,sid,'operators','Si'))
        self.assertFalse(capture_ready(self.store,self.sid,'operators','Rossi'))
        with self.store.db:
            self.store.db.execute("UPDATE records SET observed_at='2020-01-01T00:00:00+00:00' WHERE session_id=?",(sid,))
        self.assertFalse(capture_ready(self.store,sid,'operators','Rossi'))

    def test_real_detail_header_tolerates_ocr_collapsed_whitespace(self):
        image=Path(self.temp.name)/'talent.png';image.write_bytes(png())
        tokens=self.batch_tokens(name='Gilberta',page='AbilityMatrix')+[
            {'text':'Talent','score':.99,'box':[972,2,78,24]}]
        self.assertTrue(ingest_ui(self.store,'skills',image,tokens,'Gilberta','123456789','talent-05')['accepted'])
        tokens[0]['text']='Gilberta/Overview'
        self.assertFalse(ingest_ui(self.store,'skills',image,tokens,'Gilberta','123456789','talent-05')['accepted'])

    def test_observed_extra_letter_in_ability_header_keeps_detail_validation(self):
        from account_scan import batch_page_text
        tokens=[{'text':'Camille / Albility Matrix','score':.99,'box':[81,31,217,23]},
                {'text':'Basic Attack','score':.99,'box':[970,2,147,23]}]
        self.assertIsNone(batch_page_text('skills','basic',tokens)[1])
        self.assertIsNotNone(batch_page_text('skills','battle',tokens)[1])

    def test_matrix_transition_cannot_masquerade_as_talent_detail(self):
        image=Path(self.temp.name)/'matrix.png';image.write_bytes(png())
        tokens=self.batch_tokens(name='Gilberta',page='Ability Matrix')+[
            {'text':'K','score':.99,'box':[1205,22,25,30]},
            {'text':'MASTERY','score':.99,'box':[940,103,40,29]}]
        result=ingest_ui(self.store,'skills',image,tokens,'Gilberta','123456789','talent-05')
        self.assertFalse(result['accepted'])
        tokens.append({'text':'Talent','score':.99,'box':[972,2,80,24]})
        self.assertTrue(ingest_ui(self.store,'skills',image,tokens,'Gilberta','123456789','talent-05')['accepted'])

    def test_capture_cannot_pass_a_subset_of_the_observed_roster(self):
        image=Path(self.temp.name)/'page.png';image.write_bytes(png())
        first=ingest_ui(self.store,'operators',image,self.batch_tokens(),'Rossi','123456789','overview')
        ingest_ui(self.store,'operators',image,self.batch_tokens(name='Si'),'Si','123456789','overview')
        sid=first['session_id']
        capture_complete(self.store,sid,'operators',['Rossi','Si'])
        with self.assertRaisesRegex(ValueError,'differs from the owned-operator scan'):
            capture_complete(self.store,sid,'skills',['Rossi'])
        # New evidence invalidates review but must retain the observed roster receipt.
        ingest_ui(self.store,'operators',image,self.batch_tokens(),'Rossi','123456789','overview')
        self.assertEqual(self.store.db.execute("SELECT count(*) FROM capture_runs WHERE session_id=?",(sid,)).fetchone()[0],1)

    def test_observed_endministrator_layout_has_no_base_skill_pages(self):
        regular=required_capture_pages('skills',{})
        special=required_capture_pages('skills',{'operator_name':'Endministrator','talent_count':8})
        self.assertEqual(len(regular),17)
        self.assertEqual(len(special),13)
        self.assertIn('talent-08',special)
        self.assertNotIn('talent-09',special)
        with self.assertRaisesRegex(ValueError,'Unmapped'):
            required_capture_pages('skills',{'talent_count':7})

    def test_multiline_skill_name_is_kept_in_order(self):
        tokens=[{'text':'Shadow','score':.99,'box':[1061,75,71,22]},
                {'text':'Moment of Blazing','score':.99,'box':[1058,54,158,25]},
                {'text':'RANK 9','score':.99,'box':[1100,103,73,24]}]
        data=observed_fields('skills','combo',tokens)
        self.assertEqual(data['skill_names_by_page']['combo'],'Moment of Blazing Shadow')
        self.assertEqual(data['skill_ranks_by_page']['combo'],9)

    def test_equipment_draft_requires_explicit_equipped_button(self):
        tokens=[{'text':'Redeemer Gloves','score':.99,'box':[925,215,220,25]},
                {'text':'70','score':.99,'box':[924,133,54,39]}]
        draft=observed_fields('equipped_gear','gloves',tokens)
        self.assertFalse(draft['equipment_state_by_slot']['gloves'])
        self.assertNotIn('equipped_gear_by_slot',draft)
        tokens.append({'text':'Unequip','score':.99,'box':[1040,635,100,25]})
        self.assertEqual(observed_fields('equipped_gear','gloves',tokens)['equipped_gear_by_slot']['gloves'],
                         {'equipped':True,'item_name':'Redeemer Gloves','level':70})

    def test_batch_rejects_cross_operator_or_cross_uid_page(self):
        image=Path(self.temp.name)/'page.png';image.write_bytes(png())
        with self.assertRaisesRegex(ValueError,'Operator changed'):
            ingest_ui(self.store,'operators',image,self.batch_tokens(),'Si','123456789','overview')
        with self.assertRaisesRegex(ValueError,'Account UID changed'):
            ingest_ui(self.store,'operators',image,self.batch_tokens(),'Rossi','999999999','overview')

    def test_batch_retains_multiple_skill_pages_and_ranks(self):
        image=Path(self.temp.name)/'page.png';image.write_bytes(png())
        result=ingest_ui(self.store,'operators',image,self.batch_tokens(),'Rossi','123456789','overview')
        for page,name,rank in [('basic','Seething Wolfblood',9),('battle','Crimson Wind',8)]:
            tokens=self.batch_tokens(page='Ability Matrix')+[
                {'text':{'basic':'Basic Attack','battle':'Battle Skill'}[page],'score':.99,'box':[970,1,150,25]},
                {'text':name,'score':.99,'box':[1060,70,200,30]},
                {'text':'RANK '+str(rank),'score':.99,'box':[1100,110,100,20]}]
            ingest_ui(self.store,'skills',image,tokens,'Rossi','123456789',page)
        row=self.store.db.execute("SELECT data_json FROM records WHERE session_id=? AND task_id='skills'",(result['session_id'],)).fetchone()
        data=json.loads(row[0])
        self.assertEqual(set(data['pages']),{'basic','battle'})
        self.assertEqual(data['skill_ranks_by_page'],{'basic':9,'battle':8})
        self.assertEqual(data['skill_names_by_page']['basic'],'Seething Wolfblood')

    def test_ui_capture_reuses_unverified_session(self):
        image=Path(self.temp.name)/'current.png'
        image.write_bytes(png())
        first=ingest_ui(self.store,'gear_recipes',image)
        second=ingest_ui(self.store,'gear_recipes',image)
        self.assertEqual(first['session_id'],second['session_id'])
        self.assertEqual(self.store.task(first['session_id'],'gear_recipes')['status'],'partial')
        self.assertFalse(first['account_uid_verified'])

    def test_ui_capture_does_not_append_to_verified_account(self):
        self.identity()
        pointer=self.store.root/'ui_session.json'
        pointer.write_text(json.dumps({'session_id':self.sid}),encoding='utf8')
        image=Path(self.temp.name)/'current.png';image.write_bytes(png())
        result=ingest_ui(self.store,'gear_inventory',image)
        self.assertNotEqual(result['session_id'],self.sid)
        self.assertEqual(self.store.task(self.sid,'account_identity')['status'],'reviewed')

    def test_duplicate_import_is_idempotent(self):
        first=self.store.image(self.sid,'operators','op-1','overview',png())
        second=self.store.image(self.sid,'operators','op-1','overview',png())
        self.assertEqual(first,second)
        exported=json.loads(self.store.export(self.sid).read_text('utf8'))
        self.assertEqual(len(exported['evidence']),1)
        self.assertFalse(exported['complete'])

    def test_evidence_cannot_cross_accounts(self):
        eid=self.identity()
        other=self.store.create('other-account')
        with self.assertRaisesRegex(ValueError,'session, task, and entity'):
            self.store.record(other,'account_identity','account',{'uid':'x','server':'y'},[eid],True)

    def test_screenshots_do_not_complete_task(self):
        self.store.image(self.sid,'account_identity','account','profile',png())
        with self.assertRaisesRegex(ValueError,'All records must be reviewed'):
            self.store.complete(self.sid,'account_identity','profile captured')

    def test_unknown_quantity_is_not_zero(self):
        eid=self.store.image(self.sid,'craft_materials','catalyst','inventory',png())
        with self.assertRaises(ValueError):
            self.store.record(self.sid,'craft_materials','catalyst',{'item_name':'Catalyst','category':'artificing','quantity':None},[eid],True)
        self.store.record(self.sid,'craft_materials','catalyst',{'item_name':'Catalyst','category':'artificing','quantity':0},[eid],True)

    def test_dependencies_must_be_reviewed(self):
        eid=self.store.image(self.sid,'operators','Si','overview',png())
        self.store.record(self.sid,'operators','Si',{'operator_name':'Si','level':90,'potential':0},[eid],True)
        with self.assertRaisesRegex(ValueError,'Dependency'):
            self.store.complete(self.sid,'operators','entire roster checked')

    def test_new_evidence_invalidates_review(self):
        self.identity()
        self.store.image(self.sid,'account_identity','account','profile',png(90))
        self.assertEqual(self.store.task(self.sid,'account_identity')['status'],'partial')
        row=self.store.db.execute('SELECT reviewed FROM records WHERE session_id=?',(self.sid,)).fetchone()
        self.assertEqual(row['reviewed'],0)
        with self.assertRaises(ValueError):
            self.store.complete(self.sid,'account_identity','new profile added')

    def test_unreviewed_extra_page_prevents_completion(self):
        eid=self.store.image(self.sid,'account_identity','account','profile',png())
        self.store.image(self.sid,'account_identity','extra','another-page',png(90))
        self.store.record(self.sid,'account_identity','account',{'uid':'x','server':'y'},[eid],True)
        with self.assertRaisesRegex(ValueError,'Some screenshots'):
            self.store.complete(self.sid,'account_identity','profile checked')

    def test_corrupt_evidence_is_rejected(self):
        eid=self.store.image(self.sid,'account_identity','account','profile',png())
        path=self.store.db.execute('SELECT file FROM evidence WHERE id=?',(eid,)).fetchone()['file']
        (self.store.root/path).write_bytes(b'changed')
        with self.assertRaisesRegex(ValueError,'checksum'):
            self.store.record(self.sid,'account_identity','account',{'uid':'x','server':'y'},[eid],True)

    def test_equipped_preview_requires_explicit_boolean(self):
        eid=self.store.image(self.sid,'equipped_gear','Si-armor','detail',png())
        data={'operator_name':'Si','slot':'armor','item_name':'Test Armor','equipped':'unknown'}
        with self.assertRaisesRegex(ValueError,'explicitly verified'):
            self.store.record(self.sid,'equipped_gear','Si-armor',data,[eid],True)


if __name__=='__main__':
    unittest.main()
