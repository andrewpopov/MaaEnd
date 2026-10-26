"""Regression for the English potion labels recorded on 2026-10-18 at 11:26:31."""
import re
import unittest
from pathlib import Path

import json5

ROOT = Path(__file__).resolve().parents[1]
NODE = "__AutoUseSpMedicationOCRRecognitionEmergencySanityBoosterRemainingTime"


def load(relative):
    return json5.loads((ROOT / relative).read_text(encoding="utf-8-sig"))


class SanityExpiryTests(unittest.TestCase):
    def options(self):
        for path, name in [
            ("assets/tasks/ProtocolSpace.json", "ProtocolSpaceSpMedicationExpireWithinDays"),
            ("assets/tasks/AutoEssence/AutoEssence.json", "AutoEssenceSpMedicationExpireWithinDays"),
        ]:
            for case in load(path)["option"][name]["cases"]:
                yield name, case["name"], case["pipeline_override"][NODE]["expected"]

    def matches(self, patterns, text):
        return any(re.search(pattern, text) for pattern in patterns)

    def test_recorded_14h_is_eligible_for_every_option(self):
        for task, case, patterns in self.options():
            with self.subTest(task=task, case=case):
                self.assertTrue(self.matches(patterns, "14h"))
                self.assertTrue(self.matches(patterns, "14小时"))

    def test_recorded_7d_respects_selected_limit(self):
        for task, case, patterns in self.options():
            with self.subTest(task=task, case=case):
                self.assertEqual(self.matches(patterns, "7d"), case in {"All", "Days10", "Days7"})

    def test_day_boundaries(self):
        limits = {"All": 13, "Days10": 10, "Days7": 7, "Days3": 3, "Days1": 1}
        for task, case, patterns in self.options():
            for day in range(1, 15):
                with self.subTest(task=task, case=case, day=day):
                    self.assertEqual(self.matches(patterns, f"{day}d"), day <= limits[case])
                    self.assertEqual(self.matches(patterns, f"{day}天"), day <= limits[case])

    def test_non_expiry_and_partial_labels_are_rejected(self):
        for task, case, patterns in self.options():
            for text in ["14", "700d", "Permanent", "x14h", "14h unexpected", "0d"]:
                with self.subTest(task=task, case=case, text=text):
                    self.assertFalse(self.matches(patterns, text))

    def test_default_resource_accepts_logged_labels(self):
        patterns = load("assets/resource/pipeline/Common/Private/AutoUseSpMedication/Recognition.json")[NODE]["expected"]
        for text in ["14h", "7d", "14小时", "7天"]:
            self.assertTrue(self.matches(patterns, text))


if __name__ == "__main__":
    unittest.main()
