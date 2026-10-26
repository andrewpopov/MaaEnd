"""Persistent read-only account scanning queue and evidence store.

Run --help for commands. This does not navigate unknown game screens or spend
resources. Unmapped screens remain pending until validated on the emulator.
"""

import argparse
import datetime as dt
import hashlib
import io
import json
import re
import sqlite3
import time
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
SPEC_PATH = ROOT / 'tools' / 'combat_reference' / 'account_scan_tasks.json'
DEFAULT_STORE = ROOT / 'install' / 'data' / 'AccountScans'


def timestamp():
    return dt.datetime.now(dt.timezone.utc).isoformat()


def specs():
    return {t['id']: t for t in json.loads(SPEC_PATH.read_text('utf-8-sig'))['tasks']}


class Store:
    def __init__(self, root=DEFAULT_STORE):
        self.root = Path(root).resolve()
        self.root.mkdir(parents=True, exist_ok=True)
        self.db = sqlite3.connect(self.root / 'account_scans.sqlite3')
        self.db.row_factory = sqlite3.Row
        self.db.execute('PRAGMA foreign_keys=ON')
        self.db.executescript('''
            CREATE TABLE IF NOT EXISTS sessions (
                id TEXT PRIMARY KEY, account_label TEXT NOT NULL,
                platform TEXT NOT NULL, created_at TEXT NOT NULL
            );
            CREATE TABLE IF NOT EXISTS tasks (
                session_id TEXT NOT NULL, task_id TEXT NOT NULL,
                status TEXT NOT NULL DEFAULT 'pending',
                coverage_note TEXT, updated_at TEXT NOT NULL,
                PRIMARY KEY(session_id,task_id),
                FOREIGN KEY(session_id) REFERENCES sessions(id)
            );
            CREATE TABLE IF NOT EXISTS capture_runs (
                session_id TEXT NOT NULL, task_id TEXT NOT NULL,
                entities_json TEXT NOT NULL, captured_at TEXT NOT NULL,
                PRIMARY KEY(session_id,task_id),
                FOREIGN KEY(session_id,task_id) REFERENCES tasks(session_id,task_id)
            );
            CREATE TABLE IF NOT EXISTS evidence (
                id TEXT PRIMARY KEY, session_id TEXT NOT NULL, task_id TEXT NOT NULL,
                entity TEXT NOT NULL, page TEXT NOT NULL, file TEXT NOT NULL,
                sha256 TEXT NOT NULL, source TEXT NOT NULL, created_at TEXT NOT NULL, captured_at TEXT NOT NULL,
                UNIQUE(session_id,task_id,entity,page,sha256),
                FOREIGN KEY(session_id,task_id) REFERENCES tasks(session_id,task_id)
            );
            CREATE TABLE IF NOT EXISTS records (
                session_id TEXT NOT NULL, task_id TEXT NOT NULL, entity TEXT NOT NULL,
                data_json TEXT NOT NULL, evidence_json TEXT NOT NULL,
                reviewed INTEGER NOT NULL, observed_at TEXT NOT NULL,
                PRIMARY KEY(session_id,task_id,entity),
                FOREIGN KEY(session_id,task_id) REFERENCES tasks(session_id,task_id)
            );
        ''')

    def close(self):
        self.db.close()

    def create(self, account, platform='emulator'):
        if not account.strip():
            raise ValueError('An account label is required; UID is verified separately')
        sid = uuid.uuid4().hex
        with self.db:
            self.db.execute('INSERT INTO sessions VALUES(?,?,?,?)', (sid, account, platform, timestamp()))
            self.db.executemany('INSERT INTO tasks(session_id,task_id,updated_at) VALUES(?,?,?)',
                                [(sid, task, timestamp()) for task in specs()])
        return sid

    def task(self, sid, task):
        row = self.db.execute('SELECT * FROM tasks WHERE session_id=? AND task_id=?', (sid, task)).fetchone()
        if row is None:
            raise ValueError('Unknown scan session or task')
        return row

    def invalidate(self, sid, task):
        # Changed evidence invalidates the task and any previously reviewed dependents.
        affected = {task}
        changed = True
        while changed:
            changed = False
            for candidate, spec in specs().items():
                if candidate not in affected and affected.intersection(spec['depends_on']):
                    affected.add(candidate)
                    changed = True
        for candidate in affected:
            if candidate != task:
                self.db.execute('UPDATE records SET reviewed=0 WHERE session_id=? AND task_id=?', (sid, candidate))
            self.db.execute("UPDATE tasks SET status=CASE WHEN status='pending' THEN 'pending' ELSE 'partial' END,coverage_note=NULL,updated_at=? WHERE session_id=? AND task_id=?", (timestamp(), sid, candidate))
        self.db.execute("UPDATE tasks SET status='partial',updated_at=? WHERE session_id=? AND task_id=?", (timestamp(), sid, task))

    def image(self, sid, task, entity, page, data, source='imported screenshot', captured_at=None):
        self.task(sid, task)
        with Image.open(io.BytesIO(data)) as image:
            if image.format != 'PNG' or image.size != (1280, 720):
                raise ValueError('Evidence must be a 1280x720 PNG; do not silently resize it')
            image.verify()
        digest = hashlib.sha256(data).hexdigest()
        existing = self.db.execute('SELECT id FROM evidence WHERE session_id=? AND task_id=? AND entity=? AND page=? AND sha256=?', (sid, task, entity, page, digest)).fetchone()
        if existing:
            return existing['id']
        eid = uuid.uuid4().hex
        relative = Path('screenshots') / sid / task / (digest + '.png')
        destination = self.root / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        temporary = destination.with_suffix('.tmp')
        temporary.write_bytes(data)
        temporary.replace(destination)
        with self.db:
            self.db.execute('INSERT INTO evidence VALUES(?,?,?,?,?,?,?,?,?,?)', (eid, sid, task, entity, page, relative.as_posix(), digest, source, timestamp(), captured_at or timestamp()))
            self.invalidate(sid, task)
            self.db.execute('UPDATE records SET reviewed=0 WHERE session_id=? AND task_id=? AND entity=?', (sid, task, entity))
        return eid

    def record(self, sid, task, entity, data, evidence_ids, reviewed=False):
        self.task(sid, task)
        if not isinstance(data, dict) or not evidence_ids:
            raise ValueError('Records require an object and at least one evidence ID')
        observed_times = []
        for eid in evidence_ids:
            e = self.db.execute('SELECT * FROM evidence WHERE id=? AND session_id=? AND task_id=?', (eid, sid, task)).fetchone()
            if e is None or e['entity'] != entity:
                raise ValueError('Evidence must match the session, task, and entity')
            observed_times.append(e['captured_at'])
            path = self.root / e['file']
            if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != e['sha256']:
                raise ValueError('Evidence is missing or its checksum changed')
        if reviewed:
            missing = [f for f in specs()[task]['required_for_review'] if f not in data or data[f] is None]
            if missing:
                raise ValueError('Reviewed record is missing fields: ' + ', '.join(missing))
            if task in {'gear_inventory','craft_materials'}:
                count = data.get('quantity')
                if type(count) is not int or count < 0:
                    raise ValueError('Quantity must be a known nonnegative integer')
            if task == 'equipped_gear' and type(data.get('equipped')) is not bool:
                raise ValueError('Equipped state must be explicitly verified, not inferred from a preview')
            if task == 'team_presets' and (not isinstance(data['members'], list) or len(data['members']) != 4):
                raise ValueError('Team records require four verified member slots')
        with self.db:
            self.db.execute('INSERT INTO records VALUES(?,?,?,?,?,?,?) ON CONFLICT(session_id,task_id,entity) DO UPDATE SET data_json=excluded.data_json,evidence_json=excluded.evidence_json,reviewed=excluded.reviewed,observed_at=excluded.observed_at', (sid, task, entity, json.dumps(data), json.dumps(evidence_ids), int(reviewed), min(observed_times)))
            self.invalidate(sid, task)

    def complete(self, sid, task, note):
        self.task(sid, task)
        if not note.strip():
            raise ValueError('Describe how complete page/entity coverage was verified')
        for dependency in specs()[task]['depends_on']:
            if self.task(sid, dependency)['status'] != 'reviewed':
                raise ValueError('Dependency has not been reviewed: ' + dependency)
        records = self.db.execute('SELECT reviewed FROM records WHERE session_id=? AND task_id=?', (sid, task)).fetchall()
        if not records or not all(r['reviewed'] for r in records):
            raise ValueError('All records must be reviewed; screenshots alone do not complete a scan')
        linked = set()
        for row in self.db.execute('SELECT evidence_json FROM records WHERE session_id=? AND task_id=? AND reviewed=1', (sid, task)):
            linked.update(json.loads(row['evidence_json']))
        captured = {row['id'] for row in self.db.execute('SELECT id FROM evidence WHERE session_id=? AND task_id=?', (sid, task))}
        if not captured.issubset(linked):
            raise ValueError('Some screenshots have not been included in reviewed records')
        with self.db:
            self.db.execute("UPDATE tasks SET status='reviewed',coverage_note=?,updated_at=? WHERE session_id=? AND task_id=?", (note, timestamp(), sid, task))

    def export(self, sid):
        session = self.db.execute('SELECT * FROM sessions WHERE id=?', (sid,)).fetchone()
        if session is None:
            raise ValueError('Unknown session')
        tasks = [dict(t) for t in self.db.execute('SELECT * FROM tasks WHERE session_id=? ORDER BY task_id', (sid,))]
        evidence = [dict(e) for e in self.db.execute('SELECT * FROM evidence WHERE session_id=? ORDER BY created_at', (sid,))]
        records = []
        for row in self.db.execute('SELECT * FROM records WHERE session_id=? ORDER BY task_id,entity', (sid,)):
            r = dict(row)
            r['data'] = json.loads(r.pop('data_json'))
            r['evidence_ids'] = json.loads(r.pop('evidence_json'))
            r['reviewed'] = bool(r['reviewed'])
            records.append(r)
        captures = [{**dict(r),'entities':json.loads(r['entities_json'])} for r in self.db.execute('SELECT * FROM capture_runs WHERE session_id=? ORDER BY task_id',(sid,))]
        result = {'schema_version':1, 'session':dict(session), 'tasks':tasks, 'evidence':evidence, 'records':records, 'captures':captures,
                  'complete':all(t['status']=='reviewed' for t in tasks), 'game_changes_applied':False}
        target = self.root / 'exports' / (sid + '.json')
        target.parent.mkdir(exist_ok=True)
        temporary = target.with_suffix('.tmp')
        temporary.write_text(json.dumps(result,indent=4)+'\n',encoding='utf8')
        temporary.replace(target)
        return target


