"""Audit native replay evidence offline; never issue inputs or promote routes."""
import argparse
from collections import defaultdict
import json
import math
from pathlib import Path
import re
import statistics


TERMINAL={"arrived","endpoint_mismatch","start_mismatch","controller_failed",
          "alignment_deadline","interrupted"}


def audit_replays(records,required=10):
    if type(required) is not int or required<1:raise ValueError("Positive replay requirement needed")
    attempts={};legacy=0
    for row in records:
        if row.get("stage") not in TERMINAL|{"started"}:continue
        if (not row.get("attempt_id") or not row.get("route") or row.get("mode") not in ("empty","loaded")
            or any(not re.fullmatch(r"[a-f0-9]{64}",row.get(k) or "") for k in ("agent_sha256","route_sha256"))):
            legacy+=1
            continue
        key=(row["route"],row["agent_sha256"],row["route_sha256"],row["mode"],row["attempt_id"])
        entry=attempts.setdefault(key,{"started":False,"terminal":None,"invalid":False})
        if row["stage"]=="started":
            if entry["started"] or entry["terminal"] is not None:entry["invalid"]=True
            entry["started"]=True
        else:
            if entry["terminal"] is not None:entry["invalid"]=True
            entry["terminal"]=row
    groups=defaultdict(list)
    for key,attempt in attempts.items():groups[key[:4]].append(attempt)
    output=[]
    for key,items in groups.items():
        streak=0;successes=0;durations=[]
        for item in items:
            end=item["terminal"] or {}
            seconds=end.get("elapsed_seconds")
            passed=(item["started"] and not item["invalid"] and end.get("stage")=="arrived"
                    and type(end.get("total_segments")) is int and end["total_segments"]>0
                    and end.get("completed_segments")==end["total_segments"]
                    and type(seconds) in (int,float) and math.isfinite(seconds) and seconds>0)
            streak=streak+1 if passed else 0
            successes+=bool(passed)
            if passed:durations.append(seconds)
        output.append(dict(route=key[0],agent_sha256=key[1],route_sha256=key[2],mode=key[3],
            attempts=len(items),successes=successes,failures=len(items)-successes,
            consecutive_successes=streak,qualified=streak>=required,
            median_seconds=statistics.median(durations) if durations else None))
    versions=defaultdict(dict)
    for group in output:versions[(group["route"],group["agent_sha256"],group["route_sha256"])][group["mode"]]=group
    ready=[dict(route=k[0],agent_sha256=k[1],route_sha256=k[2]) for k,modes in versions.items()
           if all(modes.get(mode,{}).get("qualified",False) for mode in ("empty","loaded"))]
    return dict(input_authorized=False,production_promoted=False,required_consecutive_per_mode=required,
                legacy_or_unattributed_records=legacy,groups=output,qualified_route_versions=ready)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("journal",type=Path)
    parser.add_argument("--required",type=int,default=10)
    args=parser.parse_args()
    records=[json.loads(line) for line in args.journal.read_text(encoding="utf-8-sig").splitlines() if line.strip()]
    print(json.dumps(audit_replays(records,args.required),indent=2))


if __name__=="__main__":main()
