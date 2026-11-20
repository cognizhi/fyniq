"""The Python SDK's typed models (PRD-001 FR-INT-17, rev 0.611): req FR-INT-17."""
import typing
import unittest

from fyniq import models_gen as m


class Models(unittest.TestCase):
    def test_required_and_optional_keys_follow_the_contract(self):
        create = m.IntegrationTaskCreate
        self.assertEqual(create.__required_keys__, {"external_ref", "title"})
        self.assertIn("due_on", create.__optional_keys__)
        body: m.IntegrationTaskCreate = {"external_ref": "OPS-1", "title": "Chase the letter"}
        self.assertEqual(sorted(body), ["external_ref", "title"])

    def test_types_are_the_contracts_and_money_is_a_decimal_string(self):
        hints = typing.get_type_hints(m.IntegrationTask, localns=vars(m))
        self.assertEqual(hints["version"], int)
        self.assertEqual(hints["title"], str)
        money = typing.get_type_hints(m.Money, localns=vars(m))
        self.assertEqual(money["amount"], str, "an amount is a decimal string, never a number")


if __name__ == "__main__":
    unittest.main()