def import_roster(store, sid, root):
    manifest = json.loads((root / 'manifest.json').read_text('utf-8-sig'))
    result = []
    for operator in manifest['operators']:
        directory = (root / operator['directory']).resolve()
        if not directory.is_relative_to(root.resolve()):
            raise ValueError('Roster directory escapes its archive')
        for page in operator['screens']:
            image = (directory / page).resolve()
            if not image.is_relative_to(directory):
                raise ValueError('Screenshot path escapes its operator directory')
            if page.startswith('01-'):
                task = 'operators'
            elif page.startswith('02-'):
                task = 'skills'
            elif page.startswith('03-'):
                task = 'equipped_weapons'
            else:
                task = 'equipped_gear'
            result.append(store.image(sid,task,'unidentified-operator-'+str(operator['index']),image.stem,image.read_bytes(),source='PC roster archive; entity identity and account UID need review', captured_at=dt.datetime.fromtimestamp(image.stat().st_mtime,dt.timezone.utc).isoformat()))
    return result


def capture(base, instance):
    parsed = urllib.parse.urlparse(base)
    if parsed.scheme != 'http' or parsed.hostname not in {'localhost','127.0.0.1','::1'}:
        raise ValueError('Only the local MaaEnd HTTP API is supported')
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    prefix = base.rstrip('/')+'/maa/instances/'+urllib.parse.quote(instance,safe='')
    subscriber = 'account-scan-'+uuid.uuid4().hex
    def post(path):
        request = urllib.request.Request(prefix+path,data=json.dumps({'subscriber_id':subscriber,'interval_ms':100}).encode(),headers={'Content-Type':'application/json'})
        with opener.open(request,timeout=10) as response:
            response.read()
    try:
        post('/screenshot/subscribe')
        time.sleep(0.3)  # Allow a shared screenshot subscription refresh; no navigation occurs.
        with opener.open(prefix+'/screenshot',timeout=10) as response:
            return response.read()
    finally:
        post('/screenshot/unsubscribe')


