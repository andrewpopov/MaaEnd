"""Replay evidence must prove repeated same-build, same-route arrivals."""
import unittest
from inn_calibration_audit import audit_replays


def attempt(i,mode="empty",stage="arrived",**changes):
    common=dict(route="counter-table1",agent_sha256="a"*64,route_sha256="b"*64,mode=mode,attempt_id=str(i))
    common.update(changes)
    return [dict(common,stage="started"),dict(common,stage=stage,completed_segments=2,total_segments=2,elapsed_seconds=3)]


class ReplayAuditTests(unittest.TestCase):
    def test_both_modes_require_ten_consecutive_arrivals(self):
        records=sum((attempt(i) for i in range(10)),[])
        self.assertFalse(audit_replays(records)["qualified_route_versions"])
        records+=sum((attempt(i,"loaded") for i in range(10)),[])
        self.assertEqual(len(audit_replays(records)["qualified_route_versions"]),1)

    def test_failure_or_incomplete_attempt_resets_streak(self):
        records=sum((attempt(i) for i in range(10)),[])
        for end in (attempt(11,stage="endpoint_mismatch"),attempt(11)[:1]):
            group=audit_replays(records+end)["groups"][0]
            self.assertEqual(group["consecutive_successes"],0)
            self.assertFalse(group["qualified"])

    def test_build_and_route_versions_cannot_pool_successes(self):
        for change in ({"agent_sha256":"c"*64},{"route_sha256":"c"*64}):
            records=sum((attempt(i) for i in range(5)),[])+sum((attempt(i+5,**change) for i in range(5)),[])
            self.assertTrue(all(not g["qualified"] for g in audit_replays(records)["groups"]))

    def test_legacy_counters_and_duplicate_terminal_are_not_proof(self):
        self.assertFalse(audit_replays([{"route":"x","stage":"arrived","successful_replays":100}])["groups"])
        records=attempt(1);records.append(records[-1])
        self.assertEqual(audit_replays(records)["groups"][0]["successes"],0)

    def test_partial_segments_or_unstarted_arrival_cannot_pass(self):
        for records in (attempt(1)[1:],attempt(1)):
            records[-1]["completed_segments"]=1
            self.assertEqual(audit_replays(records)["groups"][0]["successes"],0)


if __name__=="__main__":unittest.main()
