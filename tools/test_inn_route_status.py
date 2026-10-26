"""Offline route diagnosis must never query a controller for saved images."""
import base64
import contextlib
import copy
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import numpy as np
from PIL import Image
from import_inn_route import sample_scene
from inn_route_status import inspect_endpoint, interval_geometry, main


class RouteStatusTests(unittest.TestCase):
    def setUp(self):
        red = np.random.default_rng(913).integers(50,180,(720,1280),dtype=np.uint8)
        self.pixels = np.stack((red,red-10,red-20),axis=2)
        self.image = Image.fromarray(self.pixels)
        self.scene = []
        for x in (570,900):
            box = [x,170,120,40]
            self.scene.append({'box':box, 'pixels':base64.b64encode(sample_scene(self.image,box)).decode(), 'architecture_only':True})
        self.interval = {'boundaries':[[[0,0],[0,0]],[[-24,0],[-12,-8]]],
                         'evidence':['first.png','last.png'], 'cross_track_pixels':4, 'progress_difference':.2}
        self.route = {'id':'measured', 'start':self.scene, 'end':self.scene, 'start_interval':self.interval}

    def shifted(self,offsets):
        current = np.zeros_like(self.pixels)
        for x,(dx,dy) in zip((570,900),offsets):
            current[100+dy:280+dy,x-75+dx:x+195+dx] = self.pixels[100:280,x-75:x+195]
        return Image.fromarray(current)

    def test_accepts_measured_parallax_and_rejects_unreviewed_travel(self):
        result = inspect_endpoint(self.shifted([[-12,0],[-6,-4]]),self.route,'start')
        self.assertTrue(result['landmark_gate_matches'])
        self.assertEqual([p['offset'] for p in result['patches']],[[-12,0],[-6,-4]])
        result = inspect_endpoint(self.shifted([[-26,0],[-14,-8]]),self.route,'start')
        self.assertTrue(result['photometric_matches'])
        self.assertFalse(result['interval_geometry_matches'])
        self.assertFalse(result['landmark_gate_matches'])

    def test_rejects_changed_architecture_and_flat_reference(self):
        result = inspect_endpoint(Image.new('RGB',(1280,720)),self.route,'start')
        self.assertFalse(result['landmark_gate_matches'])
        route = copy.deepcopy(self.route)
        route['start'][0]['pixels'] = base64.b64encode(bytes([90]*225)).decode()
        self.assertFalse(inspect_endpoint(self.image,route,'start')['landmark_gate_matches'])

    def test_retains_legacy_strict_gate_and_board_requirement(self):
        route = dict(self.route); route.pop('start_interval')
        self.assertTrue(inspect_endpoint(self.image,route,'start')['landmark_gate_matches'])
        self.assertFalse(inspect_endpoint(self.shifted([[-12,0],[-6,-4]]),route,'start')['landmark_gate_matches'])
        route['start_pose'] = {'number':'1'}
        self.assertTrue(inspect_endpoint(self.image,route,'start')['numbered_board_check_required'])

    def test_rejects_invalid_metadata_and_disagreeing_landmarks(self):
        for offsets in ([[2,0],[2,0]],[[-24,0],[0,0]],[[-12,8],[-6,4]]):
            self.assertFalse(interval_geometry(offsets,self.interval))
        interval = copy.deepcopy(self.interval); interval['cross_track_pixels'] = float('nan')
        self.assertFalse(interval_geometry([[0,0],[0,0]],interval))
        route = dict(self.route,start_interval={'boundaries':None})
        self.assertFalse(inspect_endpoint(self.image,route,'start')['landmark_gate_matches'])

    def test_explicit_settling_margin_preserves_legacy_and_distance_bounds(self):
        p = {'boundaries':[[[0,0],[0,0]],[[-28,0],[-34,-2]]], 'evidence':['a','b'], 'cross_track_pixels':4, 'progress_difference':.2}
        offsets = [[-30,0],[-38,0]]
        self.assertFalse(interval_geometry(offsets,p))
        p['endpoint_tolerance_pixels'] = 4
        self.assertFalse(interval_geometry(offsets,p))
        p['endpoint_tolerance_pixels'] = 5
        self.assertTrue(interval_geometry(offsets,p))
        self.assertFalse(interval_geometry([[-36,0],[-38,0]],p))
        self.assertFalse(interval_geometry([[-34,4],[-38,0]],p))
        self.assertFalse(interval_geometry([[-30,0],[0,0]],p))

    def test_saved_image_never_accesses_live_endpoint(self):
        with tempfile.TemporaryDirectory() as folder:
            folder = Path(folder)
            image = folder/'saved.png'; self.image.save(image)
            routes = folder/'routes.json'; routes.write_text(json.dumps([self.route]),encoding='utf8')
            output = io.StringIO()
            with patch('sys.argv',['inn_route_status','--image',str(image),'--routes',str(routes),'--search','measured']), patch('inn_route_status.fresh_frame',side_effect=AssertionError('live access')), contextlib.redirect_stdout(output):
                main()
            result = json.loads(output.getvalue())
            self.assertTrue(result['offline'])
            self.assertFalse(result['input_authorized'])
            self.assertFalse(result['interaction_verified'])
            self.assertTrue(result['closest'][0]['landmark_gate_matches'])


if __name__ == '__main__':
    unittest.main()