# Conservative English page gates: world HUD text alone never qualifies.
PAGE_MARKERS = {
    'account_identity': [(r'UID',), (r'Server', r'Account Profile')],
    'operators': [(r'Overview',), (r'Attributes',)],
    'skills': [(r'Ability Matrix', r'Combat Talents'), (r'Battle Skill', r'Combo Skill', r'Level Up')],
    'equipped_weapons': [(r'Weapons',), (r'Switch', r'Weapon Stats')],
    'weapon_inventory': [(r'Weapons', r'Weapon Inventory'), (r'Filter', r'Sort', r'Default')],
    'equipped_gear': [(r'Gear',), (r'Switch', r'Operator Stats')],
    'gear_inventory': [(r'Gear', r'Equip'), (r'Filter', r'Sort', r'Default')],
    'gear_plans': [(r'Gear Plan', r'Loadouts')],
    'gear_recipes': [(r'Gear Assembly', r'Assembly Technique'), (r'Assemble', r'Recipe')],
    'craft_materials': [(r'Valuables', r'Materials'), (r'Progression', r'Inventory', r'Catalyst')],
    'artificing': [(r'Artificing', r'Artifice'), (r'Success', r'Enhance', r'Match')],
    'team_presets': [(r'Team Setup', r'Team Management', r'Formation'), (r'Operator', r'Team')],
}


