from __future__ import annotations

import unittest
from dataclasses import dataclass

from contract import normalized_declared_output_contract


@dataclass(frozen=True)
class Output:
    path: str
    format: int
    media_type: str


class DeclaredOutputContractTest(unittest.TestCase):
    def normalize(self, outputs: list[Output]) -> list[tuple[str, int, str]]:
        return normalized_declared_output_contract(outputs, format_number=int)

    def test_order_is_not_part_of_the_contract(self) -> None:
        file_output = Output("/tmp/result.txt", 1, "text/plain")
        archive_output = Output("/tmp/result", 2, "application/x-tar")

        self.assertEqual(
            self.normalize([file_output, archive_output]),
            self.normalize([archive_output, file_output]),
        )

    def test_all_fields_and_multiplicity_are_part_of_the_contract(self) -> None:
        output = Output("/tmp/result.txt", 1, "text/plain")

        self.assertNotEqual(
            self.normalize([output]),
            self.normalize([Output(output.path, 2, output.media_type)]),
        )
        self.assertNotEqual(
            self.normalize([output]),
            self.normalize([Output(output.path, output.format, "application/json")]),
        )
        self.assertNotEqual(self.normalize([output]), self.normalize([output, output]))


if __name__ == "__main__":
    unittest.main()
