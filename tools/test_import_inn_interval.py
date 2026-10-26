"""Offline tests for measured interval import; no emulator/controller dependencies."""
import base64
import json
from pathlib import Path
import tempfile
import unittest

import numpy as np
from PIL import Image

from import_inn_interval import decode_scene, import_interval, locate, make_interval, match_score
from import_inn_route import sample_scene


class IntervalImportTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.root = Path(cls.temp.name)
        rng = np.random.default_rng(925)
        red = rng.integers(50, 180, (720,1280), dtype=np.uint8)
        pixels = np.stack((red,red-10,red-20), axis=2)
        cls.reference = Image.fromarray(pixels)
        cls.reference_path = cls.root/'reference.png'
        cls.reference.save(cls.reference_path)
        cls.scene = []
        for x in (570,900):
            box=[x,170,120,40]
            cls.scene.append({'box':box,'pixels':base64.b64encode(sample_scene(cls.reference,box)).decode(),'architecture_only':True})
        shifted = np.zeros_like(pixels)
        for x,(dx,dy) in zip((570,900),((-24,0),(-12,-8))):
            shifted[100+dy:280+dy,x-75+dx:x+195+dx] = pixels[100:280,x-75:x+195]
        cls.boundary_path=cls.root/'boundary.png'
        Image.fromarray(shifted).save(cls.boundary_path)
        cls.paths=[cls.reference_path,cls.boundary_path]

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def test_measures_independent_landmarks_and_reference(self):
        result=make_interval(self.scene,self.paths,self.reference_path)
        self.assertEqual(result['boundaries'],[[[0,0],[0,0]],[[-24,0],[-12,-8]]])
        self.assertEqual(result['cross_track_pixels'],4)
        self.assertEqual(result['evidence'],[str(p.resolve()) for p in self.paths])
        with self.assertRaisesRegex(ValueError,'Reference'):
            make_interval(self.scene,self.paths,self.boundary_path)

    def test_dry_run_atomic_write_and_reset_only_changed_route(self):
        path=self.root/'routes.json'
        route={'id':'measured','version':1,'goal':'transit','start':self.scene,
               'successful_replays':4,'inn_replays':3,'empty_replays':2,'loaded_replays':1}
        unrelated={'id':'other','successful_replays':9}
        path.write_text(json.dumps([route,unrelated]),encoding='utf-8')
        before=path.read_bytes()
        with self.assertRaisesRegex(ValueError,'reviewed-clearance'):
            import_interval(path,'measured','start',self.paths)
        self.assertEqual(path.read_bytes(),before)
        import_interval(path,'measured','start',self.paths,reviewed_clearance=True,dry_run=True)
        self.assertEqual(path.read_bytes(),before)
        import_interval(path,'measured','start',self.paths,reviewed_clearance=True)
        result=json.loads(path.read_text())
        for field in ('successful_replays','inn_replays','empty_replays','loaded_replays'):
            self.assertEqual(result[0][field],0)
        self.assertEqual(result[1],unrelated)
        result[0]['loaded_replays']=2
        path.write_text(json.dumps(result),encoding='utf-8')
        stable=path.read_bytes()
        imported=import_interval(path,'measured','start',self.paths,reviewed_clearance=True)
        self.assertFalse(imported['changed'])
        self.assertEqual(path.read_bytes(),stable)
        self.assertEqual(list(self.root.glob('routes.json.*.tmp')),[])

    def test_bad_evidence_and_reused_landmarks(self):
        with self.assertRaises(ValueError):
            make_interval(self.scene,[self.reference_path,self.reference_path])
        with self.assertRaises(FileNotFoundError):
            make_interval(self.scene,[self.reference_path,self.root/'missing.png'])
        small=self.root/'small.png'; Image.new('RGB',(64,64)).save(small)
        with self.assertRaisesRegex(ValueError,'1280'):
            make_interval(self.scene,[self.reference_path,small])
        copy=self.root/'same-content.png'; self.reference.save(copy)
        with self.assertRaisesRegex(ValueError,'distinct measured'):
            make_interval(self.scene,[self.reference_path,copy])
        with self.assertRaisesRegex(ValueError,'independent'):
            decode_scene([self.scene[0],self.scene[0]])
        blank=Image.new('RGB',(1280,720),(100,90,80))
        with self.assertRaisesRegex(ValueError,'not found'):
            locate(blank,decode_scene(self.scene))

    def test_periodic_texture_is_ambiguous(self):
        rng=np.random.default_rng(12)
        red=rng.integers(60,170,(32,32),dtype=np.uint8)
        tile=np.stack((red,red-10,red-20),axis=2)
        periodic=Image.fromarray(np.tile(tile,(23,40,1))[:720])
        scene=[]
        for patch in self.scene:
            scene.append(dict(patch,pixels=base64.b64encode(sample_scene(periodic,patch['box'])).decode()))
        with self.assertRaisesRegex(ValueError,'Ambiguous'):
            locate(periodic,decode_scene(scene))

    def test_photometric_thresholds_match_go_channel_semantics(self):
        raw=bytes([100,90,80]*50)
        reference=np.frombuffer(raw,dtype=np.uint8).reshape(50,3).astype(float)
        mask=np.ones(50,dtype=bool)
        self.assertEqual(match_score(reference+25,raw,mask),0)
        self.assertIsNone(match_score(reference+26,raw,mask))
        noise=reference.copy()
        noise[:10]+=40
        # Exactly20% changed channels accepted, centered mean error12.8 rejects.
        self.assertIsNone(match_score(noise,raw,mask))
        noise=reference.copy(); noise[:5]+=40
        self.assertIsNotNone(match_score(noise,raw,mask))
        # Mask is fixed from reference: excluded samples do not affect lighting.
        mask[:10]=False
        noise=reference.copy(); noise[:10]=255
        self.assertEqual(match_score(noise,raw,mask),0)


if __name__ == '__main__':
    unittest.main()