def page_text(task, tokens):
    if not isinstance(tokens, list):
        raise ValueError('OCR must be an array of text regions')
    accepted = []
    for token in tokens:
        if not isinstance(token, dict) or not isinstance(token.get('text'), str):
            raise ValueError('Invalid OCR text region')
        score = token.get('score')
        if type(score) not in (int, float) or not 0 <= score <= 1:
            raise ValueError('Invalid OCR confidence')
        box = token.get('box')
        if not isinstance(box,list) or len(box)!=4 or any(type(v) is not int for v in box):
            raise ValueError('Invalid OCR bounding box')
        if score >= .8:
            accepted.append(token)
    text = '\n'.join(t['text'] for t in accepted)
    # Require complete marker words, rather than matching e.g. "Gear" in "Gearless".
    matched = all(any(re.search(r'(?<!\w)' + pattern.replace(' ',r'\s*') + r'(?!\w)', text, re.I)
                      for pattern in alternatives) for alternatives in PAGE_MARKERS[task])
    if task == 'gear_inventory' and re.search(r'(?<!\w)Equip(?!\w)', text):
        matched = matched and bool(re.search(r'Operator\s*Stats\s*&\s*Attributes', text, re.I))
    headers = {'operators':'Overview', 'skills':'Ability Matrix', 'equipped_weapons':'Weapons', 'equipped_gear':'Gear'}
    if task in headers:
        matched = matched and any(re.search(r'(?:^|/)\s*' + re.escape(headers[task]).replace(r'\ ',r'\s*') + r'$', t['text'], re.I)
                                  and len(t.get('box', [])) == 4 and t['box'][1] < 80
                                  for t in accepted)
    if not matched:
        title = specs()[task]['title']
        return accepted, 'Wrong or unreadable page. Open the ' + title + ' page and run only its capture task. No account record was created.'
    return accepted, None


