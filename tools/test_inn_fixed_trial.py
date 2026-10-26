import unittest
from inn_fixed_trial import choose_order, cycle_nodes, delivery_confirmed

class FixedTrialTests(unittest.TestCase):
    def test_gourmands_and_unknown_food_never_become_ordinary_routes(self):
        view={'seats':[{'seat':'1','phase':'gourmand','dishes':['tuber_fries']},{'seat':'2','phase':'waiting','dishes':['unknown']},{'seat':'3','phase':'waiting','dishes':['hot_sour_soup','fin_dumpling']}]}
        self.assertEqual(choose_order(view),('3','fin_dumpling'))
        self.assertIsNone(choose_order({'seats':view['seats'][:2]}))
    def test_every_route_keeps_camera_fixed_and_long_travel_on_aisle_axis(self):
        for seat in '123456':
            nodes=cycle_nodes(seat,'fin_dumpling')
            for name,node in nodes.items():
                action=node['action']
                if not isinstance(action,dict) or action['type']!='Swipe':continue
                p=action['param'];x,y=p['end']
                self.assertEqual(p['begin'],[195,551])
                self.assertLessEqual(p['duration']+p['end_hold'],1500)
                self.assertTrue((x==195 and y in [471,631]) or (y==551 and x in [115,275]))
                if 'North' in name or 'South' in name:self.assertEqual(x,195)
            self.assertEqual(nodes['FixedServe']['recognition'],'And')
            self.assertEqual(nodes['FixedServe']['all_of'][1]['recognition'],'ColorMatch')
    def test_delivery_ack_requires_inventory_and_target_request_change(self):
        view={'tray':{},'seats':[{'seat':'1','dishes':['hot_sour_soup']}]}
        self.assertTrue(delivery_confirmed(view,'1','fin_dumpling',1))
        self.assertFalse(delivery_confirmed(dict(view,tray={'fin_dumpling':1}),'1','fin_dumpling',1))
        self.assertFalse(delivery_confirmed(dict(view,unknown=['tray slot1']),'1','fin_dumpling',1))
        self.assertFalse(delivery_confirmed(view,'1','hot_sour_soup',1))
        self.assertFalse(delivery_confirmed(view,'2','fin_dumpling',1))

if __name__=='__main__':unittest.main()
