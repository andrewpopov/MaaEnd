"""One middle-lane route model for inn service (1280x720, dining-facing camera).

The graph fixes topology, not localization. Counter -> lane0 -> lane1 -> lane2
is the only through corridor. Tables 1/3/5 branch right; 2/4/6 branch left.
Counter pickup spots branch along the bar from `counter`. No table is a transit
node. Route completion is an estimate until native Serve/Pickup or the existing
counter fingerprint confirms arrival. The top guest panel identifies demand,
not player position. An unknown pose must be visually localized before routing.

Durations below are the existing research driver's candidates, not newly
calibrated geometry. Forward/backward timings remain separate because recorded
runs do not establish symmetry. Physical counter-slot offsets must be supplied
from calibration; cooking completion order alone cannot establish those offsets.
Pipeline owns fresh phase guards and input; this module only plans geometry.
"""
from dataclasses import dataclass


@dataclass(frozen=True)
class Leg:
    source: str
    target: str
    direction: str
    milliseconds: int

    def pulses(self):
        x, y = {"forward": (195, 471), "backward": (195, 631),
                "left": (115, 551), "right": (275, 551)}[self.direction]
        remaining = self.milliseconds
        while remaining:
            duration = min(remaining, 1400)
            yield x, y, duration
            remaining -= duration


class InnRoutes:
    """Named waypoints shared by goToTable(), counterSpot(), and recenter()."""

    def __init__(self, counter_offsets=None, branch_ms=500):
        if not isinstance(branch_ms, int) or isinstance(branch_ms, bool) or not 100 <= branch_ms <= 500:
            raise ValueError("Table branch requires a measured 100–500ms duration")
        self.edges = {}
        self._pair("counter", "lane0", "forward", 700, "backward", 900)
        for row in range(2):
            # Same edge everywhere, rather than 1000ms from the counter and
            # 1100ms between tables for the identical lane segment.
            self._pair(f"lane{row}", f"lane{row+1}", "forward", 1000, "backward", 900)
        for table in range(1, 7):
            out, back = ("right", "left") if table % 2 else ("left", "right")
            self._pair(f"lane{(table-1)//2}", f"table{table}", out, branch_ms, back, branch_ms)
        for slot, offset in (counter_offsets or {}).items():
            if not isinstance(slot, int) or slot < 1 or not isinstance(offset, int) or offset == 0:
                raise ValueError("Counter slots require positive IDs and nonzero calibrated offsets")
            out, back = ("right", "left") if offset > 0 else ("left", "right")
            self._pair("counter", f"pickup{slot}", out, abs(offset), back, abs(offset))

    def _pair(self, a, b, out, out_ms, back, back_ms):
        self.edges.setdefault(a, {})[b] = Leg(a, b, out, out_ms)
        self.edges.setdefault(b, {})[a] = Leg(b, a, back, back_ms)

    def between(self, source, target):
        if source not in self.edges or target not in self.edges:
            raise ValueError("Unknown waypoint; localize at the counter anchor before routing")
        # The layout is a tree: there is one collision-avoiding path, no search
        # policy or alternate outside lane to tune.
        pending = [(source, [])]
        seen = {source}
        while pending:
            node, legs = pending.pop(0)
            if node == target:
                return legs
            for neighbor, leg in self.edges[node].items():
                if neighbor not in seen:
                    seen.add(neighbor)
                    pending.append((neighbor, legs + [leg]))
        raise ValueError("Disconnected inn waypoint")

    def goToTable(self, table, source="counter"):
        if type(table) is not int or table not in range(1, 7):
            raise ValueError("Table must be 1 through 6")
        return self.between(source, f"table{table}")

    def counterSpot(self, slot, source="counter"):
        if type(slot) is not int or slot < 1:
            raise ValueError("Counter slot must be a positive integer")
        return self.between(source, f"pickup{slot}")

    def recenter(self, source):
        # Unknown pose deliberately has no guessed timed route. Existing native
        # architectural matching must identify a known anchor first.
        return self.between(source, "counter")

    @staticmethod
    def pulses(legs):
        return [pulse for leg in legs for pulse in leg.pulses()]


def pickup_step(box, lateral):
    """Bounded visual correction; a saturated side target never implies depth."""
    center=box[0]+box[2]/2
    if 580<=center<=760:
        return (195,631,200),0,"center"
    sign=1 if center>760 else -1
    duration=min(500,max(120,int(abs(center-670)/.6)))
    duration=min(duration,max(0,1400-sign*lateral))
    if duration<100:return None
    return (275 if sign>0 else 115,551,duration),sign*duration,"right" if sign>0 else "left"


def counter_motion_observed(before,after,intended):
    """A successful touch is not evidence of lateral displacement.

    Match only unique, same-sized ready-food markers. Newly completed dishes and
    duplicate copies cannot establish camera translation. Opposite movement of
    at least 20 pixels confirms a lateral pulse; camera settling must not turn a
    failed touch into a fictitious return distance.
    """
    if not intended:return True
    shifts=[]
    for marker in before or []:
        same_before=[m for m in before if m["dish"]==marker["dish"]]
        same_after=[m for m in after or [] if m["dish"]==marker["dish"]]
        if len(same_before)!=1 or len(same_after)!=1:continue
        a,b=marker["box"],same_after[0]["box"]
        if a[1]<260 or b[1]<260 or min(a[3],b[3])<60 or abs(a[3]-b[3])>20:continue
        shifts.append((b[0]+b[2]/2)-(a[0]+a[2]/2))
    if not shifts:return False
    shifts.sort();shift=shifts[len(shifts)//2]
    return abs(shift)>=20 and shift*intended<0


class CounterSlots:
    """Round-local pickup poses, separate from changing dish occupancy."""
    def __init__(self,poses=None):
        self.poses=[dict(p) for p in poses or []]

    def observe_pickup(self,offset,dish):
        near=min(self.poses,key=lambda p:abs(p["offset"]-offset),default=None)
        if near is None or abs(near["offset"]-offset)>110:
            near={"offset":offset,"replays":0}
            self.poses.append(near)
        n=near["replays"]
        near["offset"]=round((near["offset"]*n+offset)/(n+1))
        near["replays"]=n+1
        near["last_dish"]=dish
        return dict(near)

    def direct_step(self,box,current):
        center=box[0]+box[2]/2
        if 580<=center<=760:return None
        projected=current+(center-670)/.55
        pose=min(self.poses,key=lambda p:abs(p["offset"]-projected),default=None)
        if pose is None or abs(pose["offset"]-projected)>110:return None
        delta=pose["offset"]-current
        if not 120<=abs(delta)<=1400 or delta*(center-670)<=0:return None
        return (275 if delta>0 else 115,551,abs(delta)),delta,"right" if delta>0 else "left"