def batch_page_text(task, page, tokens):
    if page == 'overview' or not page:
        return page_text(task,tokens)
    valid, _ = page_text(task,tokens)
    text = '\n'.join(t['text'] for t in valid)
    header = '\n'.join(t['text'] for t in valid if len(t.get('box',[])) == 4 and t['box'][1] < 80)
    matched = False
    if task == 'skills':
        matched = bool(re.search(r'/\s*Al?bility\s*Matrix',header,re.I))
        if page != 'matrix':
            categories = {'basic':'Basic Attack','battle':'Battle Skill','combo':'Combo Skill','ultimate':'Ultimate'}
            category = re.escape(categories[page]) if page in categories else r'(?:Attribute Increase|Talent|Base Skill)'
            matched = matched and any(t['box'][0]>=960 and t['box'][1]<55 and re.fullmatch(category.replace(r'\ ',r'\s*').replace(' ',r'\s*'),t['text'],re.I) for t in valid)
    elif task == 'equipped_weapons':
        if page == 'potential':
            matched = bool(re.search(r'/\s*POTENTIAL',header,re.I)) and 'Select Material' in text
        elif page == 'essence':
            matched = bool(re.search(r'/\s*ACTIVATION',header,re.I)) and 'Activation Preview' in text
        else:
            matched = bool(re.search(r'/\s*Weapons',header,re.I)) and 'Switch' in text
    elif task == 'equipped_gear':
        if page == 'gear':
            matched = bool(re.search(r'/\s*Gear',header,re.I)) and 'Gear Bonus' in text
        else:
            matched = 'Operator Stats & Attributes' in text and any(re.fullmatch(r'Equip',t['text'],re.I) for t in valid if t['box'][1]<80)
            slot = page.split('-scroll-')[0]
            expected = {'armor':'Armor','gloves':'Gloves','kit_1':'Kit','kit_2':'Kit'}.get(slot)
            matched = matched and any(expected and expected.lower() in t['text'].lower() and t['box'][0]>=920 for t in valid)
    return valid, None if matched else 'Expected capture page is not readable: ' + str(page)


def observed_fields(task,page,tokens):
    result = {}
    if task == 'operators':
        numbers = [t for t in tokens if 920<=t['box'][0]<=1110 and 80<=t['box'][1]<180 and t['box'][3]>=25 and re.fullmatch(r'\d{1,3}',t['text'])]
        if numbers:
            result['level'] = int(max(numbers,key=lambda t:t['box'][3])['text'])
    if task == 'skills' and page in {'basic','battle','combo','ultimate'}:
        right = [t for t in tokens if t['box'][0]>=970]
        titles = [t for t in right if 50<=t['box'][1]<=110 and not re.search(r'RANK',t['text'],re.I)]
        if titles: result['skill_names_by_page'] = {page:' '.join(t['text'] for t in sorted(titles,key=lambda t:(t['box'][1],t['box'][0])) if t['box'][2]>=40)}
        ranks = [re.search(r'RANK\s*(\d+)',t['text'],re.I) for t in right if 90<=t['box'][1]<=165]
        ranks = [int(m.group(1)) for m in ranks if m]
        if ranks: result['skill_ranks_by_page'] = {page:ranks[0]}
    if task == 'skills' and page.startswith('talent-'):
        titles = [t for t in tokens if t['box'][0]>=1030 and 50<=t['box'][1]<=110 and t['box'][2]>=40]
        if titles: result['talent_names_by_page'] = {page:' '.join(t['text'] for t in sorted(titles,key=lambda t:(t['box'][1],t['box'][0])))}
    if task == 'equipped_weapons' and page == 'weapons':
        titles = [t for t in tokens if t['box'][0]>=920 and 105<=t['box'][1]<=135 and t['box'][2]>=90]
        if titles: result['weapon_name'] = ' '.join(t['text'] for t in sorted(titles,key=lambda t:(t['box'][1],t['box'][0])))
        numbers = [t for t in tokens if 920<=t['box'][0]<980 and 145<=t['box'][1]<190 and t['box'][3]>=30 and re.fullmatch(r'\d{1,3}',t['text'])]
        if numbers: result['level'] = int(max(numbers,key=lambda t:t['box'][3])['text'])
    if task == 'equipped_gear' and page in {'armor','gloves','kit_1','kit_2'}:
        result['equipment_state_by_slot'] = {page:any(re.fullmatch('Unequip',t['text'],re.I) for t in tokens if t['box'][1]>=610)}
        if result['equipment_state_by_slot'][page]:
            item = {'equipped':True}
            titles = [t for t in tokens if t['box'][0]>=920 and 205<=t['box'][1]<=240 and t['box'][2]>=80]
            if titles: item['item_name'] = ' '.join(t['text'] for t in sorted(titles,key=lambda t:(t['box'][1],t['box'][0])))
            levels = [t for t in tokens if 920<=t['box'][0]<980 and 125<=t['box'][1]<160 and t['box'][3]>=25 and re.fullmatch(r'\d{1,3}',t['text'])]
            if levels: item['level'] = int(max(levels,key=lambda t:t['box'][3])['text'])
            result['equipped_gear_by_slot'] = {page:item}

    return result


def export_text(store,sid):
    export = store.export(sid)
    report = store.root/'exports'/(sid+'-ocr.txt')
    snapshot = json.loads(export.read_text('utf-8'))
    sections = []
    for row in snapshot['records']:
        pages = row['data'].get('pages') or {'current-page':{'recognized_text':row['data'].get('recognized_text',[])}}
        for page,entry in pages.items():
            if entry['recognized_text']:
                sections.append(row['task_id']+' / '+row['entity']+' / '+page+' (UNREVIEWED)\n'+'\n'.join(t['text'] for t in entry['recognized_text']))
    report.write_text('\n\n'.join(sections)+'\n',encoding='utf-8')
    return export,report


CAPTURE_PAGES = {
        'operators':['overview'],
        'skills':['matrix','basic','battle','combo','ultimate']+['talent-'+str(i).zfill(2) for i in range(1,13)],
        'equipped_weapons':['weapons','potential','essence'],
        'equipped_gear':['gear','armor','gloves','kit_1','kit_2'],
}

def required_capture_pages(task,data):
    if task=='skills':
        count=data.get('talent_count',12)
        if count not in {8,12}: raise ValueError('Unmapped talent layout')
        return ['matrix','basic','battle','combo','ultimate']+['talent-'+str(i).zfill(2) for i in range(1,count+1)]
    return CAPTURE_PAGES[task]


def capture_ready(store,sid,task,entity):
    row = store.db.execute('SELECT data_json,observed_at FROM records WHERE session_id=? AND task_id=? AND entity=?',(sid,task,entity)).fetchone()
    if row is None: return False
    age = (dt.datetime.now(dt.timezone.utc)-dt.datetime.fromisoformat(row['observed_at'])).total_seconds()
    if not 0<=age<86400: return False
    data=json.loads(row['data_json'])
    return set(required_capture_pages(task,data))<=set(data.get('pages',{}))


def capture_complete(store,sid,task,entities):
    if not isinstance(entities,list) or not entities or len(set(entities))!=len(entities):
        raise ValueError('Capture completion requires distinct observed operator names')
    baseline = store.db.execute("SELECT entities_json,captured_at FROM capture_runs WHERE session_id=? AND task_id='operators'",(sid,)).fetchone()
    if baseline:
        age = (dt.datetime.now(dt.timezone.utc)-dt.datetime.fromisoformat(baseline['captured_at'])).total_seconds()
        known = set(json.loads(baseline['entities_json']))
        if 0<=age<86400 and ((task=='operators' and not known<=set(entities)) or (task!='operators' and known!=set(entities))):
            raise ValueError('Captured roster differs from the owned-operator scan; refresh the roster before claiming completion')
    required_by_entity = {}
    for name in entities:
        row = store.db.execute('SELECT data_json FROM records WHERE session_id=? AND task_id=? AND entity=?',(sid,task,name)).fetchone()
        data = json.loads(row['data_json']) if row else {}
        required_by_entity[name] = required_capture_pages(task,data)
        missing = set(required_by_entity[name])-set(data.get('pages',{}))
        if missing:
            raise ValueError('Missing captured pages for '+name+': '+', '.join(sorted(missing)))
    with store.db:
        store.db.execute('INSERT INTO capture_runs VALUES(?,?,?,?) ON CONFLICT(session_id,task_id) DO UPDATE SET entities_json=excluded.entities_json,captured_at=excluded.captured_at',(sid,task,json.dumps(entities),timestamp()))
        store.db.execute('UPDATE tasks SET status=?,coverage_note=?,updated_at=? WHERE session_id=? AND task_id=?',
                         ('captured',json.dumps({'entities':entities,'required_pages_by_entity':required_by_entity,'review_pending':True}),timestamp(),sid,task))
    export,report = export_text(store,sid)
    value = {'accepted':True,'operator_count':len(entities),'status':'captured_pending_review','export':str(export),'ocr_report':str(report)}
    pointer = store.root / 'ui_session.json'
    if pointer.exists():
        current = json.loads(pointer.read_text(encoding='utf-8'))
        if current.get('session_id') == sid:
            current.update(value)
            current['last_completed_task'] = task
            pointer.write_text(json.dumps(current,indent=4)+'\n',encoding='utf-8')
    return value


def ingest_ui(store, task, image, ocr=None, entity=None, uid=None, page=None, talent_count=None):
    """Append UI evidence to an unverified session without trusting account identity."""
    tokens = None
    if ocr is not None:
        tokens, rejection = batch_page_text(task, page, ocr) if entity else page_text(task, ocr)
        if rejection:
            return {'accepted': False, 'reason': rejection, 'ocr_text_count': len(tokens),
                    'status': 'wrong_page', 'screenshot': str(image), 'records_created': 0}
    if entity and tokens is not None:
        for token in tokens:
            if token['box'][1] < 80:
                header = re.fullmatch(r'(.+?)\s*/\s*(Overview|Al?bility\s*Matrix|Weapons|Gear)',token['text'],re.I)
                if header and header.group(1).strip() != entity:
                    raise ValueError('Operator changed during capture')
            match = re.search(r'UID\s*[:\uFF1A]?\s*(\d{8,12})',token['text'],re.I)
            if match and match.group(1) != uid:
                raise ValueError('Account UID changed during capture')
    pointer = store.root / 'ui_session.json'
    source_pointer = SPEC_PATH.parent / 'account_scan_resume.json'
    sid = None
    for candidate in (pointer, source_pointer):
        if candidate.is_file():
            value = json.loads(candidate.read_text('utf-8-sig'))
            candidate_id = value.get('session_id')
            try:
                identity = store.task(candidate_id, 'account_identity')
            except ValueError:
                continue
            if identity['status'] != 'reviewed':
                sid = candidate_id
                break
    if entity:
        if not isinstance(entity, str) or not entity.strip() or not re.fullmatch(r'\d{8,12}', uid or '') or not page:
            raise ValueError('Batch capture requires operator, observed UID and page')
        sid = None
        if pointer.exists():
            previous = json.loads(pointer.read_text('utf-8'))
            if previous.get('uid_observed') == uid:
                candidate = previous.get('session_id')
                row = store.db.execute('SELECT created_at FROM sessions WHERE id=?', (candidate,)).fetchone()
                if row and (dt.datetime.now(dt.timezone.utc) - dt.datetime.fromisoformat(row['created_at'])).total_seconds() < 86400:
                    sid = candidate
        if sid is None:
            sid = store.create('observed-uid-' + uid, 'Endies read-only roster capture; server unverified')
    if sid is None:
        sid = store.create('ui-unverified-account', 'MaaEnd UI capture; controller/account need review')
    token = uuid.uuid4().hex
    entity_name = entity or 'unidentified-ui-' + token
    eid = store.image(sid, task, entity_name, page or 'current-page', image.read_bytes(),
                      source='MaaEnd current-page OCR; account identity and coverage require review' if tokens is not None else 'MaaEnd UI task; current page captured without navigation or account identification')
    if tokens is not None:
        previous = store.db.execute('SELECT data_json,evidence_json FROM records WHERE session_id=? AND task_id=? AND entity=?', (sid,task,entity_name)).fetchone()
        data = json.loads(previous['data_json']) if previous and entity else {}
        evidence_ids = json.loads(previous['evidence_json']) if previous and entity else []
        if entity:
            data.setdefault('pages', {})[page] = {'recognized_text':tokens,'evidence_id':eid}
            data['operator_name'] = entity
            if task=='skills' and page=='matrix' and talent_count is not None:
                if talent_count not in {8,12}: raise ValueError('Unmapped talent layout')
                data['talent_count']=talent_count
            for key,value in observed_fields(task,page,tokens).items():
                if isinstance(value,dict): data.setdefault(key,{}).update(value)
                else: data[key] = value
            data['recognized_text'] = [t for record in data['pages'].values() for t in record['recognized_text']]
        else:
            data['recognized_text'] = tokens
        data.update({'extraction_status':'unreviewed_page_text','structured_fields_verified':False,
                     'automatic_navigation':bool(entity)})
        store.record(sid,task,entity_name,data,list(dict.fromkeys(evidence_ids+[eid])),False)
    export = store.root / 'exports' / (sid + '.json')
    report = store.root / 'exports' / (sid + '-ocr.txt')
    if not entity or page == 'overview' or not export.exists():
        export,report = export_text(store,sid)
    value = {'session_id':sid, 'export':str(export), 'evidence_id':eid,
             'status':'partial_unreviewed', 'account_uid_verified':False,
             'accepted':True, 'ocr_text_count':len(tokens or []), 'records_created':int(tokens is not None),
             'ocr_report':str(report) if tokens is not None else None}
    if uid:
        value['uid_observed'] = uid
    temporary = pointer.with_suffix('.tmp')
    temporary.write_text(json.dumps(value,indent=4)+'\n',encoding='utf8')
    temporary.replace(pointer)
    return value


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--store',type=Path,default=DEFAULT_STORE)
    sub = parser.add_subparsers(dest='command',required=True)
    ui = sub.add_parser('ui-capture');ui.add_argument('--task',choices=list(specs()),required=True);ui.add_argument('--input',type=Path,required=True);ui.add_argument('--ocr',type=Path);ui.add_argument('--entity');ui.add_argument('--uid');ui.add_argument('--page');ui.add_argument('--talent-count',type=int,choices=[8,12])
    done = sub.add_parser('capture-complete');done.add_argument('--session',required=True);done.add_argument('--task',choices=['operators','skills','equipped_weapons','equipped_gear'],required=True);done.add_argument('--entities',required=True)
    ready = sub.add_parser('capture-ready');ready.add_argument('--session',required=True);ready.add_argument('--task',choices=list(CAPTURE_PAGES),required=True);ready.add_argument('--entity',required=True)
    init = sub.add_parser('init');init.add_argument('--account',required=True);init.add_argument('--platform',default='emulator')
    for name in ['status','export','import-roster','import-image','record','complete','capture']:
        p = sub.add_parser(name);p.add_argument('--session',required=True)
        if name in {'import-image','record','complete','capture'}:
            p.add_argument('--task',choices=list(specs()),required=True)
        if name in {'import-image','record','capture'}:
            p.add_argument('--entity',required=True)
        if name in {'import-image','capture'}:
            p.add_argument('--page',required=True)
        if name in {'import-roster','import-image','record'}:
            p.add_argument('--input',type=Path,required=True)
        if name=='record':
            p.add_argument('--evidence',nargs='+',required=True);p.add_argument('--reviewed',action='store_true')
        if name=='complete':
            p.add_argument('--coverage-note',required=True)
        if name=='capture':
            p.add_argument('--api-url',default='http://127.0.0.1:12702/api');p.add_argument('--instance',required=True)
    args = parser.parse_args(argv)
    store = Store(args.store)
    try:
        if args.command=='ui-capture':
            print(json.dumps(ingest_ui(store,args.task,args.input,json.loads(args.ocr.read_text('utf-8-sig')) if args.ocr else None,args.entity,args.uid,args.page,args.talent_count)))
            return 0
        if args.command=='capture-complete':
            print(json.dumps(capture_complete(store,args.session,args.task,json.loads(args.entities))))
            return 0
        if args.command=='capture-ready':
            print(json.dumps({'ready':capture_ready(store,args.session,args.task,args.entity)}))
            return 0
        if args.command=='init':
            sid=store.create(args.account,args.platform);print(sid);print(store.export(sid));return 0
        sid=args.session
        if args.command=='import-roster':
            print('Evidence entries:',len(import_roster(store,sid,args.input)))
        elif args.command=='import-image':
            print(store.image(sid,args.task,args.entity,args.page,args.input.read_bytes()))
        elif args.command=='capture':
            print(store.image(sid,args.task,args.entity,args.page,capture(args.api_url,args.instance),source='MaaEnd screenshot subscription; verify current page and cache freshness during review'))
        elif args.command=='record':
            store.record(sid,args.task,args.entity,json.loads(args.input.read_text('utf-8-sig')),args.evidence,args.reviewed)
        elif args.command=='complete':
            store.complete(sid,args.task,args.coverage_note)
        path=store.export(sid)
        if args.command=='status':
            data=json.loads(path.read_text('utf8'))
            for task in data['tasks']:
                count=sum(e['task_id']==task['task_id'] for e in data['evidence'])
                print(task['task_id'],task['status'],str(count)+' screenshots')
        print(path)
        return 0
    except (ValueError,OSError,sqlite3.Error) as error:
        print('Scan stopped:',error)
        return 1
    finally:
        store.close()


if __name__=='__main__':
    raise SystemExit(main())
